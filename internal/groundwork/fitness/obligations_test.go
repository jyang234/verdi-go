package fitness

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// The obligsvc golden carries one obligation verdict per shape (path-obligations
// plan §7); groundwork judges them: VIOLATED → violation, CANT-PROVE and
// UNMATCHED → caution, SATISFIED → nothing.
func TestObligationsJudged(t *testing.T) {
	g := loadGraph(t, "obligsvc.graph.json")
	res := Check(&policy.Policy{Service: "obligsvc", Version: 1}, graph.NewIndex(g))

	v := res.Violations()
	if len(v) != 3 {
		t.Fatalf("want 3 obligation violations (Transfer leak, DisburseRacy, DeferredPublish), got %v", v)
	}
	froms := map[string]bool{}
	for _, f := range v {
		if f.Rule != "obligation" {
			t.Errorf("violation rule = %q, want obligation", f.Rule)
		}
		froms[f.From] = true
	}
	for _, want := range []string{
		"example.com/obligsvc/internal/app.Transfer",
		"example.com/obligsvc/internal/app.DisburseRacy",
		"example.com/obligsvc/internal/app.DeferredPublish",
	} {
		if !froms[want] {
			t.Errorf("violations name %v, missing %s", froms, want)
		}
	}

	// CX-1/CX-2: the lift shapes land as cautions — sendFanoutOpen's
	// unproven entry, sendFanoutTaken's taken address, TransferMaybeHelper's
	// unprovable handoff — never as minted or borrowed verdicts.
	c := res.Cautions()
	if len(c) != 6 {
		t.Fatalf("want 6 obligation cautions (5x CANT-PROVE, UNMATCHED), got %v", c)
	}
	var inert, cantProve bool
	for _, f := range c {
		if strings.Contains(f.Summary, "inert guardrail") {
			inert = true
		}
		if strings.Contains(f.Summary, "cannot prove") {
			cantProve = true
		}
	}
	if !inert || !cantProve {
		t.Errorf("cautions = %v, want one inert-rule and one cannot-prove", c)
	}
}

// SATISFIED is the proof: it must produce no finding at all.
func TestObligationsSatisfiedIsSilent(t *testing.T) {
	g := loadGraph(t, "obligsvc.graph.json")
	res := Check(&policy.Policy{Service: "obligsvc", Version: 1}, graph.NewIndex(g))
	for _, f := range res.Findings {
		switch f.From {
		case "example.com/obligsvc/internal/app.TransferDefer",
			"example.com/obligsvc/internal/app.Disburse",
			"example.com/obligsvc/internal/app.TransferClosure",
			"example.com/obligsvc/internal/app.TransferAnnotate",
			"example.com/obligsvc/internal/app.TransferConcrete",
			"example.com/obligsvc/internal/app.HoldSem",
			"example.com/obligsvc/internal/app.DeferredPublishAudited":
			t.Errorf("SATISFIED site produced a finding: %v", f)
		}
	}
}

// RF-1: fail closed on vocabulary drift. A status this groundwork does not
// recognize must surface as a caution, never read as a pass.
func TestUnknownObligationStatusIsCaution(t *testing.T) {
	g := loadGraph(t, "obligsvc.graph.json")
	g.Obligations = append(g.Obligations, graph.Obligation{
		Rule: "tx-must-close", Kind: "must-release",
		Fn: "example.com/obligsvc/internal/app.Transfer", Site: "internal/app/app.go:99",
		Status: "CANT-PROVE-OWNERSHIP", // a future producer-side refinement
	})
	res := Check(&policy.Policy{Service: "obligsvc", Version: 1}, graph.NewIndex(g))
	found := false
	for _, c := range res.Cautions() {
		if strings.Contains(c.Summary, `status "CANT-PROVE-OWNERSHIP" is not understood`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("unknown status produced no caution; findings: %v", res.Findings)
	}
}

// TestObligationCharacterization pins every Finding field checkObligations
// emits, and the order it emits them in, before obligation-status
// interpretation moves to the shared facts package. It calls the check directly
// rather than through Check so that emission order — not just the canonical
// post-sort order — is captured: an extraction that reorders records would
// otherwise hide behind Result.sort, which ties on everything but Detail.
// Presentation changes require an explicit compatibility review.
func TestObligationCharacterization(t *testing.T) {
	const (
		transfer = "example.com/svc/internal/app.Transfer"
		disburse = "example.com/svc/internal/app.Disburse"
		site     = "internal/app/app.go:42"
		other    = "internal/app/app.go:99"
	)
	obligation := func(rule, kind, fn, at, status, detail string) graph.Obligation {
		return graph.Obligation{Rule: rule, Kind: kind, Fn: fn, Site: at, Status: status, Detail: detail}
	}

	tests := []struct {
		name string
		obs  []graph.Obligation
		want []Finding
	}{
		{
			name: "violated",
			obs: []graph.Obligation{
				obligation("tx-must-close", "must-release", transfer, site, "VIOLATED", "leaks on the error path"),
			},
			want: []Finding{{
				Rule:     "obligation",
				Severity: Violation,
				Summary:  "tx-must-close: must-release at app.Transfer",
				From:     transfer,
				To:       site,
				Detail:   "leaks on the error path",
			}},
		},
		{
			name: "cant prove",
			obs: []graph.Obligation{
				obligation("tx-must-close", "must-release", transfer, site, "CANT-PROVE", "handoff is unprovable"),
			},
			want: []Finding{{
				Rule:     "obligation",
				Severity: Caution,
				Summary:  "tx-must-close: cannot prove at app.Transfer",
				From:     transfer,
				To:       site,
				Detail:   "handoff is unprovable",
			}},
		},
		{
			// An UNMATCHED record names no function or site: the rule's anchor
			// matched nothing, so there is no edge to attribute the finding to.
			name: "unmatched",
			obs: []graph.Obligation{
				obligation("tx-must-close", "must-release", "", "", "UNMATCHED", "no acquire site"),
			},
			want: []Finding{{
				Rule:     "obligation",
				Severity: Caution,
				Summary:  "tx-must-close: rule matches nothing — inert guardrail",
				Detail:   "no acquire site",
			}},
		},
		{
			name: "satisfied is silent",
			obs: []graph.Obligation{
				obligation("tx-must-close", "must-release", transfer, site, "SATISFIED", "closed on every path"),
			},
		},
		{
			// Vocabulary drift fails closed, and drops Detail: the producer's prose
			// describes a verdict this judge did not understand.
			name: "unknown status",
			obs: []graph.Obligation{
				obligation("tx-must-close", "must-release", transfer, site, "CANT-PROVE-OWNERSHIP", "a future refinement"),
			},
			want: []Finding{{
				Rule:     "obligation",
				Severity: Caution,
				Summary:  `tx-must-close: status "CANT-PROVE-OWNERSHIP" is not understood by this groundwork — upgrade or investigate`,
				From:     transfer,
				To:       site,
			}},
		},
		{
			// Mixed statuses across two rules, supplied in non-canonical order.
			// Findings follow the graph's record order, one per non-SATISFIED
			// record; no rule-level aggregation or dominance collapses them.
			name: "mixed statuses follow record order",
			obs: []graph.Obligation{
				obligation("z-rule", "must-release", disburse, other, "CANT-PROVE", "unprovable"),
				obligation("a-rule", "must-release", transfer, site, "VIOLATED", "leaks"),
				obligation("a-rule", "must-release", disburse, site, "SATISFIED", "closed"),
				obligation("a-rule", "must-release", disburse, other, "VIOLATED", "also leaks"),
			},
			want: []Finding{
				{
					Rule: "obligation", Severity: Caution,
					Summary: "z-rule: cannot prove at app.Disburse",
					From:    disburse, To: other, Detail: "unprovable",
				},
				{
					Rule: "obligation", Severity: Violation,
					Summary: "a-rule: must-release at app.Transfer",
					From:    transfer, To: site, Detail: "leaks",
				},
				{
					Rule: "obligation", Severity: Violation,
					Summary: "a-rule: must-release at app.Disburse",
					From:    disburse, To: other, Detail: "also leaks",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &graph.Graph{Nodes: []graph.Node{{FQN: transfer}, {FQN: disburse}}, Obligations: tt.obs}
			var r Result
			checkObligations(nil, graph.NewIndex(g), &r)
			if !reflect.DeepEqual(r.Findings, tt.want) {
				t.Fatalf("obligation findings mismatch\nwant: %#v\ngot:  %#v", tt.want, r.Findings)
			}
		})
	}
}
