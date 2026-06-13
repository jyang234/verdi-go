# CX field measurement run — protocol

> **`DESIGN RECORD`** · field-run protocol; the 2026-06-12 run is complete · _reviewed 2026-06-13_

**For:** the deployment that produced the 2026-06-12 measurement report.
**Answers:** O-CX2 (trust monotonicity), E-CX1 (named field cases), E-CX2
(abstention budget), E-CX4 (sensitive-flow noise), E-CX6 (the promotion
gate). Phases CX-0/1/2/3/4 do not promote until this run's numbers are in.

## Setup

Build flowmap and groundwork twice, from the same repo at two refs:

- **OFF** — `9ae5b15` (your current baseline; intraprocedural verdicts).
- **ON** — the `claude/correctness-value-blind-dependent-r45wfo` branch tip.

Producer and judge are lockstep (the `via` field is new schema): always pair
a graph with the binary that produced it. Generate per service, per binary:
`flowmap graph <svc> > <svc>.<off|on>.graph.json`.

## Run 1 — the off/on verdict diff (O-CX2, E-CX2)

With your existing trial rules in place (`tx-released`,
`validate-before-publish`), diff the `obligations[]` sections keyed on
(rule, fn, site):

1. **Monotonicity (hard gate):** any finding VIOLATED in ON that is not
   VIOLATED in OFF is a soundness defect — report it verbatim, it blocks
   promotion outright.
2. **Abstention budget (E-CX2 kill threshold):** count VIOLATED→CANT-PROVE
   transitions. If they outnumber the false-VIOLATEDs the run removes, the
   handoff/entry consultation is too eager for your graphs.
3. **The wins:** VIOLATED→SATISFIED transitions, with their `detail` —
   each one should read as a proof you believe.

## Run 2 — the named field case (E-CX1)

Add `fromCallers: true` to `validate-before-publish` (it is a guard-intent
rule — the require may legitimately run in callers; see D-CX9 in the plan:
pairing-intent rules like audit-per-publish must NOT opt in). Regenerate with
ON. Expected: both `publishWithFanout` VIOLATEDs flip to SATISFIED with a
witness naming `doPublish`, zero other rule changes. If they land CANT-PROVE
instead, the `detail` names why (an address-taken handler, an unresolved
invoke of the method name, a graph source on the entry chain) — report the
note text; that is the lift meeting your dispatch reality, and it is the
exact data we need.

## Run 3 — derived effect facts (E-CX1 case ii)

Diff `effect_order[]`: report the count of rows carrying `via`, and
spot-check the `PublishEventCommand.Handle` /
`doPublish`→`publishWithFanout` chain — the dual fan-out question ("the
version topic published, the fault hit before the template topic — what
already happened?") should now be answerable from the handler's own rows.
Also report total row growth per service: ALWAYS-only derivation is
proof-true by construction, but orchestrator-shaped functions accrue rows
(observed on the fixture), and your numbers decide whether a scoping knob
ships.

## Run 4 — the sensitive-flow trial (E-CX4), cgate only

Per the pack's shipping preconditions (usage.md, "sensitive-flow rule
pack"): cgate qualifies (0 blind spots, PII in ~3 files); event-bus is the
documented do-not-ship case — do not configure it there. Name the
PII-handling functions as explicit FQNs (receiver-qualified for methods —
bare package selectors do not bind methods; verify binding with
`groundwork ground`), the log sinks as `to`. Report dismissed-vs-accepted
on whatever fires, dispatcher first (`outbound/dispatcher.go` holds both
sides).

## Report back

The four numbers/lists above, plus wall-clock for `flowmap graph` on
event-bus OFF vs ON (the summary engine adds whole-program passes; the
fixture says linear, your 891-node graph says whether that holds). Anything
that surprised you is more valuable than anything that confirmed.
