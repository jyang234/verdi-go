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
// Caution, escalated by require_proof. The state fold is TOTAL for the same
// reason checkMustNotReach's is: the clean arm is silent, so a state this gate
// has not been taught must be disclosed rather than fall through as a clean hold.
func checkNoConcurrentReach(p *policy.Policy, surface facts.ConcurrentSurface, r *Result) {
	for _, rule := range p.NoConcurrentReach {
		// Parity with must_not_reach (reach.go): a To that binds nothing ANYWHERE in
		// the graph is a dead selector (a typo'd or stale label), not a proof the
		// concurrent cone is clean. Disclose it like an unbindable must_not_reach
		// target — a Caution by default, escalated under require_proof — so a guard
		// that quietly stopped existing is loud, not silently "enforced".
		fact := surface.Evaluate(rule.To)
		// TOTAL over ConcurrentState, for the reason checkMustNotReach's switch is:
		// an if-chain that names only Unbound and Blind emits nothing for a state it
		// has not been taught, and the rule then reports as a clean hold — a silent
		// pass in a live gate (tenet 4).
		switch fact.State {
		case facts.ConcurrentUnbound:
			r.add(unbindableTargetFinding("no_concurrent_reach", rule.Name, "to", rule.RequireProof))
		case facts.ConcurrentHit:
			for _, hit := range fact.Hits {
				// shortTarget, NOT ShortName: a concurrent target is a function FQN or a
				// boundary label, and only the FQN may be shortened. See shortTarget's
				// doc for what ShortName does to a label, and why Summary being part of
				// Finding.Key() makes that more than a cosmetic problem.
				r.add(Finding{
					Rule:     "no_concurrent_reach",
					Severity: Violation,
					Summary:  fmt.Sprintf("%s: %s reachable on a concurrent path", rule.Name, shortTarget(hit.To)),
					From:     hit.From,
					To:       hit.To,
				})
			}
		case facts.ConcurrentBlind:
			sev, note := Caution, "cannot prove the concurrent cone avoids the target"
			if rule.RequireProof {
				sev, note = Violation, "require_proof is set and avoidance cannot be proven"
			}
			r.add(Finding{
				Rule:     "no_concurrent_reach",
				Severity: sev,
				Summary:  fmt.Sprintf("%s: no concurrent path found, but the frontier is blind (%s) — %s", rule.Name, blindDescription(fact.Blind), note),
			})
		case facts.ConcurrentClean:
			// No target on the visible concurrent surface: nothing to report. What
			// "visible" excludes is disclosed at facts.ConcurrentClean.
		default:
			r.add(unrecognizedStateFinding("no_concurrent_reach", rule.Name,
				fmt.Sprintf("concurrent state %d", fact.State), rule.RequireProof))
		}
	}
}
