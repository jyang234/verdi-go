package fitness

import (
	"fmt"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// Obligation statuses as flowmap emits them (the obligations section of
// graph.json). Kept as string constants here rather than imported: the graph
// JSON is the interface between the two programs, decoded independently.
const (
	obligationViolated  = "VIOLATED"
	obligationCantProve = "CANT-PROVE"
	obligationUnmatched = "UNMATCHED"
	obligationSatisfied = "SATISFIED"
)

// checkObligations judges the path-obligation verdicts flowmap computed from
// each function's SSA CFG (the rules live in .flowmap.yaml, where the SSA is;
// groundwork only judges). VIOLATED fails the gate; CANT-PROVE is the graph
// abstaining, disclosed; UNMATCHED means the rule's anchor matches nothing —
// an inert guardrail that must not be mistaken for protection. SATISFIED is
// the desired state and produces no finding. Finding identity is (rule, fn,
// site) — summaries are built from those fields only, so re-worded detail
// prose never makes an old finding look new.
func checkObligations(_ *policy.Policy, ix *graph.Index, r *Result) {
	for _, o := range ix.Obligations() {
		switch o.Status {
		case obligationViolated:
			r.add(Finding{
				Rule:     "obligation",
				Severity: Violation,
				Summary:  fmt.Sprintf("%s: %s at %s", o.Rule, o.Kind, ShortName(o.Fn)),
				From:     o.Fn,
				To:       o.Site,
				Detail:   o.Detail,
			})
		case obligationCantProve:
			r.add(Finding{
				Rule:     "obligation",
				Severity: Caution,
				Summary:  fmt.Sprintf("%s: cannot prove at %s", o.Rule, ShortName(o.Fn)),
				From:     o.Fn,
				To:       o.Site,
				Detail:   o.Detail,
			})
		case obligationUnmatched:
			r.add(Finding{
				Rule:     "obligation",
				Severity: Caution,
				Summary:  fmt.Sprintf("%s: rule matches nothing — inert guardrail", o.Rule),
				Detail:   o.Detail,
			})
		case obligationSatisfied:
			// The desired state: the universal proof. No finding.
		default:
			// Fail closed on vocabulary drift: a status this judge does not
			// recognize must never read as a pass. flowmap and groundwork decode
			// the graph independently (deliberately, across the trust boundary),
			// so a renamed or added status on the producer side arrives here as
			// an arbitrary string — surface it instead of falling through.
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
