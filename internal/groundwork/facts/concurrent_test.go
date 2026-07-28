package facts

import (
	"reflect"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

func TestConcurrentSurfaceStatesAndBindings(t *testing.T) {
	const (
		launcher = "svc.Launcher"
		worker   = "svc.Worker"
		child    = "svc.Child"
		direct   = "svc.Direct"
		isolated = "svc.Isolated"
		update   = "boundary:db UPDATE users"
		dynamic  = "boundary:bus PUBLISH <dynamic>"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: isolated}, {FQN: direct}, {FQN: child}, {FQN: worker}, {FQN: launcher},
		},
		Edges: []graph.Edge{
			{From: direct, To: dynamic, Boundary: "outbound-async", Concurrent: true},
			{From: launcher, To: worker, Concurrent: true},
			{From: worker, To: child},
			{From: child, To: update, Boundary: "outbound-sync"},
			{From: child, To: update, Boundary: "outbound-sync"},
		},
		BlindSpots: []graph.BlindSpot{{
			Kind: "ConcurrentDispatch", Site: launcher, Detail: "go f()",
		}},
	}
	surface := BuildConcurrentSurface(graph.NewIndex(g))

	hit := surface.Evaluate([]string{child, dynamic, worker, update, child})
	wantHit := ConcurrentResult{
		State: ConcurrentHit,
		To:    []string{dynamic, update, child, worker},
		Hits: []ConcurrentWitness{
			{To: child},
			{To: worker},
			{From: child, To: update},
			{From: direct, To: dynamic},
		},
	}
	if !reflect.DeepEqual(hit, wantHit) {
		t.Fatalf("hit result mismatch\nwant: %#v\ngot:  %#v", wantHit, hit)
	}

	clean := surface.Evaluate([]string{isolated})
	if clean.State != ConcurrentBlind {
		t.Fatalf("surface with graph-wide blindness = %#v, want ConcurrentBlind", clean)
	}

	visible := BuildConcurrentSurface(graph.NewIndex(&graph.Graph{
		Nodes: []graph.Node{{FQN: launcher}, {FQN: worker}, {FQN: isolated}},
		Edges: []graph.Edge{{From: launcher, To: worker, Concurrent: true}},
	}))
	wantClean := ConcurrentResult{State: ConcurrentClean, To: []string{isolated}}
	if got := visible.Evaluate([]string{isolated}); !reflect.DeepEqual(got, wantClean) {
		t.Fatalf("clean result mismatch\nwant: %#v\ngot:  %#v", wantClean, got)
	}

	unbound := visible.Evaluate([]string{"svc.ZMissing", "svc.AMissing", "svc.ZMissing"})
	wantUnbound := ConcurrentResult{
		State:     ConcurrentUnbound,
		UnboundTo: []string{"svc.AMissing", "svc.ZMissing"},
	}
	if !reflect.DeepEqual(unbound, wantUnbound) {
		t.Fatalf("unbound result mismatch\nwant: %#v\ngot:  %#v", wantUnbound, unbound)
	}
}

func TestConcurrentSurfaceHitDominatesBlindness(t *testing.T) {
	const (
		owner   = "svc.Owner"
		dynamic = "boundary:bus PUBLISH <dynamic>"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{{FQN: owner}},
		Edges: []graph.Edge{{
			From: owner, To: dynamic, Boundary: "outbound-async", Concurrent: true,
		}},
		BlindSpots: []graph.BlindSpot{{
			Kind: "ConcurrentDispatch", Site: owner, Detail: "go f()",
		}},
	}
	got := BuildConcurrentSurface(graph.NewIndex(g)).Evaluate([]string{"boundary:bus PUBLISH"})
	want := ConcurrentResult{
		State: ConcurrentHit,
		To:    []string{dynamic},
		Hits:  []ConcurrentWitness{{From: owner, To: dynamic}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matching dynamic direct edge must be a hit\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestConcurrentSurfaceBlindPrecedence(t *testing.T) {
	const (
		launcher = "svc.Launcher"
		worker   = "svc.Worker"
		target   = "svc.Target"
		dynamic  = "boundary:bus PUBLISH <dynamic>"
	)
	tests := []struct {
		name string
		g    *graph.Graph
		want *BlindWitness
	}{
		{
			name: "cone frontier precedes dynamic direct and dispatch",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: launcher}, {FQN: worker}, {FQN: target}},
				Edges: []graph.Edge{
					{From: launcher, To: worker, Concurrent: true},
					{From: launcher, To: dynamic, Boundary: "outbound-async", Concurrent: true},
				},
				BlindSpots: []graph.BlindSpot{
					{Kind: "ConcurrentDispatch", Site: launcher, Detail: "go f()"},
					{Kind: "reflect", Site: worker, Detail: "opaque dispatch"},
				},
			},
			want: &BlindWitness{
				From: worker, Site: worker, Kind: "reflect", Detail: "opaque dispatch",
				Location: BlindAtFunction,
			},
		},
		{
			name: "dynamic direct precedes dispatch",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: launcher}, {FQN: target}},
				Edges: []graph.Edge{{
					From: launcher, To: dynamic, Boundary: "outbound-async", Concurrent: true,
				}},
				BlindSpots: []graph.BlindSpot{{
					Kind: "ConcurrentDispatch", Site: launcher, Detail: "go f()",
				}},
			},
			want: &BlindWitness{
				Site: launcher, Kind: dynamicEffectKind, Detail: dynamic,
				Location: BlindAtConcurrentBoundary,
			},
		},
		{
			name: "graph wide dispatch is load bearing",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: launcher}, {FQN: target}},
				BlindSpots: []graph.BlindSpot{{
					Kind: "ConcurrentDispatch", Site: launcher, Detail: "go f()",
				}},
			},
			want: &BlindWitness{
				Site: launcher, Kind: "ConcurrentDispatch", Detail: "go f()",
				Location: BlindAtConcurrentDispatch,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildConcurrentSurface(graph.NewIndex(tt.g)).Evaluate([]string{target})
			if got.State != ConcurrentBlind || !reflect.DeepEqual(got.Blind, tt.want) {
				t.Fatalf("blind result mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

func TestConcurrentSurfaceCanonicalOverShuffledDuplicateInput(t *testing.T) {
	const (
		aOwner   = "svc.AOwner"
		launcher = "svc.Launcher"
		worker   = "svc.Worker"
		zOwner   = "svc.ZOwner"
		target   = "svc.Target"
		update   = "boundary:db UPDATE users"
		aDynamic = "boundary:bus PUBLISH <dynamic> alpha"
		zDynamic = "boundary:bus PUBLISH <dynamic> zeta"
	)
	nodes := []graph.Node{
		{FQN: zOwner}, {FQN: target}, {FQN: worker}, {FQN: launcher}, {FQN: aOwner},
	}
	edges := []graph.Edge{
		{From: zOwner, To: zDynamic, Boundary: "outbound-async", Concurrent: true},
		{From: worker, To: update, Boundary: "outbound-sync"},
		{From: launcher, To: worker, Concurrent: true},
		{From: aOwner, To: aDynamic, Boundary: "outbound-async", Concurrent: true},
		{From: worker, To: update, Boundary: "outbound-sync"},
		{From: launcher, To: worker, Concurrent: true},
	}
	spots := []graph.BlindSpot{
		{Kind: "ConcurrentDispatch", Site: zOwner, Detail: "go z()"},
		{Kind: "ConcurrentDispatch", Site: aOwner, Detail: "go a()"},
		{Kind: "ConcurrentDispatch", Site: aOwner, Detail: "go a()"},
	}
	reversedNodes := append([]graph.Node(nil), nodes...)
	reversedEdges := append([]graph.Edge(nil), edges...)
	reversedSpots := append([]graph.BlindSpot(nil), spots...)
	reverseNodes(reversedNodes)
	reverseEdges(reversedEdges)
	reverseBlindSpots(reversedSpots)

	a := BuildConcurrentSurface(graph.NewIndex(&graph.Graph{
		Nodes: nodes, Edges: edges, BlindSpots: spots,
	}))
	b := BuildConcurrentSurface(graph.NewIndex(&graph.Graph{
		Nodes: reversedNodes, Edges: reversedEdges, BlindSpots: reversedSpots,
	}))
	selectors := []string{update, worker, update}
	if gotA, gotB := a.Evaluate(selectors), b.Evaluate(selectors); !reflect.DeepEqual(gotA, gotB) {
		t.Fatalf("shuffled duplicate surface changed hits\na: %#v\nb: %#v", gotA, gotB)
	}

	wantBlind := &BlindWitness{
		Site: aOwner, Kind: dynamicEffectKind, Detail: aDynamic,
		Location: BlindAtConcurrentBoundary,
	}
	if got := a.Evaluate([]string{target}); got.State != ConcurrentBlind || !reflect.DeepEqual(got.Blind, wantBlind) {
		t.Fatalf("canonical dynamic witness mismatch\nwant: %#v\ngot:  %#v", wantBlind, got)
	}
	if gotA, gotB := a.Evaluate([]string{target}), b.Evaluate([]string{target}); !reflect.DeepEqual(gotA, gotB) {
		t.Fatalf("shuffled duplicate surface changed blindness\na: %#v\nb: %#v", gotA, gotB)
	}
}

func reverseNodes(values []graph.Node) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseEdges(values []graph.Edge) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}

func reverseBlindSpots(values []graph.BlindSpot) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
