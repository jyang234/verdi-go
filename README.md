# flowmap

A dual-pipeline PR-verification system for Go microservices. A human (via
CODEOWNERS) is the oracle — there is no AI in the verdict path.

- **Static pipeline** — from a service's source, build a call graph
  (`go/packages` → `go/ssa` → `go/callgraph`) and derive a *gated* inter-service
  **boundary contract** (published/consumed events, external HTTP/RPC
  dependencies, entry points) plus a **blind-spot manifest**. The full call graph
  + signatures are a *generated, non-gated* view. Gate = currency (regenerate,
  `git diff --exit-code`).
- **Behavioral pipeline** — service authors write in-process flow tests against
  flowmap; it captures OTel traces, canonicalizes them into a deterministic IR,
  commits **golden snapshots**, and gates via snapshot-assertion (`-update` to
  rebase).

Both pipelines share a **tier-map classifier** (features → tier 1–4) and a strict
**determinism discipline** (sort everything; canonical JSON). A **structural
diff** turns IR changes into a prioritized change set, and a **coverage-delta**
surfaces boundary effects no test exercises.

## Status

Under construction, phase by phase. See:

- `docs/` — the seven component specifications (the source of truth).
- `docs`-aligned `example-loan-svc-artifacts.md` — a worked example of every
  artifact flowmap emits.
- The implementation plan (phased, with per-phase verification).

## Layout

```
cmd/flowmap/   CLI: boundary [--check] | graph --entry | version (diff/coverage arrive later)
harness/       PUBLIC: in-process capture harness for flow tests
flow/          PUBLIC: the flow test DSL
ir/            PUBLIC: the authoritative canonical IR (golden file shape)
internal/      the analysis engine (canonjson, glob, model, tiermap, static/, canon/, …)
testdata/      hermetic fixture service (its own module) + committed goldens
```

## Develop

```
make verify    # build + vet + test + fixture build + gofmt check (the per-phase gate)
```
# golang-code-graph
