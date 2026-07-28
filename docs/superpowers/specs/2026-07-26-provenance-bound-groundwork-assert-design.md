# Provenance-bound `groundwork assert`

> **Status:** approved design · authored 2026-07-26

## Summary

`groundwork assert` becomes a versioned, provenance-bound machine interface for
ephemeral point-in-time graph claims. Its existing human-readable mode and
exit behavior remain compatible.

The feature lands in two independently useful increments:

1. Canonical JSON output, graph provenance, stamp verification, machine-safe
   claim IDs, structured bindings, and reason codes.
2. Rich reachability, waypoint, concurrency, and obligation claims backed by
   the same typed fact evaluators as standing fitness policy.

The claims file remains ephemeral input. A PASS means the supplied predicate
matches this graph; it does not mean a human has approved the predicate as
architecture policy.

## Authority model

The two verdict surfaces answer different questions:

| Surface | Authority | Question |
|---|---|---|
| `groundwork fitness` | CODEOWNERS-gated standing policy | Does the graph obey approved architecture? |
| `groundwork assert` | Caller-supplied ephemeral claims | Does this graph match these point-in-time statements? |

The evaluator proves graph facts only. It does not judge whether a predicate
adequately represents a natural-language design decision.

No claims operation may mutate `policy.json`, `.flowmap.yaml`, or the graph.

## Goals

1. Provide a versioned JSON contract suitable for CI and orchestration.
2. Bind machine results to graph provenance and optional expected code
   identity.
3. Require stable, unambiguous claim IDs in machine mode.
4. Preserve PASS, FAIL, and ERROR without requiring prose parsing.
5. Expose resolved graph identities and deterministic witnesses.
6. Add a small closed vocabulary for transitive reach, all-path waypoint,
   concurrent reach, and existing obligation status.
7. Share traversal, selector binding, blindness, and status interpretation
   with fitness.
8. Preserve existing text output and exit-code precedence.

## Non-goals

- An LLM, heuristic judge, or natural-language evaluator.
- A general expression language or arbitrary claim DSL.
- Authoring new path obligations from a claims file.
- Treating ephemeral claims as standing architecture policy.
- Policy or graph mutation.
- Write-capable MCP tools.
- Multi-language abstraction.
- Base-to-branch stable delta signatures.
- A required native MCP claim tool.
- Versioning the claims input envelope in this increment.

## Existing behavior

The current command supports:

- `edge`
- `no_edge`
- `edge_count`
- `node`
- `no_node`
- `in_degree`
- `out_degree`
- `entrypoint`

It already provides strict single-value JSON decoding, unknown-field refusal,
wrong-kind-field errors, unique-or-error structural name resolution,
deterministic ordering, PASS/FAIL/ERROR outcomes, and existing text/exit
semantics.

The machine increment extends that behavior; it does not replace it.

## Command contract

```console
groundwork assert <graph.json> <claims.json> [--expect <stamp>] [--json]
```

Flags may appear wherever the command's existing flag helpers permit.

### Stamp verification

`--expect` calls `verifyGateStamp` on the evaluated graph:

- missing graph stamp: operational error;
- mismatched stamp: operational error;
- matching stamp: evaluation proceeds.

When `GROUNDWORK_REQUIRE_STAMP=1`, omitting `--expect` is an operational error.
This keeps CI's “all verdicts are identity-bound” setting from having an
`assert` loophole. Local runs remain opt-in when the environment variable is
unset.

### Exit codes

Existing precedence remains:

| Aggregate result | Exit |
|---|---:|
| one or more FAIL results | 1 |
| no FAIL and one or more ERROR results | 2 |
| every claim PASS | 0 |
| usage, decode, stamp, or machine-ID error | 2 |

Both text and JSON reports are written before returning a computed exit 1 or
per-claim exit 2. A file-level decode failure, stamp failure, or invalid
machine-ID set produces no partial report.

## Machine ID contract

Text mode keeps IDs optional for compatibility.

JSON mode validates the complete claim list before evaluation:

- every `id` must contain at least one non-whitespace character;
- IDs are unique by exact UTF-8 byte equality;
- the first missing or duplicate ID is reported deterministically in
  claims-file order.

The command rejects the whole machine run on an ID error because results could
not be mapped unambiguously to their source predicates.

## Selector input

`from`, `to`, and `through` use one strict scalar-or-list type:

```json
"from": "example.com/service.Handler"
```

or:

```json
"from": [
  "example.com/service.Handler",
  "example.com/service.Worker"
]
```

The decoder accepts only:

- one non-empty JSON string; or
- a non-empty array of non-empty strings.

It rejects `null`, empty arrays, whitespace-only selectors, non-string members,
and trailing data.

Existing structural kinds require exactly one selector for each applicable
field and retain their current normalized-suffix/regex semantics. An array on a
structural kind becomes a per-claim `MALFORMED_CLAIM` ERROR, so old semantics
cannot widen silently.

Rich kinds accept one or more selectors and use fitness's
boundary-aware exact-or-prefix matcher, including `entrypoint:*` where a
from-bearing evaluator supports it. The two selector grammars are intentionally
different and must be documented beside their claim families.

## Canonical JSON schema

JSON mode serializes a dedicated DTO through `canonjson.Marshal`.

```json
{
  "schema_version": "groundwork.assert/v1",
  "fixture": {
    "stamp": "789abc",
    "producer_tool": "v0.0.0-...",
    "algo": "vta",
    "caveats": []
  },
  "results": [
    {
      "id": "adapter-reaches-lifecycle",
      "kind": "reach",
      "outcome": "PASS",
      "detail": "path found",
      "bindings": {
        "from": [
          "example.com/service/internal/adapter.Dynamo.Write"
        ],
        "to": [
          "example.com/service/internal/outbox.Lifecycle.Publish"
        ]
      },
      "witnesses": [
        {
          "from": "example.com/service/internal/adapter.Dynamo.Write",
          "to": "example.com/service/internal/outbox.Lifecycle.Publish",
          "path": [
            "example.com/service/internal/adapter.Dynamo.Write",
            "example.com/service/internal/outbox.Lifecycle.Publish"
          ]
        }
      ]
    }
  ],
  "summary": {
    "passed": 1,
    "failed": 0,
    "errored": 0,
    "nodes": 100,
    "unique_edges": 240
  }
}
```

### Top-level fields

| Field | Contract |
|---|---|
| `schema_version` | Exact string `groundwork.assert/v1` |
| `fixture` | Identity and evaluator-substrate provenance |
| `results` | One item per input claim, in claims-file order |
| `summary` | Existing outcome and graph counts |

### Fixture

`fixture` contains:

- `stamp`: `graph.Stamp`;
- `producer_tool`: `graph.Tool`;
- `algo`: `graph.Algo`;
- `caveats`: sorted, deduplicated
  `graph.NewIndex(g).GateCaveats("")`.

Using `GateCaveats` includes graph-provided caveats plus existing reclaim and
SQL-fold substrate disclosures. Sorting a copied slice makes semantically
equivalent shuffled graph input serialize identically.

All fixture fields are present. Unrecorded scalar provenance is `""`; no
caveats is `[]`.

### Result

Each result contains:

```go
type JSONResult struct {
	ID        string        `json:"id"`
	Kind      string        `json:"kind"`
	Outcome   string        `json:"outcome"`
	Reason    string        `json:"reason,omitempty"`
	Detail    string        `json:"detail,omitempty"`
	Bindings  *JSONBindings `json:"bindings,omitempty"`
	Witnesses []JSONWitness `json:"witnesses,omitempty"`
}
```

`outcome` is exactly `PASS`, `FAIL`, or `ERROR`.

`reason` is present only for ERROR and uses this closed v1 vocabulary:

- `UNRESOLVED`
- `AMBIGUOUS`
- `UNBOUND_SELECTOR`
- `BLIND_FRONTIER`
- `MALFORMED_CLAIM`
- `UNKNOWN_STATUS`
- `MISSING_GRAPH_DATA`
- `CANT_PROVE`
- `UNMATCHED`

`detail` is human explanation and never the machine discriminator.

### Bindings

Bindings expose canonical graph identities:

```go
type JSONBindings struct {
	From       []string `json:"from,omitempty"`
	To         []string `json:"to,omitempty"`
	Through    []string `json:"through,omitempty"`
	FQN        []string `json:"fqn,omitempty"`
	Of         []string `json:"of,omitempty"`
	Fn         []string `json:"fn,omitempty"`
	Entrypoint []string `json:"entrypoint,omitempty"`
	Obligation []string `json:"obligation,omitempty"`
}
```

Every slice is sorted and deduplicated. A result includes whatever bindings
were established before failure; an unresolved side does not fabricate an
entry.

### Witnesses

```go
type JSONWitness struct {
	From      string   `json:"from,omitempty"`
	To        string   `json:"to,omitempty"`
	Path      []string `json:"path,omitempty"`
	BlindSite string   `json:"blind_site,omitempty"`
	Rule      string   `json:"rule,omitempty"`
	Fn        string   `json:"fn,omitempty"`
	Site      string   `json:"site,omitempty"`
	Status    string   `json:"status,omitempty"`
	Detail    string   `json:"detail,omitempty"`
}
```

Witnesses are sorted by their complete intrinsic field tuple. Paths come from
BFS over the index's sorted adjacency. A fact that has no trustworthy complete
path may emit endpoint evidence without inventing a path.

## Internal architecture

### Typed fact package

Create `internal/groundwork/facts`, a presentation-free package imported by
both fitness and claims.

It owns:

- fitness-compatible selector binding;
- target liveness checks;
- reachability traversal;
- blind-frontier classification;
- waypoint-removal traversal;
- deterministic shortest paths;
- concurrent-surface construction;
- obligation-status interpretation;
- structured bindings and witnesses.

It imports `groundwork/graph` and uses the existing
`policy.MatchPrefix` matcher as the single source of prefix semantics. It does
not load a policy, import `claims`, construct prose findings, or assign policy
severity.

### Domain-specific results

Avoid one overly generic status enum. Each fact family gets a closed result
type:

```go
func EvaluateReach(ix *graph.Index, from, to []string) ReachResult
func EvaluatePassThrough(ix *graph.Index, in PassThroughInput) PassThroughResult
func BuildConcurrentSurface(ix *graph.Index) ConcurrentSurface
func (s ConcurrentSurface) Evaluate(to []string) ConcurrentResult
func EvaluateObligation(ix *graph.Index, name string) ObligationResult
```

`PassThroughInput` includes selectors and a facts-local allow-list shape.
Fitness converts `policy.Exception`; claims passes no exceptions.

The concurrent surface is built once per `fitness.Check` or claims evaluation,
preserving the current rule-independent computation.

### Adapters

Fitness adapters map typed facts to:

- existing `Finding` identity;
- existing summary/detail prose;
- Caution versus Violation;
- `require_proof` escalation.

Claims adapters map them to:

- PASS, FAIL, or ERROR;
- structured reason;
- resolved bindings;
- deterministic witnesses.

Before extraction, tests byte-pin the existing fitness findings for each fact
family. Refactoring must not change them unless a separately named fail-closed
correction is reviewed and documented.

Constructing temporary policies and invoking `fitness.Check` is prohibited:
positive reach is not expressible, unrelated obligation findings leak into the
result, and policy severity erases the native fact state.

## Rich claim vocabulary

### `reach`

```json
{
  "id": "adapter-reaches-lifecycle",
  "kind": "reach",
  "from": ["example.com/service/internal/adapter.Dynamo.Write"],
  "to": ["example.com/service/internal/outbox.Lifecycle.Publish"],
  "expect": "present"
}
```

`expect` is exactly `present` or `absent`.

| Evaluated fact | `present` | `absent` |
|---|---:|---:|
| path found | PASS | FAIL |
| proven absent | FAIL | PASS |
| no path over blind frontier | ERROR `BLIND_FRONTIER` | ERROR `BLIND_FRONTIER` |
| source or target unbound | ERROR `UNBOUND_SELECTOR` | ERROR `UNBOUND_SELECTOR` |

Reachable dominates blindness because a concrete witness already proves or
disproves the expectation. Otherwise blindness dominates proven absence.

The primary path witness preserves current fitness target selection: sources
are sorted, reachable functions are considered before boundary effects, and
targets retain deterministic index order.

### `pass_through`

```json
{
  "id": "all-publishes-use-lifecycle",
  "kind": "pass_through",
  "from": ["example.com/service/internal/adapter.Dynamo.Write"],
  "to": ["boundary:bus PUBLISH"],
  "through": ["example.com/service/internal/outbox.Lifecycle.Publish"]
}
```

| Fact | Outcome |
|---|---:|
| one or more bypass paths | FAIL |
| no bypass, visible frontier | PASS |
| no bypass, blind frontier | ERROR `BLIND_FRONTIER` |
| any selector family unbound | ERROR `UNBOUND_SELECTOR` |

All bypass pairs and shortest paths are sorted and returned.

The claim is intentionally vacuously true when no source-to-target path exists,
matching the mathematical all-path property. A design that also requires
existence uses a companion positive `reach` claim.

Fitness may preserve its current dead-waypoint presentation while consuming the
same binding facts; the claims adapter is stricter because the ephemeral claim
requires every named selector to bind.

### `no_concurrent_reach`

```json
{
  "id": "no-unsupervised-write",
  "kind": "no_concurrent_reach",
  "to": ["boundary:db INSERT", "boundary:db UPDATE"]
}
```

| Fact | Outcome |
|---|---:|
| target reached on concurrent surface | FAIL |
| no hit, visible concurrent surface | PASS |
| no hit, blind concurrent surface | ERROR `BLIND_FRONTIER` |
| target unbound | ERROR `UNBOUND_SELECTOR` |

Hit witnesses are sorted and deduplicated by intrinsic endpoint fields.
Existing `ConcurrentDispatch` and dynamic-boundary blindness rules remain the
single source of abstention.

### `obligation`

```json
{
  "id": "transaction-closes",
  "kind": "obligation",
  "name": "tx-must-close",
  "expect": "satisfied"
}
```

Only `expect: "satisfied"` is accepted in v1. The claim inspects obligations
already carried by the graph and cannot author new rules.

Aggregate exact-name matches as follows:

1. No `obligations` section: ERROR `MISSING_GRAPH_DATA`.
2. A non-empty section with no exact rule-name match: ERROR `UNRESOLVED`.
3. Any `VIOLATED`: FAIL, even if another record abstains.
4. Otherwise, any unknown status: ERROR `UNKNOWN_STATUS`.
5. Otherwise, any `CANT-PROVE`: ERROR `CANT_PROVE`.
6. Otherwise, any `UNMATCHED`: ERROR `UNMATCHED`.
7. One or more records and all `SATISFIED`: PASS.

A concrete violation dominates abstention because it already disproves
`satisfied`. Every matched record is exposed as a sorted witness.

## Existing structural claims

Existing structural semantics and text remain unchanged. JSON adds bindings
where resolution succeeded and maps existing errors:

| Existing condition | JSON reason |
|---|---|
| missing required or wrong-kind field | `MALFORMED_CLAIM` |
| unknown kind | `MALFORMED_CLAIM` |
| malformed regex | `MALFORMED_CLAIM` |
| zero plain/regex matches where required | `UNRESOLVED` |
| non-unique plain or one-required match | `AMBIGUOUS` |
| graph lacks entrypoint join entirely | `MISSING_GRAPH_DATA` |

`no_node` keeps its deliberate polarity: zero matches is PASS, not unresolved.

## Determinism

For equal semantic inputs:

- result order follows claims-file order;
- every binding, witness set, caveat set, and candidate list is independently
  sorted and deduplicated;
- BFS visits sorted adjacency;
- JSON struct field order is declaration order;
- maps, where unavoidable, serialize through `canonjson`;
- no timestamp, random ID, absolute path, pointer identity, or map arrival
  order reaches output.

Tests shuffle graph nodes, edges, blind spots, obligations, entrypoints, and
caveats independently and require identical JSON bytes.

## Documentation

Update:

- `groundwork` command help;
- the claims section of `docs/groundwork/usage.md`;
- the gate-stamp environment-variable documentation;
- claim examples for every new kind;
- the authority distinction between policy verdicts and assert results;
- the scalar-versus-list selector rule;
- the JSON schema and reason vocabulary.

## Compatibility and rollout

Increment 1 is independently releasable. Existing invocations without
`--json`, `--expect`, or a stamp-enforcement environment remain unchanged.

Increment 2 widens the accepted claim vocabulary and scalar/list input shape.
It does not change existing structural evaluations.

Rollback is incremental:

- rich claim kinds can be reverted while retaining the machine contract;
- the machine contract can be reverted without changing existing text mode.

No dependency or data migration is required.

## Acceptance criteria

### Machine increment

- `--json` emits `groundwork.assert/v1`.
- JSON contains every input claim, including passes.
- Every result carries its machine ID, outcome, and structured reason when
  errored.
- Resolved graph identities are exposed.
- Fixture provenance contains stamp, producer, algorithm, and canonical
  caveats.
- `--expect` rejects missing and mismatched stamps.
- `GROUNDWORK_REQUIRE_STAMP=1` makes `--expect` mandatory.
- JSON mode rejects empty and duplicate IDs.
- Existing text bytes and exit precedence remain unchanged.
- Repeated and shuffled-input runs produce identical JSON bytes.

### Rich-claim increment

- Positive and negative reach claims implement the three-valued table.
- Waypoint claims expose deterministic bypass witnesses.
- Concurrent claims reuse the existing concurrent-surface and blindness rules.
- Named obligation claims aggregate statuses fail-closed.
- Unbound or ambiguous required selectors never pass.
- Blind-frontier cases never pass.
- Fitness and claims consume the same typed evaluators.
- Characterization tests prove unchanged existing fitness findings.

### Repository gates

- `make fmt-check` passes.
- `make comment-drift` is reviewed.
- `make verify` passes.
