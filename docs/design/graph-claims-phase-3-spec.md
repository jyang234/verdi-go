# Phase 3 spec — `--focus` induced subgraph (FR-4) + FR-1 conformance closure

> **`READY TO IMPLEMENT`** · authored 2026-07-04 · supersedes the sketch in
> `graph-claims-drift-phase-3-handoff.md` (kept for its gotchas section, which
> remains binding). Companion to `docs/design/graph-claims-drift-plan.md`.

Phase 3 was planned as FR-4 (`--focus`) alone. A conformance audit of Phases 0–2
against the feature-request report and its companion behavioral spec
(`verdi-go-fr-prototype-spec-2026-07-03`) found FR-2/FR-3 fully satisfied but a
**claims-file interop gap in FR-1** that blocks the requesting consumer from
running their existing 138-claim suite at all. Phase 3 therefore has two
workstreams, independently landable, in this order:

- **3a — FR-1 conformance closure**: accept the companion spec's claim schema
  (`id`, `fn`) and report-line shape, and pin the spec's §1.6 acceptance case
  byte-for-byte. Small, highest urgency: until it lands, `groundwork assert`
  rejects every claims file the FR's own examples describe.
- **3b — FR-4 `flowmap graph --mermaid --focus`**: the induced-subgraph render,
  as planned.

---

## Part 1 — Phases 0–2 conformance audit (what the FR asked vs what shipped)

Verified against `main` at `b91cb4e` by reading the code and running the shipped
binaries against `testdata/fixtures/loansvc`.

### Phase 0 (FR-3) — **satisfied**

- `docs/groundwork/usage.md` §"Consuming graph.json directly" documents record
  identity as the **full six-field attribute tuple** (deliberately stronger than
  the FR's suggested `(from, to, mode)` — documents what is true), the
  sync+concurrent example, and the raw-records-vs-unique-pairs counting rule.
- Cross-referenced from `docs/specs/static-extractor-spec.md` §8 (line 150).
- The FR's "note on which convention the tool's own summaries use" is covered
  where it matters: `assert`'s summary line self-describes (`N unique edges`)
  and the delta docs state the unique-pair basis. No residual.

### Phase 1 (FR-1) — **semantics satisfied and hardened; schema interop NOT satisfied**

What shipped matches or exceeds the FR on semantics:

- All seven claim kinds; resolution per companion spec §1.3 (plain =
  normalized suffix, unique-or-die; `/regex/` = raw-FQN RE2, multi-match legal);
  unique-pair counting; exit codes 0/1/2 riding the existing
  verdict/operational split; deterministic reports; endpoint universe includes
  boundary pseudo-nodes; the loansvc pin reproduces (40 nodes, 49 unique edges,
  3 `Score` candidates).
- Adopted spec-recommended tightenings: **per-side** plain/regex enforcement
  (§1.3's "quirk to tighten natively"), `counterpart_matching` canonical with
  `to_matching` as alias.
- Went beyond the spec, in the fail-closed direction only (each converts a
  potential vacuous pass into an ERROR — none can flip a FAIL/PASS):
  1. a counterpart filter matching **nothing in the whole endpoint universe**
     ERRORs (prototype: silently counts 0, so a typo'd filter passes `eq: 0`);
  2. a degree anchor (`of`) must resolve to exactly one endpoint even as a regex;
  3. a **known field on the wrong kind** ERRORs (`eq` on an `edge` claim);
  4. `//` is not a regex (an empty pattern matches everything — fail closed to
     plain);
  5. a `tier` claim over an FQN the graph carries at more than one tier abstains.

**The gap — the shipped schema rejects spec-conformant claims files.** Verified
by running the shipped `assert`:

```
$ groundwork assert loansvc-rta.json spec-style.claims.json
groundwork: …: decode claims: json: unknown field "id"   (exit 2)
```

Three concrete divergences from the companion spec:

| # | spec (and the FR's own examples) | shipped | consequence |
|---|---|---|---|
| 1 | every claim carries `id`, "echoed in every report line" (§1.2) | no `id` field; strict decode **rejects** it | the consumer's 138-claim suite — and every example in the FR — fails to load. Decision 4 of the plan ("so the consumer's existing suite runs unmodified") is defeated |
| 2 | `fn` names the anchor of `node`/`no_node`/`in_degree`/`out_degree` (§1.4) | `fqn` (node kinds) and `of` (degrees), no alias | same: strict decode rejects `fn` |
| 3 | report lines `FAIL  <id> [<kind>] <detail>` / `ERROR <id> [<kind>] <detail>`, details per §1.6's verbatim expected output | `FAIL <kind> <label>: <detail>` with different detail wording | spec §1.6's field-validated expected output is not reproducible; the plan's Phase 1d promise ("commit spec §1.6's seven-claim file… byte-equal") was downgraded to an equivalent-but-different in-repo fixture |

The Phase 1 commit justified #3 with "the companion spec is a consumer-side
artifact not in this repo" — but the spec's schema, §1.6 claims file, and
expected output were transmitted in full and are quoted in the plan's sources.
The divergence is fixable cheaply now (assert shipped days ago, one known
consumer, and that consumer explicitly offered the shape for agreement);
workstream 3a below closes it.

### Phase 2 (FR-2) — **satisfied**

- One comparison, three renderings: `graphio.Delta` (unique-pair basis; a pair's
  attribute state is the **set** of its distinct `(tier, boundary, concurrent,
  via)` tuples, so multiplicity changes register), consumed by the bare-`--diff`
  JSON `GraphDelta`, the mermaid `changed` class (amber, `Δ old→new`,
  force-kept, over-cap disclosed), and the rollup (documented no-op — nothing
  attribute-shaped exists at package grain).
- `TestLayerDiffAttributeChange` flipped from limitation-pin to behavior-pin;
  mermaid/JSON parity test; determinism-under-reorder (the review pass fixed a
  real last-write-wins tie-break); `--diff`+`--entry` now refused (fail closed).
- Release-noting discipline landed in `CONTRIBUTING.md` §"Semantic
  (output-meaning) changes", with the JSON delta as the recommended evidence.
- Boundary-label case changes surface as removed+added pairs plus delta records,
  per the plan. No residual.

---

## Part 2 — Workstream 3a: FR-1 conformance closure

Goal: a claims file written against the companion spec — including the
consumer's live 138-claim suite — loads and evaluates unmodified, and the spec's
§1.6 acceptance case reproduces **byte-for-byte**.

### 3a.1 Schema additions (`internal/groundwork/claims`)

- `Claim` gains `ID string \`json:"id,omitempty"\``. Free-form, echoed in report
  lines (below). Uniqueness per file is recommended in docs, not enforced (the
  spec doesn't require it, and an ERROR on duplicates would break suites that
  validated under the prototype).
- `Claim` gains `Fn string \`json:"fn,omitempty"\`` as a **documented alias**:
  for `node`/`no_node` it aliases `fqn`; for `in_degree`/`out_degree` it aliases
  `of`. Same treatment as the `to_matching` alias: canonical spelling preferred,
  alias accepted, **both present on one claim → that claim ERRORs**. Add `fn` to
  `allowedFields` for those four kinds only (an `fn` on an `edge` claim stays a
  wrong-kind ERROR). Route the precedence through one accessor (the
  `counterpartQuery` idiom) so the evaluator and `label()` cannot drift.

### 3a.2 Report-line shape (spec §1.5, byte-compatible with §1.6)

```
FAIL  <label> [<kind>] <detail>
ERROR <label> [<kind>] <detail>
assert: N passed, N failed, N errored (graph: N nodes, N unique edges)
```

- `FAIL` is followed by **two** spaces (column-aligns with `ERROR `, as in the
  spec's verbatim output). Summary line is already byte-identical — keep it.
- `<label>` = the claim's `id` when present, else the current endpoint-derived
  label (an id-less claim must still be identifiable). Order unchanged: FAIL
  lines then ERROR lines, claims-file order within each.
- Detail strings align with §1.6's verbatim output where the spec pins them:
  - resolution errors: `UNRESOLVED: '<query>' matches no node/endpoint` (node
    universe: `matches no node`) and
    `AMBIGUOUS: '<query>' matches <N>: <c1>; <c2>; …` — single-quoted query,
    candidates **semicolon**-separated, sorted, capped at 4 with ` (+N more)`;
  - `edge` FAIL: `0 edge(s)`;
  - kinds the spec's output doesn't pin (`no_edge` offenders, counts, tiers,
    `no_node` matches) keep their current wording — but adopt the semicolon
    separator for all in-detail lists so the format has one list convention.
- This is a breaking change to `assert`'s output. Acceptable now: the command is
  days old, the one consumer wrote the target format, and output byte-stability
  is exactly what their `--check`-style CI wants — better to break once into the
  agreed shape than freeze the accidental one. Update the usage-docs example in
  the same commit (comments-and-docs-honest rule).

### 3a.3 The §1.6 acceptance fixture, verbatim

- Commit the companion spec's seven-claim file **unmodified** (ids `L1`–`L7`,
  `fn` fields, `to_matching` on L3) as
  `testdata/groundwork/claims/loansvc-spec-acceptance.claims.json`.
- `TestAssertSpecAcceptance` in `cmd/groundwork` runs it against the pinned
  loansvc golden and asserts the report equals the spec's expected output
  **byte-for-byte**, and the error is a `verdictError` (exit class 1):

```
FAIL  L5-deliberate-fail [edge] 0 edge(s)
ERROR L6-ambiguous-name [node] AMBIGUOUS: 'Score' matches 3: (*example.com/loansvc/internal/client.Bureau).Score; (*example.com/loansvc/internal/scoring.Remote).Score; (*example.com/loansvc/internal/scoring.Stub).Score
ERROR L7-unresolved-name [edge] UNRESOLVED: 'handler.App).Delete' matches no node/endpoint
assert: 4 passed, 1 failed, 2 errored (graph: 40 nodes, 49 unique edges)
```

- Keep the existing seven-claim fixture and `TestAssertLoansvcAcceptance`
  (updated to the new line shape) — it exercises cases the spec file doesn't
  (`no_node`, a boundary-endpoint edge claim).

### 3a.4 Docs + consumer disclosure

- usage.md "Claims files": document `id`, the `fn` alias (and both-present
  ERROR), and the report format; refresh the example output.
- Add a short "Deviations from the validated prototype" note (usage.md, same
  section) listing the five intentional fail-closed tightenings from Part 1 —
  each can convert a prototype-era pass into an ERROR, and the consumer should
  hear that from the docs, not from CI. This is the disclosure channel for the
  138-claim suite: any claim newly ERRORing under per-side enforcement or the
  counterpart-filter rule is a latent vacuous pass their prototype was hiding.

### 3a.5 Tests

- `TestAssertSpecAcceptance` (byte-pinned, above).
- claims unit tests: `id` echoed / absent-id fallback label; `fn` alias on all
  four kinds; `fn`+`fqn` and `fn`+`of` both-present ERRORs; `fn` on an `edge`
  claim ERRORs (wrong-kind); report-shape golden updated; determinism rerun.
- Existing suites updated mechanically for the line shape; no semantic changes.

**Acceptance 3a:** the spec-style file from Part 1's repro loads and runs; §1.6
reproduces byte-for-byte; `make verify` green.

---

## Part 3 — Workstream 3b: `flowmap graph --mermaid --focus`

The generic core of FR-4 only: induced subgraph + shared name resolution + tier
labels. Manifests, aliases, and judgment overlays stay consumer-side (the FR's
own scoping). A view, never a gate. Substrate references verified at `b91cb4e`:
`MermaidRootedAt`/`rootedSubgraph` (`mermaid_rooted.go:25,40`), `pinRoot`
(`mermaid.go:65`, consumed at `:294–:321`, `keepNode` `:569`), `cmdGraph`
exclusivity guards (`cmd/flowmap/main.go:175,213–221,289–298`), `splitList`
(`main.go:1241`), endpoint-universe construction (`claims.go` `newModel`).

### 3b.1 CLI contract

```
flowmap graph --mermaid --focus <name[,name…]> [--focus …] <dir>
```

- `--focus` is **repeatable**; occurrences accumulate (a `flag.Func` collecting
  into a slice). Within one occurrence the value is comma-split via `splitList`,
  **except** when the entire value is a single well-formed `/regex/` (leading
  and trailing `/`, length ≥ 3 — `fqnres.isRegex`'s rule), which is taken as one
  name. This is how a regex containing a comma (`/a{1,2}/`, alternations over
  FQN lists) is passed: as the sole value of its own `--focus` occurrence.
- **Fail closed on split damage:** after splitting, a fragment that starts with
  `/` xor ends with `/` is an operational error — `--focus: <fragment> looks
  like part of a regex split on ','; pass a comma-bearing regex as its own
  --focus flag`. A comma inside a regex must never silently become two
  wrong plain names.
- Empty focus list after splitting (e.g. `--focus ""`) is a usage error.
- **`--focus` requires `--mermaid`** (error otherwise — never silently ignored)
  and is mutually exclusive with `--root`, `--entry`, `--diff`, and `--rollup`,
  each pairing refused with a reason-bearing error modeled on the existing
  guards. `--entry` is load-bearing: an entry-scoped build drops the Frontier
  section, and the focus view discloses pruned frontier markers, so it must run
  over the unscoped graph (the same reason `--root` refuses `--entry`).
  Reclaimer flags (`--reclaim*`, `--rebind`) compose — they mutate the graph
  before any view.

### 3b.2 Resolution — `internal/fqnres`, fail-closed

- Build the **endpoint universe** exactly as `claims.newModel` does: sorted,
  deduped `node FQNs ∪ every edge from/to string` (boundary pseudo-nodes are
  focusable). The construction is three lines; a comment in each package names
  the parity with the other, and the resolver-parity test (3b.5) guards the
  shared behavior — the resolution itself is only ever `fqnres.Resolve`.
- Per name: plain → unique-or-die (`0` → UNRESOLVED, `Ambiguous` → AMBIGUOUS;
  both are **operational errors** naming the query and, for AMBIGUOUS, the
  sorted candidates capped at 4 with ` (+N more)` — the `assert` convention).
  `/regex/` → all matches join; a regex matching **zero** endpoints is an error
  too (a focus name that selects nothing is a typo, and rendering without it
  would lie about the induced set); compile errors are errors.
- Focus set = union of every name's matches. **Never a partial render** — any
  bad name aborts before rendering (tenet 2; a silently dropped focus node is a
  lie about the induced subgraph).

### 3b.3 The induced sub-graph (`internal/static/graphio/mermaid_focus.go`)

```go
// MermaidFocus renders the induced subgraph over the resolved focus names —
// exactly those nodes and every edge with BOTH endpoints in the set. g must be
// UNSCOPED (Build with entry == ""), so the Frontier/blind-spot disclosure
// channels are present. Fail-closed: an UNRESOLVED or AMBIGUOUS name (or a
// regex matching nothing) is an error carrying the sorted candidates — never a
// partial render.
func (g *Graph) MermaidFocus(names []string, opts MermaidOptions) (string, error)
```

(Error return, not `MermaidRootedAt`'s `(string, bool)` — the failure has
content the CLI must print.)

- **Nodes** = `g.Nodes` filtered to the focus set, canonical order preserved.
  No reachability walk.
- **Edges** = every record with `from ∈ focus AND to ∈ focus`. All records of a
  kept pair are kept (the base renderer draws each record; a sync+concurrent
  pair renders both arrows — the multiplicity is information, Phase 0).
- **Disclosure filtering shares `rootedSubgraph`'s logic.** Refactor: extract
  the blind-spot / frontier-marker / annotation filtering plus dropped-count
  notes from `rootedSubgraph` (`mermaid_rooted.go:67–127`) into one helper
  taking the keep-set and a context word ("this handler's reach" / "the focus
  set"), and call it from both. One source of truth beats a mirrored copy; the
  existing `--root` goldens pin that the refactor is behavior-preserving.
- **Boundary focus names with no induced edge are disclosed, not dropped.** A
  boundary endpoint exists only on edges; if it joins the focus set but no
  focused caller reaches it, nothing would render — a silent hole in the drawn
  set. Emit a note: `N focus name(s) resolve only to boundary endpoints with no
  induced edge — not drawn: <sorted list>`.
- **Every focus-set FQN joins the force-keep set.** Generalize the pin:
  replace `MermaidOptions.pinRoot string` with `pinNodes map[string]bool`
  internally (`MermaidRootedAt` sets a one-element set — behavior identical,
  pinned by existing goldens; the `force` map at `mermaid.go:294` takes the set
  directly). An isolated focus node renders as a lone box — its isolation IS
  the finding (the FR's `newReadinessCheck$1` example: drawing the graph's
  silence is the point).
- **Scope label:** `sub.Entrypoint = "focus: " + strings.Join(names, ", ")`
  (the raw CLI names, input order — deterministic because it is input). The
  header path (`writeFlowchartHeader`) renders it like `--root`'s label.
- `--max-nodes` keeps its existing meaning; pinned nodes ride the existing
  keep idiom. (A focus list is hand-curated and small; no new cap logic.)

*Amended by the round-2 review (2026-07-04):* three refinements to what the
draft above specified, without rewriting it. (1) The boundary-note wording is
**endpoint-based** — it counts the undrawn boundary *endpoints* in the focus set,
not "focus names" (a single regex can resolve to several, and a name may also
draw first-party nodes). (2) A focus name that resolves **only to a dangling edge
endpoint** — an edge `from`/`to` with no node record, so the base renderer draws
no box and silently drops the edges it induces — is a **fail-closed refusal**
(named, capped through the shared `; ` list), not a partial render; it joins the
enumerated abort causes. (3) A focus node shown only because the pin **rescued it
from tier-collapse** is disclosed in a header note (`… pinned node(s) above tier
N (plumbing) …`, parameterized on `MaxTier` since tier 4 exists), the same
honesty channel the pruned-disclosure notes use. Separately, the `--focus`
query-list grammar (comma-split, whole-value `/regex/` exemption, and the
fail-closed ambiguity/split-damage refusals) is owned by
`fqnres.SplitQueries` — beside the resolver's query forms, so the CLI keeps no
drifting copy of the `/`-boundary rules.

### 3b.4 CLI wiring

Inside `cmdGraph`'s `if *asMermaid` block, alongside the `--root` branch:
resolve exclusivity first (before the build where possible — `--focus`+`--entry`
can be refused pre-`Analyze`, like `--diff`+`--entry` at `main.go:175`), then
`g.MermaidFocus(names, opts)`, wrapping in `render.Fence`. Errors from
`MermaidFocus` surface verbatim (they carry the candidate lists).

### 3b.5 Tests

1. **Golden over loansvc** (`assertGolden` + `assertValidMermaid` idiom): a
   curated focus of ~6 names including one boundary endpoint reached by a
   focused caller, one **isolated** node, and one `/regex/` name; assert the
   induced edges, the lone box, the tier styling, and the disclosure notes.
2. **Fail-closed:** `Score` (AMBIGUOUS, 3 sorted candidates in the error),
   `handler.App).Delete` (UNRESOLVED), a zero-match regex, a compile-error
   regex — each errors, no output.
3. **Split damage:** `--focus '/a{1,2}/'` alone resolves as one regex;
   `--focus 'x,/a{1,2}/'` errors with the unbalanced-fragment message.
4. **Exclusivity + requires-mermaid:** `--focus` with each of `--root`,
   `--entry`, `--diff`, `--rollup`, and without `--mermaid` — all refused
   (extend `cmd/flowmap/main_test.go`).
5. **Determinism:** repeat-render byte-identical; invariance under input
   node/edge reordering (`delta_test.go`'s `reversed()` pattern).
6. **Resolver parity with `assert`:** the same name resolves to the same FQN
   set through `--focus` and through a claims evaluation over the same graph —
   pinning Part 3 of the companion spec ("one resolver, both features"),
   including the `(*T).Method`-style case that resolves in `fqnres` but not in
   `impact.ResolveFrame`.
7. **Refactor safety:** existing `--root` goldens unchanged after the
   disclosure-filter extraction and the `pinRoot` → `pinNodes` generalization.

### 3b.6 Docs

usage.md gains "Focused subgraph — `flowmap graph --mermaid --focus`" next to
the rollup/diff sections: the induced-set rule (nodes named, edges only among
them — absence of an edge in the render is a graph fact, not an omission), the
shared resolver (link the Claims-files section for suffix-vs-regex), the
repeatable-flag/comma-regex rule, fail-closed behavior, the boundary-no-edge
disclosure note, and the exclusivity list. State plainly that the manifest /
overlay / `--check` layer from the FR stays consumer-side, and that a consumer
`--check` workflow is byte-safe because the render is deterministic.

---

## Decisions taken (argue with these, not the code)

1. **3a lands before 3b.** The interop gap blocks the requesting consumer
   today; `--focus` blocks no one.
2. **Adopt the spec's report shape rather than document divergence.** One
   consumer, days-old command, format author = consumer, byte-stability is the
   consumer's stated CI need. Breaking once now beats freezing an accidental
   format. The five fail-closed semantic tightenings are NOT reverted — they
   are disclosed (3a.4); fail-closed strictness is this repo's contract and
   only converts vacuous passes into ERRORs.
3. **`fn` is an alias, not the canonical name.** `fqn`/`of` are the clearer
   spellings; the alias exists for spec conformance, mirroring `to_matching`.
4. **Regex-matching-nothing is an error for `--focus`** even though the same
   outcome in an `edge_count` claim is a legal 0: a focus name exists to select
   drawable content, and selecting nothing is indistinguishable from a typo —
   fail closed (same polarity as the counterpart-filter rule).
5. **Repeatable `--focus` + whole-value-regex exemption** is the comma/regex
   escape hatch; fragments that look like split regexes are refused loudly.
6. **Refactor `rootedSubgraph`'s disclosure filtering into a shared helper**
   rather than mirroring it — one source of truth, guarded by the untouched
   `--root` goldens.

## Acceptance (whole phase)

- A companion-spec-conformant claims file (with `id`/`fn`) evaluates
  unmodified; spec §1.6 reproduces byte-for-byte from a committed fixture.
- `flowmap graph --mermaid --focus` renders exactly the induced subgraph with
  `assert`'s resolver, failing closed (sorted candidate lists) on any
  unresolved/ambiguous/zero-match name; isolated focus nodes render; pruned
  disclosures and undrawn boundary names are noted, never silent.
- Exclusivity: `--focus` refuses `--root`/`--entry`/`--diff`/`--rollup` and
  requires `--mermaid`.
- Determinism tests ship in the same change as every new ordering path;
  `make verify` green at the end of each workstream; `/code-review` (max) run
  before each lands (Phase 1 and 2 reviews both caught fail-open/determinism
  bugs the green build did not).
