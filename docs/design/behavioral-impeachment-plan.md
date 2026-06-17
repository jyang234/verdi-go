# Static × behavioral impeachment — finding counterexamples to the analyzer's own negatives

> **`IN PROGRESS`** · Phases 0–3 landed, 4–5 designed-not-built · _drafted
> 2026-06-17, updated 2026-06-17_

**Status:** **Phases 0–3 are implemented** (`internal/impeach`): the read-only
`observed × unreachable` join + witness report (Phase 0), the five-rung downgrade
ladder that classifies a candidate into IMPEACHMENT vs the four benign downgrades
(Phase 1), the **L0 severance localization** (`severance.go`) that records WHERE
static lost the effect — `Site`, flavor, known/unknown sort, the proof obligation
(Phase 2, §6) — and now the **span↔node map + `canonFQN`** (`canonfqn.go`,
`spanmap.go`): the total, pure `canonFQN` reconciler (with a fixture parity test
**and** a passing ⊥-symmetry fuzz, §12.5), the internal-span map's four outcomes
(mapped / absent-from-graph / untagged / ambiguous), and an **L1-precise severance
walk** that resolves the `Site` to the exact severed node on the observed causal
path when `flowmap.fqn` tags are present (falling back to a sound L0 otherwise) —
the node `Site` a `blind_spot` repair self-extinguishes (Phase 3, §6/§7). All four
are disclosure-only and carry **zero substrate/gate risk** — the natural resting
point (§10). **Phases 4–5** (the discovery loop, verdict integration) remain a
plan. It is the design record of a single
extended exploration: how to combine the static call graph with captured runtime
behavior so that each covers the other's blind spot, *without* risking the prime
directive. The load-bearing idea — the **impeachment cell** (§3) — is a
**counterexample finder** for the static analyzer's own negatives: it can only find
unsoundness on *exercised* paths and never proves static is sound (it is not a
complete "audit"), built so that its worst failure mode points at abstention, not at
a confident wrong answer. It has been **pressure-tested (§13)**; the cracks that
surfaced — most importantly that a witnessed policy breach is a `VIOLATED`, not a
downgrade, and that a gate may consume only the committed corpus — are folded back
into §6–§9. The one shipped prerequisite
it leans on is the producer/code-identity provenance (`--stamp` / `--expect` /
`tool`, see `internal/groundwork/graph` and `cmd/flowmap`); everything beyond
Phases 0–1 here is a plan. Companion docs:
[`frontier-instrumentation-plan.md`](frontier-instrumentation-plan.md) (the static
frontier this reuses),
[`post-hoc-behavioral-ingestion.md`](post-hoc-behavioral-ingestion.md) (the trace
ingest this builds on), and
[`policy-coverage-extensions-plan.md`](policy-coverage-extensions-plan.md).

The framing question this doc answers: **the static side over-approximates and the
behavioral side under-approximates — can we join them so the result is sound, and
so non-observation is never silently read as "nothing happened"?**

---

## §1 — Two unsound halves, combined soundly

The toolset's product is the *trustworthiness* of its verdicts. Two analyses with
opposite, well-understood unsoundness can be combined so that each is only ever
asked the question it is sound for:

- **Static reachability** is an **over-approximation**. "No path" (NEVER) is a sound
  *negative*, valid only *outside the disclosed blind spots* (tenet 4). "Reachable"
  is a *may*.
- **Behavioral observation** is an **under-approximation** — a single capture proves
  "this happened" (a sound *positive*: it definitely *can*), but "did not observe X"
  is **not** a proof of absence; the path was merely unexercised.

The join's invariant, stated once:

> **The only sound negative comes from the static (over-approximating) side; the only
> sound positive comes from the behavioral (observing) side. The join refuses to let
> either make the claim it is unsound for.**

The tool already computes a crude slice of this: `flowmap coverage`
(`coverage.Delta(contract, traces)`) returns *statically-reachable minus
behaviorally-observed* — the "reachable but untested" set. This plan structures that
into a soundness-labeled join and adds the inverse direction, which is where the
value is.

---

## §2 — The join lattice

For each `(flow, boundary-effect)` pair, cross the static verdict with the
behavioral one:

| | **Behavior: Observed** | **Behavior: Not observed** |
|---|---|---|
| **Static: Reachable** (a *may*) | **CONFIRMED-LIVE** — both agree | **COVERAGE GAP** — reachable but untested; **never absence**; calibrates the green |
| **Static: Unreachable** (sound *no-path*) | **IMPEACHMENT** — static is *unsound here*; fail-closed alarm | **SOUND ABSENCE** — the **only** cell where non-observation = absence, and it is licensed by *static*, not behavior |
| **Static: Blind** (dynamic dispatch / reflection on the path) | **RECLAIMED-LIVE** — behavior fills a static blind spot | **UNKNOWN** — double-blind → CANT-PROVE → abstain |

Non-observation is trusted as absence in exactly **one** cell, and that trust is
borrowed from static, never asserted by behavior.

**Granularity (a soundness, not a cosmetic, choice).** The cells must key on the
`(emitting-site, effect-label)` pair, **not the bare label** — because one label
(`db DELETE ledger`) can be emitted from a statically-reachable site *and* an
unreachable one, so a label-level trigger would see the label as globally reachable
and **miss** the unreachable site (a false-negative audit). Site-level triggering
needs the emitter span to map (§7); where it does not, the trigger falls back to
label-level **with the false-negative risk disclosed**. So the map's fidelity bounds
the *trigger's* precision, not only the localization's.

**The combination is more than additive — each side resolves the other's ambiguity:**

- *Behavior's "LOST effect — regression or just unexercised?"* → ask static on the
  branch. Observed-on-base, not-on-branch, **and static now Unreachable** → a genuine
  removal (static licenses the negative) → gate-able. Static still Reachable → a
  coverage gap → Caution.
- *Static's "Reachable — real or spurious over-approximation?"* → ask behavior. The
  **IMPEACHMENT** cell catches static's *false negatives*; **RECLAIMED-LIVE** fills
  its dynamic-dispatch blind spots with positive runtime evidence.

**The coverage frontier (green scoped to its evidence).** A behavioral gate must
never emit a bare "no behavioral change" — that is a false green over unexercised
paths. It carries its denominator, mirroring the static frontier's "attribution loss
is a *lower bound*" disclosure: *"no-new-effects over the exercised+reachable surface;
N reachable-but-unobserved effects (listed, attached to their routes) — CANT-PROVE
there."* The markers hang off the static structure (route → reachable-but-unlit
effect), so each reads *"route X can reach `db DELETE ledger` but no captured flow
exercised it — your green here is blind."*

---

## §3 — The impeachment cell, and why it is the *safest* thing to add

`Static: Unreachable ∧ Behavior: Observed`. Because static's NEVER is valid only
outside the disclosed blind spots, an impeachment is a **proof that the blind-spot
disclosure was incomplete** — an undisclosed seam through which the effect escaped
the over-approximation. That is precisely the failure the prime directive fears most
(a confidently-wrong silent negative), caught with a concrete runtime witness. It is
tenet 5 incarnate: the analyzer cannot be the sole grader of its own completeness;
behavior is the independent grader.

**The property that makes it special — with one carve-out the pressure test (§13)
forced.** Against a **bare reachability negative** the impeachment cell never makes a
new positive claim of its own — it can only *remove* trust (turn a SATISFIED into a
CANT-PROVE), so its *own* worst failure (a **false** impeachment) degrades a proof to
**abstention**, never to a confident wrong answer. A sloppy implementation makes it
*noisy*, not *unsafe*. **But** when the impeached negative is a *policy*
`must_not_reach` whose forbidden target the witness actually *observed reaching*, the
cell does the opposite — it surfaces a **behaviorally-confirmed `VIOLATED`** (a true
positive: the forbidden thing demonstrably happened). That is the one case it *adds*
a finding rather than removing trust, and §9 must not launder it down to a passing
caution. Outside that case, the abstain-biased property holds.

**The payoff — it discovers the blind spots static didn't know it had.** Static
discloses the blind spots it *knows* about (reflect, unsafe, high-fanout, detected
dynamic dispatch). Impeachment finds the **unknown unknowns** — an unmodeled
framework registration, a `go:linkname`, a codegen seam — because they are the only
way an effect escapes a sound over-approximation. The localized site becomes a
newly-discovered blind spot (or a reclaimer target), so the *next* static run is
honest there (false NEVER → honest CANT-PROVE). The virtuous loop: **behavior
discovers → static discloses → static's negatives become trustworthy again.**

---

## §4 — The downgrade ladder

A naive impeachment is usually a false alarm. A sound one requires **ruling out every
benign explanation first** — a fixed, ordered ladder where each failed precondition
emits a *specific weaker disclosure* instead of an impeachment:

1. **`static-asserts-no-path`** — static says *unreachable* (a real negative), not
   *blind*. Else `NOT-A-CONTRADICTION` (static already abstains here).
2. **`code-identity`** — the graph was built from the *same code* that produced the
   trace (graph `stamp`/`tool` vs the trace's captured code identity). Else
   `VERSION-SKEW`. *(This is the impeachment cell's most demanding consumer of the
   shipped R11 provenance: production traces may only impeach the production-stamped
   graph.)*
3. **`label`** — the observed effect and the static effect share the **one-source**
   label vocabulary (`WriteLabel`/`sqlverb`/the canon-sql normalizer). Else
   `LABEL-MISMATCH`.
4. **`service-scope`** — the effect is on the impeached service's *own* spans (the
   per-service flow fragments `ingest` groups). Else `CROSS-SERVICE`.
5. **`capture-fidelity`** — the capture is real (production/integration), not a
   mock/test-double span. Else `CAPTURE-UNTRUSTED`.

All gating rungs pass → `IMPEACHMENT`. The ladder is recorded **whole** (not just the
failing rung), so a partial rule-out is itself actionable. The conjunction is what
keeps impeachments *rare*, and rare is what keeps them *trusted* (R2 anti-fatigue).

The missed-edge vs missed-root distinction is carried separately (in the witness's
`EntryDiscovered`), not as a gating rung: both are real impeachments, but of
different kinds.

**`capture-fidelity` is the weakest rung — and the only one that is not mechanically
verified.** It rests on the self-declared `CaptureProvenance` label; a mocked
integration capture mislabeled "production" (a test double emitting a boundary span
the real code gates out) passes the rung and yields a false impeachment. The tool's
ethos disfavors a human assertion as a gate input, so until provenance can be
*attested* (or mock-shaped spans detected structurally), an impeachment is only as
sound as that label is honest — which caps verdict-integration (§9) on capture
pipelines whose provenance is trusted. Tracked in §12.

---

## §5 — The witness schema

A pure function of `(stamped graph, canonical-trace corpus)` → a byte-identical,
digested, recomputable artifact in the same mold as the review artifact. It does
three jobs: the ladder's output, the loop's input, and a deterministic disclosure.
Disclosure-only in this form — it **records**, it never mutates the graph.

```go
type ImpeachmentReport struct {
	Service string `json:"service"`

	// The impeached graph's provenance — the DENOMINATOR's identity (mirrors the
	// graph header, R11). An impeachment is only meaningful against the graph for
	// the code the trace ran.
	GraphStamp string `json:"graph_stamp,omitempty"`
	GraphTool  string `json:"graph_tool,omitempty"`
	GraphAlgo  string `json:"graph_algo,omitempty"`

	// The NUMERATOR's identity. An absent TraceIdentity forces every witness to
	// VERSION-SKEW (identity unestablished ⇒ nothing impeached). CorpusDigest pins
	// the exact canonical trace set audited.
	TraceIdentity string `json:"trace_identity,omitempty"`
	CorpusDigest  string `json:"corpus_digest"`

	// "production" | "integration" | "synthetic". A synthetic/mocked capture caps
	// every witness at CAPTURE-UNTRUSTED. Recorded, never inferred.
	CaptureProvenance string `json:"capture_provenance"`

	Caveats   []string  `json:"caveats,omitempty"`
	Witnesses []Witness `json:"witnesses"` // sorted by (Effect, Flow, Entry, CausalPath)
	Digest    string    `json:"digest"`
}

type Witness struct {
	Effect            string          `json:"effect"`             // canonical label — the join key
	Claim             Claim           `json:"claim"`              // the static negative under test + dependent rule verdicts
	Observed          Observation     `json:"observed"`           // the canonical runtime counterexample
	Rungs             []Rung          `json:"rungs"`              // the FULL ordered ladder evaluation
	Verdict           string          `json:"verdict"`            // CANDIDATE | <downgrade> | IMPEACHMENT | VIOLATED (witnessed policy breach, §9) — only when every gating rung passed
	Repair            *ProposedRepair `json:"repair,omitempty"`   // present on IMPEACHMENT/VIOLATED; never enacted here
}

type Claim struct {
	Reachability string   `json:"reachability"`    // "unreachable" | "blind" | "reachable"  (the self-extinguish hook)
	Rules        []string `json:"rules,omitempty"` // must_not_reach rule names asserting PROVEN-ABSENT
}

type Observation struct {
	Flow            string   `json:"flow"`
	Entry           string   `json:"entry"`             // registration-site literal (route/topic)
	EntryDiscovered bool     `json:"entry_discovered"`  // did the graph model Entry as a root? (missed-root vs missed-edge)
	CausalPath      []string `json:"causal_path"`       // canonical span sigs, entry → effect (no ids, no timestamps)
}

type Rung struct {
	Name     string `json:"name"`     // canonical ordered set (§4)
	Passed   bool   `json:"passed"`   // true = the benign explanation was RULED OUT
	Evidence string `json:"evidence"`
}

type ProposedRepair struct {
	Kind   string `json:"kind"`   // "blind_spot" (default, always sound) | "reclaimer" (needs ratification)
	Site   string `json:"site"`   // the severance point (§6)
	Detail string `json:"detail"`
}
```

**Decisions that make this carry both halves.** (1) Evidence (`Effect`/`Claim`/
`Observed` — facts) is split hard from classification (`Rungs`/`Verdict` — judgment),
so a future stricter ladder can re-classify without re-capturing. (2) `ProposedRepair`
mirrors `blindspots.BlindSpot`/`frontier.Marker` on purpose, so ratification is "move
this proposed blind spot into the graph," not a translation (one source of truth for
the blind-spot shape). (3) `Claim.Reachability` is the **self-extinguish hook** (§6).
(4) Absent provenance fails closed *by representation* — `TraceIdentity == ""` is
"unestablished," forcing `VERSION-SKEW`; the schema cannot encode "unknown" as "ok."

---

## §6 — Severance localization

How `Observed.CausalPath` ∧ the static graph yields `Site`: **project the observed
span chain onto the graph and find the first hop static cannot reproduce.**

```
anchors = [ map(s) for s in CausalPath if map(s) != ⊥ ]   # ordered; last = effect emitter→boundary
severance = ∅
for i in 0 .. len(anchors)-2:
    if not graph.Reaches(anchors[i], anchors[i+1]):       # graph.Index reachability
        severance = anchors[i]; break
if severance == ∅:  → NOT AN IMPEACHMENT                  # the proof obligation, below
else: Site = severance; bin = frontier.Classify(Site); known = staticFrontier.markerAt(Site) != ∅
```

`graph.Reaches`, the `Entrypoints` route→fn join, and `frontier.Classify` already
exist; this is mostly wiring them against the observed chain.

**The proof obligation (fail-closed for free).** If the walk finds *no* broken link,
the effect *is* statically reproducible along the observed anchors — so the `Claim`
of "unreachable" was a mis-read, and we must **not** impeach. The severance search
*is* the verification that a real contradiction exists. No severance ⇒ a
self-inconsistency caveat, never a fabricated seam.

**Three flavors, classified for free:** break *upstream of the emitter* → a dispatch
seam; break *at the emitter* (reaches it, no edge to the boundary) → an effect static
couldn't model/label (the §15 opaque-SQL frontier); `EntryDiscovered == false` → a
missed root (the entry registration site *is* the seam).

**Known vs unknown frontier (value sort):** a `Site` the static frontier already
marks → behavior confirms a *disclosed* seam (a "the negative should have respected
the frontier" bug); no static marker → an *undisclosed* blind spot (the high-value
discovery).

**Self-extinguish polices `Site`.** Ratify the `blind_spot` at `Site` → the graph
marks it blind → because a blind spot means "`Site` may reach anything," the effect's
`Claim.Reachability` flips `unreachable`→`blind` → the impeachment disappears. A
mislocalized `Site` does **not** flip it, so the impeachment persists — the
mislocalization announces itself. You never have to *trust* the localizer;
regenerate-and-diff catches a bad `Site` mechanically.

The acceptance criterion is **monotonic, not unit** (a §13 correction): since
blinding `Site` makes everything past it `CANT-PROVE`, one ratified repair may
extinguish *several* impeachments and downgrade *many* `PROVEN-ABSENT → CANT-PROVE`.
That is all the safe direction, so the test is "*the target impeachment extinguishes
and **no `PROVEN-ABSENT` is newly created***" (proofs only weaken), never "the count
drops by exactly one."

---

## §7 — The span↔node map and `canonFQN`

The map is a **total function to `{node FQN} ∪ {⊥}` that never guesses**; `⊥` is an
honest "doesn't anchor," and §6 absorbs `⊥` gaps. **Precision is a dial; soundness is
invariant.**

Three span classes: **entry** (route → `Entrypoints` join), **effect** (canonical
label → emitter = the span's parent), **internal** (the hard one). The internal map
yields one of four outcomes: `mapped` (anchor), **`absent-from-graph`** (tag parses
to a valid FQN the graph lacks → a *directly localized* missing node, sharper than
the walk), `untagged` (⊥ gap), `ambiguous` (⊥, fail closed).

### `canonFQN` — the one helper reconciling runtime and ssa identities

The same function has two spellings; the divergence is structured:

| function | ssa (graph node) | runtime (L1 tag) | reconcilable? |
|---|---|---|---|
| package func | `…/origination.NewEvaluator` | `…/origination.NewEvaluator` | identical ✓ |
| ptr method | `(*…/client.Bureau).Score` | `…/client.(*Bureau).Score` | re-parenthesize ✓ |
| value method | `(…/client.Bureau).Score` | `…/client.Bureau.Score` | re-parenthesize ✓ |
| closure | `…/svc.run$4` | `…/svc.run.func1` | no correspondence → ⊥ |
| generic | `…/m.Map[int]` | `…/m.Map[go.shape.int]` | concrete vs shape → ⊥ |
| method value | `(*T).M$bound` | `…(*T).M-fm` | synthetic → ⊥ |

```go
type FQNKey struct { Pkg, Recv string; Ptr bool; Name string } // equality = "same function"
func canonFQN(raw string) (FQNKey, bool)                       // total, pure; (_, false) = ⊥
```

The pkg/name split **reuses `features.PkgPath`** (already unified once in PR-44 —
re-implementing reopens that drift). The `⊥` policy: closures (`$N` vs `.funcN`),
generics (concrete vs `go.shape`), method values/thunks (`-fm`/`$bound`), `init`,
and unparseable inputs — each recorded with a reason that rides into the witness.

**Three fail-closed properties:** (1) `absent-from-graph` is sound **only when `⊥` is
symmetric** — `canonFQN` must succeed-or-fail *identically* on both spellings of a
function, or a method whose runtime form keys but whose ssa form `⊥`s reads as
`absent-from-graph` for a node that **exists** (a phantom missing node). A *fixture*
parity test only spot-checks this, so (§13 correction) **`absent-from-graph` is
trusted only at L2** (the tag *is* the ssa string — same parser, same result by
construction); at L1 it is a weak hint until `canonFQN` symmetry is *fuzz-proven*
over generated FQNs. (2) Key collisions → `ambiguous` → `⊥`. (3) Total and pure →
deterministic.

**The parity test (the one-source guard CLAUDE.md requires):** for fixture functions
of each reconcilable class, pin `canonFQN(ssaName) == canonFQN(runtimeName)`, and pin
that closures/generics `⊥` on both spellings. If Go ever changes either convention,
this breaks before the loop can mislocalize.

### Harness-investment levels (precision, recorded as provenance)

- **L0** — entry+effect only; coarse range localization; no FQN parity needed.
- **L1** — runtime FQN tags (`flowmap.fqn`); precise walk; needs `canonFQN` + parity.
- **L2** — build-time tags from the *ssa* FQN; exact, **no reconciliation at all**
  (same string by construction); even closures map.

**Soundness of `Site` never depends on the level** — L0 yields a coarser-but-sound
`Site`; L2 a precise one. The one caveat the pressure test (§13) added: L1's sharp
`absent-from-graph` signal *can* introduce a false localization if `canonFQN` is
asymmetric — caught downstream by self-extinguish, but it means **only L2 is
reconciliation-free**. So the harness investment is a resolution dial for `Site`; the
*sharp* missing-node signal is an L2-only privilege until symmetry is proven.

---

## §8 — The discovery loop (effector; human-ratified; writes the substrate)

The loop **proposes; a human ratifies** — it never silently rewrites the soundness
substrate (the same model as `init` proposing + a CODEOWNER committing, and the
opt-in `--reclaim`). An impeachment-localized `Site` becomes a *proposed* disclosed
blind spot (default — always sound: it makes static abstain at the seam, NEVER →
CANT-PROVE) or, only for a recognized recoverable shape, a *proposed* reclaimer
(needs ratification because a wrong edge fabricates reachability → a false VIOLATED).

Inside the loop, a fail-toward-abstain gradient: **prefer disclosing a blind spot
over writing a reclaimer.** Disclosure is always safe (abstain); a reclaimer is the
*precise* repair that demands a provably-real edge.

**A ratified repair co-updates the blind-spot ratchet (§13).** Adding a blind spot is
exactly what `blind_spot_ratchet` gates as "a new blind spot," so a ratification that
disclosed a seam without allow-listing it would trip that very gate. The ratification
is one reviewed act: it adds the blind spot *and* records it in the ratchet's
allow-list with the impeachment witness as its reason — a reviewed, intentional
disclosure, not drift.

**Per-repair acceptance gate: the self-extinguish test** — after ratifying,
regenerate and confirm the **target impeachment extinguishes while no `PROVEN-ABSENT`
is newly created** (monotonic, §6), else reject the repair as mislocalized. A loop
whose repairs don't extinguish their own impeachments is the thing to refuse to ship.

---

## §9 — Verdict integration (last; first time it touches a PR)

Verdict integration reuses the existing three-valued machinery — it invents no new
gate — but the pressure test (§13) showed the mapping is **two-valued, not one**, and
that it must be fed from a **fixed corpus**. Both corrections are load-bearing.

**A witnessed policy breach is a `VIOLATED`, not a downgrade.** An impeachment carries
a positive witness — *the effect was observed reaching from that entry*. So:

- If `(Observed.Entry, Effect)` falls under a `must_not_reach` rule in `Claim.Rules`
  (the entry is in the rule's `from`, the effect in its `to`), the impeachment is a
  **behaviorally-confirmed `VIOLATED`** — the gate **fails**, full stop. Downgrading
  it to `CANT-PROVE` (which passes without `require_proof`) would launder a witnessed
  violation into a caution — the §13 crack this fix closes.
- Only a **bare** reachability impeachment (no dependent rule, or the entry is outside
  the rule's `from`) invalidates the proof and **downgrades `PROVEN-ABSENT →
  CANT-PROVE`** → `require_proof` rules fail closed, advisory rules disclose.

**Gate-affecting impeachments come only from the committed corpus.** A report over
*live* traffic is not byte-identical run-to-run, so an impeachment sourced from it
must never move a verdict (that would be a non-deterministic gate — a prime-directive
violation). Live-corpus impeachments are **audit-only**; only the committed corpus
(and the same self-extinguish acceptance, §8) may reach a gate. Stated as an
invariant, not a guideline.

The machine never decides intent; it refuses to let an impeached proof stand and
surfaces a witnessed breach as the violation it is. Observe-first even here: ship the
downgrade/violation as a *disclosed* substrate-line note before it fails a gate.

---

## §10 — Sequencing, phases, and off-ramps

Order forced by three rules: **risk-ascending** (read-only → substrate-writing →
gate-affecting), **observe-first**, **calibrate-before-trust**. Each phase is
independently shippable and valuable; the plan is a set of off-ramps.

| Phase | Ships | Stop-value | Go/no-go gate |
|---|---|---|---|
| **0 — spine** ✅ **LANDED** | **(prereq, §14-A) DB boundary effects in the corpus** — extend capture (`otelsql`) + `ingest`/`coverage` so the join's effect vocabulary includes `db <verb>` writes, not only the otelaws bus/dep surface; then witness types + the `observed × unreachable` join (fold in `coverage.Delta` for the other direction); `Verdict: CANDIDATE`, disclosure-only | coverage-calibrated behavioral view **that can see the marquee `db DELETE` case** | run on real corpora; ~zero candidates ⇒ analyzer already sound, **stop** — **measured:** 0 candidates on loansvc/obligsvc/blindsvc (sound), and the cell fires on a genuine *undisclosed missed root* (the impeachsvc fixture); the real candidate justified proceeding to Phase 1 |
| **1 — ladder** ✅ **LANDED** | the five rungs (`internal/impeach/ladder.go`) → candidates classified IMPEACHMENT vs the four downgrades (`NOT-A-CONTRADICTION`/`VERSION-SKEW`/`LABEL-MISMATCH`/`CROSS-SERVICE`/`CAPTURE-UNTRUSTED`); ladder recorded **whole**, verdict = first failing rung | a trustworthy counterexample finder (over exercised paths), **zero substrate/gate risk** — the natural resting point | measure the rung distribution; *mostly downgrades, rare impeachments* = healthy; mostly IMPEACHMENT = too credulous, fix before proceeding — **measured:** downgrade-dominated, **0 IMPEACHMENT without attested provenance** (no commit stamp on the corpus today ⇒ `VERSION-SKEW`, §14-D); the genuine impeachsvc candidate promotes to IMPEACHMENT only under a stamped graph + matching production capture — healthy |
| **2 — severance L0** ✅ **LANDED** | coarse `Site` (entry+effect anchors) + the proof obligation (`internal/impeach/severance.go`): the entrypoint join maps the observed entry, `staticEmitters` the effect, and the L0 walk classifies the break (missed-root / severed-emitter / unmodeled-effect) + sorts it known/unknown via the frontier section; a reproducible effect localizes to `SeveranceNone` (the proof obligation, disclosed in a caveat, never a fabricated seam) | impeachments carry a coarse location + known/unknown sort | proof obligation holds; spot-check Sites — **measured:** the impeachsvc missed root localizes to its entry registration literal, sorted UNDISCLOSED; the synthetic severed-emitter/unmodeled/absent-missed-root flavors localize as designed; determinism preserved (severance rides the byte-identical digest) |
| **3 — map + `canonFQN`** ✅ **LANDED** | the span↔node map (`spanmap.go`: node reverse-index by `FQNKey`, the four internal-span outcomes), `canonFQN` + `FQNKey` (`canonfqn.go`: total, pure, ⊥-with-reason) + the fixture **parity test** and the **⊥-symmetry fuzz** (`FuzzCanonFQNSymmetry`) the sharp `absent-from-graph` needs, and the L1-precise walk (`localizeL1`: the first severed path node carrying the effect → a node `Site`, with the `absent-from-graph` hint riding beside it as a weak-at-L1 signal); causal-path threaded into the witness; untagged corpora fall back to L0 | precise localization, sharp `absent-from-graph` | parity test green + self-extinguish **dry run** — **measured:** parity + symmetry fuzz green (the fuzz surfaced and pinned a dotted-final-segment asymmetry as the documented L2-only carve-out, and a leaky-key regression now fixed); the L1 walk localizes the severed-node `Site`, and the self-extinguish dry run confirms a `blind_spot` there extinguishes the target while creating no new candidate (monotonic) |
| **4 — loop** | propose → human-ratify → blind-spot/reclaimer; durable record | findings resolve instead of re-firing | per-repair self-extinguish test |
| **5 — verdict** | witnessed policy breach → `VIOLATED`; bare impeachment → dependent PROVEN → CANT-PROVE; **committed corpus only** (§9) | gating on analyzer-unsoundness and on witnessed breaches | observe-first: disclosed before it fails a gate |

**Cross-cutting:** every canonicalization ships with its determinism test (report
digest P0, severance P2, `canonFQN` parity P3 — plus the `canonFQN` ⊥-symmetry fuzz
that L1 `absent-from-graph` needs). The corpus convention (decide in P0) is an
**invariant, not a guideline**: a *committed* representative corpus is the **only**
input that may reach a gate (P5); *live* traffic is audit-only, with `CorpusDigest`
pinning what was seen.

**Build first:** Phase 0 — and within it, *first* the DB-effect prerequisite
(§14-A), then the `observed × unreachable` join. The join is the one direction that
doesn't already exist and the probe that tells you whether any of the rest is worth
doing; but fed only by the existing otelaws-only effect vocabulary it would abstain
on exactly the highest-value target (a false-`NEVER` on `db DELETE`), so the corpus
must carry DB effects before the probe is meaningful.

**Next (Phases 0–1 + the capture-side stamp done):** the read-only detector and
the ladder are landed and measured healthy, and the **capture-side code-identity
stamp** that the `code-identity` rung consumes is now wired end to end (§12.1,
§14-E): a live corpus self-describes its deploy through `flowmap.code.stamp`, so a
Phase-1 impeachment is meaningful on real captured behavior rather than only under
a caller assertion. A committed corpus stays stampless and takes the gated
identity at audit time. **Phase 2 (severance L0) is the next build.** The remaining
stamp-adjacent gap is the base-vs-branch *gate* identity (§12.2), which only binds
at Phase 5. **Phases 2–3 are now landed** — every candidate carries a `Site` (coarse
L0, or precise L1 when the corpus is FQN-tagged) with the known/unknown sort, the
proof obligation fail-closed, and `canonFQN`'s ⊥-symmetry fuzz green over the
realistic domain — so **Phase 4 (the discovery loop: propose → human-ratify →
blind-spot/reclaimer, with the per-repair self-extinguish gate) is the next
build**. The L1 `absent-from-graph` signal stays a weak hint until the symmetry
fuzz is promoted to gate L1 trust (§12.5); the capture-side `flowmap.fqn` tags
(§14-D) and `CaptureProvenance` (rung 5) remain the absent substrate Phase 4+
budgets honestly.

---

## §11 — The prime-directive ledger

How each piece stays inside "determinism and trust before everything":

- **Determinism** — every artifact is a pure function of `(canonical graph, canonical
  trace corpus)`; all ordering/keys are intrinsic (effect label, route, canonical
  span sig, `FQNKey`), never wall-clock/span-id/arrival. The behavioral side leans
  entirely on the existing `canon` determinism (causal order, canonical
  concurrent-sibling order, zero timing). **Only the committed corpus reaches a gate**
  (§9); a live corpus is audit-only, so no gate is ever a function of run-varying
  traffic.
- **Fail closed** — non-observation is absence in exactly one licensed cell;
  unestablished identity/provenance forces a downgrade *by representation*; `⊥`
  coarsens rather than guesses; no-severance ⇒ no impeachment.
- **Soundness asymmetry** — behavior only ever asserts positives, static only ever
  asserts negatives; the join never crosses them. The cell *removes* trust on a bare
  reachability negative (→ CANT-PROVE) and *adds* a true positive on a witnessed
  policy breach (→ VIOLATED, §9) — never the reverse (it cannot fabricate a SATISFIED).
- **Self-honest about blind spots** — the coverage frontier scopes every green to its
  evidence; double-blind cells are CANT-PROVE; the map's `⊥` classes are disclosed.
- **The machine is not the oracle** — the loop proposes, a human ratifies; correctness
  is mechanically checked (proof obligation, self-extinguish, parity test), never
  self-graded.

The structural safety argument: **nothing that can write the trust substrate (P4) or
move a verdict (P5) runs until the read-only detector beneath it has been measured
(P1) and the localization beneath *that* has been validated by self-extinguish (P3).
The risk is admitted in exactly the order it can be retired.**

---

## §12 — Open questions (not yet decided)

> Resolutions landed 2026-06-17 are recorded in §14; the entries below are annotated
> **RESOLVED**/**STANDING** so the original question survives next to its answer.

1. **The capture-side code-identity stamp.** `code-identity` (§4 rung 2) needs the
   trace to carry the deployed commit. Which resource attribute, and how the capture
   harness sets it, is unspecified — it is the hinge of the whole ladder.
   **RESOLVED (§14-E)** — a flowmap-specific OTel **resource** attribute
   `flowmap.code.stamp` (`capture.CodeStampAttr`, one owner), read post-hoc by
   `otlpjson`/`ingest` and set in-process by the harness `WithCodeStamp` option,
   rides to `ir.CanonicalTrace.Stamp`. It mirrors the graph's `--stamp`:
   caller-supplied, never derived, and **excluded from snapshot equality** (a
   committed golden stays stampless, so it never churns per deploy — identity is
   injected at audit time, the live corpus self-describes). `impeach.Audit`
   reconciles the two sources (`resolveIdentity`) and fails closed on a mixed or
   contradicted identity. The base-vs-branch *gate* framing (§12.2) is still its own
   STANDING piece for Phase 5.
2. **Corpus governance for a gate.** A committed corpus is reproducible but can go
   stale against the reachable surface; a live corpus is current but non-portable.
   The behavior-goldens model (`*.effects.json`, `--update`) is the likely answer but
   needs the base-vs-branch framing worked out. **RESOLVED (§14-B)** — reuse the
   shipped behavior-goldens model as *the* committed corpus; live stays audit-only.
   Only the base-vs-branch framing remains STANDING.
3. **`canonFQN` on generics at L1.** Refused as `⊥` here; L2 (ssa-injected tags) makes
   them exact. Whether L1 ever needs a generics story depends on how much real effect
   surface sits behind generic dispatch — a measurement, not a guess.
4. **Reclaimer auto-proposal shapes.** §8 defaults to blind-spot disclosure; which
   severance bins are safe to *propose* (not enact) a reclaimer for is deferred to the
   reclaimer framework (`internal/static/reclaim`).
5. **`canonFQN` ⊥-symmetry (gates L1 `absent-from-graph`, §7/§13).** The sharp
   missing-node signal is sound only if `canonFQN` succeeds-or-fails identically on a
   function's ssa and runtime spellings. A fixture parity test under-covers it; a fuzz
   over generated FQNs is needed before L1 may trust `absent-from-graph` (until then,
   L2-only). **PARTIALLY RESOLVED (Phase 3)** — `FuzzCanonFQNSymmetry`
   (`internal/impeach/canonfqn_fuzz_test.go`) now generates matching ssa/runtime
   spellings of each reconcilable class and asserts canonFQN agrees; it is **green
   over the realistic domain** (clean-identifier final package segment). It pinned
   the one **STANDING** gap: a dotted-final-segment import path (`gopkg.in/yaml.v3`)
   can split asymmetrically, because the value-method-vs-package-func boundary is
   only recoverable from a clean final segment — so `absent-from-graph` stays a
   weak L1 hint / L2-only until that carve-out is closed or proven irrelevant to
   first-party code. Methods reconcile symmetrically regardless (the receiver path
   splits at its last `.`).
6. **Capture-provenance attestation (§4/§13).** `capture-fidelity` is the one
   human-asserted rung; whether it can be mechanically attested (or mock-shaped spans
   detected structurally) decides whether a live-mislabeled capture can ever produce a
   false impeachment. Until resolved, verdict-integration is restricted to trusted
   pipelines.
7. **Trigger granularity (§2/§13).** Site-level triggering needs the emitter span to
   map; the fallback to label-level carries a disclosed false-negative risk. How often
   real effect surface forces the fallback is a measurement that decides whether L1+
   instrumentation of emitters is mandatory rather than optional.

---

## §13 — Pressure test (the cracks, and where each is handled)

The design was adversarially stress-tested before any of it was built. Eight cracks
surfaced; none were fatal, all are folded back above. Recorded here so the doc is
honest about its own stress test rather than presenting only the polished face.

| # | Crack | Severity | Resolution |
|---|---|---|---|
| 1 | §9 downgraded a **witnessed `must_not_reach` breach** to a passing `CANT-PROVE` — laundering a real, observed violation into a caution | **prime-directive** | §9 rewritten: a witnessed policy breach is a `VIOLATED` (gate fails); only a *bare* reachability impeachment downgrades. §3 carve-out added. |
| 2 | A **live corpus** feeding a gate makes the gate non-deterministic | **prime-directive** | §9/§10/§11: only the *committed* corpus may reach a gate; live is audit-only. Stated as an invariant. |
| 3 | `absent-from-graph` can be **fabricated at L1** if `canonFQN` is asymmetric (a phantom missing node) | broken property | §7: `absent-from-graph` trusted **only at L2** until ⊥-symmetry is fuzz-proven; weak hint at L1. §12.5. |
| 4 | Self-extinguish "drops by **exactly one**" rejects a correct repair (blinding a site downgrades many) | broken property | §6/§8: acceptance is **monotonic** — target extinguishes, no new `PROVEN-ABSENT`. |
| 5 | Trigger keyed on the **bare label** misses an unreachable site when the label is reachable elsewhere | broken property | §2: trigger on `(emitting-site, label)`; label-level fallback discloses the false-negative risk. §12.7. |
| 6 | A ratified blind-spot repair **trips `blind_spot_ratchet`** (its sibling gate) | integration gap | §8: ratification co-updates the ratchet allow-list with the witness as its reason. |
| 7 | `capture-fidelity` is **human-asserted**, mechanically unverified — a mislabeled mock yields a false impeachment | soft spot | §4: flagged as the weakest rung; verdict-integration restricted to trusted pipelines. §12.6. |
| 8 | "Audit" **overclaims** — it can only find counterexamples on exercised paths, never prove soundness | framing | Reframed throughout to "counterexample finder"; the coverage frontier (§2) already scopes the green. |

The two prime-directive cracks (#1, #2) were both in the **verdict-integration** layer
(§9) — exactly where the phasing put the most scrutiny and the latest, gated step. The
core idea survived: static still licenses the negatives, behavior still licenses the
positives, and the failure modes still bias toward abstention — but only after §9 was
corrected to surface a witnessed breach as the violation it is.

---

## §14 — Pre-implementation reconciliation (decided 2026-06-17)

Before any code, the plan was checked against the **actual** state of the primitives
it leans on. The spine and phasing survived intact; four reconciliations below keep
the doc honest about what exists vs. what it names aspirationally, plus the two scope
decisions the owner made. None change the soundness argument — they pin the plan to
the substrate so Phase 0 builds on facts, not on the doc's vocabulary.

**A — Phase 0 blocks on DB boundary effects first (owner decision).** The marquee
impeachment — a false-`NEVER` on `db DELETE ledger` — sits *outside* today's
behavioral surface: `coverage.Delta` (`internal/coverage`) excludes DB ops and
entrypoints, and the post-hoc effect goldens (`*.effects.json`, `internal/ingest`)
exclude DB spans because capture is `otelaws`-only. So the `observed × unreachable`
join, fed by the existing vocabulary, would *abstain on exactly the case the plan
exists to catch*. Phase 0 therefore gains a prerequisite: extend capture (`otelsql`)
and `ingest`/`coverage` to carry `db <verb>` writes into the canonical op-key space —
reusing the **one-source** write vocabulary (`sqlverb.MutatingVerbs`:
`DELETE/INSERT/MERGE/REPLACE/UPDATE/UPSERT`, mirrored by `fitness.IsWrite` /
`graphio.mutatingSQLOp`) so the behavioral DB label and the static DB label key
identically. This pulls forward capture-pipeline work the plan had implicitly
deferred; it is the admission price of the join being meaningful on day one.

**B — Corpus model: reuse the behavior-goldens (owner decision).** The committed
`*.effects.json` corpus (+ `--update` + CODEOWNERS routing, already shipped) *is* the
committed corpus that may reach a gate (§9/§10); live traffic stays audit-only with
`CorpusDigest` pinning what was seen. No new corpus artifact. The base-vs-branch
framing (§12.2) is the only piece left to work out, and only binds at Phase 5.

**C — Verdict-name reconciliation (the obligation layer spells the poles
differently).** This doc uses the *conceptual* three-valued names (CLAUDE.md's
PROVEN / VIOLATED / CANT-PROVE). The code spells them across two layers:
- **Obligations** (`internal/groundwork/fitness/obligations.go`): `SATISFIED` /
  `VIOLATED` / `CANT-PROVE` / `UNMATCHED`. There is **no `PROVEN-ABSENT` constant** —
  a proven-absent `must_not_reach` is a `SATISFIED` obligation. So everywhere this
  doc writes **`PROVEN-ABSENT`, read `SATISFIED`**; §6/§8/§9's "downgrade
  `PROVEN-ABSENT → CANT-PROVE`" is `SATISFIED → CANT-PROVE`, and the self-extinguish
  acceptance ("no `PROVEN-ABSENT` newly created", §6/§8) is written against
  `SATISFIED`.
- **Review gate** (`internal/groundwork/review/artifact.go`): `BLOCK` /
  `STRUCTURALLY-CLEAR` / `NO-STRUCTURAL-SIGNAL`. §9's "the gate **fails**" is the
  review layer returning `BLOCK`. §9 correctly spans both layers; the implementation
  must not conflate them.

**D — Helpers the §6/§7 pseudocode names but that are wiring, not existing API.**
- `graph.Reaches(a, b)` ⇒ `contains(Index.Reachable(a), b)`
  (`internal/groundwork/graph/index.go`: `Reachable`/`Reaching`/`EntrypointCover`).
- `staticFrontier.markerAt(Site)` ⇒ a lookup over the **shipped** frontier disclosure
  section (`Index.Frontier()`, the frontier-instrumentation companion); the helper
  itself must be written. `frontier.Classify` takes a minimal `Input`, not a `Graph`.
- The capture-side substrate rungs 2/5 and Phase 3+ need **does not exist yet**:
  `CaptureProvenance` (the "production|integration|synthetic" label), the deployed-
  commit stamp, and runtime FQN tags (`flowmap.fqn`) are all absent — `ingest.FlowCapture`
  carries only `Synthesized bool`. Phase 0 is L0 (entry+effect anchors) and needs none
  of them; this is recorded so Phase 1 (rung 2, §12.1) and Phase 3 (the map, §7) budget
  the capture-pipeline work honestly rather than assuming a field that isn't there.
  *(Update: the deployed-commit stamp **now exists** — §14-E. `CaptureProvenance`
  (rung 5) and the `flowmap.fqn` tags (Phase 3) remain absent.)*

**E — The capture-side code-identity stamp, wired (resolves §12.1).** Built before
Phase 2 to make the `code-identity` rung meaningful on a real corpus. The deployed
commit travels as a flowmap-specific OTel **resource** attribute,
`flowmap.code.stamp` (`capture.CodeStampAttr` — one owner, keyed identically by
both capture paths): `otlpjson` folds it off the resource and `ingest` lifts it
onto the per-service `CapturedFlow.Stamp` (failing closed on a mixed-deploy
disagreement), and the in-process harness sets it via `WithCodeStamp`. The
canonicalizer carries it to `ir.CanonicalTrace.Stamp`. It deliberately mirrors the
static graph's `--stamp` discipline: **caller-supplied, never derived** (deriving
from git HEAD would make the trace a function of more than the captured behavior),
and **excluded from snapshot equality** (`golden.canonicalBytes` zeroes it and the
`-update` writer is stampless, so a committed golden never carries a run-varying
stamp and never churns per deploy). `impeach.Audit` reconciles the corpus-carried
identity with an optionally-injected one (`resolveIdentity`): the live corpus
self-describes, the committed corpus takes the gated SHA, and any contradiction or
mixed corpus fails closed to `VERSION-SKEW`. Determinism is preserved throughout —
the report stays a pure function of `(graph, corpus)`.
