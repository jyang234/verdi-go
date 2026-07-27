package fitness

import (
	"fmt"

	"github.com/jyang234/golang-code-graph/internal/groundwork/facts"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// checkObligations judges the path-obligation verdicts flowmap computed from
// each function's SSA CFG (the rules live in .flowmap.yaml, where the SSA is;
// groundwork only judges). VIOLATED fails the gate; CANT-PROVE is the graph
// abstaining, disclosed; UNMATCHED means the rule's anchor matches nothing —
// an inert guardrail that must not be mistaken for protection. SATISFIED is
// the desired state and produces no finding. Finding identity is (rule, fn,
// site) — summaries are built from those fields only, so re-worded detail
// prose never makes an old finding look new.
//
// The status vocabulary is owned by facts.ClassifyObligationStatus, shared with
// the assert `obligation` claim so the two judges cannot drift on what a
// producer verdict means. Only the vocabulary is shared, NOT
// facts.EvaluateObligation: that aggregates per rule name and returns canonically
// sorted records, while fitness owes ONE finding PER RECORD in the graph's own
// order. Findings tie in Result.sort on everything but Detail, so re-driving this
// loop from sorted per-rule records would silently reorder two findings differing
// only in Detail — an output change with no verdict behind it. Dominance is a
// claims-side question; fitness discloses every record.
func checkObligations(_ *policy.Policy, ix *graph.Index, r *Result) {
	for _, o := range ix.Obligations() {
		switch facts.ClassifyObligationStatus(o.Status) {
		case facts.ObligationViolated:
			r.add(Finding{
				Rule:     "obligation",
				Severity: Violation,
				Summary:  fmt.Sprintf("%s: %s at %s", o.Rule, o.Kind, ShortName(o.Fn)),
				From:     o.Fn,
				To:       o.Site,
				Detail:   o.Detail,
			})
		case facts.ObligationCantProve:
			r.add(Finding{
				Rule:     "obligation",
				Severity: Caution,
				Summary:  fmt.Sprintf("%s: cannot prove at %s", o.Rule, ShortName(o.Fn)),
				From:     o.Fn,
				To:       o.Site,
				Detail:   o.Detail,
			})
		case facts.ObligationUnmatched:
			r.add(Finding{
				Rule:     "obligation",
				Severity: Caution,
				Summary:  fmt.Sprintf("%s: rule matches nothing — inert guardrail", o.Rule),
				Detail:   o.Detail,
			})
		case facts.ObligationSatisfied:
			// The desired state: the universal proof. No finding.
		default:
			// facts.ObligationUnknown, and any state a later facts release adds.
			// Fail closed on vocabulary drift: a status this judge does not
			// recognize must never read as a pass. flowmap and groundwork decode
			// the graph independently (deliberately, across the trust boundary),
			// so a renamed or added status on the producer side arrives here as
			// an arbitrary string — surface it instead of falling through. Staying
			// a default, not an ObligationUnknown case, keeps that true for a state
			// this switch has not been taught yet.
			r.add(Finding{
				Rule:     "obligation",
				Severity: Caution,
				Summary:  fmt.Sprintf("%s: status %q is not understood by this groundwork — upgrade or investigate", o.Rule, o.Status),
				From:     o.Fn,
				To:       o.Site,
			})
		}
	}
}
