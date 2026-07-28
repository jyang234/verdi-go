package fitness

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// TestEvalReachUnboundIsNotAProof pins the asymmetry the compatibility wrapper
// used to break: a family that bound nothing walked zero seeds (or looked for
// zero targets) and the wrapper's catch-all returned provenAbsent — an
// abstention typed as a proof. There is no production caller today, but the
// wrapper exists to gain them, and "no path over a surface the caller never
// named" is exactly the false proof tenet 4 forbids.
func TestEvalReachUnboundIsNotAProof(t *testing.T) {
	const (
		caller = "example.com/svc.Caller"
		callee = "example.com/svc.Callee"
	)
	ix := graph.NewIndex(&graph.Graph{
		Nodes: []graph.Node{{FQN: caller, Tier: 1}, {FQN: callee, Tier: 1}},
		Edges: []graph.Edge{{From: caller, To: callee, Tier: 2}},
	})

	tests := []struct {
		name  string
		froms []string
		to    []string
	}{
		{
			// The target pattern names nothing in this graph: the walk cannot
			// disprove reaching a sink it could never recognize.
			name:  "target family binds nothing",
			froms: []string{caller},
			to:    []string{"example.com/other.Sink"},
		},
		{
			// The supplied identity is not a graph node, so zero seeds are walked.
			name:  "source identities bind nothing",
			froms: []string{"example.com/svc.Absent"},
			to:    []string{callee},
		},
		{
			name:  "both families bind nothing",
			froms: nil,
			to:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, ev := evalReach(ix, tt.froms, tt.to)
			if v == provenAbsent {
				t.Fatalf("unbound reach returned provenAbsent — a fabricated proof (evidence %+v)", ev)
			}
			if v != noPathFound {
				t.Fatalf("verdict = %v, want noPathFound", v)
			}
			if ev != (evidence{}) {
				t.Fatalf("evidence = %+v, want empty: nothing bound, so there is nothing to point at", ev)
			}
		})
	}

	// The proof pole is still reachable when the families DO bind and no path
	// exists — the fix must not have turned every absence into an abstention.
	if v, _ := evalReach(ix, []string{callee}, []string{caller}); v != provenAbsent {
		t.Fatalf("bound-but-unreached verdict = %v, want provenAbsent", v)
	}
}
