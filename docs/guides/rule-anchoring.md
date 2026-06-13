# Rule anchoring: keep `require` and `before` on the same resolved side of a dispatch boundary

> **`ACTIVE`** · authoring guide · _reviewed 2026-06-13_

**Audience:** anyone authoring a `must-precede` obligation (`require` + `before`)
in `.flowmap.yaml`.
**Source:** this is the field-facing answer to ask #2 from the second
correctness field run; the measurement behind it is
`docs/design/wrapper-fanout-investigation.md` (D-CX10).

## The one rule

When you write a must-precede obligation, keep its `require` site and its
`before` site **on the same statically-resolved side of any framework dispatch
boundary** — both inside the handler body and below it, not one above and one
below a router/middleware hop.

A rule that spans the boundary (e.g. the `require` in the outer wrapper, the
`before` reached only through `doPublish`→`publishWithFanout`) will report
**UNKNOWN**, not SATISFIED — and that abstention is correct, not a tool gap.

## Why the boundary is real (not just "wide")

The interprocedural lifts (CX-2 entry domination, CX-3 effect derivation)
deliver wherever dispatch is statically resolved — the field confirmed it: on
clean subgraphs the orchestrator took every derived row and the obligations
SATISFY. They fall silent at exactly one kind of place: a framework dispatch
boundary. Two shapes, both measured under both RTA and VTA, both genuinely
unprovable (D-CX10 §4):

- **Handler stored as a func value in a router** (`r.handle(pubHandler)`) — the
  real chi/oapi shape. The handler's address is taken, so it can be invoked from
  framework code the analyzed unit cannot see. Address-taking is a true
  soundness boundary: the analysis cannot enumerate who calls the handler or
  what ran first.
- **Shared middleware** (`mw.Serve`→`next.Serve`, `next` resolving back through
  `mw`) — a self-referential dispatch cycle the resolution must guard against
  (recursion guard, D-CX1).

In both, across the dispatch the analysis **cannot prove what precedes the
handler**. A must-precede whose two ends straddle that line is asking a question
static analysis can't answer on this code — so it correctly abstains.

This is *not* the "the call site is merely wide" case. A shared middleware whose
ten callees are all enumerated is fully resolved, and the engine already reasons
soundly over it (it requires the property to hold for *all* candidates). The
abstention here is about the boundary being real, not about fan-out width.

## What to do instead

Anchor both endpoints in the **resolved cone** — the handler body and the
functions it calls. Put the `require` and the `before` where one provably
dominates the other within a single statically-resolved region.

```yaml
# ABSTAINS (UNKNOWN): the require is registered up in the wrapper/middleware,
# the publish is only reached through the router→handler→doPublish→publishWithFanout
# dispatch. The analysis cannot prove the ordering across that hop.
- name: validate-before-publish-spanning   # don't author this shape
  require: "example.com/svc/middleware#Authn"
  before:  "example.com/svc/internal/bus#Publish"

# SATISFIES (when it holds): both ends live inside the handler's resolved cone,
# so domination is provable one frame at a time.
- name: audit-before-publish
  require: "example.com/svc/internal/audit#Write"
  before:  "example.com/svc/internal/bus#Publish"
```

If the property you actually want to assert genuinely lives across the dispatch
boundary (e.g. "auth middleware must run before any handler publishes"), that is
a real ordering question — but it is **not** something the single-service static
graph can prove, and a must-precede rule is the wrong tool for it. State it as a
documented assumption (the same shape the CX-5 broker declaration uses), not as
a gating rule that will read UNKNOWN forever.

## What will *not* change this

Two levers were proposed and measured against the abstention; neither helps
(D-CX10 §3–4):

- **`--algo vta`** halves the spurious fan-out (22→10 callees at the shared
  site) and is a real precision win for callee-counting and blind-spot noise —
  but it does **not** unlock the lift. The handler-as-func-value boundary
  (address taken) and the self-referential middleware cycle abstain under VTA
  too. Use `--algo vta` for precision, not to rescue a boundary-spanning rule.
- **Router-aware middleware-chain recovery** would not change the truth either:
  a shared middleware's `next` reaches every handler regardless of how perfectly
  the chain is modeled, and the dominant real shape (address-taken handler)
  remains opaque even with a perfect chain.

The honest framing: the lifts are a **resolved-cone instrument**. Anchor your
rules inside the cone and they pay off; span them across the framework boundary
and they will, correctly, fall silent.
