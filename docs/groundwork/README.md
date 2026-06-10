# groundwork

`groundwork` is a deterministic consumer of flowmap's static call graph: it turns
the non-gated "what can happen" map into computed verification artifacts —
architectural fitness functions, blast-radius/impact, pre-merge gates, and
unfakeable MR review artifacts. No AI sits in any verdict; every output is a pure
function of (graph, policy, delta), inheriting flowmap's human-as-oracle model.

This directory is the design record. **It is the source of truth; no code exists
yet.** Read in this order:

1. [`distilled-learnings.md`](distilled-learnings.md) — the core thesis, what the
   graph deterministically is and isn't, the failure modes a structural verdict
   leaves open, build-vs-buy, and the engineering lessons (the *why*).
2. [`mr-review-artifacts.md`](mr-review-artifacts.md) — the deterministic
   base-vs-branch review artifact and its anti-fake guarantee (one surface, in
   depth).
3. [`pressure-test.md`](pressure-test.md) — the adversarial analysis that
   corrected "unfakeable digest," surfaced the trusted-graph trust anchor, and
   made the verdict three-valued.
4. [`implementation-plan.md`](implementation-plan.md) — the plan-of-record:
   verified interface, package layout, phased build order, the four corrections
   from the plan's own pressure test, and why loansvc alone is an insufficient
   fixture.

## Relationship to flowmap

flowmap **produces** the graph (`flowmap graph <dir>`) and the gated boundary
contract (`flowmap boundary <dir>`). groundwork **consumes** those JSON files and
judges them. They are deliberately separate programs so CI can control which runs
where — the security boundary is around flowmap execution, not around groundwork.
