package facts

import (
	"reflect"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
)

func TestEvaluatePassThroughStatesAndIndependentBindings(t *testing.T) {
	const (
		source   = "svc.Source"
		mid      = "svc.Mid"
		target   = "svc.Target"
		guard    = "svc.Guard"
		blind    = "svc.Blind"
		missingA = "svc.MissingA"
		missingZ = "svc.MissingZ"
	)

	tests := []struct {
		name string
		g    *graph.Graph
		in   PassThroughInput
		want PassThroughResult
	}{
		{
			name: "unbound source and target retain independently bound waypoint",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: guard}},
			},
			in: PassThroughInput{
				From:    []string{missingZ, missingA, missingZ},
				To:      []string{"boundary:missing Z", "boundary:missing A", "boundary:missing Z"},
				Through: []string{guard, guard},
			},
			want: PassThroughResult{
				State:       PassThroughUnbound,
				Through:     []string{guard},
				UnboundFrom: []string{missingA, missingZ},
				UnboundTo:   []string{"boundary:missing A", "boundary:missing Z"},
			},
		},
		{
			name: "unbound target retains source and waypoint bindings",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: guard}},
			},
			in: PassThroughInput{
				From:    []string{source},
				To:      []string{missingA},
				Through: []string{guard},
			},
			want: PassThroughResult{
				State:     PassThroughUnbound,
				From:      []string{source},
				Through:   []string{guard},
				UnboundTo: []string{missingA},
			},
		},
		{
			name: "unbound waypoint is recorded but a path still bypasses",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: mid}, {FQN: target}},
				Edges: []graph.Edge{
					{From: source, To: mid},
					{From: mid, To: target},
				},
			},
			in: PassThroughInput{
				From:    []string{source, source},
				To:      []string{target},
				Through: []string{missingZ, missingA, missingZ},
			},
			want: PassThroughResult{
				State: PassThroughBypassed,
				From:  []string{source},
				To:    []string{target},
				Bypasses: []PathWitness{{
					From: source,
					To:   target,
					Path: []string{source, mid, target},
				}},
				UnboundThrough: []string{missingA, missingZ},
			},
		},
		{
			name: "unbound waypoint without a path is guarded",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: target}},
			},
			in: PassThroughInput{
				From:    []string{source},
				To:      []string{target},
				Through: []string{missingA},
			},
			want: PassThroughResult{
				State:          PassThroughGuarded,
				From:           []string{source},
				To:             []string{target},
				UnboundThrough: []string{missingA},
			},
		},
		{
			name: "bound waypoint guards the only path",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: guard}, {FQN: target}},
				Edges: []graph.Edge{
					{From: source, To: guard},
					{From: guard, To: target},
				},
			},
			in: PassThroughInput{
				From:    []string{source},
				To:      []string{target},
				Through: []string{guard},
			},
			want: PassThroughResult{
				State:   PassThroughGuarded,
				From:    []string{source},
				To:      []string{target},
				Through: []string{guard},
			},
		},
		{
			name: "blind visible frontier abstains",
			g: &graph.Graph{
				Nodes: []graph.Node{
					{FQN: source}, {FQN: blind}, {FQN: target}, {FQN: guard},
				},
				Edges: []graph.Edge{{From: source, To: blind}},
				BlindSpots: []graph.BlindSpot{{
					Kind: "reflect", Site: blind, Detail: "opaque dispatch",
				}},
			},
			in: PassThroughInput{
				From:    []string{source},
				To:      []string{target},
				Through: []string{guard},
			},
			want: PassThroughResult{
				State:   PassThroughBlind,
				From:    []string{source},
				To:      []string{target},
				Through: []string{guard},
				Blind: &BlindWitness{
					From:     source,
					Site:     blind,
					Kind:     "reflect",
					Detail:   "opaque dispatch",
					Location: BlindAtFunction,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluatePassThrough(graph.NewIndex(tt.g), tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("EvaluatePassThrough() mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

func TestEvaluatePassThroughCollectsCanonicalBypassesAndAllowsExactPairs(t *testing.T) {
	const (
		sourceA   = "svc.ASource"
		sourceY   = "svc.YSource"
		sourceZ   = "svc.ZSource"
		left      = "svc.Left"
		right     = "svc.Right"
		target    = "svc.GetUserAvatar"
		guard     = "svc.Guard"
		users     = "boundary:db UPDATE users"
		usersLog  = "boundary:db UPDATE users_audit"
		health    = "boundary:db UPDATE health"
		otherSink = "boundary:db UPDATE version"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: sourceZ}, {FQN: target}, {FQN: right}, {FQN: sourceA},
			{FQN: left}, {FQN: sourceY}, {FQN: guard}, {FQN: left},
		},
		Edges: []graph.Edge{
			{From: sourceA, To: right},
			{From: right, To: target},
			{From: sourceA, To: left},
			{From: left, To: target},
			{From: left, To: target},
			{From: sourceA, To: usersLog, Boundary: "outbound-sync"},
			{From: sourceA, To: users, Boundary: "outbound-sync"},
			{From: sourceA, To: usersLog, Boundary: "outbound-sync"},
			{From: sourceZ, To: otherSink, Boundary: "outbound-sync"},
			{From: sourceY, To: health, Boundary: "outbound-sync"},
		},
	}
	got := EvaluatePassThrough(graph.NewIndex(g), PassThroughInput{
		From:    []string{sourceZ, sourceY, sourceA},
		To:      []string{target, "boundary:db UPDATE"},
		Through: []string{guard},
		Allow: []AllowPair{
			{From: sourceA, To: users},
			{From: sourceZ},
			{To: health},
		},
	})
	want := PassThroughResult{
		State:   PassThroughBypassed,
		From:    []string{sourceA, sourceY, sourceZ},
		To:      []string{health, users, usersLog, otherSink, target},
		Through: []string{guard},
		Bypasses: []PathWitness{
			{
				From: sourceA,
				To:   usersLog,
				Path: []string{sourceA, usersLog},
			},
			{
				From: sourceA,
				To:   target,
				Path: []string{sourceA, left, target},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical bypasses mismatch\nwant: %#v\ngot:  %#v", want, got)
	}

	nearMiss := EvaluatePassThrough(graph.NewIndex(g), PassThroughInput{
		From:    []string{sourceA},
		To:      []string{"svc.GetUser"},
		Through: []string{guard},
	})
	if nearMiss.State != PassThroughUnbound ||
		!reflect.DeepEqual(nearMiss.UnboundTo, []string{"svc.GetUser"}) {
		t.Fatalf("identifier-prefix near miss bound unexpectedly: %#v", nearMiss)
	}
}

func TestEvaluatePassThroughSourceSemanticsAndEntrypointSelector(t *testing.T) {
	const (
		source = "svc.Source"
		guard  = "svc.Guard"
		target = "boundary:db UPDATE users"
	)

	t.Run("source matching waypoint is skipped", func(t *testing.T) {
		g := &graph.Graph{
			Nodes: []graph.Node{{FQN: source}},
			Edges: []graph.Edge{{
				From: source, To: target, Boundary: "outbound-sync",
			}},
		}
		got := EvaluatePassThrough(graph.NewIndex(g), PassThroughInput{
			From:    []string{policy.EntrypointSelector},
			To:      []string{source, target},
			Through: []string{source},
		})
		want := PassThroughResult{
			State:   PassThroughGuarded,
			From:    []string{source},
			To:      []string{target, source},
			Through: []string{source},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("source-as-waypoint mismatch\nwant: %#v\ngot:  %#v", want, got)
		}
	})

	t.Run("source self target is not a bypass", func(t *testing.T) {
		g := &graph.Graph{Nodes: []graph.Node{{FQN: source}, {FQN: guard}}}
		got := EvaluatePassThrough(graph.NewIndex(g), PassThroughInput{
			From:    []string{source},
			To:      []string{source},
			Through: []string{guard},
		})
		want := PassThroughResult{
			State:   PassThroughGuarded,
			From:    []string{source},
			To:      []string{source},
			Through: []string{guard},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("source-self target mismatch\nwant: %#v\ngot:  %#v", want, got)
		}
	})
}

func TestEvaluatePassThroughBlindClassificationAndDominance(t *testing.T) {
	const (
		source  = "svc.Source"
		mid     = "svc.Mid"
		other   = "svc.Other"
		target  = "boundary:db UPDATE users"
		guard   = "svc.Guard"
		dynamic = "boundary:bus PUBLISH <dynamic>"
	)

	t.Run("bypass dominates blind evidence", func(t *testing.T) {
		g := &graph.Graph{
			Nodes: []graph.Node{
				{FQN: source}, {FQN: mid}, {FQN: guard},
			},
			Edges: []graph.Edge{
				{From: source, To: mid},
				{From: source, To: target, Boundary: "outbound-sync"},
			},
			BlindSpots: []graph.BlindSpot{{
				Kind: "reflect", Site: mid, Detail: "opaque dispatch",
			}},
		}
		got := EvaluatePassThrough(graph.NewIndex(g), PassThroughInput{
			From:    []string{source},
			To:      []string{target},
			Through: []string{guard},
		})
		if got.State != PassThroughBypassed || len(got.Bypasses) != 1 {
			t.Fatalf("bypass must dominate blind frontier: %#v", got)
		}
	})

	t.Run("disclosure-only blind spots do not prevent guarded proof", func(t *testing.T) {
		g := &graph.Graph{
			Nodes: []graph.Node{
				{FQN: source}, {FQN: mid}, {FQN: other}, {FQN: guard},
			},
			Edges: []graph.Edge{
				{From: source, To: mid},
				{From: other, To: target, Boundary: "outbound-sync"},
			},
			BlindSpots: []graph.BlindSpot{{
				Kind: string(blindspots.ExternalBoundaryCall),
				Site: mid, Detail: "known external leaf",
			}},
		}
		got := EvaluatePassThrough(graph.NewIndex(g), PassThroughInput{
			From:    []string{source},
			To:      []string{target},
			Through: []string{guard},
		})
		if got.State != PassThroughGuarded || got.Blind != nil {
			t.Fatalf("disclosure-only spot changed guarded result: %#v", got)
		}
	})

	t.Run("dynamic effect blinds a target absence", func(t *testing.T) {
		g := &graph.Graph{
			Nodes: []graph.Node{
				{FQN: source}, {FQN: other}, {FQN: guard},
			},
			Edges: []graph.Edge{
				{From: source, To: dynamic, Boundary: "outbound-async"},
				{From: other, To: target, Boundary: "outbound-sync"},
			},
		}
		got := EvaluatePassThrough(graph.NewIndex(g), PassThroughInput{
			From:    []string{source},
			To:      []string{target},
			Through: []string{guard},
		})
		want := &BlindWitness{
			From:     source,
			Site:     source,
			Kind:     "DynamicEffect",
			Detail:   dynamic,
			Location: BlindAtDynamicEffect,
		}
		if got.State != PassThroughBlind || !reflect.DeepEqual(got.Blind, want) {
			t.Fatalf("dynamic blind mismatch\nwant: %#v\ngot:  %#v", want, got)
		}
	})
}

func TestEvaluatePassThroughCanonicalAcrossShuffledDuplicateInput(t *testing.T) {
	const (
		source = "svc.Source"
		left   = "svc.Left"
		right  = "svc.Right"
		target = "boundary:db UPDATE users"
		guard  = "svc.Guard"
	)
	first := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: right}, {FQN: source}, {FQN: left}, {FQN: guard}, {FQN: left},
		},
		Edges: []graph.Edge{
			{From: source, To: right},
			{From: right, To: target, Boundary: "outbound-sync"},
			{From: source, To: left},
			{From: left, To: target, Boundary: "outbound-sync"},
			{From: left, To: target, Boundary: "outbound-sync"},
			{From: source, To: left},
		},
		BlindSpots: []graph.BlindSpot{
			{Kind: "unsafe", Site: right, Detail: "opaque right"},
			{Kind: "reflect", Site: left, Detail: "opaque left"},
			{Kind: "reflect", Site: left, Detail: "opaque left"},
		},
	}
	second := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: left}, {FQN: guard}, {FQN: left}, {FQN: source}, {FQN: right},
		},
		Edges: []graph.Edge{
			{From: source, To: left},
			{From: left, To: target, Boundary: "outbound-sync"},
			{From: source, To: left},
			{From: source, To: right},
			{From: left, To: target, Boundary: "outbound-sync"},
			{From: right, To: target, Boundary: "outbound-sync"},
		},
		BlindSpots: []graph.BlindSpot{
			{Kind: "reflect", Site: left, Detail: "opaque left"},
			{Kind: "reflect", Site: left, Detail: "opaque left"},
			{Kind: "unsafe", Site: right, Detail: "opaque right"},
		},
	}

	firstResult := EvaluatePassThrough(graph.NewIndex(first), PassThroughInput{
		From:    []string{source, source},
		To:      []string{target, "boundary:db UPDATE"},
		Through: []string{guard, guard},
	})
	secondResult := EvaluatePassThrough(graph.NewIndex(second), PassThroughInput{
		From:    []string{source},
		To:      []string{"boundary:db UPDATE", target},
		Through: []string{guard},
	})
	if !reflect.DeepEqual(firstResult, secondResult) {
		t.Fatalf("semantic permutations changed result\nfirst:  %#v\nsecond: %#v", firstResult, secondResult)
	}
	want := PassThroughResult{
		State:   PassThroughBypassed,
		From:    []string{source},
		To:      []string{target},
		Through: []string{guard},
		Bypasses: []PathWitness{{
			From: source,
			To:   target,
			Path: []string{source, left, target},
		}},
	}
	if !reflect.DeepEqual(firstResult, want) {
		t.Fatalf("canonical result mismatch\nwant: %#v\ngot:  %#v", want, firstResult)
	}
}
