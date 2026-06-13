# Documentation map

> **`ACTIVE`** · index for the docs space · _reviewed 2026-06-13_

This is the front door to `docs/`. The tree is organized by **role**, and every
doc carries a lifecycle banner directly under its title so you can tell at a
glance what is current and what is kept for history.

## Lifecycle legend

Every doc header begins with one of:

| Badge | Meaning |
|---|---|
| **`ACTIVE`** | Current source of truth or in active use — specs, guides, living measurement records, and design work still in flight. Trust it; keep it current. |
| **`DESIGN RECORD`** | The work it describes has shipped. Kept for the rationale, decisions, and pressure-test trail — not a to-do list. Accurate as history, not necessarily a guide to today's surface. |
| **`HISTORICAL`** | Superseded — its content has been folded into a spec or refined into a plan. Kept only for the reasoning that produced what replaced it. Do not treat as current. |

## The layout

```
docs/
├── specs/        component specifications — the source of truth (ACTIVE)
├── guides/       how-to: adoption, authoring, integration (ACTIVE)
│   └── integration/   tapping an existing OTel/OTLP e2e suite
├── groundwork/   the groundwork layer: guides + its design record
├── design/       feature plans and design briefs (mostly shipped records)
│   └── ideas/    pre-plan design discussions (HISTORICAL — refined into plans)
└── archive/      superseded session artifacts (HISTORICAL)
```

## specs/ — component specifications · **`ACTIVE`**

The assembled-system contract and the seven component specs. Start here for "how
the system actually works."

| Doc | What it covers |
|---|---|
| [`scope-enforcement-guarantees.md`](specs/scope-enforcement-guarantees.md) | The capstone — what the assembled system is, how it's enforced, and what it does *not* guarantee. Read this first. |
| [`static-extractor-spec.md`](specs/static-extractor-spec.md) | The static pipeline: call graph → inter-service boundary contract + blind-spot manifest. |
| [`tier-map-spec.md`](specs/tier-map-spec.md) | The single salience classifier both pipelines call to assign `Tier`. |
| [`trace-canonicalization-spec.md`](specs/trace-canonicalization-spec.md) | The deterministic transform from a raw OTel trace to a run-independent snapshot. |
| [`trace-capture-harness-spec.md`](specs/trace-capture-harness-spec.md) | Producing canonicalization's input: a complete, scoped trace for one flow. |
| [`golden-diff-spec.md`](specs/golden-diff-spec.md) | Golden files and the semantic, prioritized behavioral diff. |

## guides/ — how-to · **`ACTIVE`**

| Doc | What it covers |
|---|---|
| [`adopting-flowmap.md`](guides/adopting-flowmap.md) | End-to-end recipe for wiring flowmap into a Go service. |
| [`rule-anchoring.md`](guides/rule-anchoring.md) | Authoring `must-precede` obligations across a dispatch boundary. |
| [`integration/README.md`](guides/integration/README.md) | Conventions + copy-ready scaffolding for an out-of-process e2e suite. |
| [`integration/otlp-integration-guide.md`](guides/integration/otlp-integration-guide.md) | Step-by-step for adding post-hoc capture to an existing OTLP e2e suite. |

## groundwork/ — the verification layer

The deterministic consumer of flowmap's graph. A mix of active guides and the
design record behind them.

| Doc | Status | What it covers |
|---|---|---|
| [`README.md`](groundwork/README.md) | `ACTIVE` | Overview + index into the design record. |
| [`usage.md`](groundwork/usage.md) | `ACTIVE` | Practical guide: commands and a worked review. |
| [`personas.md`](groundwork/personas.md) | `ACTIVE` | Before/after for the responder, developer, and reviewer. |
| [`drills.md`](groundwork/drills.md) | `ACTIVE` | Triage effectiveness, measured as committed tests. |
| [`scorecard.md`](groundwork/scorecard.md) | `ACTIVE` | Honest capability assessment, graded by evidence class. |
| [`distilled-learnings.md`](groundwork/distilled-learnings.md) | `DESIGN RECORD` | The thesis and what was established about the substrate. |
| [`mr-review-artifacts.md`](groundwork/mr-review-artifacts.md) | `DESIGN RECORD` | The unfakeable MR review artifact design. |
| [`pressure-test.md`](groundwork/pressure-test.md) | `DESIGN RECORD` | Adversarial pressure test of the central claims. |
| [`implementation-plan.md`](groundwork/implementation-plan.md) | `DESIGN RECORD` | Plan-of-record for building groundwork (built). |

## design/ — plans & briefs

Feature plans and design briefs. Most describe shipped work and are kept as the
decision record; a few are still in flight.

| Doc | Status | What it covers |
|---|---|---|
| [`correctness-expansion-plan.md`](design/correctness-expansion-plan.md) | `ACTIVE` | The correctness (CX) expansion; CX-5 parked at the adopter gate. |
| [`cx5-chains-surface.md`](design/cx5-chains-surface.md) | `ACTIVE` | The shipped cross-service chain surface (observational; gate pending). |
| [`cx5-inputs-request.md`](design/cx5-inputs-request.md) | `ACTIVE` | The two human inputs that unblock chain-card gating. |
| [`cx5-inputs-response.md`](design/cx5-inputs-response.md) | `ACTIVE` | The field response, recorded and independently verified. |
| [`wrapper-fanout-investigation.md`](design/wrapper-fanout-investigation.md) | `ACTIVE` | HighFanOut wrapper investigation (D-CX10); owner decision pending. |
| [`correctness-field-run.md`](design/correctness-field-run.md) | `DESIGN RECORD` | Protocol for the 2026-06-12 field measurement run. |
| [`implementation_plan.md`](design/implementation_plan.md) | `DESIGN RECORD` | The original flowmap phased plan; v1 core (Phases 0–8) shipped. |
| [`post-hoc-behavioral-ingestion.md`](design/post-hoc-behavioral-ingestion.md) | `DESIGN RECORD` | Post-hoc (`ModePostHoc`) ingestion brief; stages 1–2 landed. |
| [`guardrail-extensions-plan.md`](design/guardrail-extensions-plan.md) | `DESIGN RECORD` | Deterministic guardrail extensions GX-1..5 (shipped). |
| [`path-obligations-plan.md`](design/path-obligations-plan.md) | `DESIGN RECORD` | Path-obligation checks OB-0..3 (shipped). |
| [`incident-triage-plan.md`](design/incident-triage-plan.md) | `DESIGN RECORD` | Incident triage IT-0..4 (shipped). |
| [`mcp-expansion-plan.md`](design/mcp-expansion-plan.md) | `DESIGN RECORD` | MCP tiers 2–3 (all built). |
| [`policy-coverage-extensions-plan.md`](design/policy-coverage-extensions-plan.md) | `DESIGN RECORD` | Policy coverage extensions PC-1..3 (shipped; PC-4 parked). |
| [`review-fixes-plan.md`](design/review-fixes-plan.md) | `DESIGN RECORD` | The branch-wide review fixes RF-1..7 (shipped). |

### design/ideas/ — pre-plan discussions · **`HISTORICAL`**

| Doc | Refined into |
|---|---|
| [`ideas/incident-triage.md`](design/ideas/incident-triage.md) | [`incident-triage-plan.md`](design/incident-triage-plan.md) |
| [`ideas/path-obligations.md`](design/ideas/path-obligations.md) | [`path-obligations-plan.md`](design/path-obligations-plan.md) |

## archive/ — superseded · **`HISTORICAL`**

| Doc | Why it's here |
|---|---|
| [`consistency-pass.md`](archive/consistency-pass.md) | A design-consistency sweep whose findings and corrections are now folded into the specs. Kept for the reasoning trail. |
