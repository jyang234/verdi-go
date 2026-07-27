package fitness

import (
	"fmt"

	"github.com/jyang234/golang-code-graph/internal/groundwork/facts"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// checkNoConcurrentReach evaluates each concurrency invariant: no target
// matching To may be reached along a path entered via a concurrent edge (a
// go/defer call site). Two ways in: a concurrent boundary edge IS the target
// directly (`go publish(...)`), or a concurrently-spawned function's forward
// cone reaches the target.
//
// The supplied concurrent surface is rule-independent and computed once by
// Check; each rule only filters it. Findings are emitted from its canonical,
// de-duplicated (from, target) witnesses, so the same target reached via
// several spawn sites (or via both a direct concurrent edge and the cone's
// effects) is one finding, not a multiset that churns the base-vs-branch diff.
//
// Three-valued like the other reach checks: no hit over a blind frontier is a
// Caution, escalated by require_proof.
func checkNoConcurrentReach(p *policy.Policy, surface facts.ConcurrentSurface, r *Result) {
	for _, rule := range p.NoConcurrentReach {
		// Parity with must_not_reach (reach.go): a To that binds nothing ANYWHERE in
		// the graph is a dead selector (a typo'd or stale label), not a proof the
		// concurrent cone is clean. Disclose it like an unbindable must_not_reach
		// target — a Caution by default, escalated under require_proof — so a guard
		// that quietly stopped existing is loud, not silently "enforced".
		fact := surface.Evaluate(rule.To)
		if fact.State == facts.ConcurrentUnbound {
			r.add(unbindableTargetFinding("no_concurrent_reach", rule.Name, "to", rule.RequireProof))
			continue
		}

		for _, hit := range fact.Hits {
			r.add(Finding{
				Rule:     "no_concurrent_reach",
				Severity: Violation,
				Summary:  fmt.Sprintf("%s: %s reachable on a concurrent path", rule.Name, ShortName(hit.To)),
				From:     hit.From,
				To:       hit.To,
			})
		}

		if fact.State == facts.ConcurrentBlind {
			sev, note := Caution, "cannot prove the concurrent cone avoids the target"
			if rule.RequireProof {
				sev, note = Violation, "require_proof is set and avoidance cannot be proven"
			}
			r.add(Finding{
				Rule:     "no_concurrent_reach",
				Severity: sev,
				Summary:  fmt.Sprintf("%s: no concurrent path found, but the frontier is blind (%s) — %s", rule.Name, blindDescription(fact.Blind), note),
			})
		}
	}
}
