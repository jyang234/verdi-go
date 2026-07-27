package facts

import (
	"reflect"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

func obligationIndex(obs ...graph.Obligation) *graph.Index {
	return graph.NewIndex(&graph.Graph{
		Nodes:       []graph.Node{{FQN: "example.com/svc/internal/app.Transfer"}},
		Obligations: obs,
	})
}

func obligationRecord(rule, status, fn string) graph.Obligation {
	return graph.Obligation{
		Rule: rule, Kind: "must-release", Fn: fn,
		Site: "internal/app/app.go:1", Status: status, Detail: status + " detail",
	}
}

func TestClassifyObligationStatus(t *testing.T) {
	tests := []struct {
		status string
		want   ObligationState
	}{
		{ObligationStatusViolated, ObligationViolated},
		{ObligationStatusCantProve, ObligationCantProve},
		{ObligationStatusUnmatched, ObligationUnmatched},
		{ObligationStatusSatisfied, ObligationSatisfied},
		{"CANT-PROVE-OWNERSHIP", ObligationUnknown},
		{"", ObligationUnknown},
		{"violated", ObligationUnknown}, // exact bytes; no case folding
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := ClassifyObligationStatus(tt.status); got != tt.want {
				t.Fatalf("ClassifyObligationStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// TestClassifyObligationStatusNeverAggregates guards the split between the
// per-record classifier and the aggregate evaluator: the two aggregate-only
// states must be unreachable from a single status string, or a caller could
// mistake "this record is fine" for "this rule is proven".
func TestClassifyObligationStatusNeverAggregates(t *testing.T) {
	for _, status := range []string{
		ObligationStatusViolated, ObligationStatusCantProve,
		ObligationStatusUnmatched, ObligationStatusSatisfied, "DRIFTED", "",
	} {
		switch got := ClassifyObligationStatus(status); got {
		case ObligationMissingData, ObligationUnresolved:
			t.Fatalf("ClassifyObligationStatus(%q) = %v, an aggregate-only state", status, got)
		}
	}
}

func TestEvaluateObligationStates(t *testing.T) {
	const (
		name  = "tx-must-close"
		other = "other-rule"
		fn    = "example.com/svc/internal/app.Transfer"
	)
	tests := []struct {
		name  string
		ix    *graph.Index
		want  ObligationState
		count int
	}{
		{
			name: "no obligations section",
			ix:   obligationIndex(),
			want: ObligationMissingData,
		},
		{
			name: "present section without the name",
			ix:   obligationIndex(obligationRecord(other, ObligationStatusSatisfied, fn)),
			want: ObligationUnresolved,
		},
		{
			name:  "all satisfied",
			ix:    obligationIndex(obligationRecord(name, ObligationStatusSatisfied, fn)),
			want:  ObligationSatisfied,
			count: 1,
		},
		{
			name:  "violated dominates cant-prove",
			ix:    obligationIndex(obligationRecord(name, ObligationStatusCantProve, fn), obligationRecord(name, ObligationStatusViolated, fn)),
			want:  ObligationViolated,
			count: 2,
		},
		{
			name:  "violated dominates an unknown status",
			ix:    obligationIndex(obligationRecord(name, "DRIFTED", fn), obligationRecord(name, ObligationStatusViolated, fn)),
			want:  ObligationViolated,
			count: 2,
		},
		{
			name:  "unknown dominates cant-prove",
			ix:    obligationIndex(obligationRecord(name, ObligationStatusCantProve, fn), obligationRecord(name, "DRIFTED", fn)),
			want:  ObligationUnknown,
			count: 2,
		},
		{
			name:  "cant-prove dominates unmatched",
			ix:    obligationIndex(obligationRecord(name, ObligationStatusUnmatched, fn), obligationRecord(name, ObligationStatusCantProve, fn)),
			want:  ObligationCantProve,
			count: 2,
		},
		{
			name:  "unmatched dominates satisfied",
			ix:    obligationIndex(obligationRecord(name, ObligationStatusSatisfied, fn), obligationRecord(name, ObligationStatusUnmatched, fn)),
			want:  ObligationUnmatched,
			count: 2,
		},
		{
			// Exact-name matching: a rule whose name merely shares a prefix is a
			// different obligation, never folded into this one.
			name:  "prefix name is not a match",
			ix:    obligationIndex(obligationRecord(name+"-strict", ObligationStatusViolated, fn), obligationRecord(name, ObligationStatusSatisfied, fn)),
			want:  ObligationSatisfied,
			count: 1,
		},
		{
			name: "nil index",
			ix:   nil,
			want: ObligationMissingData,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateObligation(tt.ix, name)
			if got.State != tt.want {
				t.Fatalf("state = %v, want %v (records %v)", got.State, tt.want, got.Records)
			}
			if got.Name != name {
				t.Errorf("name = %q, want %q", got.Name, name)
			}
			if len(got.Records) != tt.count {
				t.Fatalf("matched %d record(s), want %d: %v", len(got.Records), tt.count, got.Records)
			}
			for _, record := range got.Records {
				if record.Rule != name {
					t.Errorf("record %v is not an exact %q match", record, name)
				}
			}
		})
	}
}

// TestEvaluateObligationCanonicalRecordsOverShuffledInput pins the sort key and
// proves the evaluator never reorders the caller's graph.
func TestEvaluateObligationCanonicalRecordsOverShuffledInput(t *testing.T) {
	const name = "tx-must-close"
	records := []graph.Obligation{
		{Rule: name, Kind: "must-release", Fn: "svc.Zed", Site: "z.go:1", Status: "VIOLATED", Detail: "z"},
		{Rule: name, Kind: "must-release", Fn: "svc.Alpha", Site: "a.go:2", Status: "VIOLATED", Detail: "second"},
		{Rule: name, Kind: "must-release", Fn: "svc.Alpha", Site: "a.go:2", Status: "VIOLATED", Detail: "first"},
		{Rule: name, Kind: "must-acquire", Fn: "svc.Zed", Site: "z.go:1", Status: "VIOLATED", Detail: "k"},
	}
	want := []graph.Obligation{
		{Rule: name, Kind: "must-acquire", Fn: "svc.Zed", Site: "z.go:1", Status: "VIOLATED", Detail: "k"},
		{Rule: name, Kind: "must-release", Fn: "svc.Alpha", Site: "a.go:2", Status: "VIOLATED", Detail: "first"},
		{Rule: name, Kind: "must-release", Fn: "svc.Alpha", Site: "a.go:2", Status: "VIOLATED", Detail: "second"},
		{Rule: name, Kind: "must-release", Fn: "svc.Zed", Site: "z.go:1", Status: "VIOLATED", Detail: "z"},
	}

	g := &graph.Graph{Nodes: []graph.Node{{FQN: "svc.Zed"}}, Obligations: records}
	input := append([]graph.Obligation(nil), records...)
	got := EvaluateObligation(graph.NewIndex(g), name)
	if !reflect.DeepEqual(got.Records, want) {
		t.Fatalf("records not canonical\nwant: %v\ngot:  %v", want, got.Records)
	}
	if !reflect.DeepEqual(g.Obligations, input) {
		t.Fatalf("evaluator mutated the caller's graph\nwant: %v\ngot:  %v", input, g.Obligations)
	}

	// Duplicate records are evidence, not noise: two identical producer verdicts
	// are two matched records, so the count a caller reports stays honest.
	dup := EvaluateObligation(obligationIndex(
		obligationRecord(name, ObligationStatusViolated, "svc.Zed"),
		obligationRecord(name, ObligationStatusViolated, "svc.Zed"),
	), name)
	if len(dup.Records) != 2 {
		t.Fatalf("duplicate records collapsed: %v", dup.Records)
	}
}
