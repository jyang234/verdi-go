package claims

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

func TestPassThroughClaimStatesAndFailClosedBindings(t *testing.T) {
	const (
		source  = "svc.Source"
		mid     = "svc.Mid"
		target  = "svc.Target"
		guard   = "svc.Guard"
		blind   = "svc.Blind"
		missing = "svc.Missing"
	)
	base := func() Claim {
		return Claim{
			ID: "guarded", Kind: "pass_through",
			From: sel(source), To: sel(target), Through: sel(guard),
		}
	}

	tests := []struct {
		name string
		g    *graph.Graph
		edit func(*Claim)
		want Result
	}{
		{
			name: "bypass fails with full path",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: mid}, {FQN: target}, {FQN: guard}},
				Edges: []graph.Edge{{From: source, To: mid}, {From: mid, To: target}},
			},
			want: Result{
				ID: "guarded", Kind: "pass_through", Label: "guarded", Outcome: Fail,
				Detail: "bypass path found",
				Bindings: Bindings{
					From: []string{source}, To: []string{target}, Through: []string{guard},
				},
				Witnesses: []Witness{{
					From: source, To: target, Path: []string{source, mid, target},
				}},
			},
		},
		{
			name: "guarded path passes",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: guard}, {FQN: target}},
				Edges: []graph.Edge{{From: source, To: guard}, {From: guard, To: target}},
			},
			want: Result{
				ID: "guarded", Kind: "pass_through", Label: "guarded", Outcome: Pass,
				Detail: "all visible paths pass through the waypoint",
				Bindings: Bindings{
					From: []string{source}, To: []string{target}, Through: []string{guard},
				},
			},
		},
		{
			name: "disconnected visible target is vacuously guarded",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: guard}, {FQN: target}},
			},
			want: Result{
				ID: "guarded", Kind: "pass_through", Label: "guarded", Outcome: Pass,
				Detail: "all visible paths pass through the waypoint",
				Bindings: Bindings{
					From: []string{source}, To: []string{target}, Through: []string{guard},
				},
			},
		},
		{
			name: "blind frontier errors",
			g: &graph.Graph{
				Nodes: []graph.Node{
					{FQN: source}, {FQN: blind}, {FQN: guard}, {FQN: target},
				},
				Edges: []graph.Edge{{From: source, To: blind}},
				BlindSpots: []graph.BlindSpot{{
					Kind: "reflect", Site: blind, Detail: "opaque dispatch",
				}},
			},
			want: Result{
				ID: "guarded", Kind: "pass_through", Label: "guarded",
				Outcome: Errored, Reason: ReasonBlindFrontier,
				Detail: "no bypass found, but the frontier is blind at " + blind,
				Bindings: Bindings{
					From: []string{source}, To: []string{target}, Through: []string{guard},
				},
				Witnesses: []Witness{{From: source, BlindSite: blind}},
			},
		},
		{
			name: "unbound source errors first",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: guard}, {FQN: target}},
			},
			edit: func(c *Claim) { c.From = sel(missing) },
			want: Result{
				ID: "guarded", Kind: "pass_through", Label: "guarded",
				Outcome: Errored, Reason: ReasonUnboundSelector,
				Detail:   "from selector(s) bind nothing: " + missing,
				Bindings: Bindings{To: []string{target}, Through: []string{guard}},
			},
		},
		{
			name: "unbound target errors",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: guard}},
			},
			edit: func(c *Claim) { c.To = sel(missing) },
			want: Result{
				ID: "guarded", Kind: "pass_through", Label: "guarded",
				Outcome: Errored, Reason: ReasonUnboundSelector,
				Detail:   "to selector(s) bind nothing: " + missing,
				Bindings: Bindings{From: []string{source}, Through: []string{guard}},
			},
		},
		{
			name: "unbound waypoint errors even when traversal bypasses",
			g: &graph.Graph{
				Nodes: []graph.Node{{FQN: source}, {FQN: target}},
				Edges: []graph.Edge{{From: source, To: target}},
			},
			edit: func(c *Claim) { c.Through = sel(missing) },
			want: Result{
				ID: "guarded", Kind: "pass_through", Label: "guarded",
				Outcome: Errored, Reason: ReasonUnboundSelector,
				Detail: "through selector(s) bind nothing: " + missing,
				Bindings: Bindings{
					From: []string{source}, To: []string{target},
				},
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
				t.Fatalf("pass_through result mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

func TestPassThroughClaimValidationAndFallbackLabel(t *testing.T) {
	const (
		sourceA = "svc.ASource"
		sourceZ = "svc.ZSource"
		target  = "svc.Target"
		guard   = "svc.Guard"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: sourceZ}, {FQN: guard}, {FQN: target}, {FQN: sourceA},
		},
		Edges: []graph.Edge{{From: sourceA, To: target}},
	}
	tests := []struct {
		name string
		c    Claim
		want string
	}{
		{
			name: "missing from",
			c:    Claim{Kind: "pass_through", To: sel(target), Through: sel(guard)},
			want: "pass_through requires 'from', 'to', and 'through'",
		},
		{
			name: "missing to",
			c:    Claim{Kind: "pass_through", From: sel(sourceA), Through: sel(guard)},
			want: "pass_through requires 'from', 'to', and 'through'",
		},
		{
			name: "missing through",
			c:    Claim{Kind: "pass_through", From: sel(sourceA), To: sel(target)},
			want: "pass_through requires 'from', 'to', and 'through'",
		},
		{
			name: "wrong expect field",
			c: Claim{
				Kind: "pass_through", From: sel(sourceA), To: sel(target),
				Through: sel(guard), Expect: "present",
			},
			want: `pass_through does not accept field "expect"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalOneG(t, g, tt.c)
			if got.Outcome != Errored || got.Reason != ReasonMalformedClaim || got.Detail != tt.want {
				t.Fatalf("malformed pass_through = %#v, want %q", got, tt.want)
			}
		})
	}

	listClaim := Claim{
		Kind: "pass_through",
		From: Selectors{
			values: []string{sourceZ, sourceA}, wasList: true,
		},
		To:      sel(target),
		Through: sel(guard),
	}
	got := evalOneG(t, g, listClaim)
	if got.Label != sourceZ+", "+sourceA+" -> "+guard+" -> "+target {
		t.Fatalf("fallback label = %q", got.Label)
	}
	if !reflect.DeepEqual(got.Bindings.From, []string{sourceA, sourceZ}) {
		t.Fatalf("From bindings = %#v", got.Bindings.From)
	}
}

func TestPassThroughClaimMachineEvidence(t *testing.T) {
	const (
		source = "svc.Source"
		mid    = "svc.Mid"
		target = "boundary:db UPDATE users"
		guard  = "svc.Guard"
	)
	g := &graph.Graph{
		Stamp: "pass-stamp", Tool: "v0.0.0-test", Algo: "vta",
		Nodes: []graph.Node{{FQN: source}, {FQN: mid}, {FQN: guard}},
		Edges: []graph.Edge{
			{From: source, To: mid},
			{From: mid, To: target, Boundary: "outbound-sync"},
		},
	}
	report := Evaluate(g, &File{Claims: []Claim{{
		ID: "bypass", Kind: "pass_through",
		From: sel(source), To: sel(target), Through: sel(guard),
	}}})
	data, err := MarshalMachine(g, report)
	if err != nil {
		t.Fatal(err)
	}
	var dto JSONReport
	if err := json.Unmarshal(data, &dto); err != nil {
		t.Fatal(err)
	}
	want := []JSONResult{{
		ID: "bypass", Kind: "pass_through", Outcome: "FAIL",
		Detail: "bypass path found",
		Bindings: &JSONBindings{
			From: []string{source}, To: []string{target}, Through: []string{guard},
		},
		Witnesses: []JSONWitness{{
			From: source, To: target, Path: []string{source, mid, target},
		}},
	}}
	if !reflect.DeepEqual(dto.Results, want) {
		t.Fatalf("machine pass_through mismatch\nwant: %#v\ngot:  %#v\n%s", want, dto.Results, data)
	}
}
