# Investigation: the HTTP-wrapper HighFanOut that gates the CX lifts (D-CX10)

**Status:** investigated, measured, recommendation below. No engine change shipped
yet — the deeper fix is a framework-wide decision for the owner (§4).

The second field run (correctness-expansion-plan §2c) found the interprocedural
lifts (CX-2 entry domination, CX-3 effect derivation) abstaining at one
chokepoint: `doPublish`/`publishWithFanout`, a chi/oapi wrapper that is a
HighFanOut blind spot. The lifts are sound — they refuse to over-claim through a
disclosed frontier — so the question (the field's ask #2) is whether the
frontier can be *shrunk*, and how. This note answers it with a measurement, not
a guess.

## 1. The mechanism (verified in code)

- **HighFanOut** (`internal/static/blindspots/blindspots.go:141-158`) flags any
  single SSA call site whose distinct in-graph callee count exceeds the
  threshold (default 8), attributed to the containing function.
- **The wrapper site.** A shared middleware's `next.ServeHTTP(w, r)` is one
  invoke instruction on an `http.Handler` interface value. The same middleware
  wraps every route, so `next` is bound, across the program, to *every* handler.
- **Root discovery deliberately skips middleware**
  (`internal/static/roots/roots.go`, `variadicLastFunc`): a route is rooted at
  its final handler only; the `Use(...)` stack and chain order are never
  recovered. So the route→handler endpoints exist (`Entrypoints`), but the
  middleware *chain* — what a router-aware refinement would need — does not.
- **VTA exists, unexposed.** `internal/static/callgraph/callgraph.go` has
  `AlgoVTA` (RTA-seeded variable-type analysis); `flowmap graph` always uses the
  `Options{}` zero value, RTA.

## 2. The measurement (`callgraph/wrapperfanout_test.go`, committed, deterministic)

A faithful reproduction — one shared `mw{next Handler}` whose `Serve` calls
`next.Serve()`, wrapping ten handlers `H0..H9`, the oapi/chi shape — measured at
the `next.Serve()` site:

| algorithm | distinct callees at the shared site |
|---|---|
| **RTA** (default) | **22** |
| **VTA** (refine) | **10** |

Two facts, both load-bearing:

1. **VTA materially helps.** It removes the 12 spurious candidates RTA carries —
   the `mw` type itself (which implements `Handler` and flows to the interface)
   and the SSA method-value/wrapper thunks — landing on exactly the ten real
   handlers `next` can hold. Soundly: VTA tracks which concrete types flow to
   `next` and finds only `H0..H9`.
2. **Neither prunes below ten.** At a genuinely-shared middleware, `next`
   *dynamically does* range over all ten handlers. The width is real program
   behavior, not over-approximation. With threshold 8, the blind spot **still
   fires under VTA** (10 > 8).

## 3. What this rules in and out

- **Router-aware chain recovery is NOT the lever.** It needs infrastructure that
  doesn't exist (middleware-stack extraction, per-router and brittle), and the
  measurement shows it wouldn't change the truth anyway: a shared middleware's
  `next` reaches every handler regardless of how perfectly the chain is modeled.
  Recovery would only help the *distinct-wrapper-per-route* shape, which the
  shared-middleware oapi/chi pattern is not. Drop it.
- **VTA is a real, cheap win — but partial.** It roughly halves the fan-out and
  removes the junk candidates, so borderline sites (real callee count ≤
  threshold, inflated by RTA over the line) would clear and un-blind their cones
  for the lifts. It will not rescue a middleware that genuinely fans to more than
  `threshold` handlers.
- **The deeper conflation.** HighFanOut bundles two different things under one
  "the call graph may be over-approximated here" disclosure:
  - **truly blind** — reflect, `<dynamic>` boundary, unresolved invoke: edges are
    *hidden*; reasoning through it is unsound (the lifts must abstain);
  - **wide but fully resolved** — a shared middleware whose ten callees are all
    *enumerated*: nothing is hidden; the callee set is complete (a sound superset
    under RTA, the exact set under VTA). Reasoning through it is *not* unsound —
    the summary engine already requires a property to hold for *all* candidates,
    which is exactly right here. The lift abstains only because the blind-spot
    label does not distinguish the two cases.

## 4. Recommendation

Two moves, in order of cost:

1. **Expose `--algo` on `flowmap graph` (small, measured benefit).** Thread
   `callgraph.Options{Algo: …}` through `analyze.Analyze`. Default stays RTA
   (determinism, golden discipline unchanged); a dense adopter trades CI time for
   tighter cones. The measurement justifies it: 22→10 at the canonical site, and
   the removed candidates are pure noise. This is the field's offered experiment,
   now warranted by data. Add the shared-middleware fixture's RTA/VTA delta as a
   golden so the benefit is locked.

2. **Split the HighFanOut disclosure — `resolved-wide` vs `blind` — (the real
   fix, owner's decision).** A site whose every callee is a known in-graph
   function, with no reflect/`<dynamic>`/unresolved component, is *resolved-wide*:
   the lifts and groundwork's frontier checks can soundly reason over its full
   candidate set instead of abstaining. This would let the lifts fire at exactly
   the field's chokepoint *if* `doPublish`'s fan-out is fully enumerated (likely,
   for a chi/oapi wrapper with no reflection). It changes what "blind spot" means
   across the whole framework, so it ships as its own plan with both-ways
   fixtures per the extension recipe — not as a rider here. The honest caveat:
   "wide but resolved" still means the lift must hold for *all* N handlers, so a
   guard rule will SATISFY only if every wrapped route is genuinely guarded —
   which is the correct, useful answer, not an abstention.

**Not recommended:** crediting a lift *through* a HighFanOut entry without
resolving it (the literal reading of the field's ask #2). At a truly-blind site
that is unsound (D-CX2); at a resolved-wide site the right move is to stop
calling it blind (move 2), not to wave the lift through. Either way, "credit
through the frontier" is the wrong framing — the measurement is why.
