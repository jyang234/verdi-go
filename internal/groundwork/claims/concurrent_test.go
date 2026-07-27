package claims

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

func TestConcurrentClaimStates(t *testing.T) {
	const (
		launcher = "svc.Launcher"
		worker   = "svc.Worker"
		target   = "svc.Target"
		missing  = "svc.Missing"
	)
	base := func() Claim {
		return Claim{ID: "no-async", Kind: "no_concurrent_reach", To: sel(target)}
	}
	tests := []struct {
		name string
		g    *graph.Graph
		edit func(*Claim)
		want Result
	}{
		{
			name: "hit fails",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: launcher}, {FQN: worker}, {FQN: target}},
				Edges: []graph.Edge{
					{From: launcher, To: worker, Concurrent: true},
					{From: worker, To: target},
				},
			},
			want: Result{
				ID: "no-async", Kind: "no_concurrent_reach", Label: "no-async",
				Outcome: Fail, Detail: "target reachable on a concurrent path",
				Bindings:  Bindings{To: []string{target}},
				Witnesses: []Witness{{To: target}},
			},
		},
		{
			name: "clean passes",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: launcher}, {FQN: worker}, {FQN: target}},
				Edges: []graph.Edge{{From: launcher, To: worker, Concurrent: true}},
			},
			want: Result{
				ID: "no-async", Kind: "no_concurrent_reach", Label: "no-async",
				Outcome: Pass, Detail: "no concurrent path found",
				Bindings: Bindings{To: []string{target}},
			},
		},
		{
			name: "blind errors",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: launcher}, {FQN: worker}, {FQN: target}},
				Edges: []graph.Edge{{From: launcher, To: worker, Concurrent: true}},
				BlindSpots: []graph.BlindSpot{{
					Kind: "reflect", Site: worker, Detail: "opaque dispatch",
				}},
			},
			want: Result{
				ID: "no-async", Kind: "no_concurrent_reach", Label: "no-async",
				Outcome: Errored, Reason: ReasonBlindFrontier,
				Detail:    "no concurrent path found, but the frontier is blind at " + worker,
				Bindings:  Bindings{To: []string{target}},
				Witnesses: []Witness{{BlindSite: worker}},
			},
		},
		{
			name: "unbound errors",
			g:    &graph.Graph{Nodes: []graph.Node{{FQN: target}}},
			edit: func(c *Claim) { c.To = sel(missing) },
			want: Result{
				ID: "no-async", Kind: "no_concurrent_reach", Label: "no-async",
				Outcome: Errored, Reason: ReasonUnboundSelector,
				Detail: "to selector(s) bind nothing: " + missing,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claim := base()
			if tt.edit != nil {
				tt.edit(&claim)
			}
			got := evalOneG(t, tt.g, claim)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("concurrent result mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

func TestConcurrentClaimValidationListsAndFallbackLabel(t *testing.T) {
	const (
		targetA = "boundary:db UPDATE alpha"
		targetZ = "boundary:db UPDATE zeta"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{{FQN: "svc.Worker"}},
		Edges: []graph.Edge{
			{From: "svc.OffConeZ", To: targetZ, Boundary: "outbound-sync"},
			{From: "svc.OffConeA", To: targetA, Boundary: "outbound-sync"},
		},
	}
	tests := []struct {
		name string
		c    Claim
		want string
	}{
		{
			name: "missing to",
			c:    Claim{Kind: "no_concurrent_reach"},
			want: "no_concurrent_reach requires 'to'",
		},
		{
			name: "wrong from",
			c: Claim{
				Kind: "no_concurrent_reach", From: sel("svc.Source"), To: sel(targetA),
			},
			want: `no_concurrent_reach does not accept field "from"`,
		},
		{
			name: "wrong through",
			c: Claim{
				Kind: "no_concurrent_reach", To: sel(targetA), Through: sel("svc.Guard"),
			},
			want: `no_concurrent_reach does not accept field "through"`,
		},
		{
			name: "wrong expect",
			c: Claim{
				Kind: "no_concurrent_reach", To: sel(targetA), Expect: "absent",
			},
			want: `no_concurrent_reach does not accept field "expect"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalOneG(t, g, tt.c)
			if got.Outcome != Errored || got.Reason != ReasonMalformedClaim || got.Detail != tt.want {
				t.Fatalf("malformed no_concurrent_reach = %#v, want %q", got, tt.want)
			}
		})
	}

	claim := Claim{
		Kind: "no_concurrent_reach",
		To: Selectors{
			values: []string{targetZ, targetA, targetZ}, wasList: true,
		},
	}
	got := evalOneG(t, g, claim)
	want := Result{
		Kind:    "no_concurrent_reach",
		Label:   "concurrent -> " + targetZ + ", " + targetA + ", " + targetZ,
		Outcome: Pass,
		Detail:  "no concurrent path found",
		Bindings: Bindings{
			To: []string{targetA, targetZ},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list-selector concurrent mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestConcurrentClaimBindingsAndMachineWitnesses(t *testing.T) {
	const (
		launcher = "svc.Launcher"
		worker   = "svc.Worker"
		owner    = "svc.Owner"
		blind    = "svc.Blind"
		update   = "boundary:db UPDATE users"
		publish  = "boundary:bus PUBLISH user.updated"
		target   = "svc.Target"
	)
	g := &graph.Graph{
		Stamp: "concurrent-stamp", Tool: "v0.0.0-test", Algo: "vta",
		Nodes: []graph.Node{
			{FQN: target}, {FQN: owner}, {FQN: blind}, {FQN: worker}, {FQN: launcher},
		},
		Edges: []graph.Edge{
			{From: owner, To: publish, Boundary: "outbound-async", Concurrent: true},
			{From: launcher, To: worker, Concurrent: true},
			{From: worker, To: update, Boundary: "outbound-sync"},
			{From: worker, To: update, Boundary: "outbound-sync"},
		},
		BlindSpots: []graph.BlindSpot{{
			Kind: "ConcurrentDispatch", Site: blind, Detail: "go f()",
		}},
	}
	file := &File{Claims: []Claim{
		{
			ID: "hits", Kind: "no_concurrent_reach",
			To: Selectors{
				values: []string{publish, worker, update, publish}, wasList: true,
			},
		},
		{
			ID: "blind", Kind: "no_concurrent_reach", To: sel(target),
		},
	}}
	report := Evaluate(g, file)
	wantResults := []Result{
		{
			ID: "hits", Kind: "no_concurrent_reach", Label: "hits",
			Outcome: Fail, Detail: "target reachable on a concurrent path",
			Bindings: Bindings{To: []string{publish, update, worker}},
			Witnesses: []Witness{
				{To: worker},
				{From: owner, To: publish},
				{From: worker, To: update},
			},
		},
		{
			ID: "blind", Kind: "no_concurrent_reach", Label: "blind",
			Outcome: Errored, Reason: ReasonBlindFrontier,
			Detail:    "no concurrent path found, but the frontier is blind at " + blind,
			Bindings:  Bindings{To: []string{target}},
			Witnesses: []Witness{{BlindSite: blind}},
		},
	}
	if !reflect.DeepEqual(report.Results, wantResults) {
		t.Fatalf("concurrent claim evidence mismatch\nwant: %#v\ngot:  %#v", wantResults, report.Results)
	}

	data, err := MarshalMachine(g, report)
	if err != nil {
		t.Fatal(err)
	}
	var dto JSONReport
	if err := json.Unmarshal(data, &dto); err != nil {
		t.Fatal(err)
	}
	wantJSON := []JSONResult{
		{
			ID: "hits", Kind: "no_concurrent_reach", Outcome: "FAIL",
			Detail:   "target reachable on a concurrent path",
			Bindings: &JSONBindings{To: []string{publish, update, worker}},
			Witnesses: []JSONWitness{
				{To: worker},
				{From: owner, To: publish},
				{From: worker, To: update},
			},
		},
		{
			ID: "blind", Kind: "no_concurrent_reach", Outcome: "ERROR",
			Reason:    string(ReasonBlindFrontier),
			Detail:    "no concurrent path found, but the frontier is blind at " + blind,
			Bindings:  &JSONBindings{To: []string{target}},
			Witnesses: []JSONWitness{{BlindSite: blind}},
		},
	}
	if !reflect.DeepEqual(dto.Results, wantJSON) {
		t.Fatalf("machine concurrent evidence mismatch\nwant: %#v\ngot:  %#v\n%s", wantJSON, dto.Results, data)
	}
}

func TestConcurrentClaimsLazilyReuseOneModelLocalSurface(t *testing.T) {
	const (
		launcher = "svc.Launcher"
		target   = "svc.Target"
	)
	cleanGraph := &graph.Graph{Nodes: []graph.Node{{FQN: launcher}, {FQN: target}}}
	hitGraph := &graph.Graph{
		Nodes: []graph.Node{{FQN: launcher}, {FQN: target}},
		Edges: []graph.Edge{{From: launcher, To: target, Concurrent: true}},
	}
	claim := Claim{ID: "same", Kind: "no_concurrent_reach", To: sel(target)}

	m := newModel(cleanGraph)
	m.reachIndex = graph.NewIndex(hitGraph)
	if got := m.eval(claim); got.Outcome != Fail {
		t.Fatalf("surface must be lazy and observe the index at first concurrent claim: %#v", got)
	}
	m.reachIndex = graph.NewIndex(cleanGraph)
	if got := m.eval(claim); got.Outcome != Fail {
		t.Fatalf("second concurrent claim rebuilt instead of reusing the first surface: %#v", got)
	}

	other := newModel(cleanGraph)
	if got := other.eval(claim); got.Outcome != Pass {
		t.Fatalf("surface cache leaked across models: %#v", got)
	}
}
