# Contributing

## Gate

`make verify` is the per-change gate — it must stay green:

```
make verify   # build + vet + lint (golangci-lint v2) + test + fixture gate + gofmt
```

CI additionally runs the suite under `-race` and splits into the two gate jobs
(currency + snapshot). Run `go test -race ./...` locally before pushing changes
that touch capture/await/canon/harness.

## Go version floor (read before `go get`)

The **engine module keeps a Go 1.24 floor** (`go.mod: go 1.24.0`) — that is the
version adopters build against, and it must build standalone (`GOWORK=off go
build ./...`). The **workspace stays at 1.25** (`go.work: go 1.25.0`) because a
few fixture modules declare `go 1.25.0` (e.g. `testdata/fixtures/{reflectsvc,
impeachsvc,reclaimsvc,dispatchsvc}`) and the workspace `go` directive must be at
least the highest of its member modules.

`go get`-ing a new dependency can silently bump the `go` directive to match the
local toolchain. After adding a dependency:

1. Keep the engine's deps 1.24-compatible (pin if a transitive dep demands 1.25).
2. Restore `go.mod`/`go.work` `go` directives if `go get` bumped them
   (engine → `1.24.0`, workspace → `1.25.0`).
3. Verify both: `GOWORK=off go build ./...` (engine at 1.24) and
   `go build ./...` (workspace at 1.25).

## Schema versions

The gated artifacts carry a `schema_version` (`flowmap.boundary/v1`,
`flowmap.trace/v1`). Changing a canonical form is a coordinated regeneration:
bump the version, then regenerate every committed artifact (`flowmap boundary`,
`go test -update`) in the same change. The version is part of snapshot equality,
so a bump deliberately fails stale goldens.

## Semantic (output-meaning) changes

Some changes alter what the output *means* without changing which elements it
contains — the fail-open class this codebase exists to catch. Examples: a
different `tier` attribution, whether a `go`-launched call carries `concurrent`,
the boundary-label vocabulary (`POSTGRES` → `postgres`), or a new/renamed
blind-spot kind. A set-based diff (and a reviewer skimming for added/removed
lines) reads these as "no change", so they must be named explicitly.

When a change moves output *semantics* rather than the element set, **say so in
the PR description.** The recommended, mechanical evidence is an attribute-aware
JSON delta over a committed golden:

```
flowmap graph --diff testdata/.../some.graph.json testdata/fixtures/somesvc
```

The `nodes_changed` / `edges_changed` lists are the exact drift to describe (see
`docs/groundwork/usage.md` → "Attribute-aware diff"). The goldens-manifest ratchet
(`testdata/groundwork/regen.sh` + `TestGoldenSectionManifest`) already makes an
attribute change *visible* in the review diff; this convention makes **naming** it
mandatory rather than left to reviewer alertness.
