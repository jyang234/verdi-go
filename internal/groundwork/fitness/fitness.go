package fitness

import (
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// Check evaluates every configured invariant in p against the graph ix and
// returns the findings, sorted deterministically (violations first). The result
// is a pure function of (policy, graph): the same inputs always produce the same
// findings in the same order, which is what lets a green verdict be a hard,
// reproducible CI gate.
func Check(p *policy.Policy, ix *graph.Index) Result {
	var r Result
	checkSubstrate(p, ix, &r)
	checkLayering(p, ix, &r)
	checkMustNotReach(p, ix, &r)
	checkMustPassThrough(p, ix, &r)
	checkNoConcurrentReach(p, ix, &r)
	checkIOBudget(p, ix, &r)
	checkObligations(p, ix, &r)
	r.sort()
	return r
}

// checkSubstrate flags a policy-vs-graph algorithm mismatch as a Caution: the
// policy was proposed on one call-graph algorithm but is being checked against a
// graph built on another. It is advisory, not blocking — the algorithms are all
// sound, so nothing is unprovable; they differ in PRECISION, so a coarser graph
// can surface reachability findings a refined one ruled out. Surfacing the
// mismatch where those findings appear lets the reader read them as an analyzer
// artifact rather than a regression (the field footgun: a vta-proposed policy
// swept with the rta default produced 40 spurious must_not_reach violations).
func checkSubstrate(p *policy.Policy, ix *graph.Index, r *Result) {
	if note := graph.SubstrateMismatchCaveat(p.Substrate, ix.Algo()); note != "" {
		r.add(Finding{Rule: "substrate", Severity: Caution, Summary: note})
	}
}
