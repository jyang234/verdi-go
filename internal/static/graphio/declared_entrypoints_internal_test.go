package graphio

import "testing"

// TestRouteEntrypointCountExcludesDeclared pins that the frontier's
// attribution_loss denominator counts only DISCOVERED routes (http + consumer);
// declared callbacks/workers are author-asserted entries, not routes, so they must
// not dilute the per-route metric.
func TestRouteEntrypointCountExcludesDeclared(t *testing.T) {
	g := &Graph{Entrypoints: []Entrypoint{
		{Kind: "http", Name: "GET /x", Fn: "a"},
		{Kind: "consumer", Name: "ev", Fn: "b"},
		{Kind: "callback", Name: "p#H", Fn: "c"},
		{Kind: "worker", Name: "p#W", Fn: "d"},
	}}
	if got := g.RouteEntrypointCount(); got != 2 {
		t.Errorf("RouteEntrypointCount = %d, want 2 (http+consumer only)", got)
	}
}

// TestFrontierInputExcludesDeclaredRoots pins that declared callbacks/workers never
// enter the frontier's route-severance universe. Otherwise a declared worker that
// reaches no effect (a logging-only reconcile loop — legitimate per the design)
// would be classified as a CONFIRMED starved-entrypoint severance, a false signal
// that also inflates StarvedEntrypoints.
func TestFrontierInputExcludesDeclaredRoots(t *testing.T) {
	g := &Graph{Entrypoints: []Entrypoint{
		{Kind: "http", Name: "GET /x", Fn: "x"},
		{Kind: "callback", Name: "p#H", Fn: "c"},
		{Kind: "worker", Name: "p#W", Fn: "w"},
	}}
	in := frontierInput(g)
	if len(in.Entrypoints) != 1 || in.Entrypoints[0].Fn != "x" {
		t.Fatalf("frontier route set = %+v, want only the http route 'x'", in.Entrypoints)
	}
}
