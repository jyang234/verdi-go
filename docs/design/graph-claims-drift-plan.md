# Graph-claims verification & attribute-drift reporting — implementation plan

> **`ACTIVE`** · plan awaiting implementation · _authored 2026-07-04_

**Status:** planned, not started. Responds to the consumer feature-request report
`verdi-go feature requests — graph-claims verification & drift reporting, 2026-07-03`
and its companion behavioral spec (`verdi-go-fr-prototype-spec-2026-07-03`), the
follow-up to the flowmap findings closed at `86dc687`. Substrate facts below were
re-verified against this tree at `86dc687`; the companion spec's loansvc acceptance
numbers (40 nodes, 49 unique edges, 3 `Score` candidates) **reproduce exactly** on
`flowmap graph --algo rta testdata/fixtures/loansvc` at that pin, so the spec's §1.6
expected output can be pinned verbatim as a fixture.

## What is being asked, in this repo's terms

The consumer audited 20 design docs against pinned graphs: every error found was
*consumer-side transcription drift* (fictitious compressed edges, hand-typed tiers,
an edge drawn across a disclosed `ExternalBoundaryCall` blind spot) — and separately,
node/edge *attributes* (tier, `concurrent`) changed between pins while both their
set-based validation and our own `--diff` reported "additive only". Both gaps are
fail-open directions for artifacts *derived from* our output, sitting just outside
the honesty machinery the graphs already carry. Four asks:

| FR | priority | one-line | verdict on fit |
|---|---|---|---|
| FR-1 | P1 | `groundwork assert <graph.json> <claims.json>` — verify doc claims against a graph, fail-loud on ambiguity | accept; it is the point-in-time complement to `fitness` (same judge philosophy, inverted lifecycle) |
| FR-2 | P1 | attribute-aware `--diff` (third *changed* class) + JSON delta + release-noting semantic changes | accept; the blind spot is already pinned as a KNOWN LIMITATION in `mermaid_layers_test.go:94` |
| FR-3 | P3 | document edge-record multiplicity | accept, docs-only |
| FR-4 | P2 | `--mermaid --focus <fqn,…>` induced-subgraph rendering | accept the generic core; the manifest/overlay layer stays consumer-side per the FR itself |

Non-duplication check (the FR did its own; confirmed here): `verify-artifact` proves
authenticity, not content claims; `groundwork diff` is boundary-contract level;
`fitness`/policy rules gate ongoing invariants and would *fight* a spec that documents
a defect by asserting an edge's absence. Nothing existing evaluates point-in-time
claims files. `groundwork reach`/MCP `ground` are exact-FQN, single-function views.

## Sequencing

Four phases, independently landable, in this order:

1. **Phase 0 (FR-3)** — docs paragraph. Zero risk, unblocks consumer counting today.
2. **Phase 1 (FR-1)** — shared resolver + `groundwork assert`. Highest value; turns
   the consumer's manual audit into a CI step.
3. **Phase 2 (FR-2)** — attribute-aware diff (mermaid `:::changed`, JSON delta,
   rollup note) + the release-notes discipline in CONTRIBUTING.
4. **Phase 3 (FR-4)** — `flowmap graph --mermaid --focus`, reusing Phase 1's resolver
   and the existing render-time sub-graph machinery.

---

## Phase 0 (FR-3): document edge-record multiplicity

`edges[]` legitimately carries the same `(from, to)` pair more than once: `sortGraph`
(`internal/static/graphio/graphio.go:1445`) totally orders edges on **all six fields**
(From, To, Tier, Boundary, Concurrent, Via) and `dedupEdges` (`graphio.go:1498`)
collapses only full-struct-equal records — deliberately, per the comment at
`graphio.go:1469-1475`: a plain reference and a `go`-launched call to the same callee
are two facts. So record identity is the **full attribute tuple**, not `(from, to)` —
slightly stronger than the FR's suggested `(from, to, mode)`, and we should document
what is true rather than the approximation.

**Change:** one paragraph in `docs/groundwork/usage.md` §"Consuming graph.json
directly", under the `edges[]` row of the field table (`usage.md:1207`): record
identity, the sync+concurrent example from the field report, and the counting rule —
consumers counting "edges" must choose raw records vs unique `(from, to)` pairs, and
every count in FR-1's `assert` (below) is over **unique pairs**. Cross-reference from
`docs/specs/static-extractor-spec.md` §8 (serialization) in one sentence.

**Acceptance:** docs only; `make verify` green (no code).

---

## Phase 1 (FR-1): shared resolver + `groundwork assert`

### 1a. The resolver — new leaf package `internal/fqnres`

The companion spec's Part 3 requirement ("one resolver, both features") plus FR-4
means the resolver must be importable from **both** `cmd/groundwork` (via
`internal/groundwork/claims`) and `internal/static/graphio` (for `--focus`). Neither
side should import the other, so it lives in a neutral stdlib-only leaf package —
same layering precedent as `internal/boundarylabel` and `internal/tiermap`.

Semantics, exactly as validated by the prototype (spec §1.3):

- **Plain string = normalized-FQN suffix.** Normalize by stripping the receiver
  punctuation `(`, `)`, `*` from *both* the claim string and each candidate, then
  `strings.HasSuffix`. `handler.App).Create` and `handler.App.Create` both resolve to
  `(*example.com/loansvc/internal/handler.App).Create`. Exactly-one-or-die:
  0 matches → `UNRESOLVED`, ≥2 → `AMBIGUOUS` carrying up to 4 candidates, **sorted**.
- **`/re/` = explicit regex**, unanchored search against the **raw** FQN string
  (receiver punctuation visible for anchoring), multi-match legal. Go RE2 syntax; a
  compile error is a claim ERROR. The raw-vs-normalized asymmetry is intentional and
  gets a doc comment: plain names are the ergonomic form and forgiving about receiver
  syntax; regexes are the precision form and see every byte.
- **Per-side enforcement** (the spec's own recommended tightening of its prototype's
  quirk): in an `edge` claim, each endpoint enforces the rule its own form implies —
  a regex on one side does not relax uniqueness on the other.

API sketch:

```go
package fqnres

type Result struct {
    Matches   []string // sorted; len==1 on unique success
    Ambiguous bool     // len(Matches) > 1 (plain form only)
    IsRegex   bool
}
// Resolve matches query against universe (a sorted slice the caller builds once).
// Plain queries use normalized-suffix + unique-or-die; /re/ queries return every
// raw-string match. err covers only regex compile failures.
func Resolve(query string, universe []string) (Result, error)
```

**Relationship to the existing triage resolver** — deliberate non-unification:
`impact.ResolveFrame` (`internal/groundwork/impact/resolve.go:30`) does *raw*
token-bounded suffix matching for **runtime stack frames**, a different input grammar
with a different forgiveness contract (it must not strip `(*T)` punctuation because
frames arrive in runtime form and go through `frameToFQN`). Claims are hand-authored
doc labels; frames are machine-emitted. The two contracts stay separate, each with a
comment naming the other and why they differ — per CLAUDE.md's one-source-of-truth
rule this is two *rules*, not one rule in two places, and a test in each package pins
the distinguishing case (`handler.App).Create` resolves in `fqnres`, not in `impact`).
A token-boundary tightening of `fqnres` (requiring the suffix to start at `.(*/`) is
deliberately **deferred**: the shipped semantics must match the 138-claim-validated
prototype byte-for-byte first; looseness here is fail-closed anyway (over-matching
produces AMBIGUOUS, an error — never a silent wrong pass).

### 1b. Core evaluation — new package `internal/groundwork/claims`

Consumes the graph through the existing strict loader (`graph.LoadFile` →
`graph.Load`, `internal/groundwork/graph/graph.go:540,591` — `DisallowUnknownFields`
already gives fail-loud schema-skew behavior for free).

Internal model built once per run:

- **Pair set**: unique `(from, to)` over `g.Edges` — the FR-mandated counting basis
  for every kind. (loansvc currently has zero duplicate pairs, so a synthetic-graph
  unit test with a sync+concurrent duplicate pins the dedup.)
- **Endpoint universe** (for `edge`/`no_edge`/`edge_count`/degrees): node FQNs ∪ all
  edge `from`/`to` strings — boundary pseudo-nodes (`boundary:db QueryContext`) occur
  only as edge endpoints and must be claimable.
- **Node universe** (for `node`/`no_node`): node FQNs only.

Claim kinds and evaluation exactly per spec §1.4 (table reproduced in the package doc
comment, not here): `edge`, `no_edge`, `edge_count`, `node` (+optional `tier`),
`no_node` (never errors — zero matches IS the pass; ≥1 match FAILs listing them, so a
rename cannot vacuously pass it… which is precisely why every *other* kind must ERROR
on resolution failure), `in_degree`, `out_degree` with optional counterpart filter.

Two schema decisions where the spec advises deviating from its own prototype, both
adopted:

1. **Counterpart filter field name**: implement `counterpart_matching` as canonical
   (the spec's recommended rename — `to_matching` on `in_degree` filters the *from*
   side and the name lies); also accept `to_matching` as a documented alias so the
   consumer's existing 138-claim suite runs unmodified, erroring if both are present.
2. **Unknown `kind`** is a claim-level ERROR, not a file abort; remaining claims run.
   A malformed claims *file* (bad JSON, missing `claims`) is an operational error.

Claims-file decoding is strict (`DisallowUnknownFields`) — a typo'd field name
(`form:` for `from:`) must not silently become a zero-value claim; per-claim
kind-specific field validation (e.g. `edge` requires `from`+`to`, `eq` required on
counts) errors that claim.

### 1c. CLI — `groundwork assert <graph.json> <claims.json>`

Wired like every other subcommand: a `case "assert"` in the `run` switch
(`cmd/groundwork/main.go:83-122`), a `cmdAssert` using `flag.NewFlagSet` +
`fs.NArg() != 2` usage error (the `cmdDiff`/`cmdReach` idiom), a `usageBody` line —
the meta-tests `TestUsageBodyDocumentsEverySubcommand` and
`TestRunHelpFlagIsCleanAcrossSubcommands` enforce the wiring.

**Exit codes — the FR's ask maps onto the existing convention with zero force-fit**
(`main.go:44-67`, pinned by `TestVerdictVsOperationalErrors`): exit 1 = a computed
verdict failed (`verdictError`), exit 2 = the gate could not run. So: ≥1 FAIL →
`verdictf(...)` (exit 1, taking precedence over errors, per spec §1.1); zero FAILs
but ≥1 claim ERROR → a plain error (exit 2 — "unresolvable claim" *is* "this claim's
gate could not run", the same honesty split); all pass → nil. Report always prints to
stdout before the error return, in **claims-file order** (FAIL lines, then ERROR
lines), then the summary line:

```
assert: N passed, N failed, N errored (graph: N nodes, N unique edges)
```

Output is fully deterministic: candidate lists sorted and capped at 4, `no_edge`
offender lists sorted and capped at 3, counts over the deduped pair set.

### 1d. Tests and fixtures

- **The loansvc acceptance case, verbatim**: commit spec §1.6's seven-claim file as
  `testdata/groundwork/claims/loansvc-acceptance.claims.json`; a test in
  `cmd/groundwork` builds the loansvc graph in-process (the `enactment_test.go`
  fixture idiom) and asserts the report **byte-equal** to the spec's expected output
  (adjusted only for the `tool` stamp) and the 1 exit class — all four outcome
  classes (PASS / FAIL / AMBIGUOUS / UNRESOLVED) exercised in one fixture, expected
  output already field-validated.
- Unit tests in `internal/groundwork/claims`: per-kind tables over a small inline
  graph; the duplicate-pair dedup case; `no_node` polarity (rename → FAIL not ERROR
  asymmetry, both directions); per-side regex/plain enforcement; `counterpart_matching`
  alias + both-present error; unknown kind isolation; boundary-endpoint claims.
- Resolver tests in `internal/fqnres`: normalization table (`)`-stripping, `$N`
  closures, generic brackets pass through as raw bytes), AMBIGUOUS candidate cap +
  sort, regex-sees-raw-bytes (`/PostgresStore\).GetMessage$/`).
- Determinism: run twice over a shuffled-input graph, byte-compare reports (the canon
  fuzz-corpus discipline, sized to fit).
- Docs: a "Claims files" section in `docs/groundwork/usage.md` — suffix-vs-regex
  convention, the FAIL/ERROR distinction and why it exists (rename must not convert
  an absence claim into a vacuous pass), unique-pair counting (linking Phase 0's
  paragraph), exit codes.

**Acceptance for the phase:** the FR's suggested acceptance verbatim — claim kinds +
resolution semantics shipped, the loansvc fixture exercising all outcome classes,
suffix-vs-regex documented, pair-counting defined and documented.

---

## Phase 2 (FR-2): attribute-aware diff + release-noting semantic changes

### 2a. Where the blind spot lives (verified)

`mermaid_diff.go` classifies presence-only: node identity = FQN (`nodeIndex`
`mermaid_diff.go:77`), edge identity = `ekey{from,to}` (`:75`), `stateOf` (`:37`) →
kept/added/removed. In the edge loop (`:225-261`) both `eBase` and `eBr` are already
in hand and the branch record silently wins — the exact seam. The limitation is
pinned in `TestLayerDiffAttributeChange` (`mermaid_layers_test.go:88-106`), which
this phase flips from documenting the gap to asserting the fix.

### 2b. Mermaid diff: a third class, `changed`

- **Nodes**: a kept node whose `tier` differs base→branch gets `:::changed` and a
  label suffix `tier 2→1`. New classDef alongside `diffClassDefs` (`:471`), a fourth
  legend entry in `writeLegend` (`:462`), reserved id in `reserveLegendIDs` (`:452`).
- **Edges**: attribute comparison must respect Phase 0's record identity — compare
  the **set of attribute tuples** `(tier, boundary, concurrent, via)` per surviving
  `(from,to)` pair, base vs branch (a pair that had {plain, concurrent} records and
  now has only {plain} lost its concurrent mode; single-record comparison would call
  that unchanged). A differing set renders the kept-shape arrow with a `Δ` label
  segment naming each changed attribute `old→new` (unset shown as `∅`), colored via
  a `changed` entry in `writeLinkStyle` (`:263`).
- **Keep-set**: changed elements join the force-keep set the way diff-touched
  endpoints already do (the `keepNode`/pinned-endpoint idiom) — an attribute change
  must never be collapsed away by tier-3 folding or `--max-nodes` caps; if the
  overview truncates, `diffOverview` (`:332`) reports the changed count so
  truncation is disclosed, never silent.
- **Boundary label case-changes** (`boundary:db POSTGRES` → `postgres`) fall out of
  edge identity naturally: the `to` string changed, so today they already render as
  removed+added. Fine — but the JSON delta (2c) additionally reports them, and the
  release-notes discipline (2d) covers the vocabulary change itself.

### 2c. JSON delta mode: `flowmap graph --diff BASE` without `--mermaid`/`--rollup`

Today `--diff` is only consumed by the two render paths; bare `--diff` with JSON
output is unused surface. Emit a canonical-JSON `GraphDelta` via `canonjson.Marshal`
(the `emitCanonJSON` path, `cmd/flowmap/main.go:293`):

```json
{
  "base": {"tool": "...", "algo": "rta"}, "branch": {"tool": "...", "algo": "rta"},
  "nodes_added": [], "nodes_removed": [],
  "nodes_changed": [{"fqn": "...", "field": "tier", "old": 2, "new": 1}],
  "edges_added": [], "edges_removed": [],
  "edges_changed": [{"from": "...", "to": "...", "field": "concurrent", "old": true, "new": null}],
  "caveats": ["algo mismatch: base=rta branch=vta"]
}
```

Sorted on intrinsic keys (fqn; from,to,field), one record per changed field, the
provenance-skew caveats reused from `provenanceCaveats` (`mermaid_diff.go:277`).
This is exactly the artifact the consumer currently hand-builds with jq, and it makes
2d mechanically checkable. Core lives in `internal/static/graphio` (e.g. `delta.go`,
`Delta(base, branch *Graph) GraphDelta`) so the mermaid path and the JSON path share
one comparison — one source of truth for "what changed", two renderings. The mermaid
`changed` classification is *derived from* the same `Delta` result, guarded by a
parity test.

**Non-goal, disclosed:** groundwork `review`'s internal `graphDelta`
(`internal/groundwork/review/delta.go:17`) stays set-based — it feeds a differently
scoped artifact; a comment cross-references the two so they don't look like an
accidental fork.

### 2d. Rollup diff

`PackageRollupDiff` identity is `(From, To, Kind)` (`rollup.go:528`) and tier/
concurrent don't exist at package grain, so there is no third class to add there.
The one attribute-shaped hole: a kind-preserving Note change is deliberately not a
delta (`rollup.go:414`) — keep that, add nothing. The FR's `--rollup` ask is
satisfied by documenting (in the delta docs) that attribute drift is a
function-grain concept surfaced by `--mermaid --diff` and the JSON delta.

### 2e. Release-notes discipline

Process, not code: a short section in `CONTRIBUTING.md` next to the existing
"Schema versions" convention — when a change alters output *semantics* (tier
attribution, concurrency carry, boundary-label vocabulary, blind-spot kinds) rather
than element sets, the PR description must say so, and the recommended evidence is a
`flowmap graph --diff old-golden.json` JSON delta over a committed golden. The
goldens-manifest ratchet (`testdata/groundwork/regen.sh` + `TestGoldenSectionManifest`)
already makes attribute changes *visible* in review diffs; the CONTRIBUTING paragraph
makes naming them mandatory rather than left to reviewer alertness.

### 2f. Tests

- Flip `TestLayerDiffAttributeChange` to assert `:::changed` + `Δ tier 2→1`.
- New golden: an attribute-drift variant pair under `internal/static/graphio/testdata`
  (tier change, concurrent flip, multiplicity change on one pair) rendered via
  `--diff`, plus a `GraphDelta` golden of the same pair; determinism repeat-render
  checks alongside `TestMermaidDiffDeterministic` (`mermaid_diff_test.go:116`).
- Mermaid/JSON parity test: every pair the delta calls changed renders `:::changed`
  and vice versa.
- CLI: `cmd/flowmap` smoke for bare `--diff` JSON mode + mutual-exclusion unchanged
  (`--root` remains exclusive with `--diff`, `main.go:201-203`).

---

## Phase 3 (FR-4): `flowmap graph --mermaid --focus <name,name,…>`

The generic core only — induced subgraph + name resolution + tier labels. The
consumer's manifest/aliases/overlay layer explicitly stays consumer-side.

**Mechanism:** reuse the render-time sub-Graph pattern of `MermaidRootedAt`
(`mermaid_rooted.go:25`) — build the unscoped graph, then construct a sub-`*Graph`
and let the existing renderer do the rest, keeping Frontier/blind-spot filtering
with dropped-count disclosure notes exactly as `rootedSubgraph` (`:40`) does. The
differences from `--root`:

- **Node set** = the resolved focus list, verbatim — no reachability walk.
- **Edge set** = induced: every edge whose `from` AND `to` are both in the set
  (boundary endpoints claimable via the same endpoint universe as Phase 1).
- **Resolution** = `internal/fqnres.Resolve` per name, **fail-closed**: any
  UNRESOLVED or AMBIGUOUS name is an operational error listing sorted candidates —
  never a partial render (a silently dropped focus node is a lie about the induced
  subgraph; same reason `resolveRoot` fails closed on ambiguity).
- **Focus nodes join the force-keep set** (`opts` pin idiom, `mermaid.go:65-69`): a
  focused node with no induced edges still renders — its isolation IS the finding
  (the FR's `newReadinessCheck$1` example: the graph records no edge to the closure,
  and drawing that silence is the point).
- Flag exclusivity: `--focus` mutually exclusive with `--root`, `--entry`, `--diff`,
  and `--rollup` in v1 (each pairing has a coherent meaning someday; none is asked
  for; fail closed on combination rather than guess).

Tests: golden over loansvc (a curated 6-node focus incl. one boundary endpoint and
one isolated node), ambiguity/unresolved CLI errors (`Score` → 3 candidates),
determinism repeat-render, resolver-parity with `assert` (same name, same
resolution — pinning Part 3's "one resolver" requirement).

---

## Decisions taken (so review argues with these, not the code)

1. **Resolver semantics ship prototype-exact, tightenings deferred.** The 138-claim
   field validation and byte-pinned acceptance output are worth more than a marginally
   stricter boundary rule; over-matching fails closed (AMBIGUOUS) anyway.
2. **`fqnres` is a new leaf package**, not an extension of `impact`'s frame resolver
   or `graph.Index` — two genuinely different input grammars (doc labels vs runtime
   frames), and flowmap must import it without touching groundwork packages.
3. **Exit 2 for claim errors** rides the existing operational-error meaning rather
   than adding a third error type; FAIL keeps `verdictError` precedence.
4. **`counterpart_matching` canonical, `to_matching` accepted as alias** — spec's own
   recommendation without breaking the consumer's existing suite.
5. **One `Delta` computation feeds both diff renderings** (mermaid + JSON), guarded
   by a parity test; groundwork review's set-based delta stays separate and
   cross-referenced.
6. **Rollup diff gains no changed-class** — nothing attribute-shaped exists at that
   grain; documented instead.
7. **FR-4 renders exactly the induced set** with fail-closed resolution and pinned
   isolated nodes; overlays/manifests remain out of scope.

## Risks / open questions

- **Regex dialect skew**: the prototype's regexes ran under a different engine; Go's
  RE2 has no backreferences/lookahead. The spec's published claims use none. Document
  RE2 explicitly; a claim using unsupported syntax errors that claim (fail closed).
- **`graph.Load` strictness vs old graphs**: `assert` on a graph from a *newer*
  flowmap fails the strict decode — correct (fail closed on schema skew) but worth a
  usage-docs sentence, since claims files outlive pins by design.
- **Changed-class rendering under `--max-nodes` pressure**: the keep-set grows; the
  scale tests (`mermaid_scale_test.go`, `mermaid_pressure_test.go`) need a
  changed-heavy case to show caps still disclose rather than drop.
- **Consumer contribution**: the FR offers the assert implementation upstream. This
  plan is shaped so Phase 1's package boundaries (`fqnres`, `claims`, `cmdAssert`)
  are agreeable before any code lands, per that offer.

## Acceptance summary (whole plan)

- `groundwork assert` ships with the seven-claim loansvc fixture reproducing spec
  §1.6 byte-for-byte (all four outcome classes), exit codes 0/1/2 per the existing
  verdict convention, deterministic reports.
- `flowmap graph --diff` reports attribute changes in all three consumptions it has
  (mermaid third class, JSON delta, rollup documented no-op), with
  `TestLayerDiffAttributeChange` flipped from limitation-pin to behavior-pin.
- `usage.md` documents edge-record identity + counting conventions; CONTRIBUTING
  gains the semantic-change release-noting rule.
- `--focus` renders induced subgraphs with the same resolver as `assert`, failing
  closed on ambiguity.
- `make verify` green at the end of every phase; every new ordering path lands with
  a determinism test.
