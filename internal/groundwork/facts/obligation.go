package facts

import (
	"sort"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// Obligation statuses as flowmap emits them in the obligations section of
// graph.json. This is the single source of truth for the vocabulary: fitness
// and claims both classify through ClassifyObligationStatus rather than
// comparing these strings themselves, so the two judges cannot drift apart on
// what a producer verdict means. They stay string constants rather than a
// decoded enum because the graph JSON is the interface between two independently
// decoding programs — an unrecognized status must survive as an arbitrary string
// all the way to a disclosure, never be rejected at decode.
const (
	ObligationStatusViolated  = "VIOLATED"
	ObligationStatusCantProve = "CANT-PROVE"
	ObligationStatusUnmatched = "UNMATCHED"
	ObligationStatusSatisfied = "SATISFIED"
)

// ObligationState is the closed interpretation of one obligation record, or of
// every record an exact rule name matched.
type ObligationState uint8

const (
	// ObligationMissingData means the graph carries no obligations section, so
	// nothing about the rule can be read from it. Aggregate-only.
	ObligationMissingData ObligationState = iota
	// ObligationUnresolved means the section exists but names no such rule.
	// Aggregate-only: an absent rule is not a proven one.
	ObligationUnresolved
	// ObligationViolated means the producer disproved the obligation.
	ObligationViolated
	// ObligationUnknown means a producer status this groundwork does not
	// recognize. It fails closed: vocabulary drift is never read as a pass.
	ObligationUnknown
	// ObligationCantProve means the producer abstained.
	ObligationCantProve
	// ObligationUnmatched means the rule's anchor matched nothing — an inert
	// guardrail, never protection.
	ObligationUnmatched
	// ObligationSatisfied means the producer proved the obligation holds.
	ObligationSatisfied
)

// ObligationResult carries the dominant state for one exact rule name and every
// record that name matched, in canonical order.
type ObligationResult struct {
	State   ObligationState
	Name    string
	Records []graph.Obligation
}

// ClassifyObligationStatus maps one producer status to the state that single
// record contributes. It never returns ObligationMissingData or
// ObligationUnresolved: those describe a whole rule's evidence, not a record, and
// a caller that saw them here could mistake "this record is fine" for "this rule
// is proven" (guarded by TestClassifyObligationStatusNeverAggregates).
//
// Matching is on exact bytes. An unrecognized status — a renamed or added
// producer verdict arriving across the trust boundary — is ObligationUnknown,
// never silently folded into the nearest known pole (tenet 2).
func ClassifyObligationStatus(status string) ObligationState {
	switch status {
	case ObligationStatusViolated:
		return ObligationViolated
	case ObligationStatusCantProve:
		return ObligationCantProve
	case ObligationStatusUnmatched:
		return ObligationUnmatched
	case ObligationStatusSatisfied:
		return ObligationSatisfied
	default:
		return ObligationUnknown
	}
}

// EvaluateObligation aggregates every record whose Rule is exactly name. A
// prefix or case variant is a different obligation and is not folded in.
//
// Dominance is ordered so that the strongest evidence wins and abstention can
// never be laundered into a proof: a concrete VIOLATED already disproves the
// rule even if a sibling record abstains; an unrecognized status outranks a
// known abstention because its meaning is unavailable, not merely unproven; and
// SATISFIED is reachable only when every matched record proved it.
//
// An empty obligations section is ObligationMissingData whether it was omitted
// or present-but-empty. The decoder DOES preserve that difference (an omitted
// section decodes to a nil slice, "obligations": [] to an empty non-nil one), so
// the fold is a deliberate choice, not a limitation: ObligationUnresolved is
// defined as "the section names other rules but not this one", which carries the
// real information that the producer evaluated obligations and this rule was not
// among them. An empty section carries no such evidence, so it reads as missing
// data. Both are ERRORs either way — neither can be mistaken for a proof of
// absence. Pinned by TestEvaluateObligationEmptySectionIsMissingData.
//
// Records are copied before sorting; the caller's graph is not mutated.
func EvaluateObligation(ix *graph.Index, name string) ObligationResult {
	result := ObligationResult{State: ObligationMissingData, Name: name}
	if ix == nil || len(ix.Obligations()) == 0 {
		return result
	}

	for _, record := range ix.Obligations() {
		if record.Rule == name {
			result.Records = append(result.Records, record)
		}
	}
	if len(result.Records) == 0 {
		result.State = ObligationUnresolved
		return result
	}
	sortObligations(result.Records)

	var sawUnknown, sawCantProve, sawUnmatched bool
	for _, record := range result.Records {
		switch ClassifyObligationStatus(record.Status) {
		case ObligationViolated:
			result.State = ObligationViolated
			return result
		case ObligationUnknown:
			sawUnknown = true
		case ObligationCantProve:
			sawCantProve = true
		case ObligationUnmatched:
			sawUnmatched = true
		}
	}
	switch {
	case sawUnknown:
		result.State = ObligationUnknown
	case sawCantProve:
		result.State = ObligationCantProve
	case sawUnmatched:
		result.State = ObligationUnmatched
	default:
		result.State = ObligationSatisfied
	}
	return result
}

// sortObligations orders records on their complete intrinsic tuple, so a tie on
// one field never leaves producer emission order deciding the witness order.
func sortObligations(records []graph.Obligation) {
	sort.Slice(records, func(i, j int) bool {
		left, right := records[i], records[j]
		if left.Rule != right.Rule {
			return left.Rule < right.Rule
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Fn != right.Fn {
			return left.Fn < right.Fn
		}
		if left.Site != right.Site {
			return left.Site < right.Site
		}
		if left.Status != right.Status {
			return left.Status < right.Status
		}
		return left.Detail < right.Detail
	})
}
