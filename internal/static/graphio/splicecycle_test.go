package graphio_test

import (
	"path/filepath"
	"runtime"
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
// cap. The build succeeding at all IS the regression assertion (it panicked
// before); the edge checks pin that cutting the back-edge kept the real spliced
// targets and still renders no wrapper node.
func TestSpliceCutsCHAPromotionWrapperCycle(t *testing.T) {
	g, err := graphio.Build(embedIfaceFixture(t, callgraph.Options{Algo: callgraph.AlgoCHA}), "")
	if err != nil {
		t.Fatal(err)
	}

	const (
		caller  = "(*example.com/embedifacesvc.ClientWithResponses).DoThingWithResponse"
		wrappee = "(*example.com/embedifacesvc.Client).DoThing"
		wrapper = "(*example.com/embedifacesvc.ClientWithResponses).DoThing"
	)
	spliceKept := false
	for _, e := range g.Edges {
		if e.From == caller && e.To == wrappee {
			spliceKept = true
		}
	}
	if !spliceKept {
		t.Errorf("cutting the cycle back-edge must keep the acyclic spliced target: want edge %s -> %s", caller, wrappee)
	}
	for _, n := range g.Nodes {
		if n.FQN == wrapper {
			t.Errorf("the promotion wrapper must be spliced out of the rendered nodes, found %s", n.FQN)
		}
	}
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
