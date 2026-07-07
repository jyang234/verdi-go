# verdi-bench integration — external measurement of groundwork's agent value

> **`DESIGN RECORD (pointer)`** · 2026-07-07 · the full plans live in the verdi-bench
> repo (`jyang234/verdi-bench`): `docs/design/verdi-go-integration-plan.md` (Tracks
> A/B) and `docs/design/workspace-observability-plan.md` (Track C, unrelated to this
> repo's verdicts). This doc records what verdi-bench consumes from this repo and
> which guarantees it leans on, so changes here can price in the external dependent.

## What verdi-bench is, in one paragraph

A benchmark-grade A/B instrument for agent stacks: sha-locked pre-registered
experiments, paired trials in hermetic digest-pinned containers, deterministic-first
grading (structurally LLM-free), an identity-blind advisory judge, gaming forensics,
and a hash-chained ledger. It is the external, pre-registered measurement apparatus
that drill **E4** (`docs/groundwork/drills.md`) was deliberately parked to wait for:
E4 puts an AI in the measurement loop, so it must not live in this repo's test
suite — verdi-bench is where it runs.

## The two roles this repo plays there

1. **Subject under test (arm-side).** A payload-gated trial image ships pinned
   `flowmap`/`groundwork` binaries plus the `groundwork-workflow` skill; the treatment
   arm runs the ground → edit → verify loop over `groundwork mcp`, the control arm
   gets the identical image with the tool unexposed. The flagship experiment is a
   2×2 (model tier × groundwork access) over trap-seeded Go feature tasks, designed
   directly from the postmortem's surviving claims
   (`docs/groundwork/ab-testing-postmortem.md`): value = systematic in-loop surfacing
   + calibration, not per-instance capability; nulls are published.
2. **Grader backend (harness-side).** verdi-bench's grade container regenerates the
   branch graph from the agent's workspace (`flowmap graph --stamp`) and evaluates
   `groundwork verify/fitness --json` against the committed policy and base graph.
   Verdicts map onto its assertion model with the epistemics preserved:
   pass → passed, fail → failed, **NO-STRUCTURAL-SIGNAL → abstain (never pass)**;
   operational failure → the trial is un-gradeable, never silently clean.

## Guarantees verdi-bench leans on (do not weaken without pricing this in)

- **Exit-code contract**: `0` clean / `1` verdict failed / `2` operational error
  (`cmd/groundwork/main.go`). The 1-vs-2 distinction maps to "graded fail" vs
  "cant_grade" and is load-bearing.
- **Canonical `--json` output** of `fitness`/`review`/`verify`, byte-stable per tool
  version, with rule ids present.
- **Byte-determinism per build (R11)** of `graph.json` for a fixed tree — verdi-bench
  pins one binary across trial images, grader image, and committed base graphs, and
  runs a k=5 zero-tolerance flake baseline that would surface any regression here.
- **Stamp discipline**: `--stamp`/`--expect` (and `GROUNDWORK_REQUIRE_STAMP`) bind
  graphs to source identity across its trust boundary.
- **Graph-integrity doctrine** (`docs/groundwork/distilled-learnings.md`): the grader
  always regenerates the graph from the workspace; agent-supplied graphs are never
  trusted. verdi-bench implements this in a fresh-copy `--network none` grade
  container.
- **Read-only MCP surface + deterministic `--log` transcript** (`cmd/groundwork/mcp.go`):
  the call log is the source of verdi-bench's tool-usage funnel metrics
  (ground-before-edit, check-after-last-edit, verdict-heeded) — the quantitative half
  of E4, exactly as instrumented here. Keep the JSONL shape stable or version it.
- **Pinned install path**: `go install …/cmd/{flowmap,groundwork}@<ref>` (the
  `setup-groundwork` action pattern) + `internal/buildinfo` self-reported versions,
  which land in verdi-bench's grade/trial provenance.

## What this asks of this repo

Nothing new. No verdi-bench dependency, no AI anywhere near a verdict, no code
change. The fixtures (`testdata/groundwork/{layeredsvc,blindsvc,obligsvc}`,
`testdata/fixtures/loansvc`) are used as **mutated copies** to seed benchmark task
workspaces — never modified in place, and mutation is deliberate (they are public;
verdi-bench runs training-set contamination probes regardless).

What comes back: externally measured, pre-registered evidence for the scorecard's
honest gaps — E4 (📋 designed → 📐 measured), the "agent benefit" row, and the
postmortem's one untested residual (multi-impl interface with a single live impl),
which is a first-class task class in the corpus. Tool-version A/Bs
(`groundwork@vA` vs `@vB`, same model, control reuse) give this repo a per-release
outcome-level regression signal its own drills cannot provide — drills measure the
tool's recall; verdi-bench measures agent outcomes attributable to the tool.
