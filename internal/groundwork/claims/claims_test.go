package claims

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// evalOneG evaluates a single claim against an arbitrary graph (the sibling of
// evalOne, which is fixed to testGraph) so an entrypoint test can pin behavior over
// a purpose-built entrypoints[] universe.
func evalOneG(t *testing.T, g *graph.Graph, c Claim) Result {
	t.Helper()
	rep := Evaluate(g, &File{Claims: []Claim{c}})
	if len(rep.Results) != 1 {
		t.Fatalf("want 1 result, got %d", len(rep.Results))
	}
	return rep.Results[0]
}

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
		// Entrypoint records exercise the join kind: an exact+method-less pair that
		// AGREE on a handler (multi-match PASS), a wildcard-template record
		// (observed-path tolerance), a consumer record (entry_kind filter), and an
		// overlapping /widget/ pair that DISAGREE on the handler (the ambiguous-join
		// ERROR). All handlers are declared nodes so `fn` resolves.
		Entrypoints: []graph.Entrypoint{
			{Kind: "http", Name: "POST /loan", Fn: "(*svc/handler.App).Create"},
			{Kind: "http", Name: "/loan", Fn: "(*svc/handler.App).Create"}, // method-less; agrees with the above
			{Kind: "http", Name: "GET /loan/{id}", Fn: "(*svc/handler.App).Create"},
			{Kind: "consumer", Name: "payment.settled", Fn: "(*svc/repo.Store).Save"},
			{Kind: "http", Name: "GET /widget/{id}", Fn: "(*svc/handler.App).Create"},
			{Kind: "http", Name: "GET /widget/{name}", Fn: "(*svc/repo.Store).Save"}, // overlapping pair, different handler
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

// TestUnexpectedFieldRejected pins that a KNOWN field on the wrong kind ERRORs
// rather than being silently ignored — the failure mode where `eq` on an `edge`
// (author meant an absence assertion) would evaluate as a presence check.
func TestUnexpectedFieldRejected(t *testing.T) {
	r := evalOne(t, Claim{Kind: "edge", From: "App).Create", To: "repo.Store).Save", Eq: intp(0)})
	if r.Outcome != Errored {
		t.Errorf("edge with stray eq = %+v, want Errored (not a silent presence check)", r)
	}
	// A node claim must not silently accept a degree filter field.
	if r := evalOne(t, Claim{Kind: "node", FQN: "App).Create", CounterpartMatching: "/x/"}); r.Outcome != Errored {
		t.Errorf("node with stray counterpart_matching = %+v, want Errored", r)
	}
}

// TestAnchorErrorLabelNamesBothFields pins FIX 6: a claim with BOTH the canonical anchor
// and its `fn` alias ERRORs (the anchor is empty), and the report LABEL falls back to
// naming both fields ("<canonical>/<fn>") so the id-less error line still identifies the
// claim instead of rendering with a blank label.
func TestAnchorErrorLabelNamesBothFields(t *testing.T) {
	// node/no_node: fqn + fn both set.
	r := evalOne(t, Claim{Kind: "node", FQN: "handler.App).Create", Fn: "handler.App).Status"})
	if r.Outcome != Errored {
		t.Fatalf("node with fqn+fn = %+v, want Errored", r)
	}
	if want := "handler.App).Create/handler.App).Status"; r.Label != want {
		t.Errorf("node anchor-error label = %q, want %q", r.Label, want)
	}
	// in_degree/out_degree: of + fn both set.
	r = evalOne(t, Claim{Kind: "in_degree", Of: "repo.Store).Save", Fn: "repo.Store).Load", Eq: intp(0)})
	if r.Outcome != Errored {
		t.Fatalf("in_degree with of+fn = %+v, want Errored", r)
	}
	if want := "repo.Store).Save/repo.Store).Load"; r.Label != want {
		t.Errorf("degree anchor-error label = %q, want %q", r.Label, want)
	}
}

// TestCounterpartFilterFailsClosed pins that a counterpart filter matching
// NOTHING anywhere in the graph is an ERROR (a typo/rename), not a vacuous pass
// on an eq:0 claim.
func TestCounterpartFilterFailsClosed(t *testing.T) {
	r := evalOne(t, Claim{Kind: "in_degree", Of: "repo.Store).Save", Eq: intp(0), CounterpartMatching: "/nonexistent-pkg/"})
	if r.Outcome != Errored {
		t.Errorf("typo'd counterpart filter with eq:0 = %+v, want Errored (not a vacuous pass)", r)
	}
	// The detail reads in the shared UNRESOLVED convention (FIX 7), not the old bespoke
	// "unresolved counterpart filter \"…\"" wording.
	if want := "UNRESOLVED: '/nonexistent-pkg/' matches no node/endpoint (counterpart filter)"; r.Detail != want {
		t.Errorf("counterpart-miss detail = %q, want %q", r.Detail, want)
	}
	// A filter that resolves in the graph (matches a real package) but none of
	// App.Create's callees are in it is a LEGITIMATE zero — it passes eq:0.
	// (App.Create's callees are Evaluate + the boundary db endpoint; none match
	// /scoring/.)
	if r := evalOne(t, Claim{Kind: "out_degree", Of: "App).Create", Eq: intp(0), CounterpartMatching: "/scoring/"}); r.Outcome != Pass {
		t.Errorf("legit zero-intersection out_degree = %+v, want Pass", r)
	}
}

// TestDuplicateFQNTierAbstains pins that a graph carrying one FQN at two tiers
// makes a tier claim ABSTAIN (ERROR), never grade against an arbitrary record.
func TestDuplicateFQNTierAbstains(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: "svc/gen.F[int]", Tier: 1},
			{FQN: "svc/gen.F[int]", Tier: 3}, // same display FQN, different tier
		},
	}
	rep := Evaluate(g, &File{Claims: []Claim{{Kind: "node", FQN: "gen.F[int]", Tier: intp(1)}}})
	if rep.Results[0].Outcome != Errored {
		t.Errorf("tier claim on duplicate-FQN node = %+v, want Errored (abstain)", rep.Results[0])
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
	for i, j := 0, len(g2.Entrypoints)-1; i < j; i, j = i+1, j-1 {
		g2.Entrypoints[i], g2.Entrypoints[j] = g2.Entrypoints[j], g2.Entrypoints[i]
	}
	cf := &File{Claims: []Claim{
		{Kind: "no_edge", From: "App).Create", To: "does.Not.Exist"},           // errored
		{Kind: "in_degree", Of: "repo.Store).Save", Eq: intp(3)},               // pass
		{Kind: "edge", From: "repo.Store).Save", To: "App).Create"},            // fail
		{Kind: "no_node", FQN: ".Save"},                                        // fail (offenders listed)
		{Kind: "entrypoint", Name: "GET /widget/7", Fn: "handler.App).Create"}, // errored (disagreeing joins — order-independent)
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
	// Trailing data after the single object must fail closed (parity with
	// graph.Load's single-value guard).
	if _, err := LoadFile(write(`{"claims":[{"kind":"node","fqn":"x"}]} garbage`)); err == nil {
		t.Error("trailing data after claims JSON must error")
	}
}

// TestIDLabelEcho pins that a claim's `id` becomes the report-line label, and
// an id-less claim falls back to the endpoint-derived label.
func TestIDLabelEcho(t *testing.T) {
	// With id: the label is the id verbatim.
	r := evalOne(t, Claim{ID: "my-check", Kind: "edge", From: "repo.Store).Save", To: "App).Create"})
	if r.Outcome != Fail || r.Label != "my-check" {
		t.Errorf("id-labelled claim = %+v, want Fail with Label \"my-check\"", r)
	}
	// Without id: the endpoint-derived label.
	if r := evalOne(t, Claim{Kind: "edge", From: "repo.Store).Save", To: "App).Create"}); r.Label != "repo.Store).Save -> App).Create" {
		t.Errorf("id-less label = %q, want the endpoint-derived label", r.Label)
	}
}

// TestFnAlias pins that `fn` aliases the anchor field on all four kinds that
// accept it — fqn on node/no_node, of on the degree kinds.
func TestFnAlias(t *testing.T) {
	if r := evalOne(t, Claim{Kind: "node", Fn: "App).Create", Tier: intp(1)}); r.Outcome != Pass {
		t.Errorf("node fn alias = %+v, want Pass", r)
	}
	if r := evalOne(t, Claim{Kind: "no_node", Fn: "svc/handler.deletedThing"}); r.Outcome != Pass {
		t.Errorf("no_node fn alias = %+v, want Pass", r)
	}
	if r := evalOne(t, Claim{Kind: "in_degree", Fn: "repo.Store).Save", Eq: intp(3)}); r.Outcome != Pass {
		t.Errorf("in_degree fn alias = %+v, want Pass", r)
	}
	if r := evalOne(t, Claim{Kind: "out_degree", Fn: "App).Create", Eq: intp(2)}); r.Outcome != Pass {
		t.Errorf("out_degree fn alias = %+v, want Pass", r)
	}
}

// TestFnCanonicalBothPresentErrors pins that supplying both the alias and its
// canonical spelling on one claim ERRORs — no silent precedence, mirroring the
// counterpart_matching/to_matching handling.
func TestFnCanonicalBothPresentErrors(t *testing.T) {
	if r := evalOne(t, Claim{Kind: "node", FQN: "App).Create", Fn: "App).Create"}); r.Outcome != Errored {
		t.Errorf("node fn+fqn = %+v, want Errored", r)
	}
	if r := evalOne(t, Claim{Kind: "no_node", FQN: "x", Fn: "y"}); r.Outcome != Errored {
		t.Errorf("no_node fn+fqn = %+v, want Errored", r)
	}
	if r := evalOne(t, Claim{Kind: "in_degree", Of: "repo.Store).Save", Fn: "repo.Store).Save", Eq: intp(3)}); r.Outcome != Errored {
		t.Errorf("in_degree fn+of = %+v, want Errored", r)
	}
	if r := evalOne(t, Claim{Kind: "out_degree", Of: "App).Create", Fn: "App).Create", Eq: intp(2)}); r.Outcome != Errored {
		t.Errorf("out_degree fn+of = %+v, want Errored", r)
	}
}

// TestFnOnEdgeIsWrongKind pins that `fn` on an edge kind (which has no anchor
// field) is a wrong-kind ERROR, not a silently-accepted field.
func TestFnOnEdgeIsWrongKind(t *testing.T) {
	if r := evalOne(t, Claim{Kind: "edge", From: "App).Create", To: "repo.Store).Save", Fn: "x"}); r.Outcome != Errored {
		t.Errorf("edge with fn = %+v, want Errored (wrong-kind)", r)
	}
}

// TestIDAllowedOnEveryKind pins that `id` is claim metadata, never a wrong-kind
// field: it does not trip the unexpected-field check on any kind.
func TestIDAllowedOnEveryKind(t *testing.T) {
	if r := evalOne(t, Claim{ID: "x", Kind: "node", FQN: "App).Create"}); r.Outcome != Pass {
		t.Errorf("id on node = %+v, want Pass", r)
	}
	if r := evalOne(t, Claim{ID: "x", Kind: "edge", From: "App).Create", To: "repo.Store).Save"}); r.Outcome != Pass {
		t.Errorf("id on edge = %+v, want Pass", r)
	}
}

// TestEntrypointPass pins the PASS forms: an exact/method-less agreeing
// multi-match, an observed-path wildcard, mount-prefix tolerance, an
// entry_kind-filtered consumer join, and a /regex/ fn whose match SET (>1)
// contains the record's handler (membership is the test).
func TestEntrypointPass(t *testing.T) {
	// "POST /loan" matches both the exact record and the method-less "/loan"
	// record; both carry Create, so they agree and PASS.
	if r := evalOne(t, Claim{Kind: "entrypoint", Name: "POST /loan", Fn: "handler.App).Create"}); r.Outcome != Pass {
		t.Errorf("exact/method-less agreeing = %+v, want Pass", r)
	}
	// Observed path against a {id} template segment.
	if r := evalOne(t, Claim{Kind: "entrypoint", Name: "GET /loan/42", Fn: "handler.App).Create"}); r.Outcome != Pass {
		t.Errorf("wildcard observed-path = %+v, want Pass", r)
	}
	// Mount tolerance: the alert's mounted path aligns against the registration tail.
	if r := evalOne(t, Claim{Kind: "entrypoint", Name: "POST /api/v1/loan", Fn: "handler.App).Create"}); r.Outcome != Pass {
		t.Errorf("mount-prefix tolerance = %+v, want Pass", r)
	}
	// entry_kind filter selects the consumer record; a topic degrades to equality.
	if r := evalOne(t, Claim{Kind: "entrypoint", EntryKind: "consumer", Name: "payment.settled", Fn: "repo.Store).Save"}); r.Outcome != Pass {
		t.Errorf("entry_kind-filtered consumer = %+v, want Pass", r)
	}
	// A /regex/ fn resolving to MORE than one node still passes when the record's
	// handler is in that set (the Save record's handler is one of the two matches).
	if r := evalOne(t, Claim{Kind: "entrypoint", EntryKind: "consumer", Name: "payment.settled", Fn: `/\)\.(Save|Score)$/`}); r.Outcome != Pass {
		t.Errorf("regex fn membership = %+v, want Pass", r)
	}
}

// TestEntrypointZeroMatchFails pins the existence polarity: a name matching no
// record FAILs (not ERRORs), including when an entry_kind filter EXCLUDES the
// only records the name would otherwise match.
func TestEntrypointZeroMatchFails(t *testing.T) {
	r := evalOne(t, Claim{Kind: "entrypoint", Name: "DELETE /nonexistent", Fn: "handler.App).Create"})
	if r.Outcome != Fail {
		t.Fatalf("absent route = %+v, want Fail", r)
	}
	if want := "no entrypoint matches 'DELETE /nonexistent'"; r.Detail != want {
		t.Errorf("absent-route detail = %q, want %q", r.Detail, want)
	}
	// "POST /loan" matches only http records; filtering to consumer excludes them
	// all, so the claim FAILs zero-match rather than passing.
	r = evalOne(t, Claim{Kind: "entrypoint", EntryKind: "consumer", Name: "POST /loan", Fn: "handler.App).Create"})
	if r.Outcome != Fail || r.Detail != "no entrypoint matches 'POST /loan'" {
		t.Errorf("entry_kind-excluded = %+v, want Fail zero-match", r)
	}
}

// TestEntrypointWrongFnFails pins that a name-matched record whose handler is
// NOT the resolved fn FAILs with the exact `handled by <fqn>` detail (the raw
// record FQN), and that the id-less label is the "name -> fn" join shape.
func TestEntrypointWrongFnFails(t *testing.T) {
	r := evalOne(t, Claim{Kind: "entrypoint", Name: "POST /loan", Fn: "repo.Store).Save"})
	if r.Outcome != Fail {
		t.Fatalf("wrong fn = %+v, want Fail", r)
	}
	if want := "handled by (*svc/handler.App).Create"; r.Detail != want {
		t.Errorf("wrong-fn detail = %q, want %q", r.Detail, want)
	}
	if want := "POST /loan -> repo.Store).Save"; r.Label != want {
		t.Errorf("id-less entrypoint label = %q, want %q", r.Label, want)
	}
}

// TestEntrypointDisagreeingJoinsError pins that name-matched records disagreeing
// on the handler ERROR with the exact join-list detail (deterministic: sorted,
// deduped, single-quoted names).
func TestEntrypointDisagreeingJoinsError(t *testing.T) {
	r := evalOne(t, Claim{Kind: "entrypoint", Name: "GET /widget/7", Fn: "handler.App).Create"})
	if r.Outcome != Errored {
		t.Fatalf("disagreeing joins = %+v, want Errored", r)
	}
	want := "ambiguous entrypoint: 'GET /widget/7' matches 2 joins with differing handlers: " +
		"'GET /widget/{id}' -> (*svc/handler.App).Create; 'GET /widget/{name}' -> (*svc/repo.Store).Save"
	if r.Detail != want {
		t.Errorf("disagree detail = %q, want %q", r.Detail, want)
	}
}

// TestEntrypointFnResolutionErrors pins that a bad `fn` ERRORs before any FAIL
// polarity is computed — ambiguous and unresolved both, matching every other kind.
func TestEntrypointFnResolutionErrors(t *testing.T) {
	if r := evalOne(t, Claim{Kind: "entrypoint", Name: "POST /loan", Fn: ".Score"}); r.Outcome != Errored {
		t.Errorf("ambiguous fn = %+v, want Errored", r)
	}
	if r := evalOne(t, Claim{Kind: "entrypoint", Name: "POST /loan", Fn: "does.Not.Exist"}); r.Outcome != Errored {
		t.Errorf("unresolved fn = %+v, want Errored", r)
	}
}

// TestEntrypointSchemaErrors pins the fail-closed schema checks: an invalid
// entry_kind, a missing name, and a missing fn each ERROR.
func TestEntrypointSchemaErrors(t *testing.T) {
	r := evalOne(t, Claim{Kind: "entrypoint", Name: "POST /loan", Fn: "handler.App).Create", EntryKind: "htp"})
	if r.Outcome != Errored {
		t.Fatalf("invalid entry_kind = %+v, want Errored", r)
	}
	if want := `unknown entry_kind "htp" (known kinds: "callback", "consumer", "http", "worker")`; r.Detail != want {
		t.Errorf("invalid entry_kind detail = %q, want %q", r.Detail, want)
	}
	if r := evalOne(t, Claim{Kind: "entrypoint", Fn: "handler.App).Create"}); r.Outcome != Errored || r.Detail != "entrypoint requires 'name'" {
		t.Errorf("missing name = %+v, want Errored 'requires name'", r)
	}
	if r := evalOne(t, Claim{Kind: "entrypoint", Name: "POST /loan"}); r.Outcome != Errored || r.Detail != "entrypoint requires 'fn'" {
		t.Errorf("missing fn = %+v, want Errored 'requires fn'", r)
	}
}

// TestEntrypointWrongKindFields pins the unexpected-field closure both ways:
// `name` on an edge claim ERRORs (it must not be silently ignored — the exact
// verdict-inverting hazard), `fqn` on an entrypoint claim ERRORs, and `id` is
// allowed on an entrypoint claim.
func TestEntrypointWrongKindFields(t *testing.T) {
	if r := evalOne(t, Claim{Kind: "edge", From: "App).Create", To: "repo.Store).Save", Name: "x"}); r.Outcome != Errored {
		t.Errorf("name on edge = %+v, want Errored (wrong-kind)", r)
	}
	if r := evalOne(t, Claim{Kind: "entrypoint", Name: "POST /loan", Fn: "handler.App).Create", FQN: "x"}); r.Outcome != Errored {
		t.Errorf("fqn on entrypoint = %+v, want Errored (wrong-kind)", r)
	}
	r := evalOne(t, Claim{ID: "e1", Kind: "entrypoint", Name: "POST /loan", Fn: "handler.App).Create"})
	if r.Outcome != Pass || r.Label != "e1" {
		t.Errorf("id on entrypoint = %+v, want Pass with Label \"e1\"", r)
	}
}

// TestEntrypointKindAwareMatching pins the closed name-matching bleed: route
// grammar applies ONLY between a route-shaped claim name and an http record; every
// other case is exact equality. testGraph carries param-tailed http routes that,
// under the old unconditional routematch, co-matched a consumer topic and a
// slash-less name (universal-wildcard false matches).
func TestEntrypointKindAwareMatching(t *testing.T) {
	// (1) A consumer-topic claim WITHOUT entry_kind matches ONLY the consumer
	// record — not the param-tailed http routes ("GET /widget/{id}" etc.) that used
	// to co-match "payment.settled" via their wildcard tail.
	if r := evalOne(t, Claim{Kind: "entrypoint", Name: "payment.settled", Fn: "repo.Store).Save"}); r.Outcome != Pass {
		t.Errorf("topic without entry_kind = %+v, want Pass (consumer-only match)", r)
	}
	// (4) A slash-less name "GET" is not a route path: it must NOT tail-match a
	// param-wildcard registration tail — zero-match FAIL, not a wildcard hit.
	r := evalOne(t, Claim{Kind: "entrypoint", Name: "GET", Fn: "handler.App).Create"})
	if r.Outcome != Fail || r.Detail != "no entrypoint matches 'GET'" {
		t.Errorf("slash-less name = %+v, want zero-match Fail", r)
	}
}

// TestEntrypointConsumerTopicFalsePassRegression pins the reproduced silent false
// SATISFIED: a graph with NO consumer record but an http route whose param tail
// wildcards the topic name. The old matcher passed (route tail matched the topic);
// the kind-aware matcher FAILs zero-match (an http record is never matched by a
// slash-less topic name).
func TestEntrypointConsumerTopicFalsePassRegression(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{{FQN: "(*svc/replay.Handler).Replay", Tier: 1}},
		Entrypoints: []graph.Entrypoint{
			{Kind: "http", Name: "POST /replay/{topic}", Fn: "(*svc/replay.Handler).Replay"},
		},
	}
	r := evalOneG(t, g, Claim{Kind: "entrypoint", Name: "payment.settled", Fn: "replay.Handler).Replay"})
	if r.Outcome != Fail || r.Detail != "no entrypoint matches 'payment.settled'" {
		t.Errorf("topic vs http param-tail = %+v, want zero-match Fail (was a silent false PASS)", r)
	}
}

// TestEntrypointDollarTopicNotWildcard pins that a "$"-prefixed consumer record —
// a param token to routematch — does NOT act as a universal wildcard: matched by
// exact equality only. Old behavior FALSE-PASSED an unrelated claim whose fn equaled
// the $-topic's handler; the kind-aware matcher FAILs zero-match.
func TestEntrypointDollarTopicNotWildcard(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: "(*svc/audit.Sink).Consume", Tier: 1},
			{FQN: "(*svc/handler.App).Create", Tier: 1},
		},
		Entrypoints: []graph.Entrypoint{
			{Kind: "consumer", Name: "$internal.audit", Fn: "(*svc/audit.Sink).Consume"},
			{Kind: "http", Name: "POST /loans", Fn: "(*svc/handler.App).Create"},
		},
	}
	// entry_kind omitted; fn equals the $-topic's handler so an OLD universal-wildcard
	// match would have PASSED. The $-topic must not match the unrelated route name.
	r := evalOneG(t, g, Claim{Kind: "entrypoint", Name: "GET /unrelated", Fn: "audit.Sink).Consume"})
	if r.Outcome != Fail || r.Detail != "no entrypoint matches 'GET /unrelated'" {
		t.Errorf("$-topic universal-wildcard = %+v, want zero-match Fail", r)
	}
}

// TestEntrypointCallbackWorkerKinds pins the four-kind vocabulary: a declared
// callback record (Name an "import/path#Symbol" reference) matches by exact equality;
// entry_kind "callback"/"worker" are accepted (no unknown-kind ERROR); "htp" still
// ERRORs with the shared-vocabulary detail.
func TestEntrypointCallbackWorkerKinds(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{{FQN: "(*svc/jobs.Worker).OnEvent", Tier: 1}},
		Entrypoints: []graph.Entrypoint{
			{Kind: "callback", Name: "svc/jobs#OnEvent", Fn: "(*svc/jobs.Worker).OnEvent"},
		},
	}
	// Exact-name claim against the declared callback → PASS.
	if r := evalOneG(t, g, Claim{Kind: "entrypoint", Name: "svc/jobs#OnEvent", Fn: "jobs.Worker).OnEvent"}); r.Outcome != Pass {
		t.Errorf("callback exact-name = %+v, want Pass", r)
	}
	// entry_kind "callback" is accepted (selects the record) → PASS.
	if r := evalOneG(t, g, Claim{Kind: "entrypoint", EntryKind: "callback", Name: "svc/jobs#OnEvent", Fn: "jobs.Worker).OnEvent"}); r.Outcome != Pass {
		t.Errorf("entry_kind callback = %+v, want Pass", r)
	}
	// entry_kind "worker" is a KNOWN kind (no unknown-kind ERROR): it excludes the
	// callback record here, so the claim FAILs zero-match — not ERRORs.
	if r := evalOneG(t, g, Claim{Kind: "entrypoint", EntryKind: "worker", Name: "svc/jobs#OnEvent", Fn: "jobs.Worker).OnEvent"}); r.Outcome != Fail {
		t.Errorf("entry_kind worker (known, excludes) = %+v, want zero-match Fail", r)
	}
	// "htp" is still an unknown kind → ERROR with the shared-vocabulary detail.
	r := evalOneG(t, g, Claim{Kind: "entrypoint", EntryKind: "htp", Name: "svc/jobs#OnEvent", Fn: "jobs.Worker).OnEvent"})
	if r.Outcome != Errored || r.Detail != `unknown entry_kind "htp" (known kinds: "callback", "consumer", "http", "worker")` {
		t.Errorf("unknown entry_kind htp = %+v, want the shared-vocabulary ERROR", r)
	}
}

// TestEntrypointEmptyUniverseAbstains pins the fail-closed abstention: a graph with
// ZERO entrypoints[] records ERRORs (the join is absent — blindness, not a removed
// route), while an entry_kind filter excluding every record over a NON-empty universe
// stays a zero-match FAIL.
func TestEntrypointEmptyUniverseAbstains(t *testing.T) {
	g := &graph.Graph{Nodes: []graph.Node{{FQN: "(*svc/handler.App).Create", Tier: 1}}}
	r := evalOneG(t, g, Claim{Kind: "entrypoint", Name: "POST /loan", Fn: "handler.App).Create"})
	want := "graph carries no entrypoints[] records: the route/topic -> handler join is absent (routers outside root discovery's coverage, or a pre-join producer)"
	if r.Outcome != Errored || r.Detail != want {
		t.Errorf("empty entrypoints universe = %+v, want abstain ERROR", r)
	}
	// A NON-empty universe with an entry_kind that excludes every match stays a FAIL
	// (only the empty UNIVERSE abstains): testGraph has no worker records.
	if r := evalOne(t, Claim{Kind: "entrypoint", EntryKind: "worker", Name: "payment.settled", Fn: "repo.Store).Save"}); r.Outcome != Fail {
		t.Errorf("entry_kind excludes all over non-empty universe = %+v, want zero-match Fail", r)
	}
}

// TestEntrypointExactNameTiebreak pins Fix 4: for the literal-vs-template overlap,
// a claim whose name equals a registration literal grades against THAT record only
// (the tightest spelling), so PASS/FAIL is read from the author's exact spelling; a
// non-verbatim param spelling that matches disagreeing records still ERRORs.
func TestEntrypointExactNameTiebreak(t *testing.T) {
	g := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: "(*svc/handler.App).Me", Tier: 1},
			{FQN: "(*svc/handler.App).ByID", Tier: 1},
		},
		Entrypoints: []graph.Entrypoint{
			{Kind: "http", Name: "GET /users/me", Fn: "(*svc/handler.App).Me"},
			{Kind: "http", Name: "GET /users/{id}", Fn: "(*svc/handler.App).ByID"},
		},
	}
	// "GET /users/me" matches both records (me == literal; me ~ {id}); the exact-name
	// tiebreak selects the literal record → grades against H1 (Me).
	if r := evalOneG(t, g, Claim{Kind: "entrypoint", Name: "GET /users/me", Fn: "handler.App).Me"}); r.Outcome != Pass {
		t.Errorf("literal spelling = %+v, want Pass against H1", r)
	}
	// "GET /users/{id}" matches both ({id} query wildcards the literal); the tiebreak
	// selects the verbatim template record → grades against H2 (ByID).
	if r := evalOneG(t, g, Claim{Kind: "entrypoint", Name: "GET /users/{id}", Fn: "handler.App).ByID"}); r.Outcome != Pass {
		t.Errorf("template spelling = %+v, want Pass against H2", r)
	}
	// A NON-verbatim param spelling matches both records and equals neither, so no
	// tiebreak applies and the disagree ERROR remains (forcing a tighter name). NOTE:
	// the FR's example query "GET /users/42" cannot reach this branch — a concrete
	// segment matches only the {id} template, not the "me" literal (verified against
	// routematch), so a param spelling is used to exercise the still-ambiguous case.
	r := evalOneG(t, g, Claim{Kind: "entrypoint", Name: "GET /users/{uid}", Fn: "handler.App).Me"})
	if r.Outcome != Errored {
		t.Fatalf("non-verbatim param spelling = %+v, want disagree Errored", r)
	}
	wantErr := "ambiguous entrypoint: 'GET /users/{uid}' matches 2 joins with differing handlers: " +
		"'GET /users/me' -> (*svc/handler.App).Me; 'GET /users/{id}' -> (*svc/handler.App).ByID"
	if r.Detail != wantErr {
		t.Errorf("disagree detail = %q, want %q", r.Detail, wantErr)
	}
}

// TestEntrypointWhitespaceName pins Fix 7: a whitespace-only name is not a real
// route (it would grade against the bare-"/" root), so it hits the requires-'name'
// ERROR, not a fabricated match.
func TestEntrypointWhitespaceName(t *testing.T) {
	r := evalOne(t, Claim{Kind: "entrypoint", Name: "   ", Fn: "handler.App).Create"})
	if r.Outcome != Errored || r.Detail != "entrypoint requires 'name'" {
		t.Errorf("whitespace-only name = %+v, want requires-'name' ERROR", r)
	}
}

// TestUnexpectedFieldCoversClaim pins Fix 6: claimFieldChecks is a COMPLETE parallel
// of the Claim struct — every json-tagged field except `kind` and `id` (claim
// metadata) is screened by unexpectedField. A new Claim field missed there would be
// silently ignored on the wrong kind, the verdict-inverting hazard the check closes.
func TestUnexpectedFieldCoversClaim(t *testing.T) {
	checked := map[string]bool{}
	for _, f := range claimFieldChecks {
		checked[f.name] = true
	}
	rt := reflect.TypeOf(Claim{})
	for i := 0; i < rt.NumField(); i++ {
		name := strings.SplitN(rt.Field(i).Tag.Get("json"), ",", 2)[0]
		if name == "" || name == "-" || name == "kind" || name == "id" {
			continue
		}
		if !checked[name] {
			t.Errorf("Claim field %q (json:%q) is not screened by claimFieldChecks — a wrong-kind use would be silently ignored", rt.Field(i).Name, name)
		}
	}
}

// TestReportFormat pins the exact line shapes the CLI/golden depend on.
func TestReportFormat(t *testing.T) {
	rep := Evaluate(testGraph(), &File{Claims: []Claim{
		{Kind: "edge", From: "App).Create", To: "repo.Store).Save"}, // pass (silent)
		{Kind: "edge", From: "repo.Store).Save", To: "App).Create"}, // fail
		{Kind: "node", FQN: ".Score"},                               // errored (ambiguous)
	}})
	want := "FAIL  repo.Store).Save -> App).Create [edge] 0 edge(s)\n" +
		"ERROR .Score [node] AMBIGUOUS: '.Score' matches 2: (*svc/scoring.Remote).Score; svc/scoring.Score\n" +
		"assert: 1 passed, 1 failed, 1 errored (graph: 5 nodes, 4 unique edges)\n"
	if got := rep.String(); got != want {
		t.Errorf("report format:\n got %q\nwant %q", got, want)
	}
}
