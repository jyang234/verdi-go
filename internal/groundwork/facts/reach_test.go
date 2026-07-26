package facts

import (
	"reflect"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
)

func TestEvaluateReachStatesAndBindings(t *testing.T) {
	const (
		source = "svc.Source"
		mid    = "svc.Mid"
		other  = "svc.Other"
		target = "boundary:db UPDATE users"
	)

	base := func() *graph.Graph {
		return &graph.Graph{
			Nodes: []graph.Node{{FQN: source}, {FQN: mid}, {FQN: other}},
			Edges: []graph.Edge{
				{From: source, To: mid},
				{From: other, To: target, Boundary: "outbound-sync"},
			},
		}
	}

	tests := []struct {
		name string
		g    func() *graph.Graph
		from []string
		to   []string
		want ReachResult
	}{
		{
			name: "found",
			g: func() *graph.Graph {
				g := base()
				g.Edges = append(g.Edges, graph.Edge{
					From: mid, To: target, Boundary: "outbound-sync",
				})
				return g
			},
			from: []string{source},
			to:   []string{"boundary:db UPDATE"},
			want: ReachResult{
				State: ReachFound,
				From:  []string{source},
				To:    []string{target},
				Paths: []PathWitness{{
					From: source,
					To:   target,
					Path: []string{source, mid, target},
				}},
			},
		},
		{
			name: "absent",
			g:    base,
			from: []string{source},
			to:   []string{"boundary:db UPDATE"},
			want: ReachResult{
				State: ReachAbsent,
				From:  []string{source},
				To:    []string{target},
			},
		},
		{
			name: "blind",
			g: func() *graph.Graph {
				g := base()
				g.BlindSpots = []graph.BlindSpot{{
					Kind: "reflect", Site: mid, Detail: "opaque dispatch",
				}}
				return g
			},
			from: []string{source},
			to:   []string{"boundary:db UPDATE"},
			want: ReachResult{
				State: ReachBlind,
				From:  []string{source},
				To:    []string{target},
				Blind: &BlindWitness{
					From:     source,
					Site:     mid,
					Kind:     "reflect",
					Detail:   "opaque dispatch",
					Location: BlindAtFunction,
				},
			},
		},
		{
			name: "unbound both sides",
			g:    base,
			from: []string{" z", "svc.Missing", " z"},
			to:   []string{"boundary:missing Z", "boundary:missing A", "boundary:missing Z"},
			want: ReachResult{
				State:       ReachUnbound,
				UnboundFrom: []string{" z", "svc.Missing"},
				UnboundTo:   []string{"boundary:missing A", "boundary:missing Z"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateReach(graph.NewIndex(tt.g()), tt.from, tt.to)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("EvaluateReach() mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

func TestEvaluateReachCanonicalAcrossDuplicateShuffledInput(t *testing.T) {
	const (
		source = "svc.Source"
		left   = "svc.Left"
		right  = "svc.Right"
		target = "boundary:db UPDATE users"
	)

	first := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: source}, {FQN: left}, {FQN: right}, {FQN: target}, {FQN: left},
		},
		Edges: []graph.Edge{
			{From: source, To: right},
			{From: left, To: target, Boundary: "outbound-sync"},
			{From: source, To: left},
			{From: left, To: target, Boundary: "outbound-sync"},
			{From: source, To: left},
		},
		BlindSpots: []graph.BlindSpot{
			{Kind: "unsafe", Site: right, Detail: "later"},
			{Kind: "reflect", Site: left, Detail: "first"},
			{Kind: "reflect", Site: left, Detail: "first"},
		},
	}
	second := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: left}, {FQN: target}, {FQN: right}, {FQN: left}, {FQN: source},
		},
		Edges: []graph.Edge{
			{From: source, To: left},
			{From: source, To: left},
			{From: source, To: right},
			{From: left, To: target, Boundary: "outbound-sync"},
			{From: left, To: target, Boundary: "outbound-sync"},
		},
		BlindSpots: []graph.BlindSpot{
			{Kind: "reflect", Site: left, Detail: "first"},
			{Kind: "reflect", Site: left, Detail: "first"},
			{Kind: "unsafe", Site: right, Detail: "later"},
		},
	}

	from := []string{source, source}
	to := []string{"boundary:db UPDATE", "boundary:db UPDATE"}
	gotFirst := EvaluateReach(graph.NewIndex(first), from, to)
	gotSecond := EvaluateReach(graph.NewIndex(second), from, to)
	if !reflect.DeepEqual(gotFirst, gotSecond) {
		t.Fatalf("semantic permutations changed result\nfirst:  %#v\nsecond: %#v", gotFirst, gotSecond)
	}
	want := ReachResult{
		State: ReachFound,
		From:  []string{source},
		To:    []string{target},
		Paths: []PathWitness{{
			From: source,
			To:   target,
			Path: []string{source, left, target},
		}},
	}
	if !reflect.DeepEqual(gotFirst, want) {
		t.Fatalf("canonical result mismatch\nwant: %#v\ngot:  %#v", want, gotFirst)
	}
}

func TestEvaluateReachDeterministicCandidatePrecedence(t *testing.T) {
	const (
		source      = "svc.Source"
		otherSource = "svc.ZSource"
		left        = "svc.Left"
		right       = "svc.Right"
		fnTarget    = "svc.Target"
		effectA     = "boundary:db UPDATE alpha"
		effectB     = "boundary:db UPDATE beta"
	)

	tests := []struct {
		name string
		g    *graph.Graph
		from []string
		to   []string
		want PathWitness
	}{
		{
			name: "sorted callees choose the first equal length function path",
			g: &graph.Graph{
				Nodes: []graph.Node{
					{FQN: source}, {FQN: right}, {FQN: left}, {FQN: fnTarget},
				},
				Edges: []graph.Edge{
					{From: source, To: right},
					{From: right, To: fnTarget},
					{From: source, To: left},
					{From: left, To: fnTarget},
				},
			},
			from: []string{source},
			to:   []string{fnTarget},
			want: PathWitness{From: source, To: fnTarget, Path: []string{source, left, fnTarget}},
		},
		{
			name: "function target precedes boundary target at the same level",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: fnTarget}},
				Edges: []graph.Edge{
					{From: source, To: effectA, Boundary: "outbound-sync"},
					{From: source, To: fnTarget},
				},
			},
			from: []string{source},
			to:   []string{effectA, fnTarget},
			want: PathWitness{From: source, To: fnTarget, Path: []string{source, fnTarget}},
		},
		{
			name: "boundary label precedes owner for equal length effects",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: left}, {FQN: right}},
				Edges: []graph.Edge{
					{From: source, To: right},
					{From: right, To: effectA, Boundary: "outbound-sync"},
					{From: source, To: left},
					{From: left, To: effectB, Boundary: "outbound-sync"},
				},
			},
			from: []string{source},
			to:   []string{"boundary:db UPDATE"},
			want: PathWitness{From: source, To: effectA, Path: []string{source, right, effectA}},
		},
		{
			name: "source fqn has priority",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: otherSource}, {FQN: source}},
				Edges: []graph.Edge{
					{From: otherSource, To: effectB, Boundary: "outbound-sync"},
					{From: source, To: effectA, Boundary: "outbound-sync"},
				},
			},
			from: []string{otherSource, source},
			to:   []string{"boundary:db UPDATE"},
			want: PathWitness{From: source, To: effectA, Path: []string{source, effectA}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateReach(graph.NewIndex(tt.g), tt.from, tt.to)
			if got.State != ReachFound || len(got.Paths) != 1 || !reflect.DeepEqual(got.Paths[0], tt.want) {
				t.Fatalf("path precedence mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

func TestEvaluateReachFoundDominatesBlindAcrossSources(t *testing.T) {
	const (
		blindSource = "svc.ASource"
		blindMid    = "svc.Blind"
		foundSource = "svc.ZSource"
		target      = "boundary:db UPDATE users"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{{FQN: blindSource}, {FQN: blindMid}, {FQN: foundSource}},
		Edges: []graph.Edge{
			{From: blindSource, To: blindMid},
			{From: foundSource, To: target, Boundary: "outbound-sync"},
		},
		BlindSpots: []graph.BlindSpot{{Kind: "reflect", Site: blindMid, Detail: "opaque"}},
	}

	got := EvaluateReach(
		graph.NewIndex(g),
		[]string{blindSource, foundSource},
		[]string{"boundary:db UPDATE"},
	)
	want := ReachResult{
		State: ReachFound,
		From:  []string{blindSource, foundSource},
		To:    []string{target},
		Paths: []PathWitness{{
			From: foundSource,
			To:   target,
			Path: []string{foundSource, target},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("found must dominate blind\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestEvaluateReachBlindClassification(t *testing.T) {
	const (
		source = "example.com/svc/handler.Source"
		mid    = "example.com/svc/unsafe.Mid"
		other  = "example.com/svc/handler.Other"
		target = "boundary:db UPDATE users"
		dyn    = "boundary:bus PUBLISH <dynamic>"
	)

	tests := []struct {
		name string
		g    *graph.Graph
		want ReachResult
	}{
		{
			name: "disclosure only spots do not blind absence",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: mid}, {FQN: other}},
				Edges: []graph.Edge{
					{From: source, To: mid},
					{From: other, To: target, Boundary: "outbound-sync"},
				},
				BlindSpots: []graph.BlindSpot{{
					Kind: string(blindspots.ExternalBoundaryCall),
					Site: mid, Detail: "known external leaf",
				}},
			},
			want: ReachResult{
				State: ReachAbsent,
				From:  []string{source},
				To:    []string{target},
			},
		},
		{
			name: "package blind spot is typed",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: mid}, {FQN: other}},
				Edges: []graph.Edge{
					{From: source, To: mid},
					{From: other, To: target, Boundary: "outbound-sync"},
				},
				BlindSpots: []graph.BlindSpot{{
					Kind: "unsafe", Site: "example.com/svc/unsafe", Detail: "unsafe import",
				}},
			},
			want: ReachResult{
				State: ReachBlind,
				From:  []string{source},
				To:    []string{target},
				Blind: &BlindWitness{
					From:     source,
					Site:     "example.com/svc/unsafe",
					Kind:     "unsafe",
					Detail:   "unsafe import",
					Location: BlindInPackage,
				},
			},
		},
		{
			name: "unmatched dynamic effect blinds absence",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: other}},
				Edges: []graph.Edge{
					{From: source, To: dyn, Boundary: "outbound-async"},
					{From: other, To: target, Boundary: "outbound-sync"},
				},
			},
			want: ReachResult{
				State: ReachBlind,
				From:  []string{source},
				To:    []string{target},
				Blind: &BlindWitness{
					From:     source,
					Site:     source,
					Kind:     "DynamicEffect",
					Detail:   dyn,
					Location: BlindAtDynamicEffect,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateReach(
				graph.NewIndex(tt.g),
				[]string{source},
				[]string{"boundary:db UPDATE"},
			)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("blind classification mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

func TestEvaluateReachCanonicalBlindTie(t *testing.T) {
	const (
		source = "svc.Source"
		left   = "svc.Left"
		right  = "svc.Right"
		other  = "svc.Other"
		target = "boundary:db UPDATE users"
	)
	graphWith := func(spots []graph.BlindSpot) *graph.Graph {
		return &graph.Graph{
			Nodes: []graph.Node{
				{FQN: source}, {FQN: left}, {FQN: right}, {FQN: other},
			},
			Edges: []graph.Edge{
				{From: source, To: left},
				{From: source, To: right},
				{From: other, To: target, Boundary: "outbound-sync"},
			},
			BlindSpots: spots,
		}
	}
	first := []graph.BlindSpot{
		{Kind: "unsafe", Site: left, Detail: "z"},
		{Kind: "reflect", Site: right, Detail: "z"},
		{Kind: "reflect", Site: right, Detail: "a"},
		{Kind: "reflect", Site: right, Detail: "a"},
	}
	second := []graph.BlindSpot{
		{Kind: "reflect", Site: right, Detail: "a"},
		{Kind: "reflect", Site: right, Detail: "z"},
		{Kind: "unsafe", Site: left, Detail: "z"},
		{Kind: "reflect", Site: right, Detail: "a"},
	}

	evaluate := func(spots []graph.BlindSpot) ReachResult {
		return EvaluateReach(
			graph.NewIndex(graphWith(spots)),
			[]string{source},
			[]string{"boundary:db UPDATE"},
		)
	}
	gotFirst, gotSecond := evaluate(first), evaluate(second)
	if !reflect.DeepEqual(gotFirst, gotSecond) {
		t.Fatalf("shuffled duplicate blind spots changed result\nfirst:  %#v\nsecond: %#v", gotFirst, gotSecond)
	}
	want := ReachResult{
		State: ReachBlind,
		From:  []string{source},
		To:    []string{target},
		Blind: &BlindWitness{
			From: source, Site: right, Kind: "reflect", Detail: "a", Location: BlindAtFunction,
		},
	}
	if !reflect.DeepEqual(gotFirst, want) {
		t.Fatalf("blind tie mismatch\nwant: %#v\ngot:  %#v", want, gotFirst)
	}
}

func TestEvaluateReachMatchingDynamicEffectIsAHit(t *testing.T) {
	const (
		source = "svc.Source"
		dyn    = "boundary:bus PUBLISH <dynamic>"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{{FQN: source}},
		Edges: []graph.Edge{{From: source, To: dyn, Boundary: "outbound-async"}},
	}

	got := EvaluateReach(graph.NewIndex(g), []string{source}, []string{dyn})
	want := ReachResult{
		State: ReachFound,
		From:  []string{source},
		To:    []string{dyn},
		Paths: []PathWitness{{From: source, To: dyn, Path: []string{source, dyn}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matching dynamic target must be a hit before blindness\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestEvaluateReachExcludesSourceAsFunctionHit(t *testing.T) {
	const source = "svc.Source"
	g := &graph.Graph{Nodes: []graph.Node{{FQN: source}}}

	got := EvaluateReach(graph.NewIndex(g), []string{source}, []string{source})
	want := ReachResult{State: ReachAbsent, From: []string{source}, To: []string{source}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source must not hit itself without a traversed target\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestEvaluateReachEntrypointSelector(t *testing.T) {
	const (
		source = "svc.Source"
		mid    = "svc.Mid"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{{FQN: mid}, {FQN: source}, {FQN: source}},
		Edges: []graph.Edge{{From: source, To: mid}, {From: source, To: mid}},
	}

	got := EvaluateReach(
		graph.NewIndex(g),
		[]string{policy.EntrypointSelector},
		[]string{mid},
	)
	want := ReachResult{
		State: ReachFound,
		From:  []string{source},
		To:    []string{mid},
		Paths: []PathWitness{{From: source, To: mid, Path: []string{source, mid}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entrypoint:* binding mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestEvaluateReachBoundSourcesDoesNotNameExpand(t *testing.T) {
	const (
		source  = "svc.Source"
		closure = "svc.Source$1"
		target  = "boundary:db UPDATE users"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{{FQN: source}, {FQN: closure}},
		Edges: []graph.Edge{{From: closure, To: target, Boundary: "outbound-sync"}},
	}

	got := EvaluateReachBoundSources(
		graph.NewIndex(g),
		[]string{source},
		[]string{"boundary:db UPDATE"},
	)
	want := ReachResult{State: ReachAbsent, From: []string{source}, To: []string{target}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("already-bound sources must not be name-expanded\nwant: %#v\ngot:  %#v", want, got)
	}
}
