# CX-5 inputs — field response, recorded and independently checked

Counterpart to `cx5-inputs-request.md`. Records the field partner team's
2026-06-13 response and the independent verification done from **this** repo.
Two things are deliberately left blank because they are not the tool's to
produce: John's signature on the broker block, and any incident citation.

---

## Input 1 — broker block: confirmed by the field, ready to sign

The field team returned the block as **confirm-as-is** (values verified
against the production `obligsvc` source — provisioner queue config, the
service README, the inbox `TopicUniqueness` test), no correction, no
per-topic split:

```jsonc
"brokers": {
  "bus": {
    "transport":   "sns->sqs (standard queue)",
    "delivery":    "at-least-once",
    "ordered":     false,
    "consumers":   "idempotent",
    "dedup":       "inbox UNIQUE(source_id, topic)"
  }
}
```

### What this repo can and cannot independently verify

This repository is the **analysis tool plus fixtures**, not the production
`obligsvc` service. The artifacts the field report cites as evidence — the
provisioner setting `QueueName` only (no `.fifo` / `FifoQueue` /
`ContentBasedDeduplication`), the README's at-least-once + `sourceId`
commitment, the inbox `TopicUniqueness` test — are **not present here**. So
the field values cannot be re-derived from this repo; they rest on the field
team's attestation against the production source. That is the correct shape:
D-CX5 says broker semantics are **declared in policy, never inferred**, which
is exactly why the warrant is a human signature, not the tool's.

| field | status from THIS repo |
|---|---|
| `transport: sns->sqs` | **Weakly corroborated.** `testdata/otlp/aws-sns-sqs.otlp.json` shows an `SNS Publish` span — the transport *shape* is observed. It carries no queue-type attribute, so "standard queue" specifically is the field's attestation, not an in-repo fact. |
| `delivery: at-least-once` | **Not in-repo.** Rests on the production README + provisioner. Matches the D-CX5 design record verbatim (internal consistency only). |
| `ordered: false` | **Not in-repo.** Standard-queue ⇒ unordered is the field's reading of the provisioner config; no FIFO markers exist to inspect here. Matches D-CX5. |
| `consumers: idempotent` | **Not in-repo, and conditional.** Holds only insofar as consumers use the mandated inbox library — a property of every subscriber, not of the bus. The fixture `bus.Publish` is a stub. |
| `dedup: inbox UNIQUE(source_id, topic)` | **Not in-repo.** The `TopicUniqueness` test the field cites lives in the production inbox library, not here. |

**Conclusion:** the block is internally consistent with the repo's own D-CX5
record, and the one shape this repo can observe (SNS→SQS transport) agrees.
Nothing here contradicts any field value. The block is **ready to sign** — but
the verification that gives it force is the field team's, against the
production source, not the tool's against this repo.

### Sign-off (John's — intentionally not filled by the tool)

> Signed: ______________________  Date: __________

The block is **not** written into `.flowmap.yaml` policy yet: per the request
doc, it lands in policy *with the sign-off as its warrant*. Until John signs,
it stays here as a confirmed-but-unsigned draft, not a live declaration.

---

## Input 2 — effect_order citations: still open, not manufactured

Both candidate facts came back with **empty checkboxes** — John has not yet
marked either as cited in a real incident. Recorded as-is; no citation has
been invented to keep the phase alive (the request doc's explicit guard).

| candidate fact | cited in a real incident? | the question it answers |
|---|---|---|
| `provisioning_outbox` DELETE ordering (domain row + outbox row in one `RunInTx`) | ☐ yes ☐ no — **pending John** | "When a subscription/version was deleted, were its `provisioning_outbox` rows orphaned (left behind), or deleted while the domain row survived?" |
| dual publish in `publishWithFanout` (version topic → template topic) | ☐ yes ☐ no — **pending John** | "The fault hit between the two publishes — was the event already on the per-version topic's queues when the template publish failed?" |

Open prompts, still awaiting one line each from John:

1. Any *other* effect_order fact a responder used in the last few incidents?
2. Most recent "what already committed?" incident — route/symptom + which fact
   would have answered it (becomes the chain card's worked example).

**State:** until at least one fact is confirmed cited (or John confirms
"never came up"), CX-5 stays parked at the **E-CX5 ROI gate**. That is a valid,
documented outcome — a surface no responder reached for should not ship.

---

## Update (2026-06-13): the observational surface shipped

At the field's direction ("everything short of gating"), the CX-5 cross-service
chain surface is now built — `groundwork chains` + a fleet-MCP lens, documented
in `cx5-chains-surface.md`. It composes the proven per-service facts into chain
cards and prints the broker block from policy. Crucially, it did **not** change
either answer this doc records:

- The broker block is wired into policy as `brokers` data and printed on every
  card flagged **UNSIGNED** — the values, with no warrant. The signature line
  below is still John's.
- Input 2's checkboxes are still empty. The surface ships observational on the
  honest current fleet (half-open chains and all); no citation was manufactured,
  and the *gating* `chain` rule kind is deferred to E-CX5.

## What is blocking, precisely

1. **John's signature** on the Input 1 block — then it lands in `.flowmap.yaml`
   policy and chain cards print it as the *assumed* link.
2. **John's incident answer** on Input 2 — then the first chain card is built
   for the cited fact; if neither was cited, CX-5 parks (E-CX5).

Neither is something the tool can supply on John's behalf, by design.
