# Post-hoc behavioral ingestion (`ModePostHoc`) — design brief

**Status:** proposed. Implements the deferred Phase 10 (`implementation_plan.md`)
and extends the determinism contract (`trace-canonicalization-spec.md §1`) to
out-of-process capture.

**Audience:** the flowmap team (the build asks below) and adopting service teams
(the conventions in §4 and `docs/integration/`).

---

## 0. The principle: observe, don't gate (at first)

Today flowmap captures **in-process**: the harness re-drives a flow through the
real router under an in-memory OTel pipeline and the snapshot-assertion gate
re-runs it 3× and demands byte-identical canonical IR (`flow/flow.go:182`). That
self-test exists *only because the harness drives the flow*. Out-of-process there
is nothing to re-drive — the captured trace file is a **fixed, immutable input**,
and `canon` is a pure function of it. So the two notions of determinism separate
cleanly:

- **flowmap's processing is deterministic** — `canon(file)` is reproducible. ✅
  This is all the gate needs.
- **the e2e run is reproducible** — it is not, and need not be. The real
  distributed run has wall-clock skew, async export, scheduling, and
  data-dependent volume. None of it reaches the snapshot, because `canon`
  discards exactly those dimensions (`trace-canonicalization-spec.md §1`).

This is why the in-process gate model does **not** conflict with out-of-process
capture: the conflicting machinery (the 3-run self-test, byte-exact golden
compare, hard cardinality) is the *verdict* layer, not the *mapping* layer. We
ship the mapping layer first, **un-gated**, and graduate individual proven-stable
flows to a set-based gate later (§6).

---

## 1. The integration shape

Tap the collector you already run; process post-hoc off a file. Services keep
emitting exactly as they do; one additive collector exporter; one-line per-test
authoring.

```
Playwright e2e test
  └─ withFlow("publish-fanout", …)        ← sets baggage flowmap.flow=<slug>
     └─ HTTP → Dockerized services        [otelaws/otelhttp/otelsql spans]
        │                                   ⚠ each service runs a baggagecopy
        │                                     span processor (see §4) so
        │                                     flowmap.flow lands on the SPANS
        └─ OTLP → otel-collector
           ├─ otlp/jaeger, debug          (unchanged)
           └─ NEW: tail_sampling{flowmap.flow present} → file exporter (OTLP-JSON)
                 → e2e/<svc>/.flowmap/traces/*.json

── after the e2e run (a CI step) ──
flowmap behavior ingest <traces> --flows-dir flows/ [--update]
   ├─ group spans by trace-id; keep flowmap.flow-tagged; golden named by slug
   ├─ OTLP-JSON → capture model → canon(post-hoc profile) → ir.CanonicalTrace
   ├─ (stage 1) emit coverage view: boundary effects the e2e actually exercised
   └─ (stage 2, opt-in) compare vs committed *.golden.json (set semantics);
        CODEOWNERS routes golden changes to a human
```

Zero change to how services emit. The Jaeger/debug pipelines are untouched. The
only per-test authoring is the one-line tag.

---

## 2. Build asks for flowmap

### [P10.1] Post-hoc OTLP ingestion — implement `ModePostHoc`
Read OTLP/JSON (the collector file exporter's output) and adapt it into the
**existing** `capture.CapturedFlow` model — the same seam the in-process harness
feeds (`harness/harness.go:325 fromOTel`). Everything downstream (`canon`, `ir`,
`diff`, `render`, `coverage`) is then reused unchanged (decision D8). New CLI:

```
flowmap behavior ingest <file|dir> [--flows-dir D] [--update]
```

Goldens keyed by flow slug; `flowmap diff` / `coverage` / `*.flow.md` work as-is.

> **Keep the public module lean.** `harness`/`flow`/`ir` are semver-stable and
> imported by every adopting service ([C1]); they must **not** grow a gRPC /
> `pdata` / collector dependency. Read OTLP/JSON with a small **internal**
> decoder (`internal/otlpjson`) that handles the two awkward parts — base64/hex
> trace & span ids, and the `AnyValue` attribute union — and emits
> `[]capture.Span`. Put the CLI verb in `cmd/flowmap`; keep the receiver out of
> the public surface entirely (file-based, no network server).

### [P10.2] Flow scoping over a trace file
The out-of-process analog of `capture.Scope` (`capture/capture.go:107`): group
spans into per-flow `CapturedFlow`s by **trace-id**, select the ones carrying a
`flowmap.flow` attribute, and name the golden by its slug. Support
`Correlation-Id` as an alternate grouping key (already propagated today). A trace
with no `flowmap.flow` tag is ignored, not an error — the file may carry
unrelated traffic.

### [P10.3] Post-hoc canonicalization profile — *the hard, essential part*
Out-of-process traces carry nondeterminism the in-process path never sees. A
`canon` profile (a config flag, not a fork) handles it:

- **Redaction, extended.** Strip/normalize `host.*`, `*.pod.*`, `*.port`,
  `service.instance.id`, `process.*`, k8s/instance attributes, on top of the
  existing UUID/timestamp/numeric-id redaction (`canon §3.2`).
- **Ordering — keep what survives, drop only the in-process signals.**
  `parent_span_id` and span links **survive in OTLP**, so causal (parent→child)
  happens-before is still free; keep it. What does *not* survive is the in-process
  sibling signal — `flowmap.goid` and same-process caller-clock intervals
  (`capture.Concurrent`, `capture.go:164`). So in this profile, **ambiguous
  siblings order by canonical op-key**, and `canon §3.3`'s "never compare
  cross-service timestamps" / "default to concurrent on ambiguity" rules carry
  the rest. Net: the readable sequence (charge→publishes→ledger) is preserved
  where it is causal; only genuinely-concurrent peers fall back to op-key order.
- **Gate on the boundary-effect *set*, not cardinality/ordering/timing.** The
  post-hoc assertion is the set of `boundaryKey`s the flow exercised — events
  published/consumed, deps called, entrypoints — **the same key space the
  coverage join already defines** ([H2], `implementation_plan.md:186`). The full
  ordered tree remains the human view (`*.flow.md`), but the *assertion* is
  set-based, which is what keeps the gate from flaking. `ExpectExactlyOnce`-style
  cardinality is **opt-in per flow** (§5), for flows that are genuinely
  deterministic.

> **Free consequence.** Because the post-hoc artifact lives in the coverage key
> space, ingestion feeds `coverage.Delta` directly. The stage-1 non-gated view
> ("boundary effects your e2e actually exercised") is therefore *nearly free*
> once [P10.1]/[P10.2] land — it is the union of ingested boundary keys minus the
> static boundary, rendered as coverage, exit 0.

### [P10.4] Tagging + collector conventions (a short spec, not code we import)
The `flowmap.flow` key, the recommended collector `tail_sampling`+file-export
config, the **"flowmap-tagged flows are 100% sampled"** rule, and the
**baggage→span-attribute** requirement (§4). Shipped as `docs/integration/`.

---

## 3. Determinism: what is and isn't guaranteed post-hoc

| Dimension | In-process | Post-hoc |
|---|---|---|
| canon over a fixed file | deterministic | **deterministic** (same guarantee) |
| sibling order signal | goroutine + caller-clock | op-key only (causal order kept) |
| capture completeness | quiescence + re-drive | trace-id grouping over the file |
| gate contract | full ordered tree, byte-exact | boundary-effect set (cardinality opt-in) |
| self-test (3× re-drive) | required | **N/A** — file is the fixed input |

The post-hoc golden is therefore a **different artifact** from an in-process
golden of the same flow (set vs. ordered tree, more concurrent groupings). They
are not interchangeable and must not be diffed against each other — name them in
distinct directories.

---

## 4. Conventions (the §[P10.4] spec, summarized)

1. **`flowmap.flow=<slug>`** — a baggage member set by the test (`withFlow`), and
   the golden's name. Its presence on a trace is also the tail-sampling selector.
2. **⚠ Promote baggage onto spans.** Baggage is a propagation header; it is **not
   in the exported OTLP spans**. The collector's `tail_sampling` and flowmap's
   grouping both key on **span attributes**, so each service must run a
   **`baggagecopy`** span processor
   (`go.opentelemetry.io/contrib/processors/baggagecopy`) that copies
   `flowmap.flow` (and `Correlation-Id`) onto every span at start. This is the
   out-of-process analog of the in-process `startProcessor`
   (`harness/harness.go:128`). **Without it the tag never reaches the collector
   and nothing is selectable** — the most common adoption failure.
3. **100% sampling for tagged flows.** Head-sample tagged requests at
   `AlwaysSample`, or use a `tail_sampling` policy keyed on the `flowmap.flow`
   attribute. Probabilistic sampling drops spans non-deterministically and
   silently truncates the capture.
4. **File export is additive.** A second exporter on the traces pipeline; the
   `otlp/jaeger` and `debug` exporters are untouched.

See `docs/integration/` for a ready-to-copy collector config and `withFlow`
helper.

---

## 5. Open decisions (for the flowmap team to settle)

- **Multi-service traces.** e2e flows cross services (bus → subscribers), but
  flowmap's model is per-service boundary. *Recommendation: per-service split* —
  ingestion partitions a cross-service trace by `service.name` and validates each
  service against its own spans. Matches the boundary-contract model and lets
  each team own its golden. (The whole-flow graph is a later, separate
  cross-repo-composition concern — Phase 13.)
- **Where expectations live.** In-process, the flow DSL declares them in Go.
  Post-hoc has no Go flow object. *Recommendation: snapshot/set-only to start* —
  the golden is the assertion. Add a small optional per-flow `expectations.yaml`
  (opt-in cardinality) only once a flow has earned it.

---

## 6. Staging — the DX-safe rollout

1. **Ship [P10.1]+[P10.2]+[P10.3] non-gated**, as the coverage view ("boundary
   effects the e2e actually exercised"). Harvests the existing e2e investment
   immediately, **zero flakiness risk**, and gives teams a concrete reason to add
   the `flowmap.flow` tag.
2. **Graduate individual proven-deterministic flows to set-based gating**, routed
   through CODEOWNERS. The sequencing is itself the adoption on-ramp.

This ordering keeps the gate question (§0) entirely inside stage 2, where it
belongs, and never blocks a build on day one.
