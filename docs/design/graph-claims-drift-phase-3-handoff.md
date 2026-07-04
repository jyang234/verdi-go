# Phase 3 (FR-4) handoff — `flowmap graph --mermaid --focus <fqn,…>`

> **`READY TO IMPLEMENT`** · authored 2026-07-04 for a fresh session · Phases 0–2 landed.

This is a self-contained brief for implementing **Phase 3** of
`docs/design/graph-claims-drift-plan.md` (read that plan's §"Phase 3 (FR-4)" and
decision 7 alongside this). It assumes none of the implementing session's prior
context. Everything below was re-verified against the tree at the tip of
`claude/graph-claims-drift-phase-2-i8t35v` (Phase 2 + its code-review fixes).

## Where the work stands

| Phase | FR | What it added | State |
|---|---|---|---|
| 0 | FR-3 | edge-record multiplicity docs | merged (`0673be9`) |
| 1 | FR-1 | `internal/fqnres` resolver + `internal/groundwork/claims` + `groundwork assert` | merged |
| 2 | FR-2 | `internal/static/graphio.Delta` + attribute-aware `--diff` (JSON + mermaid `changed` class) | on this branch |
| **3** | **FR-4** | **`flowmap graph --mermaid --focus <names>` induced-subgraph render** | **this handoff** |

Phase 3 is **independently landable** and the last phase. It has no dependency on
Phase 2's code, but it **reuses Phase 1's resolver** (`internal/fqnres`) — that
reuse is the point of the plan's "one resolver, both features" requirement.

## What Phase 3 delivers (scope)

`flowmap graph --mermaid --focus a.B,c.D,…` renders the **induced subgraph** over
exactly the named functions: their nodes plus only the edges whose *both*
endpoints are named. It is a **view, never a gate**.

**In scope — the generic core only:**
- name resolution (via `fqnres.Resolve`, the same resolver `groundwork assert` uses),
- the induced node/edge set,
- tier labels + Frontier/blind-spot filtering with dropped-count disclosure,
- fail-closed resolution and pinned isolated nodes.

**Out of scope (explicitly consumer-side, per the FR):** any manifest / alias /
overlay layer that maps friendly labels to FQNs. `--focus` takes FQN-resolving
names (plain suffix or `/regex/`) and nothing else.

## The mechanism — reuse the `--root` render-time subgraph pattern

The `--root` path already builds a **sub-`*Graph` at render time and hands it to
the existing renderer**. `--focus` is the same shape with a different node/edge
selection. Read these first — they are the template:

- `internal/static/graphio/mermaid_rooted.go`
  - `MermaidRootedAt(root, opts)` (`:25`) — resolves, builds sub-graph, pins, renders.
  - `rootedSubgraph(root)` (`:40`) — **the pattern to mirror**: constructs a
    `sub := &Graph{Entrypoint, Algo}`, copies in the selected `Nodes`/`Edges`,
    then **filters `BlindSpots`, `Frontier.Markers`, and `Annotations` to the
    selected set and returns dropped-count disclosure `notes`** (`:72`, `:86`,
    `:109`). Reproduce this filtering verbatim so the focus view is as honest
    about pruned disclosures as the rooted view.
  - `resolveRoot` (`:141`) — the fail-closed resolution `--root` uses (returns
    `ok=false` on no-match / ambiguity). `--focus` fails closed the same way but
    resolves through `fqnres` instead (see below).
- `internal/static/graphio/mermaid.go`
  - `MermaidOptions.pinRoot` (`:65`) and its use in `mermaid()` (`:295`, `:318`)
    — the **force-keep-against-tier-collapse** idiom. `--focus` needs to pin a
    **set** of FQNs, not one; generalize `pinRoot string` to also accept a set
    (e.g. add `pinNodes map[string]bool`, or repurpose the existing `force` map
    the renderer already threads at `:295`). Every focus node joins it.

### The differences from `--root`

1. **Node set = the resolved focus list, verbatim.** No `forwardReach` walk
   (`mermaid_rooted.go:252`); the named functions *are* the node set.
2. **Edge set = induced.** Keep an edge iff **`from` AND `to` are both in the
   focus set.** (Contrast the rooted rule, "keep when `from` is reachable",
   `mermaid_rooted.go:62`.) A **boundary** target (`isBoundary(e.To)`, i.e.
   `boundary:` prefix) is claimable when it is itself named in the focus set —
   see the endpoint universe below.
3. **Resolution = `internal/fqnres.Resolve` per name, fail-closed.**
   - `fqnres.Resolve(query string, universe []string) (Result, error)` returns
     `Result{Matches []string /*sorted*/, Ambiguous bool, IsRegex bool}`.
   - Build the **endpoint universe** exactly as Phase 1 does
     (`internal/groundwork/claims/claims.go:21` + `:210`): **sorted, deduped
     `node FQNs ∪ every edge from/to string`** — boundary pseudo-nodes
     (`boundary:db QueryContext`) occur only as edge endpoints and must be
     focusable.
   - For each name: a plain query is **unique-or-die** — `len(Matches)==0` →
     UNRESOLVED, `Ambiguous` (≥2) → AMBIGUOUS; both are an **operational error**
     listing the sorted candidates. A `/regex/` query may match many (all join
     the focus set); a regex compile error is an error. `Resolve`'s own `err`
     covers regex-compile failures. This is byte-for-byte the semantics
     `groundwork assert` resolves claim endpoints with (`claims.go`) — do not
     re-implement resolution.
   - The focus set = the **union of every name's `Matches`**.
   - **Never render a partial focus** — a silently dropped focus node is a lie
     about the induced subgraph, the same reason `resolveRoot` fails closed on
     ambiguity (CLAUDE.md: fail closed; tenet 2).
4. **Focus nodes join the force-keep set** (the pin idiom above): a focused node
   with **no induced edges still renders** as a lone box — its isolation *is* the
   finding (the FR's `newReadinessCheck$1` example: the graph records no edge to a
   closure, and drawing that silence is the point). The base renderer already
   emits a node declaration per shown node regardless of edges, so isolation
   shows naturally once the node is pinned against tier-collapse.

### Suggested surface

In `internal/static/graphio` (new file, e.g. `mermaid_focus.go`), mirroring
`MermaidRootedAt`:

```go
// MermaidFocus renders the induced subgraph over the resolved focus names. g must be
// UNSCOPED (Build with entry==""), so Frontier/blind-spot disclosures are present.
// Fail-closed: an UNRESOLVED or AMBIGUOUS name is an error naming sorted candidates —
// never a partial render.
func (g *Graph) MermaidFocus(names []string, opts MermaidOptions) (string, error)
```

Return an `error` (not the `(string, bool)` `MermaidRootedAt` uses) because the
failure has *content* — the offending name and its candidate list — that the CLI
must surface, unlike `--root`'s single opaque miss.

### The CLI wiring (`cmd/flowmap/main.go`, `cmdGraph`)

- Add `focus := fs.String("focus", "", "...")` alongside the other `graph` flags.
- Split on `,` into names (there is a `splitList` helper in `main.go`).
- Wire inside the `if *asMermaid { … }` block (the `--focus` view is mermaid-only),
  next to the `*rootAt` branch (`main.go:~212`), calling `g.MermaidFocus(names, opts)`
  and wrapping output in `render.Fence(...)` like the `--root` path.
- **Flag exclusivity — fail closed on every combination (v1):** `--focus` is
  mutually exclusive with `--root`, `--entry`, `--diff`, **and** `--rollup`. Each
  pairing has a coherent meaning *someday*; none is asked for; refuse rather than
  guess. Model the errors on the existing `--root`/`--diff` and `--root`/`--entry`
  mutual-exclusion errors already in `cmdGraph` (`main.go:214`, `:220`; and the new
  `--diff`/`--entry` one at `:176`) and the `--rollup`/`--root` one in
  `cmdGraphRollup` (`:290`). Note `--entry` is
  particularly load-bearing: an entry-scoped build drops the Frontier section, so
  `--focus` (which discloses pruned frontier markers) must run over the **unscoped**
  graph — the same reason `--root` refuses `--entry`.

## Tests to write

Follow the existing graphio test idioms (`internal/static/graphio/*_test.go`):

1. **Golden over loansvc** — a curated ~6-node focus including **one boundary
   endpoint** and **one isolated node** (no induced edges), rendered via
   `MermaidFocus`, asserted through `assertGolden` + `assertValidMermaid` (see
   `mermaid_golden_test.go`). Add the render to `regen.sh`'s
   `go test … -run 'TestCallGraphMermaidGoldens|…' -update` list if it needs
   rebasing there, or keep it a standalone `-update` golden.
2. **Fail-closed CLI errors** — an AMBIGUOUS plain name (the plan notes loansvc's
   `Score` resolves to **3 candidates**) and an UNRESOLVED name each return an
   error listing sorted candidates; assert via `run([]string{"graph","--mermaid","--focus","Score",…})`
   returning non-nil (see `cmd/flowmap/main_test.go` `TestRunGraph*`).
3. **Flag exclusivity** — `--focus` with each of `--root`/`--entry`/`--diff`/`--rollup`
   is rejected (extend the CLI test).
4. **Determinism** — repeat-render byte-identical, and invariance under input
   node/edge reordering (the `reversed()` helper pattern in `delta_test.go`
   `TestDeltaDeterministic`). See the determinism gotcha below.
5. **Resolver parity with `assert`** — the same name resolves to the same FQN set
   through `fqnres.Resolve` whether called from `--focus` or from
   `groundwork assert`; pin the Part-3 "one resolver" requirement with a shared
   case (a `(*T).Method` receiver name that resolves in `fqnres` but not in the
   triage `impact.ResolveFrame`).

## Gotchas learned in Phases 1–2 (read before coding)

- **Determinism is the whole point (CLAUDE.md prime directive).** Every ordering
  and tie-break must resolve on **intrinsic, run-independent data**, never map
  iteration or slice arrival order. Phase 2's headline review bug was exactly
  this: a per-`(from,to)`-pair render picked a record by *last-write-wins* over
  the edge slice, so reordering input flipped the output. `internal/fqnres`
  returns **sorted** `Matches`, and the endpoint universe is **sorted+deduped** —
  keep it that way, and **ship the determinism test in the same change** (a new
  ordering path with no determinism test is a CLAUDE.md violation). The base
  renderer (`mermaid()`) renders each edge record as its own arrow (no
  dedup/last-wins), so `--focus` should not hit the pair-collapse trap — but a
  BFS/adjacency built from a map still needs a sorted walk if you add one.
- **Fail closed, loudly.** UNRESOLVED/AMBIGUOUS → error, never a partial or
  best-guess render. The whole value of `--focus` is that the drawn set is
  *exactly* what was asked for.
- **One source of truth.** Do **not** copy resolution logic — call `fqnres.Resolve`.
  Do **not** re-derive the subgraph-filtering (blind spot / frontier / annotation
  prune) — factor or mirror `rootedSubgraph` so the two views cannot drift. If you
  must reuse a predicate in two places, name the parity in a comment **and** guard
  it with a test (Phase 2 added `TestAttrTupleMirrorsEdgeIdentity` for exactly this).
- **`fqnres` vs `impact.ResolveFrame` stay separate** (plan §1a): claims/focus
  names are hand-authored doc labels (strip `(`,`)`,`*`); runtime stack frames are
  machine-emitted (keep the punctuation). Do not unify them.
- **Comments are load-bearing.** When you change a function body, re-read its doc
  comment; if the change makes a checkable claim (`always`/`never`/`sorted`/
  `deterministic`/`exactly one`) false, fix it in the same edit. `make comment-drift`
  (advisory) flags moved-body-under-asserting-comment.
- **Green gate every change:** `make verify` = build + vet + `golangci-lint` +
  `go test -race ./...` + fixture (`loansvc`, `impeachsvc`) + `gofmt`. It must be
  green at the end. (`-race` over the whole module is slow — run it in the
  background.) Golden regen: `go test ./internal/static/graphio -run <T> -update`.
- **Run `/code-review` before finishing.** Phase 2's review (max effort) caught a
  determinism bug, a `--diff`+`--entry` fail-open, a shared-legend leak, and a
  latent one-source-of-truth gap that the initial green build did not. Budget for
  a review pass and its fixes.

## Acceptance (from the plan's summary)

- `flowmap graph --mermaid --focus a,b,…` renders the induced subgraph with the
  **same resolver as `assert`**, **failing closed** on ambiguity/no-match with a
  sorted candidate list.
- A focused node with no induced edges still renders (isolation is disclosed).
- `--focus` is mutually exclusive with `--root`/`--entry`/`--diff`/`--rollup`.
- Golden (loansvc, incl. a boundary endpoint + an isolated node), determinism
  test, and resolver-parity-with-`assert` test all present.
- `make verify` green.

## Suggested commit sequence

1. `internal/static/graphio/mermaid_focus.go` + unit/golden/determinism tests.
2. `cmd/flowmap` `--focus` flag + exclusivity guards + CLI tests.
3. Docs: a "Focused subgraph — `flowmap graph --mermaid --focus`" subsection in
   `docs/groundwork/usage.md` next to the rollup/diff sections (suffix-vs-regex
   convention → link the "Claims files" resolver section; the induced-set rule;
   the fail-closed behavior; the exclusivity list).
4. `/code-review max` and fix.

Develop on a fresh branch off the latest default branch (do not extend this
Phase-2 branch). Everything needed is in this repo — `internal/fqnres` (resolver),
`internal/static/graphio/mermaid_rooted.go` (the pattern), and the plan doc.
