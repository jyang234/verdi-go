package graphio_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/callgraph"
	"github.com/jyang234/golang-code-graph/internal/static/graphio"
)

func embedIfaceFixture(t *testing.T, opts ...callgraph.Options) *analyze.Result {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", "embedifacesvc")
	res, err := analyze.Analyze(dir, opts...)
	if err != nil {
		t.Fatalf("analyze embedifacesvc: %v", err)
	}
	return res
}

// TestSpliceCutsCHAPromotionWrapperCycle is the regression guard for the field
// panic "wrapper splice exceeded depth cap 16" (flowmap-findings-2026-07-03
// Finding 5, event-bus clients/admin). A struct embedding an interface gives CHA a
// promotion-wrapper self-edge — (*ClientWithResponses).DoThing invokes
// ClientInterface.DoThing, which CHA resolves back to (*ClientWithResponses).DoThing
// itself — and the splice must cut the revisit instead of recursing to the depth
// cap.
//
// The load-bearing assertion is that Build returns AT ALL: it panicked before the
// fix. The remaining checks pin that the promotion wrapper is spliced OUT — it must
// name neither a rendered node NOR an edge endpoint (a regression that stopped
// treating promotion wrappers as spliceable would render the wrapper instead of
// panicking, so node/edge absence, not the no-panic path, is what catches it).
// NOTE: the caller->(*Client).DoThing edge is deliberately NOT used as the splice
// witness — under CHA the caller's own invoke of ClientInterface.DoThing resolves
// directly to (*Client).DoThing as well, so that edge exists independent of the
// splice and would not distinguish a working splice from a broken one.
func TestSpliceCutsCHAPromotionWrapperCycle(t *testing.T) {
	g, err := graphio.Build(embedIfaceFixture(t, callgraph.Options{Algo: callgraph.AlgoCHA}), "")
	if err != nil {
		t.Fatal(err)
	}

	const wrapper = "(*example.com/embedifacesvc.ClientWithResponses).DoThing"

	for _, n := range g.Nodes {
		if n.FQN == wrapper {
			t.Errorf("the promotion wrapper must be spliced out of the rendered nodes, found node %s", n.FQN)
		}
	}
	// The self-edge that tripped the panic was (*ClientWithResponses).DoThing ->
	// (*ClientWithResponses).DoThing; if the cut ever regressed into rendering the
	// wrapper the splice would re-attribute edges to/from its FQN. Neither endpoint
	// may name it.
	for _, e := range g.Edges {
		if e.From == wrapper || e.To == wrapper {
			t.Errorf("the promotion wrapper must be spliced out of edge endpoints, found edge %s -> %s", e.From, e.To)
		}
	}
}

// TestSpliceCycleFixtureDeterministic pins the CLAUDE.md invariant that a new
// traversal/ordering path ships with a determinism test: the shared, mutated
// `spliced` visited set resolves diamonds/cycles by first-visit-wins, so its output
// must still be byte-identical across repeated builds. Two independent analyze+build
// passes must project to the same edges and nodes.
func TestSpliceCycleFixtureDeterministic(t *testing.T) {
	build := func() string {
		g, err := graphio.Build(embedIfaceFixture(t, callgraph.Options{Algo: callgraph.AlgoCHA}), "")
		if err != nil {
			t.Fatal(err)
		}
		return projectGraph(g)
	}
	if first, second := build(), build(); first != second {
		t.Errorf("cha build is not deterministic across runs:\nrun1:\n%s\nrun2:\n%s", first, second)
	}
}

// projectGraph renders a graph as a stable string of its sorted node FQNs and edges,
// so two builds can be compared without depending on run-varying *ssa.Function
// pointers.
func projectGraph(g *graphio.Graph) string {
	lines := make([]string, 0, len(g.Nodes)+len(g.Edges))
	for _, n := range g.Nodes {
		lines = append(lines, "node "+n.FQN)
	}
	for _, e := range g.Edges {
		lines = append(lines, fmt.Sprintf("edge %s -> %s tier=%d boundary=%q concurrent=%t via=%q", e.From, e.To, e.Tier, e.Boundary, e.Concurrent, e.Via))
	}
	sort.Strings(lines)
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

// The default (RTA) and VTA builds never see the cycle — they narrow the invoke to
// the concrete *Client — and must keep working unchanged with the visited-set
// plumbing in place.
func TestSpliceCycleFixtureCleanUnderRTAAndVTA(t *testing.T) {
	for _, algo := range []callgraph.Algo{callgraph.AlgoRTA, callgraph.AlgoVTA} {
		if _, err := graphio.Build(embedIfaceFixture(t, callgraph.Options{Algo: algo}), ""); err != nil {
			t.Fatalf("algo %v: %v", algo, err)
		}
	}
}
