# Design Consistency Pass

> **`HISTORICAL`** · superseded — findings folded into the specs · _reviewed 2026-06-13_

A pass over the seven component specs and four runnable demos to verify the shared contracts hold. Findings are grouped: what's confirmed consistent, the inconsistencies/gaps found (each with a resolution), the one authoritative definition that supersedes the drift, and open decisions later specs already settled.

---

## Confirmed consistent

- **Determinism discipline** — sort-everything, position-insensitive, canonical serialization — is uniform across canonicalization §3.8, static extractor §8, the renderer, and the diff engine. The determinism self-test (harness §5) is the shared safeguard.
- **The tier-map is genuinely one classifier, used three times:** static edge tiering (static §5), canonicalization salience filtering (canon §3.7), and diff prioritization (golden+diff §4). Same feature set, same ruleset.
- **Repo + bus boundary** holds in both pipelines: flows and graphs terminate at publish/consume, and the I/O framing ("no different from any async Go service") is consistent throughout.
- **Entry-points ↔ triggers align:** static roots (mains / handlers / consumers) mirror harness triggers (HTTP / event) and the IR roots (server / consumer spans). Both pipelines organize around the same entry points.
- **Salience threshold = `warn`** everywhere (canon §3.7, static, config, and diff granularity).
- **No AI in the verdict path** — every gate-relevant step is deterministic; the AI-assist surface is advisory only.
- **Golden = IR, not Mermaid** — the gate runs on the structured tree, so it's immune to renderer drift (golden+diff §5, renderer).

---

## Inconsistencies found, with resolutions

### 1. The canonical IR drifted into three definitions — collapse to one
- **canonicalization §2:** `Attrs`, `Discards`; no `Peer`, no `Service`.
- **renderer:** added `Peer` and `Service`; dropped `Attrs`/`Discards`.
- **diff engine:** has `Peer`; no `Attrs`, `Service`, or `Discards`.

This is the real one — three consumers of the same IR don't agree on its shape. **Resolution:** a single authoritative definition (below); canonicalization *produces* the full struct, each consumer reads the subset it needs. `Peer` and `Service` are both derivable in canon §3.5, so folding them in is free.

**Sub-decision — does the diff compare `Attrs`?** Recommend **yes**: a changed secondary detail (e.g. normalized SQL where the operation and table are unchanged) is a real behavioral change, but tier-prioritize it low so it never outranks a contract or tier-1 change. State this in golden+diff §3.

### 2. The tier-assignment seam in canonicalization is implicit — make it a pass
The tier-map spec claims canonicalization tiers spans, but the canonicalization spec has no explicit "derive features → classify → set `Tier`" step. **Resolution:** add a tier-assignment pass to canonicalization, *after* attribute projection and *before* §3.7 salience filtering — derive the span's features from semconv (per tier-map §2), call `Classify`, set `Tier`. The filter then operates on assigned tiers.

### 3. Two distinct gate mechanisms have been conflated under "the gate"
- **Static = currency gate:** regenerate the artifact from code, `git diff --exit-code`. The artifact is a pure function of the code.
- **Behavioral = snapshot-assertion gate:** run the flow (harness), compare observed-canonical to the committed golden, `-update` to re-baseline. The artifact is a function of *running* the code.

Mechanically different, unified only by CODEOWNERS routing + fail-on-unexpected-change + human-as-oracle. **Resolution:** state both explicitly, so the gate-wiring step (GitLab) implements **two** checks, not one — a regenerate-and-diff job for the static artifact and a test-with-golden job for behavior.

### 4. The cardinality assertion has no assigned enforcer
Config declares *prescriptive* cardinality (`ExpectExactlyOnce`); canonicalization records *descriptive* multiplicity. Nothing is named as the step that checks one against the other. **Resolution:** the test-time assertion (alongside golden-compare) enforces the prescriptive cardinality against the IR's observed multiplicity; a violation fails the test independently of the golden diff. Assign it to the test runner.

### 5. Static and behavioral artifacts use different node identity — make the seam deliberate
Static nodes are keyed by FQN (`example.com/loansvc.Service.evaluate`); behavioral nodes are keyed by canonical `Op` (`HTTP POST /loans/{id}`). They don't join at arbitrary functions. **Resolution / decision:** this is acceptable — they join at **entry points** (the shared roots) and at **event names** (the bus contract appears in both vocabularies), which is where joining actually matters. If function-level linkage is ever needed, the static detail sidecar's positions are the bridge. State it as deliberate rather than leaving it an accident.

---

## Authoritative canonical IR (supersedes the three drifted copies)

```go
type CanonicalTrace struct {
    Flow     string          // stable flow id (test name)
    Service  string          // the self lifeline (canon emits; renderer uses)
    Root     *CanonicalSpan
    Discards DiscardManifest  // what was dropped, for transparency (canon emits)
}

type CanonicalSpan struct {
    Op        string            // canonical operation key
    Kind      Kind              // server|client|internal|producer|consumer
    Peer      string            // counterparty lifeline; "" => self/internal
    Tier      int               // assigned by the tier-map classifier
    Status    string            // ok|error|unset
    ErrorType string            // normalized error class
    Attrs     map[string]string // secondary salient detail (sorted on serialize)
    Children  []ChildGroup
}

type ChildGroup struct {
    Concurrent   bool
    Multiplicity string
    Members      []*CanonicalSpan
}
```

Consumer field usage: **canonicalization** populates all of it; **renderer** reads `{Service, Op, Kind, Peer, Tier, Status, ErrorType, Children}`; **diff** reads `{Op, Kind, Peer, Tier, Status, ErrorType, Attrs, Children}`; `Discards` is review-transparency only.

---

## Open decisions later specs already settled (strike from their lists)

- canonicalization §8 "where canonicalization runs" → **resolved** by the harness: capture mode follows test topology (in-process vs. post-hoc).
- harness §8 "expected-exit declaration location" → **resolved** by config: co-located with the test.

## One consequence to record

Per-flow contracts co-located with tests means **CODEOWNERS ownership extends to the relevant test files**, not only the artifact and config paths.

---

# Second sweep (post pressure-test refinements)

After stress-testing the gating model, a set of refinements was folded in: the static pipeline narrowed from gating the full call graph to gating the **inter-service boundary contract** (with the call graph now a generated, non-gated view); the **boundary blind-spot manifest** became part of the gated artifact; DB operations moved to behavioral ownership; the **tested-behavior contract** and **static/behavioral complementarity + coverage-delta capability** were named; the **v1 enforcement model** (author-side manual regeneration + CI staleness backstop, in-process single-service per MR, inter-service E2E deferred) was fixed; and a capstone **scope & guarantees** doc was added to home all of the cross-cutting framing.

## Confirmed consistent after the refinements

- **Gated-vs-generated is now uniform across both pipelines:** static gates the boundary (+ blind-spots) and generates the call graph; behavioral gates the tier-filtered snapshot and generates the Mermaid view. Both achieve low-churn gating via a keep-only-the-consequential projection.
- **"Contract" is now explicitly two things** — behavioral *observed per-flow* (includes DB/ordering) vs. static *inter-service boundary* (excludes DB) — stated in the static spec §4 and the capstone §2, so the term no longer drifts.
- **DB ownership is consistent:** excluded from the static boundary contract (static §4), owned by the behavioral snapshot (canon, harness §5), with the call graph still recording DB *edges* as a generated view (no contradiction — different artifacts).
- **Two gate mechanisms** (currency for static, snapshot-assertion for behavioral) remain consistent across static §9, golden+diff §2, and the capstone §5, now unified under the v1 author-regeneration model.

## Inconsistencies found in this sweep, and fixed

1. **Harness internal contradiction.** §1 described in-process tests as invoking handlers *directly*, which contradicts the fidelity requirement to drive through the instrumented stack. → Reconciled: §1 now says "drives it through its real router / consumer path," and §5 adds the instrumented-stack precondition.
2. **Stale canonicalization section cross-references.** Inserting the §3.6 tier-assignment pass renumbered later sections (salience filtering → §3.7, serialization → §3.8), leaving five stale pointers (four in this doc, one in the tier-map spec). → All corrected; tiering is §3.6, filtering §3.7, serialization §3.8.
3. **Pipeline-diagram topology drift.** Canonicalization §6 depicts the post-hoc Playwright topology as the pipeline, but v1 per-MR is in-process. → Reconciled with a topology note pointing to harness §1 and the capstone, clarifying the diagram is the E2E/nightly path and canonicalization onward is identical for both.

## Still deliberately deferred (unchanged)

Error-path fault injection (and its mechanism), inter-service E2E (with consumer-driven contract tests as the cheaper intermediate), the queryable interface, and cross-repo composition. Snapshot fatigue is recorded as a standing limitation, not a solved problem (capstone §7).
