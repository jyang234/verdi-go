package claims

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

const (
	obligationRule = "tx-must-close"
	obligationFn   = "svc.Transfer"
	obligationSite = "app.go:1"
)

func obligationGraph(obs ...graph.Obligation) *graph.Graph {
	return &graph.Graph{
		Nodes:       []graph.Node{{FQN: obligationFn}},
		Obligations: obs,
	}
}

func obligationOf(rule, status, detail string) graph.Obligation {
	return graph.Obligation{
		Rule: rule, Kind: "must-release", Fn: obligationFn,
		Site: obligationSite, Status: status, Detail: detail,
	}
}

func obligationWitness(status, detail string) Witness {
	return Witness{
		Rule: obligationRule, Fn: obligationFn, Site: obligationSite,
		Status: status, Detail: detail,
	}
}

func TestObligationClaimStates(t *testing.T) {
	base := func() Claim {
		return Claim{ID: "tx", Kind: "obligation", Name: obligationRule, Expect: "satisfied"}
	}
	bound := Bindings{Obligation: []string{obligationRule}}

	tests := []struct {
		name string
		obs  []graph.Obligation
		want Result
	}{
		{
			name: "satisfied passes",
			obs:  []graph.Obligation{obligationOf(obligationRule, "SATISFIED", "closed on every path")},
			want: Result{
				ID: "tx", Kind: "obligation", Label: "tx", Outcome: Pass,
				Detail:    "SATISFIED across 1 matched record(s)",
				Bindings:  bound,
				Witnesses: []Witness{obligationWitness("SATISFIED", "closed on every path")},
			},
		},
		{
			// A concrete violation already disproves `satisfied`, so it dominates a
			// sibling abstention rather than degrading to an ERROR.
			name: "violated fails, dominating cant-prove",
			obs: []graph.Obligation{
				obligationOf(obligationRule, "CANT-PROVE", "unprovable handoff"),
				obligationOf(obligationRule, "VIOLATED", "leaks on the error path"),
			},
			want: Result{
				ID: "tx", Kind: "obligation", Label: "tx", Outcome: Fail,
				Detail:   "VIOLATED across 2 matched record(s)",
				Bindings: bound,
				Witnesses: []Witness{
					obligationWitness("CANT-PROVE", "unprovable handoff"),
					obligationWitness("VIOLATED", "leaks on the error path"),
				},
			},
		},
		{
			name: "no obligations section errors",
			obs:  nil,
			want: Result{
				ID: "tx", Kind: "obligation", Label: "tx",
				Outcome: Errored, Reason: ReasonMissingGraphData,
				Detail: "graph carries no obligations section",
			},
		},
		{
			name: "unknown rule name errors",
			obs:  []graph.Obligation{obligationOf("other-rule", "SATISFIED", "closed")},
			want: Result{
				ID: "tx", Kind: "obligation", Label: "tx",
				Outcome: Errored, Reason: ReasonUnresolved,
				Detail: `no obligation rule named "tx-must-close"`,
			},
		},
		{
			name: "unrecognized producer status errors",
			obs:  []graph.Obligation{obligationOf(obligationRule, "CANT-PROVE-OWNERSHIP", "a future refinement")},
			want: Result{
				ID: "tx", Kind: "obligation", Label: "tx",
				Outcome: Errored, Reason: ReasonUnknownStatus,
				Detail:    "unrecognized producer status across 1 matched record(s)",
				Bindings:  bound,
				Witnesses: []Witness{obligationWitness("CANT-PROVE-OWNERSHIP", "a future refinement")},
			},
		},
		{
			name: "cant-prove errors",
			obs:  []graph.Obligation{obligationOf(obligationRule, "CANT-PROVE", "unprovable handoff")},
			want: Result{
				ID: "tx", Kind: "obligation", Label: "tx",
				Outcome: Errored, Reason: ReasonCantProve,
				Detail:    "CANT-PROVE across 1 matched record(s)",
				Bindings:  bound,
				Witnesses: []Witness{obligationWitness("CANT-PROVE", "unprovable handoff")},
			},
		},
		{
			name: "unmatched errors",
			obs:  []graph.Obligation{obligationOf(obligationRule, "UNMATCHED", "no acquire site")},
			want: Result{
				ID: "tx", Kind: "obligation", Label: "tx",
				Outcome: Errored, Reason: ReasonUnmatched,
				Detail:    "UNMATCHED across 1 matched record(s)",
				Bindings:  bound,
				Witnesses: []Witness{obligationWitness("UNMATCHED", "no acquire site")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalOneG(t, obligationGraph(tt.obs...), base())
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("obligation result mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

func TestObligationClaimValidationAndFallbackLabel(t *testing.T) {
	tests := []struct {
		name   string
		claim  Claim
		reason Reason
		detail string
	}{
		{
			name:   "missing name",
			claim:  Claim{Kind: "obligation", Expect: "satisfied"},
			reason: ReasonMalformedClaim,
			detail: "obligation requires 'name'",
		},
		{
			name:   "missing expect",
			claim:  Claim{Kind: "obligation", Name: obligationRule},
			reason: ReasonMalformedClaim,
			detail: `obligation requires expect "satisfied"`,
		},
		{
			// v1 accepts only the positive pole: the claim inspects verdicts the
			// graph already carries and cannot author the inverse rule.
			name:   "expect absent is rejected",
			claim:  Claim{Kind: "obligation", Name: obligationRule, Expect: "absent"},
			reason: ReasonMalformedClaim,
			detail: `obligation requires expect "satisfied"`,
		},
		{
			name:   "wrong-kind field",
			claim:  Claim{Kind: "obligation", Name: obligationRule, Expect: "satisfied", From: sel(obligationFn)},
			reason: ReasonMalformedClaim,
			detail: `obligation does not accept field "from"`,
		},
		{
			name:   "wrong-kind fqn",
			claim:  Claim{Kind: "obligation", Name: obligationRule, Expect: "satisfied", FQN: obligationFn},
			reason: ReasonMalformedClaim,
			detail: `obligation does not accept field "fqn"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalOneG(t, obligationGraph(), tt.claim)
			if got.Outcome != Errored || got.Reason != tt.reason || got.Detail != tt.detail {
				t.Fatalf("want ERROR %s %q, got %#v", tt.reason, tt.detail, got)
			}
		})
	}

	// With no ID, the report line falls back to the exact obligation name.
	got := evalOneG(t, obligationGraph(obligationOf(obligationRule, "SATISFIED", "closed")),
		Claim{Kind: "obligation", Name: obligationRule, Expect: "satisfied"})
	if got.Label != obligationRule {
		t.Fatalf("fallback label = %q, want %q", got.Label, obligationRule)
	}
	if got.ID != "" {
		t.Errorf("ID = %q, want empty", got.ID)
	}
}

// TestObligationClaimMachineEvidence pins the machine report: every matched
// record is a witness carrying the producer's own status and detail, so a
// consumer discriminates on those fields and never on the result's prose.
func TestObligationClaimMachineEvidence(t *testing.T) {
	g := obligationGraph(
		obligationOf(obligationRule, "VIOLATED", "leaks"),
		obligationOf(obligationRule, "SATISFIED", "closed"),
		obligationOf("other-rule", "VIOLATED", "not this rule"),
	)
	g.Stamp, g.Tool, g.Algo = "abc123", "v0.0.0-test", "vta"

	rep := Evaluate(g, &File{Claims: []Claim{
		{ID: "tx", Kind: "obligation", Name: obligationRule, Expect: "satisfied"},
	}})
	out, err := MarshalMachine(g, rep)
	if err != nil {
		t.Fatal(err)
	}

	// The other-rule record is absent: exact-name matching, no prefix folding.
	// Witnesses sort on the record tuple, so SATISFIED precedes VIOLATED here —
	// canonical order, not the producer's emission order.
	const want = `{
  "schema_version": "groundwork.assert/v1",
  "fixture": {
    "stamp": "abc123",
    "producer_tool": "v0.0.0-test",
    "algo": "vta",
    "caveats": []
  },
  "results": [
    {
      "id": "tx",
      "kind": "obligation",
      "outcome": "FAIL",
      "detail": "VIOLATED across 2 matched record(s)",
      "bindings": {
        "obligation": [
          "tx-must-close"
        ]
      },
      "witnesses": [
        {
          "rule": "tx-must-close",
          "fn": "svc.Transfer",
          "site": "app.go:1",
          "status": "SATISFIED",
          "detail": "closed"
        },
        {
          "rule": "tx-must-close",
          "fn": "svc.Transfer",
          "site": "app.go:1",
          "status": "VIOLATED",
          "detail": "leaks"
        }
      ]
    }
  ],
  "summary": {
    "passed": 0,
    "failed": 1,
    "errored": 0,
    "nodes": 1,
    "unique_edges": 0
  }
}
`
	if string(out) != want {
		t.Fatalf("machine JSON mismatch\nwant: %s\ngot:  %s", want, out)
	}
	if !json.Valid(out) {
		t.Fatal("machine report is not valid JSON")
	}
}

// TestObligationClaimCanonicalOverShuffledRecords proves the machine bytes are a
// function of the graph's content, not the producer's record order.
func TestObligationClaimCanonicalOverShuffledRecords(t *testing.T) {
	forward := obligationGraph(
		obligationOf(obligationRule, "CANT-PROVE", "alpha"),
		obligationOf(obligationRule, "CANT-PROVE", "zeta"),
	)
	reversed := obligationGraph(
		obligationOf(obligationRule, "CANT-PROVE", "zeta"),
		obligationOf(obligationRule, "CANT-PROVE", "alpha"),
	)
	claim := Claim{ID: "tx", Kind: "obligation", Name: obligationRule, Expect: "satisfied"}

	marshal := func(g *graph.Graph) string {
		out, err := MarshalMachine(g, Evaluate(g, &File{Claims: []Claim{claim}}))
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	if a, b := marshal(forward), marshal(reversed); a != b {
		t.Fatalf("record order reached the machine report\n%s\n%s", a, b)
	}
}
