package fitness

import (
	"fmt"

	"github.com/jyang234/golang-code-graph/internal/groundwork/facts"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// verdict is the compatibility form of facts.ReachState retained for proposer
// tests and later-migrated fitness code.
type verdict int

const (
	provenAbsent verdict = iota // no path, and the frontier is fully resolved — a real proof
	noPathFound                 // no path found, but the frontier is blind — cannot prove
	reachable                   // a path exists — the invariant is broken
)

// checkMustNotReach evaluates each negative reachability invariant: no function
// matching rule.From may transitively reach any target (function or boundary
// effect) matching rule.To. A reachable path is a Violation; an unprovable rule
// (no path, but a blind frontier) is a Caution naming where the graph went blind.
func checkMustNotReach(p *policy.Policy, ix *graph.Index, r *Result) {
	for _, rule := range p.MustNotReach {
		fact := facts.EvaluateReach(ix, rule.From, rule.To)
		switch fact.State {
		case facts.ReachUnbound:
			if len(fact.From) == 0 {
				r.add(inertRuleFinding("must_not_reach", rule.Name, rule.RequireProof))
				continue
			}
			r.add(unbindableTargetFinding("must_not_reach", rule.Name, "to", rule.RequireProof))
		case facts.ReachFound:
			witness := fact.Paths[0]
			r.add(Finding{
				Rule:     "must_not_reach",
				Severity: Violation,
				Summary:  fmt.Sprintf("%s: %s reaches %s", rule.Name, ShortName(witness.From), witness.To),
				From:     witness.From,
				To:       witness.To,
			})
		case facts.ReachBlind:
			// Unprovable: no static path, but the frontier is blind. Advisory by
			// default; a require_proof rule treats unprovability as a failure.
			sev, note := Caution, "cannot prove absence"
			if rule.RequireProof {
				sev, note = Violation, "require_proof is set and absence cannot be proven"
			}
			r.add(Finding{
				Rule:     "must_not_reach",
				Severity: sev,
				Summary:  fmt.Sprintf("%s: no path found, but the frontier is blind (%s) — %s", rule.Name, blindDescription(fact.Blind), note),
				From:     fact.Blind.From,
			})
		case facts.ReachAbsent:
			// A real proof: nothing to report. Absence is the desired state.
		}
	}
}

// inertRuleFinding discloses a rule whose From binds nothing in this graph —
// the guard quietly stopped existing (a typo'd FQN, or a package renamed out
// from under the pattern), and the zero-seed walk would otherwise report it as
// a clean provenAbsent pass forever. A Caution by default; require_proof
// escalates to Violation — a rule that cannot even be evaluated is the
// strongest form of unprovability.
func inertRuleFinding(kind, name string, requireProof bool) Finding {
	sev, note := Caution, "inert rule"
	if requireProof {
		sev, note = Violation, "require_proof is set and an inert rule guards nothing"
	}
	return Finding{
		Rule:     kind,
		Severity: sev,
		Summary:  fmt.Sprintf("%s: from binds nothing in this graph — %s", name, note),
	}
}

// unbindableTargetFinding discloses a rule whose To/Through selector matches no
// node and no boundary effect ANYWHERE in the graph. The original design
// treated an empty To as the success state ("the forbidden thing does not
// exist"), but a real field run showed that is unsound: the graph cannot tell
// "the forbidden thing does not exist" from "the sink is unnameable" (a
// third-party logger whose methods are not graph nodes). A rule guarding an
// unnameable sink reports HOLDS forever while the unsafe call sits one line
// away — the exact silent pass the framework exists to prevent. So an
// unbindable target is disclosed like an inert From: a Caution by default,
// escalated to Violation under require_proof. (A To that DOES bind somewhere
// but is simply unreached stays a real proof — that is provenAbsent, untouched.)
func unbindableTargetFinding(kind, name, field string, requireProof bool) Finding {
	sev, note := Caution, "name a first-party sink it can bind, or this invariant is vacuous"
	if requireProof {
		sev, note = Violation, "require_proof is set and an unbindable target cannot be proven absent"
	}
	return Finding{
		Rule:     kind,
		Severity: sev,
		Summary:  fmt.Sprintf("%s: %s binds nothing in this graph — %s", name, field, note),
	}
}

// bindsAnyTarget is the compatibility wrapper for later-migrated fitness
// evaluators. facts owns target binding.
func bindsAnyTarget(ix *graph.Index, patterns []string) bool {
	return len(facts.BindTargets(ix, patterns)) > 0
}

// evidence carries the witness for a verdict: for reachable, the from function
// and the matched target; for noPathFound, a from function and the blind site.
type evidence struct {
	from   string
	target string
}

// evalReach is the compatibility wrapper for proposer tests and later-migrated
// fitness code. The supplied source identities are already bound; facts owns the
// traversal, target binding, and blind-frontier classification.
func evalReach(ix *graph.Index, froms []string, toPatterns []string) (verdict, evidence) {
	fact := facts.EvaluateReachBoundSources(ix, froms, toPatterns)
	switch fact.State {
	case facts.ReachFound:
		witness := fact.Paths[0]
		return reachable, evidence{from: witness.From, target: witness.To}
	case facts.ReachBlind:
		return noPathFound, evidence{from: fact.Blind.From, target: blindDescription(fact.Blind)}
	default:
		return provenAbsent, evidence{}
	}
}

// frontierBlindSiteWith is the compatibility presentation wrapper for fitness
// evaluators whose complete fact families move in later tasks.
func frontierBlindSiteWith(ix *graph.Index, cone []string, effects []graph.Edge) (string, bool) {
	from := ""
	if len(cone) > 0 {
		from = cone[0]
	}
	witness := facts.BlindFrontier(ix, from, cone, effects)
	if witness == nil {
		return "", false
	}
	return blindDescription(witness), true
}

func blindDescription(witness *facts.BlindWitness) string {
	if witness == nil {
		return ""
	}
	switch witness.Location {
	case facts.BlindAtFunction:
		return fmt.Sprintf("%s at %s", witness.Kind, ShortName(witness.Site))
	case facts.BlindInPackage:
		return fmt.Sprintf("%s in %s", witness.Kind, witness.Site)
	case facts.BlindAtDynamicEffect:
		return "unresolved boundary effect " + witness.Detail
	case facts.BlindAtConcurrentBoundary:
		return "unresolved concurrent boundary effect " + witness.Detail
	case facts.BlindAtConcurrentDispatch:
		return fmt.Sprintf("%s at %s", witness.Kind, ShortName(witness.Site))
	default:
		return witness.Kind
	}
}
