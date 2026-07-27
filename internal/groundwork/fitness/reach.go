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
	// noPathFound is the abstention: no path was found AND the absence is not
	// proven. Two inputs reach it — a blind frontier (evidence names the blind
	// site) and a source or target family that bound nothing (evidence is empty,
	// because there is nothing to point at). Both are "cannot prove", never a pass.
	noPathFound
	reachable // a path exists — the invariant is broken
)

// checkMustNotReach evaluates each negative reachability invariant: no function
// matching rule.From may transitively reach any target (function or boundary
// effect) matching rule.To. A reachable path is a Violation; an unprovable rule
// (no path, but a blind frontier) is a Caution naming where the graph went blind.
//
// The state switch is TOTAL: exactly one arm is silent (facts.ReachAbsent, the
// real proof), and a state this gate has not been taught reaches the default and
// is disclosed. A bare switch would emit nothing for such a state and the rule
// would read as a clean hold — a silent pass in a live gate (tenet 4).
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
				From:     blindFrom(fact.Blind),
			})
		case facts.ReachAbsent:
			// A real proof: nothing to report. Absence is the desired state.
		default:
			r.add(unrecognizedStateFinding("must_not_reach", rule.Name,
				fmt.Sprintf("reach state %d", fact.State), rule.RequireProof))
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

// unrecognizedStateFinding discloses a typed fact state this groundwork's judges
// have never been taught — a later facts release adding a state, arriving at a
// switch written before it existed. Every other arm of those switches decides a
// verdict, so a bare fall-through emits NOTHING and the rule reports as a clean
// hold: a silent pass in a live gate, the worst outcome (tenet 4). It abstains
// rather than panicking, because the prime directive prefers a disclosed
// abstention the caller surfaces to a crash. Caution by default, escalated to
// Violation under require_proof — the same escalation every other unprovable
// disposition in this package uses. It is the fitness-side twin of
// checkObligations' vocabulary-drift default, and of the "evaluator returned an
// unknown state" ERROR the claims judge emits for the same condition.
//
// state is the human name of the offending value ("reach state 7"); it is not
// part of a Severity decision, only of the disclosure.
func unrecognizedStateFinding(kind, name, state string, requireProof bool) Finding {
	sev, note := Caution, "upgrade or investigate"
	if requireProof {
		sev, note = Violation, "require_proof is set and an unrecognized state cannot be proven"
	}
	return Finding{
		Rule:     kind,
		Severity: sev,
		Summary:  fmt.Sprintf("%s: %s is not understood by this groundwork — %s", name, state, note),
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
//
// Every ReachState is mapped explicitly. ReachUnbound must NOT reach the proof
// pole: a family that bound nothing produced a zero-seed or zero-target walk, and
// calling that "no path exists" is a fabricated proof over a surface the caller
// never actually named (tenet 4). It abstains as noPathFound with empty evidence
// — the standing fitness path discloses the same condition through
// inertRuleFinding/unbindableTargetFinding before it ever gets here.
func evalReach(ix *graph.Index, froms []string, toPatterns []string) (verdict, evidence) {
	fact := facts.EvaluateReachBoundSources(ix, froms, toPatterns)
	switch fact.State {
	case facts.ReachFound:
		witness := fact.Paths[0]
		return reachable, evidence{from: witness.From, target: witness.To}
	case facts.ReachBlind:
		return noPathFound, evidence{from: blindFrom(fact.Blind), target: blindDescription(fact.Blind)}
	case facts.ReachUnbound:
		return noPathFound, evidence{}
	case facts.ReachAbsent:
		return provenAbsent, evidence{}
	default:
		// A ReachState this wrapper has never seen cannot be folded into a pole:
		// the unknown could be either, and guessing is how a false proof ships.
		panic(fmt.Sprintf("fitness: unhandled facts.ReachState %d in evalReach", fact.State))
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

// blindFrom is the nil-safe accessor for a blind witness's source, the twin of
// blindDescription. The fact types document Blind as non-nil exactly when the
// state is the blind one, but every fitness site that renders a blind finding
// reads TWO fields off the same pointer, so a producer bug would panic in the
// middle of a gate on the second one after blindDescription had already absorbed
// it. Both accessors degrade to "" instead: the finding still carries its
// Caution/Violation severity, so a broken witness abstains loudly rather than
// crashing — and can never become a pass. The claims judge closes the same hole
// by ERRORing (claims.evalReach's "blind state without evidence"); this is the
// fitness-side symmetry.
func blindFrom(witness *facts.BlindWitness) string {
	if witness == nil {
		return ""
	}
	return witness.From
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
