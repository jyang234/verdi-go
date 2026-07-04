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

	// The two endpoint universes must be byte-identical: same rule (sorted node FQNs ∪
	// edge endpoints), same graph, so fqnres.Resolve answers every name identically.
	focusUniverse := gg.endpointUniverse()
	claimsUniverse := claimsStyleUniverse(cg)
	if !reflect.DeepEqual(focusUniverse, claimsUniverse) {
		t.Fatalf("focus and claims endpoint universes differ:\n focus:  %v\n claims: %v", focusUniverse, claimsUniverse)
	}

	// Tie the rebuilt universe to claims' ACTUAL resolution: an ambiguous "Score" run
	// through claims.Evaluate lists exactly the candidates fqnres returns over the focus
	// universe, so the parity is against real claims code, not just a re-derived rule.
	fr, _ := fqnres.Resolve("Score", focusUniverse)
	rep := claims.Evaluate(cg, &claims.File{Claims: []claims.Claim{{Kind: "node", FQN: "Score"}}})
	var detail string
	for _, r := range rep.Results {
		if r.Outcome == claims.Errored {
			detail = r.Detail
		}
	}
	if detail == "" {
		t.Fatalf("expected claims to ERROR on ambiguous 'Score', got: %s", rep.String())
	}
	for _, cand := range fr.Matches {
		if !strings.Contains(detail, cand) {
			t.Errorf("claims resolution %q missing focus candidate %q", detail, cand)
		}
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

// claimsStyleUniverse rebuilds the endpoint universe by the rule claims.newModel uses
// (sorted node FQNs ∪ every edge from/to string) from the groundwork graph — the parity
// target for graphio.endpointUniverse. Kept in the test (not shared code) precisely so a
// drift between the two production constructions surfaces here as a mismatch.
func claimsStyleUniverse(g *gwgraph.Graph) []string {
	set := map[string]bool{}
	for _, n := range g.Nodes {
		set[n.FQN] = true
	}
	for _, e := range g.Edges {
		set[e.From] = true
		set[e.To] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
