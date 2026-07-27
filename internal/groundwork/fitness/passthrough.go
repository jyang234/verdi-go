package fitness

import (
	"fmt"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/groundwork/facts"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// checkMustPassThrough evaluates each waypoint invariant: every path from a
// From-matching function to a To-matching target (function or boundary effect)
// must pass through a Through-matching function. The check removes the waypoint
// nodes from the walk; any From→To path that remains is a bypass — a Violation
// naming the (source, target) pair, with one shortest bypass path as detail.
//
// Unlike must_not_reach's single witness, every unallowed bypass pair is
// reported: the base-vs-branch "new findings only" diff must surface a second
// bypass added on a branch even when the base already carries one.
//
// Three-valued like must_not_reach: no bypass over a blind frontier is a
// Caution ("cannot prove every path is guarded"), escalated by require_proof.
func checkMustPassThrough(p *policy.Policy, ix *graph.Index, r *Result) {
	for i := range p.MustPassThrough {
		rule := &p.MustPassThrough[i]
		throughLabel := shortPatterns(rule.Through)
		allow := make([]facts.AllowPair, len(rule.Allow))
		for i, exception := range rule.Allow {
			allow[i] = facts.AllowPair{From: exception.From, To: exception.To}
		}
		fact := facts.EvaluatePassThrough(ix, facts.PassThroughInput{
			From: rule.From, To: rule.To, Through: rule.Through, Allow: allow,
		})
		// Standing policy is graded on the bound FAMILY, exactly as checkMustNotReach
		// grades reach: a rule is inert only when its From binds nothing at all, and
		// its target is unbindable only when the whole To family binds nothing. The
		// fact's Unbound* fields are per-SELECTOR (a family that binds through one
		// selector still names its dead ones) and reading them here would drop the
		// live selectors' real violations.
		if len(fact.From) == 0 {
			r.add(inertRuleFinding("must_pass_through", rule.Name, rule.RequireProof))
			continue
		}
		if len(fact.To) == 0 {
			r.add(unbindableTargetFinding("must_pass_through", rule.Name, "to", rule.RequireProof))
			continue
		}

		for _, bypass := range fact.BypassOccurrences {
			r.add(Finding{
				Rule:     "must_pass_through",
				Severity: Violation,
				Summary:  fmt.Sprintf("%s: %s reaches %s without passing %s", rule.Name, ShortName(bypass.From), shortTarget(bypass.To), throughLabel),
				From:     bypass.From,
				To:       bypass.To,
				Detail:   renderBypassPath(bypass.Path),
			})
		}
		if fact.State == facts.PassThroughBlind {
			sev, note := Caution, "cannot prove every path is guarded"
			if rule.RequireProof {
				sev, note = Violation, "require_proof is set and guarding cannot be proven"
			}
			r.add(Finding{
				Rule:     "must_pass_through",
				Severity: sev,
				Summary:  fmt.Sprintf("%s: no bypass found, but the frontier is blind (%s) — %s", rule.Name, blindDescription(fact.Blind), note),
				From:     fact.Blind.From,
			})
		}
	}
}

// guardedWalk is the compatibility adapter used by the proposal lens. Facts
// owns the waypoint-removal traversal.
func guardedWalk(ix *graph.Index, from string, through []string) (cone []string, parent map[string]string) {
	return facts.GuardedWalk(ix, from, through)
}

// renderBypassPath renders the witness path a reviewer reads to see HOW the
// guard is skipped. Each hop goes through shortTarget, the single owner of the
// FQN-vs-boundary-label distinction, so the path and the summary cannot drift
// apart on how they spell the same effect.
func renderBypassPath(path []string) string {
	parts := make([]string, len(path))
	for i, value := range path {
		parts[i] = shortTarget(value)
	}
	return strings.Join(parts, " → ")
}

// shortPatterns renders Through patterns compactly for summaries.
func shortPatterns(patterns []string) string {
	out := make([]string, len(patterns))
	for i, p := range patterns {
		out[i] = ShortName(p)
	}
	return strings.Join(out, ", ")
}
