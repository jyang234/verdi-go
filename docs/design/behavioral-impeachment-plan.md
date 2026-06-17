# Static × behavioral impeachment — auditing the analyzer's own negatives

> **`PROPOSAL`** · exploratory, designed-not-built · _drafted 2026-06-17_

**Status:** **none of this is implemented.** It is the design record of a single
extended exploration: how to combine the static call graph with captured runtime
behavior so that each covers the other's blind spot, *without* risking the prime
directive. The load-bearing idea — the **impeachment cell** (§3) — is a continuous
soundness audit of the static analyzer itself, built so that its worst failure mode
points at abstention, not at a confident wrong answer. The one shipped prerequisite
it leans on is the producer/code-identity provenance (`--stamp` / `--expect` /
`tool`, see `internal/groundwork/graph` and `cmd/flowmap`); everything else here is
a plan. Companion docs:
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

**The property that makes it special:** the impeachment cell **never makes a new
positive claim of its own — it can only ever *remove* trust.** It cannot manufacture
a false SATISFIED; the most it can do is turn a SATISFIED into a CANT-PROVE.
Therefore its *own* worst failure — a **false** impeachment — degrades a proof to
**abstention**, never to a confident wrong answer. The cell can only push the system
toward *more* caution. A sloppy implementation makes it *noisy*, not *unsafe*, and
noise here also fails toward abstain.

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
	Verdict           string          `json:"verdict"`            // IMPEACHMENT only if every gating rung passed
	Repair            *ProposedRepair `json:"repair,omitempty"`   // present only on IMPEACHMENT; never enacted here
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
marks it blind → the effect's `Claim.Reachability` flips `unreachable`→`blind` → the
impeachment disappears. A mislocalized `Site` does **not** flip it, so the impeachment
persists — the mislocalization announces itself. You never have to *trust* the
localizer; regenerate-and-diff catches a bad `Site` mechanically.

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

**Three fail-closed properties:** (1) `⊥` is *symmetric*, so `absent-from-graph`
cannot be fabricated — a weak parser `⊥`s *both* the graph node and the span tag,
yielding a coarsen-gap, never a phantom missing node. (2) Key collisions →
`ambiguous` → `⊥`. (3) Total and pure → deterministic.

**The parity test (the one-source guard CLAUDE.md requires):** for fixture functions
of each reconcilable class, pin `canonFQN(ssaName) == canonFQN(runtimeName)`, and pin
that closures/generics `⊥` on both spellings. If Go ever changes either convention,
this breaks before the loop can mislocalize.

### Harness-investment levels (precision, recorded as provenance)

- **L0** — entry+effect only; coarse range localization; no FQN parity needed.
- **L1** — runtime FQN tags (`flowmap.fqn`); precise walk; needs `canonFQN` + parity.
- **L2** — build-time tags from the *ssa* FQN; exact, **no reconciliation at all**
  (same string by construction); even closures map.

**Soundness never depends on the level** — L0 yields a coarser-but-sound `Site`; L2 a
precise one. The harness investment is a resolution dial, never a correctness lever —
the part a team might under-invest in can only *blur* the picture, never falsify it.

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

**Per-repair acceptance gate: the self-extinguish test** — after ratifying, regenerate
and the impeachment count drops by exactly one, or the repair is rejected as
mislocalized. A loop whose repairs don't extinguish their own impeachments is the
thing to refuse to ship.

---

## §9 — Verdict integration (last; first time it touches a PR)

An impeachment **invalidates the specific static negative it contradicts** and reuses
the existing three-valued machinery — it invents no new gate. A `must_not_reach`
passing as PROVEN-ABSENT because static said "unreachable," once that unreachability
is impeached, can no longer claim the proof → **downgrades to CANT-PROVE** →
`require_proof` rules **fail closed**, advisory rules disclose. The machine never
decides intent; it refuses to let an impeached proof stand. Observe-first even here:
ship as a *disclosed* downgrade on the substrate line before `require_proof` acts on
it.

---

## §10 — Sequencing, phases, and off-ramps

Order forced by three rules: **risk-ascending** (read-only → substrate-writing →
gate-affecting), **observe-first**, **calibrate-before-trust**. Each phase is
independently shippable and valuable; the plan is a set of off-ramps.

| Phase | Ships | Stop-value | Go/no-go gate |
|---|---|---|---|
| **0 — spine** | witness types + the `observed × unreachable` join (fold in `coverage.Delta` for the other direction); `Verdict: CANDIDATE`, disclosure-only | coverage-calibrated behavioral view | run on real corpora; ~zero candidates ⇒ analyzer already sound, **stop** |
| **1 — ladder** | the five rungs → candidates classified IMPEACHMENT vs the four downgrades | a trustworthy soundness audit, **zero substrate/gate risk** — the natural resting point | measure the rung distribution; *mostly downgrades, rare impeachments* = healthy; mostly IMPEACHMENT = too credulous, fix before proceeding |
| **2 — severance L0** | coarse `Site` (entry+effect anchors) + the proof obligation | impeachments carry a coarse location + known/unknown sort | proof obligation holds; spot-check Sites |
| **3 — map + `canonFQN`** | the map, `canonFQN` + parity test, L1 tags; precise Sites | precise localization, sharp `absent-from-graph` | parity test green + self-extinguish **dry run** |
| **4 — loop** | propose → human-ratify → blind-spot/reclaimer; durable audit | the audit resolves instead of re-firing | per-repair self-extinguish test |
| **5 — verdict** | impeached negative downgrades dependent PROVEN → CANT-PROVE | gating on analyzer-unsoundness | observe-first: disclosed downgrade before `require_proof` acts |

**Cross-cutting:** every canonicalization ships with its determinism test (report
digest P0, severance P2, `canonFQN` parity P3). The corpus convention (decide in P0):
a *committed* representative corpus for any gate; *live* traffic for the standing
audit, `CorpusDigest` pinning what was seen.

**Build first:** Phase 0, and within it the `observed × unreachable` join — the one
direction that doesn't already exist, and the probe that tells you whether any of the
rest is worth doing.

---

## §11 — The prime-directive ledger

How each piece stays inside "determinism and trust before everything":

- **Determinism** — every artifact is a pure function of `(canonical graph, canonical
  trace corpus)`; all ordering/keys are intrinsic (effect label, route, canonical
  span sig, `FQNKey`), never wall-clock/span-id/arrival. The behavioral side leans
  entirely on the existing `canon` determinism (causal order, canonical
  concurrent-sibling order, zero timing).
- **Fail closed** — non-observation is absence in exactly one licensed cell;
  unestablished identity/provenance forces a downgrade *by representation*; `⊥`
  coarsens rather than guesses; no-severance ⇒ no impeachment.
- **Soundness asymmetry** — behavior only ever asserts positives, static only ever
  asserts negatives; the join never crosses them.
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

1. **The capture-side code-identity stamp.** `code-identity` (§4 rung 2) needs the
   trace to carry the deployed commit. Which resource attribute, and how the capture
   harness sets it, is unspecified — it is the hinge of the whole ladder.
2. **Corpus governance for a gate.** A committed corpus is reproducible but can go
   stale against the reachable surface; a live corpus is current but non-portable.
   The behavior-goldens model (`*.effects.json`, `--update`) is the likely answer but
   needs the base-vs-branch framing worked out.
3. **`canonFQN` on generics at L1.** Refused as `⊥` here; L2 (ssa-injected tags) makes
   them exact. Whether L1 ever needs a generics story depends on how much real effect
   surface sits behind generic dispatch — a measurement, not a guess.
4. **Reclaimer auto-proposal shapes.** §8 defaults to blind-spot disclosure; which
   severance bins are safe to *propose* (not enact) a reclaimer for is deferred to the
   reclaimer framework (`internal/static/reclaim`).
