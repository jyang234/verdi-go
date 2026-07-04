package claims

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// testGraph is a small hand-built graph exercising every feature a claim needs:
// pointer-receiver methods, a value/pointer Score twin (ambiguity), a boundary
// pseudo-node endpoint, and a DUPLICATE (from,to) pair carried twice under
// different modes (sync + concurrent) so the unique-pair dedup is pinned.
func testGraph() *graph.Graph {
	return &graph.Graph{
		Nodes: []graph.Node{
			{FQN: "(*svc/handler.App).Create", Tier: 1},
			{FQN: "(*svc/repo.Store).Save", Tier: 2},
			{FQN: "svc/scoring.Score", Tier: 2},
			{FQN: "(*svc/scoring.Remote).Score", Tier: 3},
			{FQN: "svc/handler.orphan", Tier: 4},
		},
		Edges: []graph.Edge{
			{From: "(*svc/handler.App).Create", To: "(*svc/repo.Store).Save", Tier: 2},
			// Same pair, concurrent mode: a distinct RECORD, one unique PAIR.
			{From: "(*svc/handler.App).Create", To: "(*svc/repo.Store).Save", Tier: 2, Concurrent: true},
			{From: "(*svc/handler.App).Create", To: "boundary:db QueryContext", Tier: 1, Boundary: "db"},
			{From: "svc/scoring.Score", To: "(*svc/repo.Store).Save", Tier: 2},
			{From: "(*svc/scoring.Remote).Score", To: "(*svc/repo.Store).Save", Tier: 2},
		},
	}
}

func intp(i int) *int { return &i }

func evalOne(t *testing.T, c Claim) Result {
	t.Helper()
	rep := Evaluate(testGraph(), &File{Claims: []Claim{c}})
	if len(rep.Results) != 1 {
		t.Fatalf("want 1 result, got %d", len(rep.Results))
	}
	return rep.Results[0]
}

func TestUniquePairDedup(t *testing.T) {
	// The graph has 5 edge records but only 4 unique pairs (App.Create→Save is
	// carried twice), and edge_count must count the pair once.
	rep := Evaluate(testGraph(), &File{})
	if rep.NumUniquePairs != 4 {
		t.Errorf("NumUniquePairs = %d, want 4", rep.NumUniquePairs)
	}
	r := evalOne(t, Claim{Kind: "edge_count", From: "App).Create", To: "repo.Store).Save", Eq: intp(1)})
	if r.Outcome != Pass {
		t.Errorf("edge_count over deduped pair = %+v, want Pass", r)
	}
}

func TestEdgePassFail(t *testing.T) {
	if r := evalOne(t, Claim{Kind: "edge", From: "App).Create", To: "repo.Store).Save"}); r.Outcome != Pass {
		t.Errorf("edge present = %+v, want Pass", r)
	}
	if r := evalOne(t, Claim{Kind: "edge", From: "repo.Store).Save", To: "App).Create"}); r.Outcome != Fail {
		t.Errorf("edge absent = %+v, want Fail", r)
	}
}

func TestNoEdgePassFailWithOffenders(t *testing.T) {
	if r := evalOne(t, Claim{Kind: "no_edge", From: "repo.Store).Save", To: "App).Create"}); r.Outcome != Pass {
		t.Errorf("no_edge on absent = %+v, want Pass", r)
	}
	r := evalOne(t, Claim{Kind: "no_edge", From: "App).Create", To: "repo.Store).Save"})
	if r.Outcome != Fail {
		t.Fatalf("no_edge on present = %+v, want Fail", r)
	}
	if r.Detail != "1 edge(s) present: (*svc/handler.App).Create -> (*svc/repo.Store).Save" {
		t.Errorf("no_edge detail = %q", r.Detail)
	}
}

func TestNoEdgeErrorsOnUnresolvedEndpoint(t *testing.T) {
	// no_edge is NOT no_node: an endpoint that fails to resolve is an ERROR, not
	// a vacuous pass. (Only no_node treats zero matches as the pass.)
	r := evalOne(t, Claim{Kind: "no_edge", From: "App).Create", To: "does.Not.Exist"})
	if r.Outcome != Errored {
		t.Errorf("no_edge with unresolved endpoint = %+v, want Errored", r)
	}
}

func TestNodeTier(t *testing.T) {
	if r := evalOne(t, Claim{Kind: "node", FQN: "App).Create", Tier: intp(1)}); r.Outcome != Pass {
		t.Errorf("node tier match = %+v, want Pass", r)
	}
	if r := evalOne(t, Claim{Kind: "node", FQN: "App).Create", Tier: intp(2)}); r.Outcome != Fail {
		t.Errorf("node tier mismatch = %+v, want Fail", r)
	}
	if r := evalOne(t, Claim{Kind: "node", FQN: "does.Not.Exist"}); r.Outcome != Errored {
		t.Errorf("node unresolved = %+v, want Errored", r)
	}
}

// TestNoNodePolarity pins the asymmetry both directions: a name that resolves
// FAILs (listing the offenders — a rename cannot vacuously pass it), and a name
// that does NOT resolve is the PASS (never an ERROR).
func TestNoNodePolarity(t *testing.T) {
	if r := evalOne(t, Claim{Kind: "no_node", FQN: "App).Create"}); r.Outcome != Fail {
		t.Errorf("no_node on existing = %+v, want Fail", r)
	}
	if r := evalOne(t, Claim{Kind: "no_node", FQN: "svc/handler.deletedThing"}); r.Outcome != Pass {
		t.Errorf("no_node on absent = %+v, want Pass (never Errored)", r)
	}
}

func TestPerSideEnforcement(t *testing.T) {
	// A plain endpoint that suffix-matches BOTH Score functions is AMBIGUOUS —
	// an ERROR — even though the other side is a precise regex. A regex on one
	// side does not relax the uniqueness the plain side implies.
	r := evalOne(t, Claim{Kind: "edge", From: ".Score", To: `/repo\.Store\).Save$/`})
	if r.Outcome != Errored {
		t.Fatalf("ambiguous plain side under a regex other side = %+v, want Errored", r)
	}
	// The precise regex side alone (unique plain other side) resolves and passes.
	if r := evalOne(t, Claim{Kind: "edge", From: "scoring.Score", To: `/repo\.Store\).Save$/`}); r.Outcome != Pass {
		t.Errorf("regex to-side edge = %+v, want Pass", r)
	}
}

func TestBoundaryEndpointClaimable(t *testing.T) {
	if r := evalOne(t, Claim{Kind: "edge", From: "App).Create", To: "boundary:db QueryContext"}); r.Outcome != Pass {
		t.Errorf("boundary endpoint edge = %+v, want Pass", r)
	}
	// out_degree over the endpoint universe counts the boundary target.
	if r := evalOne(t, Claim{Kind: "out_degree", Of: "App).Create", Eq: intp(2)}); r.Outcome != Pass {
		t.Errorf("out_degree incl. boundary = %+v, want Pass", r)
	}
}

func TestInDegreeWithCounterpartFilter(t *testing.T) {
	// Store.Save has three distinct callers; filtered to the scoring package, two.
	if r := evalOne(t, Claim{Kind: "in_degree", Of: "repo.Store).Save", Eq: intp(3)}); r.Outcome != Pass {
		t.Errorf("in_degree unfiltered = %+v, want Pass (3)", r)
	}
	if r := evalOne(t, Claim{Kind: "in_degree", Of: "repo.Store).Save", Eq: intp(2), CounterpartMatching: "/scoring/"}); r.Outcome != Pass {
		t.Errorf("in_degree filtered = %+v, want Pass (2)", r)
	}
}

func TestCounterpartAlias(t *testing.T) {
	// to_matching is the accepted alias for counterpart_matching.
	if r := evalOne(t, Claim{Kind: "in_degree", Of: "repo.Store).Save", Eq: intp(2), ToMatching: "/scoring/"}); r.Outcome != Pass {
		t.Errorf("to_matching alias = %+v, want Pass", r)
	}
	// Both present is an ERROR — no silent precedence.
	r := evalOne(t, Claim{Kind: "in_degree", Of: "repo.Store).Save", Eq: intp(2), CounterpartMatching: "/scoring/", ToMatching: "/scoring/"})
	if r.Outcome != Errored {
		t.Errorf("both counterpart fields = %+v, want Errored", r)
	}
}

func TestUnknownKindIsolated(t *testing.T) {
	// An unknown kind ERRORs its own claim; the surrounding claims still run.
	rep := Evaluate(testGraph(), &File{Claims: []Claim{
		{Kind: "edge", From: "App).Create", To: "repo.Store).Save"},
		{Kind: "bogus_kind", FQN: "x"},
		{Kind: "node", FQN: "App).Create"},
	}})
	if rep.Passed() != 2 || rep.Errored() != 1 {
		t.Errorf("want 2 passed / 1 errored, got %d/%d/%d", rep.Passed(), rep.Failed(), rep.Errored())
	}
}

func TestDegreeRequiresEq(t *testing.T) {
	if r := evalOne(t, Claim{Kind: "in_degree", Of: "repo.Store).Save"}); r.Outcome != Errored {
		t.Errorf("in_degree without eq = %+v, want Errored", r)
	}
}

// TestDeterministicOverShuffledInput pins that the report is a pure function of
// the graph content, not its edge/node arrival order.
func TestDeterministicOverShuffledInput(t *testing.T) {
	g1 := testGraph()
	g2 := testGraph()
	// Reverse both slices — a different arrival order, identical content.
	for i, j := 0, len(g2.Edges)-1; i < j; i, j = i+1, j-1 {
		g2.Edges[i], g2.Edges[j] = g2.Edges[j], g2.Edges[i]
	}
	for i, j := 0, len(g2.Nodes)-1; i < j; i, j = i+1, j-1 {
		g2.Nodes[i], g2.Nodes[j] = g2.Nodes[j], g2.Nodes[i]
	}
	cf := &File{Claims: []Claim{
		{Kind: "no_edge", From: "App).Create", To: "does.Not.Exist"}, // errored
		{Kind: "in_degree", Of: "repo.Store).Save", Eq: intp(3)},     // pass
		{Kind: "edge", From: "repo.Store).Save", To: "App).Create"},  // fail
		{Kind: "no_node", FQN: ".Save"},                              // fail (offenders listed)
	}}
	if a, b := Evaluate(g1, cf).String(), Evaluate(g2, cf).String(); a != b {
		t.Errorf("report not deterministic across input order:\n%s\n---\n%s", a, b)
	}
}

func TestLoadFileStrict(t *testing.T) {
	write := func(s string) string {
		p := filepath.Join(t.TempDir(), "c.json")
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// A typo'd field (form: for from:) must be a loud decode error, not a
	// silent zero-value claim.
	if _, err := LoadFile(write(`{"claims":[{"kind":"edge","form":"x","to":"y"}]}`)); err == nil {
		t.Error("unknown field must fail strict decode")
	}
	// A well-formed file loads.
	cf, err := LoadFile(write(`{"claims":[{"kind":"node","fqn":"x"}]}`))
	if err != nil || len(cf.Claims) != 1 {
		t.Errorf("valid file: cf=%+v err=%v", cf, err)
	}
	// Missing "claims" is an operational error.
	if _, err := LoadFile(write(`{"other":[]}`)); err == nil {
		t.Error("missing claims array must error")
	}
}

// TestReportFormat pins the exact line shapes the CLI/golden depend on.
func TestReportFormat(t *testing.T) {
	rep := Evaluate(testGraph(), &File{Claims: []Claim{
		{Kind: "edge", From: "App).Create", To: "repo.Store).Save"}, // pass (silent)
		{Kind: "edge", From: "repo.Store).Save", To: "App).Create"}, // fail
		{Kind: "node", FQN: ".Score"},                               // errored (ambiguous)
	}})
	want := "FAIL edge repo.Store).Save -> App).Create: no edge between the resolved endpoints\n" +
		"ERROR node .Score: ambiguous \".Score\" (2 candidates): (*svc/scoring.Remote).Score, svc/scoring.Score\n" +
		"assert: 1 passed, 1 failed, 1 errored (graph: 5 nodes, 4 unique edges)\n"
	if got := rep.String(); got != want {
		t.Errorf("report format:\n got %q\nwant %q", got, want)
	}
}
