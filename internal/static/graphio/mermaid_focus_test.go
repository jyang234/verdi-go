package graphio

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/fqnres"
	"github.com/jyang234/golang-code-graph/internal/groundwork/claims"
	gwgraph "github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/impact"
	"github.com/jyang234/golang-code-graph/internal/render"
)

// loansvcGolden is the committed loansvc graph.json — the same analyze-guarded golden
// the whole-graph mermaid goldens render (mermaid_golden_test.go). Decoded into a
// graphio.Graph so the focus render needs no toolchain run.
const loansvcGolden = "../../../testdata/groundwork/goldens/loansvc.graph.json"

func loadFocusGraph(t *testing.T) *Graph {
	t.Helper()
	raw, err := os.ReadFile(loansvcGolden)
	if err != nil {
		t.Fatalf("read loansvc golden: %v", err)
	}
	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("decode loansvc golden: %v", err)
	}
	return &g
}

// focusGoldenNames is the curated ~6-name focus the golden pins. It exercises every
// class the induced render must get right: a plain name reached by another focused
// caller (Status→SelectLoan), a comma-split-style pair one of which is a BOUNDARY
// endpoint drawn because its focused caller is in the set (SelectLoan→db SELECT loans),
// an ISOLATED tier-1 node (main, no induced edge → lone box), a /regex/ resolving to an
// isolated tier-3 node the pin RESCUES from tier-collapse (Stub.Score), and a boundary
// endpoint with NO focused caller (db UPDATE loans → the boundary-no-edge disclosure).
var focusGoldenNames = []string{
	"(*example.com/loansvc/internal/handler.App).Status",
	"store.Loans).SelectLoan",
	"boundary:db SELECT loans",
	"example.com/loansvc.main",
	`/Stub\).Score$/`,
	"boundary:db UPDATE loans",
}

// TestMermaidFocusGolden pins the induced-subgraph render over the curated focus: the
// induced edges, the lone boxes (isolated nodes still drawn), the tier styling, and the
// disclosure notes (pruned blind/frontier channels + the boundary-no-edge note).
func TestMermaidFocusGolden(t *testing.T) {
	g := loadFocusGraph(t)
	out, err := g.MermaidFocus(focusGoldenNames, MermaidOptions{MaxTier: 2})
	if err != nil {
		t.Fatalf("MermaidFocus: %v", err)
	}
	fenced := render.Fence(out)
	assertValidMermaid(t, fenced)
	assertGolden(t, "testdata/loansvc.focus.callgraph.md", fenced)

	// Spot-pin the load-bearing facts the golden encodes, so a careless -update rebase
	// that erased one would still be caught by an assertion naming what it dropped.
	for _, want := range []string{
		"handler_App_Status --> store_Loans_SelectLoan", // induced first-party edge
		"store_Loans_SelectLoan --> db_db_SELECT_loans", // induced boundary edge
		"loansvc_main[",                       // isolated tier-1 lone box
		"scoring_Stub_Score",                  // isolated tier-3 node, pin-rescued
		"not drawn: boundary:db UPDATE loans", // boundary-no-edge disclosure
		"outside the focus set shown only in the whole-graph view", // pruned-disclosure note
	} {
		if !strings.Contains(out, want) {
			t.Errorf("focus render missing %q:\n%s", want, out)
		}
	}
}

// TestMermaidFocusFailsClosed proves every unresolvable name aborts the WHOLE render
// with no output — never a partial induced set (CLAUDE.md tenet 2). The AMBIGUOUS case
// carries the sorted candidate list (assert's convention).
func TestMermaidFocusFailsClosed(t *testing.T) {
	g := loadFocusGraph(t)
	cases := []struct {
		name    string
		query   string
		wantSub []string
	}{
		{"ambiguous", "Score", []string{
			"AMBIGUOUS", "'Score' matches 3",
			"(*example.com/loansvc/internal/client.Bureau).Score",
			"(*example.com/loansvc/internal/scoring.Remote).Score",
			"(*example.com/loansvc/internal/scoring.Stub).Score",
		}},
		{"unresolved", "handler.App).Delete", []string{"UNRESOLVED", "'handler.App).Delete'"}},
		{"zero-match regex", `/NoSuchNodeAnywhere/`, []string{"ZERO-MATCH", "NoSuchNodeAnywhere"}},
		{"compile-error regex", `/a(b/`, []string{"invalid regex"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := g.MermaidFocus([]string{tc.query}, MermaidOptions{MaxTier: 2})
			if err == nil {
				t.Fatalf("expected an error, got render:\n%s", out)
			}
			if out != "" {
				t.Errorf("a failed resolve must render NOTHING, got:\n%s", out)
			}
			for _, sub := range tc.wantSub {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error %q missing %q", err.Error(), sub)
				}
			}
		})
	}
}

// TestMermaidFocusOrderedCandidates pins that the ambiguous candidate list is SORTED
// (deterministic) — the error is a fail-closed disclosure, so its order must not vary.
func TestMermaidFocusAmbiguousSorted(t *testing.T) {
	g := loadFocusGraph(t)
	_, err := g.MermaidFocus([]string{"Score"}, MermaidOptions{MaxTier: 2})
	if err == nil {
		t.Fatal("expected AMBIGUOUS")
	}
	i := strings.Index(err.Error(), "matches 3: ")
	if i < 0 {
		t.Fatalf("no candidate list: %v", err)
	}
	list := strings.Split(err.Error()[i+len("matches 3: "):], "; ")
	if !sort.StringsAreSorted(list) {
		t.Errorf("ambiguous candidates not sorted: %v", list)
	}
}

// TestMermaidFocusDeterministic pins byte-identity across repeats AND invariance under
// input node/edge reordering — the same property delta_test's reversed() guards. A new
// ordering path with no determinism test is a CLAUDE.md violation.
func TestMermaidFocusDeterministic(t *testing.T) {
	g := loadFocusGraph(t)
	first, err := g.MermaidFocus(focusGoldenNames, MermaidOptions{MaxTier: 2})
	if err != nil {
		t.Fatalf("MermaidFocus: %v", err)
	}
	// Repeat render is byte-identical.
	again, err := g.MermaidFocus(focusGoldenNames, MermaidOptions{MaxTier: 2})
	if err != nil {
		t.Fatalf("MermaidFocus repeat: %v", err)
	}
	if first != again {
		t.Errorf("focus render not byte-identical across repeats")
	}
	// Reordering the input nodes/edges must not change a byte of the output (the render
	// resolves every ordering on the canonical Node/Edge order, not arrival order).
	rev, err := reversed(g).MermaidFocus(focusGoldenNames, MermaidOptions{MaxTier: 2})
	if err != nil {
		t.Fatalf("MermaidFocus reversed: %v", err)
	}
	if first != rev {
		t.Errorf("focus render not invariant under input reordering:\nforward:\n%s\nreversed:\n%s", first, rev)
	}
	// The scope label is the raw names in INPUT order — reordering the names is a
	// different (honest) label, so this only pins that the same input is stable.
	if !bytes.Contains([]byte(first), []byte("focus: (*example.com/loansvc/internal/handler.App).Status,")) {
		t.Errorf("scope label must echo the raw focus names in input order:\n%s", first)
	}
}

// TestMermaidFocusDisclosesPinnedPlumbing pins that a CONNECTED tier-3 focus node —
// shown only because the focus pin rescued it from tier-collapse — is DISCLOSED, not
// hidden as silent plumbing. Determinism guard included (a new note ordering path).
func TestMermaidFocusDisclosesPinnedPlumbing(t *testing.T) {
	g := &Graph{
		Algo: "rta",
		Nodes: []Node{
			{FQN: "example.com/svc/pkg.Handler", Tier: 1},
			{FQN: "example.com/svc/pkg.plumbing", Tier: 3}, // plumbing, no effect of its own
		},
		Edges: []Edge{{From: "example.com/svc/pkg.Handler", To: "example.com/svc/pkg.plumbing", Tier: 3}},
	}
	names := []string{"pkg.Handler", "pkg.plumbing"}
	out, err := g.MermaidFocus(names, MermaidOptions{MaxTier: 2})
	if err != nil {
		t.Fatalf("MermaidFocus: %v", err)
	}
	if !strings.Contains(out, "pinned into view:") || !strings.Contains(out, "above tier 2 (plumbing)") {
		t.Errorf("focus render must disclose the pin-rescued tier-3 node, not hide it:\n%s", out)
	}
	again, _ := g.MermaidFocus(names, MermaidOptions{MaxTier: 2})
	if out != again {
		t.Error("pinned-plumbing disclosure note not deterministic across repeats")
	}
}

// TestFocusOverCapAdvice pins that the over-cap index advice is PATH-AWARE: the whole-graph
// render steers to --root, but a --focus render steers to narrowing the --focus set (the CLI
// refuses --root under --focus, so "scope with --root" would be a dead end there — FIX 10).
func TestFocusOverCapAdvice(t *testing.T) {
	g := &Graph{
		Algo:  "rta",
		Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}},
		Edges: []Edge{{From: "a.A", To: "a.B", Tier: 1}},
	}
	whole := g.Mermaid(MermaidOptions{MaxTier: 2, MaxNodes: 1})
	if !strings.Contains(whole, "scope with --root or raise --max-nodes") {
		t.Errorf("whole-graph over-cap advice must name --root:\n%s", whole)
	}
	focus, err := g.MermaidFocus([]string{"a.A", "a.B"}, MermaidOptions{MaxTier: 2, MaxNodes: 1})
	if err != nil {
		t.Fatalf("MermaidFocus: %v", err)
	}
	if !strings.Contains(focus, "narrow the --focus set or raise --max-nodes") {
		t.Errorf("focus over-cap advice must steer to --focus:\n%s", focus)
	}
	if strings.Contains(focus, "scope with --root") {
		t.Errorf("focus over-cap advice must NOT steer to --root (refused under --focus):\n%s", focus)
	}
}

// TestFocusFailsClosedOnDanglingEndpoint pins FIX 2: a focus name that resolves (via the
// endpoint universe) to an edge endpoint with NO node record has no node id, so its induced
// edges would vanish silently. That is refused with a dangling disclosure, never a partial
// render.
func TestFocusFailsClosedOnDanglingEndpoint(t *testing.T) {
	g := &Graph{
		Algo:  "rta",
		Nodes: []Node{{FQN: "a.A", Tier: 1}},
		Edges: []Edge{{From: "a.A", To: "a.GHOST", Tier: 1}}, // a.GHOST is an endpoint with no Node
	}
	out, err := g.MermaidFocus([]string{"a.A", "a.GHOST"}, MermaidOptions{MaxTier: 2})
	if err == nil {
		t.Fatalf("expected a dangling-endpoint error, got render:\n%s", out)
	}
	if out != "" {
		t.Errorf("a dangling focus endpoint must render NOTHING, got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "dangling") || !strings.Contains(err.Error(), "a.GHOST") {
		t.Errorf("error must name the dangling endpoint: %v", err)
	}
}

// TestFocusDanglingEndpointsDeterministic pins that the dangling-endpoint list is
// deterministic and SORTED even with several dangling members — the list is built from a
// map range (focus), so without the sort.Strings its order is Go's randomized map
// iteration and repeated renders would disagree. Deleting the sort must fail here (a
// fail-closed disclosure whose order varies run-to-run is a determinism violation,
// CLAUDE.md tenet 1). Every prior dangling test used a single name, so none caught this.
func TestFocusDanglingEndpointsDeterministic(t *testing.T) {
	g := &Graph{
		Algo:  "rta",
		Nodes: []Node{{FQN: "a.A", Tier: 1}},
		Edges: []Edge{
			{From: "a.A", To: "a.D", Tier: 1},
			{From: "a.A", To: "a.B", Tier: 1},
			{From: "a.A", To: "a.E", Tier: 1},
			{From: "a.A", To: "a.C", Tier: 1},
		},
	}
	names := []string{"a.A", "a.D", "a.B", "a.E", "a.C"} // a.B..a.E all dangle (no node record)
	render1, err := g.MermaidFocus(names, MermaidOptions{MaxTier: 2})
	if err == nil {
		t.Fatalf("expected a dangling-endpoint error, got render:\n%s", render1)
	}
	first := err.Error()
	// Byte-identity across many renders — with map iteration this is overwhelmingly likely
	// to break if the sort is removed (a 4-element permutation matching 16 times is ~0).
	for i := 0; i < 16; i++ {
		_, e := g.MermaidFocus(names, MermaidOptions{MaxTier: 2})
		if e == nil || e.Error() != first {
			t.Fatalf("dangling error not deterministic across renders:\n first: %s\n got:   %v", first, e)
		}
	}
	// And the list is genuinely sorted (not just stable).
	marker := "induced subgraph: "
	i := strings.LastIndex(first, marker)
	if i < 0 {
		t.Fatalf("unexpected error shape: %s", first)
	}
	list := strings.Split(first[i+len(marker):], "; ")
	if !sort.StringsAreSorted(list) {
		t.Errorf("dangling endpoints not sorted: %v", list)
	}
}

// TestFocusResolverParityWithAssert pins Part 3 of the companion spec — "one resolver,
// both features": `--focus` (graphio.endpointUniverse + fqnres) and `groundwork assert`
// (claims.newModel's endpoint universe + fqnres) resolve a name to the SAME FQN set over
// the same graph. It also pins the deliberate non-unification with impact.ResolveFrame:
// a normalized (*T).Method label resolves in fqnres but NOT in the runtime-frame resolver.
func TestFocusResolverParityWithAssert(t *testing.T) {
	raw, err := os.ReadFile(loansvcGolden)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var gg Graph
	if err := json.Unmarshal(raw, &gg); err != nil {
		t.Fatalf("decode graphio graph: %v", err)
	}
	cg, err := gwgraph.Load(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("load groundwork graph: %v", err)
	}

	// The two endpoint universes must be byte-identical: PRODUCTION vs PRODUCTION — graphio's
	// endpointUniverse() against claims' exported EndpointUniverse constructor (the one
	// newModel builds from), so a drift between the two features' universe rules surfaces
	// here rather than hiding behind a test-local re-derivation (FIX 9).
	focusUniverse := gg.endpointUniverse()
	claimsUniverse := claims.EndpointUniverse(cg)
	if !reflect.DeepEqual(focusUniverse, claimsUniverse) {
		t.Fatalf("focus and claims endpoint universes differ:\n focus:  %v\n claims: %v", focusUniverse, claimsUniverse)
	}

	// Tie the rebuilt universe to claims' ACTUAL resolution over the ENDPOINT universe: an
	// EDGE claim whose 'from' resolves UNIQUELY ('store.Loans).SelectLoan') and whose 'to'
	// is a BOUNDARY endpoint ('boundary:db SELECT loans') that lives ONLY in the endpoint
	// universe (it is never a node). claims MUST resolve the 'to' against the endpoint
	// universe: a nodes-only universe would leave the boundary 'to' UNRESOLVED and ERROR.
	// The edge is present in the graph, so with the real endpoint universe the claim
	// resolves and PASSES — genuinely exercising claims' production endpoint universe. (The
	// old 'Score' probe short-circuited on an ambiguous 'from' before the boundary 'to' was
	// ever resolved, so it passed even against a nodes-only universe — a dead probe.)
	rep := claims.Evaluate(cg, &claims.File{Claims: []claims.Claim{
		{Kind: "edge", From: "store.Loans).SelectLoan", To: "boundary:db SELECT loans"},
	}})
	if rep.Errored() != 0 || rep.Passed() != 1 {
		t.Fatalf("expected the boundary edge claim to resolve and PASS over the endpoint universe (a nodes-only universe would ERROR on the boundary 'to'), got: %s", rep.String())
	}

	// The (*T).Method asymmetry: a plain, receiver-punctuation-forgiving label resolves
	// UNIQUELY in fqnres but does NOT resolve in impact.ResolveFrame (which keeps runtime
	// punctuation and matches token-bounded) — the two resolvers are deliberately distinct.
	const dotted = "handler.App.Create"
	pr, _ := fqnres.Resolve(dotted, focusUniverse)
	if len(pr.Matches) != 1 || !strings.HasSuffix(pr.Matches[0], "handler.App).Create") {
		t.Fatalf("fqnres must resolve %q to the single Create method, got %v", dotted, pr.Matches)
	}
	ix := gwgraph.NewIndex(cg)
	if fm := impact.ResolveFrame(ix, dotted); len(fm.Matches) != 0 {
		t.Errorf("impact.ResolveFrame must NOT resolve the normalized label %q (deliberate non-unification), got %v", dotted, fm.Matches)
	}
}
