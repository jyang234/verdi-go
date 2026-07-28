package claims

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

func TestReachClaimFactExpectMatrix(t *testing.T) {
	const (
		source = "svc.Source"
		mid    = "svc.Mid"
		other  = "svc.Other"
		target = "svc.Target"
	)

	foundGraph := func() *graph.Graph {
		return &graph.Graph{
			Nodes: []graph.Node{
				{FQN: source}, {FQN: mid}, {FQN: target},
			},
			Edges: []graph.Edge{
				{From: source, To: mid},
				{From: mid, To: target},
			},
		}
	}
	absentGraph := func() *graph.Graph {
		return &graph.Graph{
			Nodes: []graph.Node{
				{FQN: source}, {FQN: other}, {FQN: target},
			},
			Edges: []graph.Edge{{From: other, To: target}},
		}
	}
	blindGraph := func() *graph.Graph {
		g := absentGraph()
		g.Nodes = append(g.Nodes, graph.Node{FQN: mid})
		g.Edges = append(g.Edges, graph.Edge{From: source, To: mid})
		g.BlindSpots = []graph.BlindSpot{{
			Kind: "reflect", Site: mid, Detail: "opaque dispatch",
		}}
		return g
	}

	tests := []struct {
		name   string
		g      func() *graph.Graph
		to     string
		expect string
		want   Result
	}{
		{
			name: "found present passes", g: foundGraph, to: target, expect: "present",
			want: Result{
				ID: "matrix", Kind: "reach", Label: "matrix", Outcome: Pass,
				Detail:   "path found",
				Bindings: Bindings{From: []string{source}, To: []string{target}},
				Witnesses: []Witness{{
					From: source, To: target, Path: []string{source, mid, target},
				}},
			},
		},
		{
			name: "found absent fails", g: foundGraph, to: target, expect: "absent",
			want: Result{
				ID: "matrix", Kind: "reach", Label: "matrix", Outcome: Fail,
				Detail:   "path found",
				Bindings: Bindings{From: []string{source}, To: []string{target}},
				Witnesses: []Witness{{
					From: source, To: target, Path: []string{source, mid, target},
				}},
			},
		},
		{
			name: "absent present fails", g: absentGraph, to: target, expect: "present",
			want: Result{
				ID: "matrix", Kind: "reach", Label: "matrix", Outcome: Fail,
				Detail:   "no path found",
				Bindings: Bindings{From: []string{source}, To: []string{target}},
			},
		},
		{
			name: "absent absent passes", g: absentGraph, to: target, expect: "absent",
			want: Result{
				ID: "matrix", Kind: "reach", Label: "matrix", Outcome: Pass,
				Detail:   "no path found",
				Bindings: Bindings{From: []string{source}, To: []string{target}},
			},
		},
		{
			name: "blind present errors", g: blindGraph, to: target, expect: "present",
			want: Result{
				ID: "matrix", Kind: "reach", Label: "matrix", Outcome: Errored,
				Reason: ReasonBlindFrontier,
				Detail: "no path found, but the frontier is blind at " + mid,
				Bindings: Bindings{
					From: []string{source}, To: []string{target},
				},
				Witnesses: []Witness{{From: source, BlindSite: mid}},
			},
		},
		{
			name: "blind absent errors", g: blindGraph, to: target, expect: "absent",
			want: Result{
				ID: "matrix", Kind: "reach", Label: "matrix", Outcome: Errored,
				Reason: ReasonBlindFrontier,
				Detail: "no path found, but the frontier is blind at " + mid,
				Bindings: Bindings{
					From: []string{source}, To: []string{target},
				},
				Witnesses: []Witness{{From: source, BlindSite: mid}},
			},
		},
		{
			name: "unbound present errors", g: absentGraph, to: "svc.Missing", expect: "present",
			want: Result{
				ID: "matrix", Kind: "reach", Label: "matrix", Outcome: Errored,
				Reason:   ReasonUnboundSelector,
				Detail:   "to selector(s) bind nothing: svc.Missing",
				Bindings: Bindings{From: []string{source}},
			},
		},
		{
			name: "unbound absent errors", g: absentGraph, to: "svc.Missing", expect: "absent",
			want: Result{
				ID: "matrix", Kind: "reach", Label: "matrix", Outcome: Errored,
				Reason:   ReasonUnboundSelector,
				Detail:   "to selector(s) bind nothing: svc.Missing",
				Bindings: Bindings{From: []string{source}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalOneG(t, tt.g(), Claim{
				ID: "matrix", Kind: "reach",
				From: sel(source), To: sel(tt.to), Expect: tt.expect,
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("reach result mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

func TestReachClaimRejectsMalformedFields(t *testing.T) {
	const (
		source = "svc.Source"
		target = "svc.Target"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{{FQN: source}, {FQN: target}},
		Edges: []graph.Edge{{From: source, To: target}},
	}

	tests := []struct {
		name string
		c    Claim
		want string
	}{
		{
			name: "missing from",
			c:    Claim{Kind: "reach", To: sel(target), Expect: "present"},
			want: "reach requires 'from' and 'to'",
		},
		{
			name: "missing to",
			c:    Claim{Kind: "reach", From: sel(source), Expect: "present"},
			want: "reach requires 'from' and 'to'",
		},
		{
			name: "missing expect",
			c:    Claim{Kind: "reach", From: sel(source), To: sel(target)},
			want: `reach requires expect "present" or "absent"`,
		},
		{
			name: "malformed expect",
			c:    Claim{Kind: "reach", From: sel(source), To: sel(target), Expect: "sometimes"},
			want: `reach requires expect "present" or "absent"`,
		},
		{
			name: "wrong kind field",
			c: Claim{
				Kind: "reach", From: sel(source), To: sel(target),
				Through: sel("svc.Waypoint"), Expect: "present",
			},
			want: `reach does not accept field "through"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evalOneG(t, g, tt.c)
			if got.Outcome != Errored || got.Reason != ReasonMalformedClaim || got.Detail != tt.want {
				t.Fatalf("malformed reach = %#v, want MALFORMED_CLAIM %q", got, tt.want)
			}
		})
	}
}

func TestReachClaimListSelectorsBindingsAndFallbackLabel(t *testing.T) {
	const (
		sourceA = "svc.ASource"
		sourceZ = "svc.ZSource"
		targetA = "boundary:db UPDATE alpha"
		targetZ = "boundary:db UPDATE zeta"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: sourceZ}, {FQN: sourceA}, {FQN: sourceA},
		},
		Edges: []graph.Edge{
			{From: sourceZ, To: targetZ, Boundary: "outbound-sync"},
			{From: sourceA, To: targetA, Boundary: "outbound-sync"},
			{From: sourceA, To: targetA, Boundary: "outbound-sync"},
		},
	}
	c := Claim{
		Kind: "reach",
		From: Selectors{
			values:  []string{sourceZ, sourceA, sourceZ},
			wasList: true,
		},
		To: Selectors{
			values:  []string{targetZ, targetA},
			wasList: true,
		},
		Expect: "present",
	}

	got := evalOneG(t, g, c)
	want := Result{
		Kind:    "reach",
		Label:   sourceZ + ", " + sourceA + ", " + sourceZ + " -> " + targetZ + ", " + targetA,
		Outcome: Pass,
		Detail:  "path found",
		Bindings: Bindings{
			From: []string{sourceA, sourceZ},
			To:   []string{targetA, targetZ},
		},
		Witnesses: []Witness{{
			From: sourceA, To: targetA, Path: []string{sourceA, targetA},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list-selector reach mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestReachClaimBothUnboundSidesAreExactSortedSets(t *testing.T) {
	g := &graph.Graph{Nodes: []graph.Node{{FQN: "svc.Live"}}}
	c := Claim{
		Kind: "reach",
		From: Selectors{
			values:  []string{" z", "svc.Missing", " z"},
			wasList: true,
		},
		To: Selectors{
			values:  []string{"boundary:missing Z", "boundary:missing A", "boundary:missing Z"},
			wasList: true,
		},
		Expect: "absent",
	}

	got := evalOneG(t, g, c)
	if got.Outcome != Errored || got.Reason != ReasonUnboundSelector {
		t.Fatalf("both-unbound reach = %#v, want UNBOUND_SELECTOR", got)
	}
	if got.Detail != "from selector(s) bind nothing:  z, svc.Missing" {
		t.Errorf("Detail = %q, want exact sorted selector bytes", got.Detail)
	}
	if len(got.Bindings.From) != 0 || len(got.Bindings.To) != 0 {
		t.Errorf("unbound claim fabricated bindings: %#v", got.Bindings)
	}
}

// TestReachClaimAbstainsOnBlindSpotAtUnparsableConeMember is the claim-level
// consequence of the unconditional package-site probe: a blind spot recorded at
// the empty site, reachable through a cone member whose FQN parses to no
// package, must abstain (ERROR BLIND_FRONTIER) instead of grading the absence
// claim as a proof. A blind frontier is never a pass.
func TestReachClaimAbstainsOnBlindSpotAtUnparsableConeMember(t *testing.T) {
	const (
		source     = "example.com/svc/handler.Source"
		unparsable = "(unclosed"
		target     = "example.com/svc/store.Sink"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{{FQN: source}, {FQN: unparsable}, {FQN: target}},
		Edges: []graph.Edge{{From: source, To: unparsable}},
		BlindSpots: []graph.BlindSpot{{
			Kind: "reflect", Site: "", Detail: "dispatch table built at init",
		}},
	}

	got := evalOneG(t, g, Claim{
		ID: "blind-empty-site", Kind: "reach",
		From: sel(source), To: sel(target), Expect: "absent",
	})
	want := Result{
		ID: "blind-empty-site", Kind: "reach", Label: "blind-empty-site",
		Outcome: Errored, Reason: ReasonBlindFrontier,
		Detail: "no path found, but the frontier is blind at ",
		Bindings: Bindings{
			From: []string{source}, To: []string{target},
		},
		Witnesses: []Witness{{From: source}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("blind-frontier abstention mismatch\nwant: %#v\ngot:  %#v", want, got)
	}
}

func TestReachClaimMachineBindingsAndWitnessJSON(t *testing.T) {
	const (
		found    = "svc.Found"
		mid      = "svc.Mid"
		target   = "svc.Target"
		blind    = "svc.Blind"
		blindMid = "svc.BlindMid"
		other    = "svc.Other"
	)
	g := &graph.Graph{
		Stamp: "reach-stamp",
		Tool:  "v0.0.0-test",
		Algo:  "vta",
		Nodes: []graph.Node{
			{FQN: found}, {FQN: mid}, {FQN: target},
			{FQN: blind}, {FQN: blindMid}, {FQN: other},
		},
		Edges: []graph.Edge{
			{From: found, To: mid},
			{From: mid, To: target},
			{From: blind, To: blindMid},
			{From: other, To: target},
		},
		BlindSpots: []graph.BlindSpot{{
			Kind: "reflect", Site: blindMid, Detail: "opaque dispatch",
		}},
	}
	file := &File{Claims: []Claim{
		{
			ID: "found", Kind: "reach",
			From: sel(found), To: sel(target), Expect: "present",
		},
		{
			ID: "blind", Kind: "reach",
			From: sel(blind), To: sel(target), Expect: "absent",
		},
	}}

	data, err := MarshalMachine(g, Evaluate(g, file))
	if err != nil {
		t.Fatal(err)
	}
	var dto JSONReport
	if err := json.Unmarshal(data, &dto); err != nil {
		t.Fatal(err)
	}
	want := []JSONResult{
		{
			ID: "found", Kind: "reach", Outcome: "PASS", Detail: "path found",
			Bindings: &JSONBindings{
				From: []string{found},
				To:   []string{target},
			},
			Witnesses: []JSONWitness{{
				From: found, To: target, Path: []string{found, mid, target},
			}},
		},
		{
			ID: "blind", Kind: "reach", Outcome: "ERROR",
			Reason: string(ReasonBlindFrontier),
			Detail: "no path found, but the frontier is blind at " + blindMid,
			Bindings: &JSONBindings{
				From: []string{blind},
				To:   []string{target},
			},
			Witnesses: []JSONWitness{{From: blind, BlindSite: blindMid}},
		},
	}
	if !reflect.DeepEqual(dto.Results, want) {
		t.Fatalf("machine reach evidence mismatch\nwant: %#v\ngot:  %#v\njson:\n%s", want, dto.Results, data)
	}
}
