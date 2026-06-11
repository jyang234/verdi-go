# MCP expansion: tiers 2–3 — plan-of-record

**Status:** Tiers 1 and 2 are built. Tier 1: entrypoints, fitness, reload +
staleness flagging, --log transcript. Tier 2: `--service` fleet serving with
the optional `service` arg on every tool, the fleet-wide prefixed
`entrypoints` listing, and the `fleet-events` lens — implemented in the
pressure-tested shape recorded below (the lone deviation: the lone service
is keyed by its graph path in the single-graph form, so one code shape
serves both). Tier 3 remains designed and deliberately deferred.

## Tier 2 — multi-service serving (BUILT)

`groundwork mcp --service payments=graphs/payments.json --service
ledger=graphs/ledger.json [--policy payments=...]` — a map of named services,
each with its own index/policy/stamp/mtime state (the existing `mcpServer`
becomes the per-service value). Every tool gains an optional `service` arg;
single-service invocations keep today's behavior with zero changes (the lone
service is the default — no breaking change). `entrypoints` with no service
arg lists across the fleet, prefixed.

**What this is and is not:** it lets one session HOLD the neighborhood's maps
so the agent can walk a publisher in service A to the consumer in service B —
the hop is explicit and the reasoning stays per-service and honest. It is NOT
the fleet model (merged cross-service graph, system-level policy); that is
the larger missing piece named in the gap analysis, and serving N maps is its
transport, not its substance. Scoping note: the boundary contracts are the
join vocabulary (published/consumed events match across services); a
`fleet-events` discovery tool (which service publishes/consumes each event,
from the contracts) is the cheap first cross-service lens and needs no merged
graph. As built, fleet-events reads the same vocabulary from the loaded
graphs themselves (PUBLISH/CONSUME boundary edges + consumer entrypoints) —
the server already holds them, so no extra contract files are taken — and
discloses dynamically-named bus effects per service.

## Tier 3 — streamable-HTTP transport for a team-shared server

A centrally-managed server fed directly by CI artifacts. The pressure-test
finding stands: this STRENGTHENS the trust posture — today the agent's
.mcp.json picks the file the server loads (claim-chain trust); a central
server means the agent cannot choose its inputs at all, and the per-deploy
graph archive becomes self-serving. Costs that defer it: auth, lifecycle,
the toolset's first deployment-shaped component. **Build only alongside a
real adopter who needs shared serving** — it is operations, and operations
without an operator is shelf-ware.

## Standing refusals (decided, recorded in the server's doc comment)

No write tools, ever (the agent must never author its own guardrails); no
graph generation in the server (producer stays in CLI/CI); no free-form graph
query language (cards are curated lenses with disclosure built in); MCP
prompts deferred (methodology belongs to the team, not the instrument).

## Order of evidence before building

The E4 drill's --log transcripts will show whether agents *want* cross-service
hops mid-session; the real-service adoption will show whether shared serving
is needed at all. Both tiers should be re-prioritized against that evidence
rather than built on speculation — the same ROI gate every other deferral in
this repo carries.
