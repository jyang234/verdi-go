# CX-5 — the cross-service chain surface (observational, shipped)

> **`DESIGN RECORD`** · the shipped observational surface (the gate itself is unbuilt — see `correctness-expansion-plan.md`) · _reviewed 2026-06-13_

CX-5 is built **up to but not including the gate**. The observational surface —
`groundwork chains` plus a fleet-MCP `chains` lens — composes the per-service
facts groundwork already holds into a cross-service happens-before chain card.
What it deliberately does *not* do: gate, sign the broker block for you, or
manufacture an incident citation. Those remain human/E-CX5 decisions.

## What a chain card is

Per bus event, the card renders a happens-before chain whose every link is
labeled:

- **proven** — a fact computed from one service's graph:
  - *producer side*: the in-frame publish ordering (effect_order — is the
    publish CERTAINLY or possibly committed before the publishing function's own
    fallible call?) and the must-precede verdicts on the publishing functions
    (e.g. `audit-before-publish: VIOLATED at app.DeferredPublish` — a real
    producer-side risk, surfaced);
  - *consumer side*: the consumer handler's downstream committed effects (the DB
    writes / publishes it makes on receipt) and its obligations.
- **assumed** — the declared broker guarantee, printed verbatim from policy,
  **never inferred** (D-CX5).

It is honest about the fleet it was handed: a half-open chain (a publish no
loaded service consumes, or a consume no loaded service produces) is printed as
**open upstream/downstream**, not hidden; events a service cannot name
statically (`<dynamic>` publishes) are disclosed as a frontier.

## How to run it

```
# CLI — positional graphs or --service name=path; --policy supplies the broker block
groundwork chains \
  --service loansvc=loansvc.graph.json \
  --service obligsvc=obligsvc.graph.json \
  --policy bus-brokers.json

# MCP — a fleet lens alongside fleet-events
groundwork mcp --service loansvc=... --service obligsvc=... --policy obligsvc=bus-brokers.json
# then call the "chains" tool
```

## The broker block — declared, not yet warranted

The broker guarantee lives in policy as a `brokers` block (see
`policy.Broker`), validated on load so a typo'd value (`atleastonce`) can't read
on a fault card as a real promise. `testdata/groundwork/policies/bus-brokers.json`
carries the field-confirmed values from the 2026-06-13 report:

```json
"brokers": { "bus": {
  "transport": "sns->sqs (standard queue)",
  "delivery":  "at-least-once",
  "ordered":   "false",
  "consumers": "idempotent",
  "dedup":     "inbox UNIQUE(source_id, topic)"
}}
```

It has **no `signed_by`**, so every card prints it flagged **UNSIGNED (values
declared, pending human sign-off)**. The tool fills the values; only a person
fills the warrant. Adding `"signed_by": "<name>, <date>"` is what turns the
flag into `signed by <name>` — that is John's to add, here or in production, and
the tool will not add it for him.

## What is still deferred (by design)

- **The gating `chain` rule kind** — "this publish must be commit-dominated in
  its producer". Trivial to add on top of the proven facts, but it waits for
  E-CX5: a card has to earn field trust before it gates. Not built.
- **The broker sign-off** — a human warrant, left UNSIGNED above.
- **The first cited incident** — Input 2's checkboxes are still empty; the
  surface ships observational on the honest current fleet, no citation invented.

So CX-5 is shipped as the resolved-cone-plus-declared-broker instrument the plan
described, observational and non-gating — everything short of the gate.
