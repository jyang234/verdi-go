package facts

import (
	"reflect"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// The identities every per-selector case below is written against. Dead* bind
// nothing under policy.MatchPrefix: no node or boundary label starts with them
// at an identifier boundary.
const (
	unboundSource     = "svc.Source"
	unboundGuard      = "svc.Guard"
	unboundTarget     = "svc.Target"
	unboundDeadFrom   = "svc.DeadFrom"
	unboundDeadTo     = "svc.DeadTo"
	unboundDeadGuard  = "svc.DeadGuard"
	unboundLauncher   = "svc.Launcher"
	unboundWorker     = "svc.Worker"
	unboundDeadWorker = "svc.DeadWorker"
)

func unboundChainGraph() *graph.Graph {
	return &graph.Graph{
		Nodes: []graph.Node{
			{FQN: unboundSource}, {FQN: unboundGuard}, {FQN: unboundTarget},
		},
		Edges: []graph.Edge{
			{From: unboundSource, To: unboundGuard},
			{From: unboundGuard, To: unboundTarget},
		},
	}
}

// TestEvaluateReachUnboundIsPerSelector pins that UnboundFrom/UnboundTo name the
// selectors that bind nothing ON THEIR OWN, in EVERY state — a family that binds
// through one selector still discloses its dead members. A family that binds
// nothing at all keeps exactly its previous value (the whole selector list), so
// the ReachUnbound transition and every fitness finding reading it are unchanged.
func TestEvaluateReachUnboundIsPerSelector(t *testing.T) {
	tests := []struct {
		name string
		from []string
		to   []string
		want ReachResult
	}{
		{
			name: "partially bound family still finds the path and names the dead selector",
			from: []string{unboundSource, unboundDeadFrom},
			to:   []string{unboundTarget, unboundDeadTo},
			want: ReachResult{
				State: ReachFound,
				From:  []string{unboundSource},
				To:    []string{unboundTarget},
				Paths: []PathWitness{{
					From: unboundSource,
					To:   unboundTarget,
					Path: []string{unboundSource, unboundGuard, unboundTarget},
				}},
				UnboundFrom: []string{unboundDeadFrom},
				UnboundTo:   []string{unboundDeadTo},
			},
		},
		{
			name: "partially bound family proving absence still names the dead selector",
			from: []string{unboundTarget, unboundDeadFrom},
			to:   []string{unboundSource},
			want: ReachResult{
				State:       ReachAbsent,
				From:        []string{unboundTarget},
				To:          []string{unboundSource},
				UnboundFrom: []string{unboundDeadFrom},
			},
		},
		{
			name: "fully bound families record nothing",
			from: []string{unboundSource},
			to:   []string{unboundTarget},
			want: ReachResult{
				State: ReachFound,
				From:  []string{unboundSource},
				To:    []string{unboundTarget},
				Paths: []PathWitness{{
					From: unboundSource,
					To:   unboundTarget,
					Path: []string{unboundSource, unboundGuard, unboundTarget},
				}},
			},
		},
		{
			name: "fully dead families record every selector, sorted and de-duplicated",
			from: []string{unboundDeadFrom, unboundDeadGuard, unboundDeadFrom},
			to:   []string{unboundDeadTo},
			want: ReachResult{
				State:       ReachUnbound,
				UnboundFrom: []string{unboundDeadFrom, unboundDeadGuard},
				UnboundTo:   []string{unboundDeadTo},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateReach(graph.NewIndex(unboundChainGraph()), tt.from, tt.to)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("EvaluateReach() mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

// TestEvaluateReachBoundSourcesUnboundIsPerSelector pins the same per-selector
// meaning on the compatibility entry point, where a "selector" is an already
// bound identity: an identity the graph does not carry is the dead one.
func TestEvaluateReachBoundSourcesUnboundIsPerSelector(t *testing.T) {
	got := EvaluateReachBoundSources(
		graph.NewIndex(unboundChainGraph()),
		[]string{unboundSource, unboundDeadFrom},
		[]string{unboundTarget},
	)
	want := ReachResult{
		State: ReachFound,
		From:  []string{unboundSource},
		To:    []string{unboundTarget},
		Paths: []PathWitness{{
			From: unboundSource,
			To:   unboundTarget,
			Path: []string{unboundSource, unboundGuard, unboundTarget},
		}},
		UnboundFrom: []string{unboundDeadFrom},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EvaluateReachBoundSources() mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

// TestEvaluatePassThroughUnboundIsPerSelector pins the per-selector disclosure
// AND the state gate that must not move with it: PassThroughUnbound still
// triggers on an empty FAMILY, so a partially bound source family is still
// traversed and still yields its bypass evidence (the standing fitness
// contract).
func TestEvaluatePassThroughUnboundIsPerSelector(t *testing.T) {
	tests := []struct {
		name string
		in   PassThroughInput
		want PassThroughResult
	}{
		{
			name: "partially bound families are still traversed and still bypass",
			in: PassThroughInput{
				From:    []string{unboundSource, unboundDeadFrom},
				To:      []string{unboundTarget, unboundDeadTo},
				Through: []string{unboundDeadGuard},
			},
			want: PassThroughResult{
				State: PassThroughBypassed,
				From:  []string{unboundSource},
				To:    []string{unboundTarget},
				Bypasses: []PathWitness{{
					From: unboundSource,
					To:   unboundTarget,
					Path: []string{unboundSource, unboundGuard, unboundTarget},
				}},
				BypassOccurrences: []PathWitness{{
					From: unboundSource,
					To:   unboundTarget,
					Path: []string{unboundSource, unboundGuard, unboundTarget},
				}},
				UnboundFrom:    []string{unboundDeadFrom},
				UnboundTo:      []string{unboundDeadTo},
				UnboundThrough: []string{unboundDeadGuard},
			},
		},
		{
			name: "a partially bound waypoint family still guards",
			in: PassThroughInput{
				From:    []string{unboundSource},
				To:      []string{unboundTarget},
				Through: []string{unboundGuard, unboundDeadGuard},
			},
			want: PassThroughResult{
				State:          PassThroughGuarded,
				From:           []string{unboundSource},
				To:             []string{unboundTarget},
				Through:        []string{unboundGuard},
				UnboundThrough: []string{unboundDeadGuard},
			},
		},
		{
			name: "an empty source family still stops before traversal",
			in: PassThroughInput{
				From:    []string{unboundDeadFrom},
				To:      []string{unboundTarget},
				Through: []string{unboundGuard},
			},
			want: PassThroughResult{
				State:       PassThroughUnbound,
				To:          []string{unboundTarget},
				Through:     []string{unboundGuard},
				UnboundFrom: []string{unboundDeadFrom},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluatePassThrough(graph.NewIndex(unboundChainGraph()), tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("EvaluatePassThrough() mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

// TestConcurrentEvaluateUnboundIsPerSelector pins the per-selector target
// disclosure on the concurrent surface, where a hit must still be reported.
func TestConcurrentEvaluateUnboundIsPerSelector(t *testing.T) {
	surface := BuildConcurrentSurface(graph.NewIndex(&graph.Graph{
		Nodes: []graph.Node{{FQN: unboundLauncher}, {FQN: unboundWorker}},
		Edges: []graph.Edge{{From: unboundLauncher, To: unboundWorker, Concurrent: true}},
	}))

	got := surface.Evaluate([]string{unboundWorker, unboundDeadWorker})
	want := ConcurrentResult{
		State:     ConcurrentHit,
		To:        []string{unboundWorker},
		Hits:      []ConcurrentWitness{{To: unboundWorker}},
		UnboundTo: []string{unboundDeadWorker},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ConcurrentSurface.Evaluate() mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}
