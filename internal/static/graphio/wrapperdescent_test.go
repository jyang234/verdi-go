package graphio

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/config"
	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
	"github.com/jyang234/golang-code-graph/internal/static/callgraph"
	"github.com/jyang234/golang-code-graph/internal/static/loader"
	"github.com/jyang234/golang-code-graph/internal/static/openapi"
	"github.com/jyang234/golang-code-graph/internal/static/roots"
	"github.com/jyang234/golang-code-graph/internal/static/ssabuild"
)

// TestWrapperDescentInertOnDirectCalls exercises the wrapper-descent seam against the
// same-module oapiclientsvc fixture (its client bodies already exist, so no SSA widening
// is needed to reach them). The fixture ships NO hand-written wrappers — every outbound
// call is either a DIRECT generated-method call or the generated CONSTRUCTOR — so
// flipping followWrappers=true programmatically must:
//   - leave every direct generated-method call NAMED via=openapi-client (descent is gated
//     on oapiLabel=="" so a direct hit never enters it), and produce NO
//     via=openapi-client-wrapper edge; and
//   - descend the non-operation constructor (NewClientWithResponses), find zero
//     operations, and append the exact zero-found suffix to its UnresolvedSpecOperation
//     disclosure.
//
// It also pins double-run byte-identity of the whole marshalled graph under the flipped
// config (the prime directive). A full separate-module wrapper→operation descent (a real
// wrapper naming an edge via=openapi-client-wrapper, and the ambiguous ≥2 append) needs a
// new fixture and is deferred to the fixture wave — see the report.
func TestWrapperDescentInertOnDirectCalls(t *testing.T) {
	dir := oapiClientFixtureDir()
	res, err := analyze.Analyze(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Flip followWrappers=true on a COPY of the declared hints (the fixture's committed
	// config leaves it false). The client is same-module, so Analyze already built its
	// bodies; for this fixture only the labeler needs the opt-in.
	hints := append([]config.OpenAPIClientHint(nil), res.Config.Classify.OpenAPIClients...)
	if len(hints) == 0 {
		t.Fatal("fixture must declare an openapi client")
	}
	for i := range hints {
		hints[i].FollowWrappers = true
	}
	lab, err := openapi.NewLabeler(hints, dir)
	if err != nil {
		t.Fatal(err)
	}

	on, err := Build(res, "", WithOpenAPI(lab))
	if err != nil {
		t.Fatal(err)
	}

	// (1) Direct calls are unchanged: the spec-named boundary edges still carry
	// via=openapi-client, and descent produced NO via=openapi-client-wrapper edge (there
	// are no real wrappers in this fixture).
	boundaryEdges := 0
	for _, e := range on.Edges {
		if e.Via == openapi.ViaWrapper {
			t.Errorf("no wrapper-descended edge expected in a wrapper-free fixture, got %+v", e)
		}
		if strings.HasPrefix(e.To, "boundary:event-bus") {
			boundaryEdges++
			if e.Via != openapi.Via {
				t.Errorf("direct generated-method edge %q: via = %q, want %q", e.To, e.Via, openapi.Via)
			}
		}
	}
	if boundaryEdges == 0 {
		t.Fatal("expected the direct generated-method calls to still be named as boundary edges")
	}

	// (2) The non-operation constructor is descended (0 operations found) and its
	// disclosure carries the EXACT zero-found suffix. N=1: the walk visits only the
	// constructor itself, which calls no declared-package function.
	const wantSuffix = "; descended 1 declared-package function(s) and found 0 operations"
	var disc *blindspots.BlindSpot
	for i := range on.BlindSpots {
		b := &on.BlindSpots[i]
		if b.Kind == blindspots.UnresolvedSpecOperation && strings.Contains(b.Detail, "NewClientWithResponses") {
			disc = b
		}
	}
	if disc == nil {
		t.Fatal("expected the constructor's UnresolvedSpecOperation disclosure")
	}
	if !strings.HasSuffix(disc.Detail, wantSuffix) {
		t.Errorf("disclosure detail = %q\nwant it to end with %q", disc.Detail, wantSuffix)
	}

	// (3) Byte-identity across two independent builds under the flipped config.
	b1, err := on.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	on2, err := Build(res, "", WithOpenAPI(lab))
	if err != nil {
		t.Fatal(err)
	}
	b2, err := on2.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatal("followWrappers-enabled graph is not byte-identical across two builds")
	}
}

// wrapClientFixtureDir resolves the wrapclientsvc fixture — a service whose SEPARATE-MODULE
// generated client (example.com/wrapclientlib/eventbus) is reached only through hand-written
// wrappers — from this file's location, independent of the caller's working directory (the
// same convention oapiClientFixtureDir uses for the same-module fixture).
func wrapClientFixtureDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", "wrapclientsvc")
}

// The boundary labels, calling handler, and descent-outcome appendices the wrapclientsvc
// fixture produces. Both operation-label lists in the ambiguous suffix are in the sorted
// order descendWrapper emits (publishers < subscribers), and the zero-found suffix's N=2 is
// Warm plus its transport helper (probe) — the two declared-package functions the descent
// visits before finding no operation.
const (
	wrapEnsureFQN       = "example.com/wrapclientsvc.ensure"
	wrapWrapperEdgeTo   = "boundary:event-bus POST /v1/eventTypeTemplates"             // EnsureTemplate → 1 op, descended
	wrapDirectEdgeTo    = "boundary:event-bus GET /v1/eventTypeTemplates/{templateId}" // GetTemplateWithResponse, direct
	wrapAmbiguousSuffix = "; descended 1 declared-package function(s) and found 2 ambiguous operations (event-bus POST /v1/publishers; event-bus POST /v1/subscribers)"
	wrapZeroFoundSuffix = "; descended 2 declared-package function(s) and found 0 operations" // Warm → probe, N=2
)

// findDisclosure returns the first UnresolvedSpecOperation blind spot whose Detail names
// calleeSubstr (a callee FQN fragment), or nil. The disclosures all share one Site (the
// handler), so the callee substring is what distinguishes them.
func findDisclosure(bs []blindspots.BlindSpot, calleeSubstr string) *blindspots.BlindSpot {
	for i := range bs {
		if bs[i].Kind == blindspots.UnresolvedSpecOperation && strings.Contains(bs[i].Detail, calleeSubstr) {
			return &bs[i]
		}
	}
	return nil
}

// TestWrapperDescentNamesOneOpDisclosesAmbiguousAndZeroAcrossModules is the wrapper-descent
// end-to-end acceptance test against the committed .flowmap.yaml (followWrappers: true). The
// declared client lives in a SEPARATE module, so Analyze widens its bodies into SSA and
// --reclaim-openapi descends the hand-written wrappers. It asserts, against BOTH the
// committed golden and the decoded graph:
//   - (a) exactly one via=openapi-client-wrapper edge — the one-operation EnsureTemplate
//     wrapper, descended through one helper hop, From the calling handler To the POST label;
//   - (b) the DIRECT generated-method call keeps via=openapi-client (descent is gated on a
//     no-name-match, so a direct hit never enters it);
//   - (c) the two-operation EnsureParticipant wrapper stays disclosed, its Detail ending with
//     BOTH ops in sorted order — never a guessed pick;
//   - (d) the zero-operation Warm wrapper stays disclosed found-0, N counting the helper hop;
//   - (e) the node horizon is unchanged — the widened declared packages became traversable
//     for descent but contribute NO graph node; and
//   - (f) the whole marshalled graph is byte-identical across two independent builds.
func TestWrapperDescentNamesOneOpDisclosesAmbiguousAndZeroAcrossModules(t *testing.T) {
	dir := wrapClientFixtureDir()
	res, err := analyze.Analyze(dir)
	if err != nil {
		t.Fatal(err)
	}
	lab, err := openapi.NewLabeler(res.Config.Classify.OpenAPIClients, dir)
	if err != nil {
		t.Fatal(err)
	}
	if lab == nil {
		t.Fatal("fixture config declares an openapi client, but NewLabeler returned nil")
	}
	on, err := Build(res, "", WithOpenAPI(lab))
	if err != nil {
		t.Fatal(err)
	}
	b1, err := on.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	// Golden (regenerate-and-diff): the committed graph is generated by
	// testdata/groundwork/regen.sh (flowmap graph --reclaim-openapi | strip_tool), which is
	// byte-identical to Build(...).Marshal() here (Build sets no `tool` field). Read and
	// compare so regen.sh stays the SINGLE generator for this golden (matching oapiclientsvc).
	golden := filepath.Join("testdata", "wrapclientsvc.openapi.graph.json")
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with testdata/groundwork/regen.sh): %v", golden, err)
	}
	if string(want) != string(b1) {
		t.Errorf("%s is stale (regenerate with testdata/groundwork/regen.sh):\n%s", golden, firstDiff(string(want), string(b1)))
	}

	// Structural assertions on the DECODED graph, not the golden bytes alone.
	var g Graph
	if err := json.Unmarshal(b1, &g); err != nil {
		t.Fatalf("decode built graph: %v", err)
	}

	// The fixture names exactly TWO boundary edges — the descended POST wrapper and the
	// direct GET — and every other wrapper call stays a disclosure, never a spurious edge.
	// This is the boundary-edge count the .callgraph.md golden renders.
	boundaryEdges := 0
	for _, e := range g.Edges {
		if strings.HasPrefix(e.To, "boundary:") {
			boundaryEdges++
		}
	}
	if boundaryEdges != 2 {
		t.Errorf("want exactly 2 boundary edges, got %d", boundaryEdges)
	}

	// (a) Exactly one via=openapi-client-wrapper edge: the descended EnsureTemplate wrapper.
	var wrapperEdges []Edge
	for _, e := range g.Edges {
		if e.Via == openapi.ViaWrapper {
			wrapperEdges = append(wrapperEdges, e)
		}
	}
	if len(wrapperEdges) != 1 {
		t.Fatalf("want exactly one via=%s edge, got %d: %+v", openapi.ViaWrapper, len(wrapperEdges), wrapperEdges)
	}
	if we := wrapperEdges[0]; we.From != wrapEnsureFQN || we.To != wrapWrapperEdgeTo {
		t.Errorf("wrapper edge = {from=%q to=%q}, want {from=%q to=%q}", we.From, we.To, wrapEnsureFQN, wrapWrapperEdgeTo)
	}

	// (b) The direct generated-method call keeps via=openapi-client.
	var directEdge *Edge
	for i := range g.Edges {
		if g.Edges[i].To == wrapDirectEdgeTo {
			directEdge = &g.Edges[i]
		}
	}
	if directEdge == nil {
		t.Fatalf("missing the direct generated-method boundary edge %q", wrapDirectEdgeTo)
	}
	if directEdge.From != wrapEnsureFQN {
		t.Errorf("direct edge from = %q, want %q", directEdge.From, wrapEnsureFQN)
	}
	if directEdge.Via != openapi.Via {
		t.Errorf("direct edge %q: via = %q, want %q", directEdge.To, directEdge.Via, openapi.Via)
	}

	// (c) The two-operation wrapper stays disclosed, both ops named in sorted order.
	ambiguous := findDisclosure(g.BlindSpots, "Registrar).EnsureParticipant")
	if ambiguous == nil {
		t.Fatal("missing the ambiguous EnsureParticipant disclosure")
	}
	if !strings.HasSuffix(ambiguous.Detail, wrapAmbiguousSuffix) {
		t.Errorf("ambiguous disclosure detail = %q\nwant it to end with %q", ambiguous.Detail, wrapAmbiguousSuffix)
	}

	// (d) The zero-operation wrapper (transport helper only) is disclosed found-0 (N=2).
	zero := findDisclosure(g.BlindSpots, "Registrar).Warm")
	if zero == nil {
		t.Fatal("missing the zero-found Warm disclosure")
	}
	if !strings.HasSuffix(zero.Detail, wrapZeroFoundSuffix) {
		t.Errorf("zero-found disclosure detail = %q\nwant it to end with %q", zero.Detail, wrapZeroFoundSuffix)
	}

	// (e) Node horizon: the widened declared packages became TRAVERSABLE for descent but
	// must never become graph NODES — no node carries the client module's package or FQN.
	for _, n := range g.Nodes {
		if strings.Contains(n.Package, "wrapclientlib") || strings.Contains(n.FQN, "wrapclientlib") {
			t.Errorf("declared-package function leaked into the node horizon: %+v", n)
		}
	}

	// (f) Byte-identity across two independent builds (the prime directive).
	on2, err := Build(res, "", WithOpenAPI(lab))
	if err != nil {
		t.Fatal(err)
	}
	b2, err := on2.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatal("wrapper-descended graph is not byte-identical across two builds")
	}
}

// TestFollowWrappersOffDisclosesWrappersWithPreDescentDetail is the inert-when-unset
// acceptance criterion: with followWrappers off the feature adds nothing. It replicates
// analyze.Analyze's pipeline (load → SSA → roots → callgraph) but flips every hint's
// FollowWrappers to false FIRST — so ssabuild.Build runs with NO extras (no SSA widening)
// and NewLabeler builds a no-descent table — then asserts:
//   - (a) NO via=openapi-client-wrapper edge is emitted (descent never runs);
//   - (b)+(c) the wrapper calls ARE disclosed as UnresolvedSpecOperation (the pre-feature
//     behavior), with NO "; descended" appendix anywhere and the EnsureTemplate wrapper's
//     detail equal to the exact pre-descent message BYTE-FOR-BYTE (the unmodified message is
//     the acceptance criterion); and
//   - (d) the direct generated call is STILL named via=openapi-client (labeling keys on the
//     callee name, never its unwidened body); plus (e) double-run byte-identity.
func TestFollowWrappersOffDisclosesWrappersWithPreDescentDetail(t *testing.T) {
	dir := wrapClientFixtureDir()

	// Replicate analyze.Analyze, but flip FollowWrappers off FIRST so ssabuild.Build gets no
	// extras (no widening) and NewLabeler builds a no-descent table — assembling the Result by
	// hand from the same steps analyze.Analyze runs.
	cfg, err := config.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Classify.OpenAPIClients) == 0 {
		t.Fatal("fixture must declare an openapi client")
	}
	for i := range cfg.Classify.OpenAPIClients {
		cfg.Classify.OpenAPIClients[i].FollowWrappers = false
	}
	svc, err := loader.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := ssabuild.Build(svc) // NO extras: the client bodies are NOT widened in
	if err != nil {
		t.Fatal(err)
	}
	rs := roots.Discover(prog, analyze.Registrars(cfg), analyze.DeclaredEntrypoints(cfg)...)
	cgph, err := callgraph.Build(prog, rs, callgraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	res := &analyze.Result{Dir: dir, Config: cfg, Service: svc, Program: prog, Roots: rs, Graph: cgph}

	lab, err := openapi.NewLabeler(cfg.Classify.OpenAPIClients, dir)
	if err != nil {
		t.Fatal(err)
	}
	off, err := Build(res, "", WithOpenAPI(lab))
	if err != nil {
		t.Fatal(err)
	}

	// (a) No wrapper-descended edge exists — descent never ran.
	for _, e := range off.Edges {
		if e.Via == openapi.ViaWrapper {
			t.Errorf("followWrappers off must emit no via=%s edge, got %+v", openapi.ViaWrapper, e)
		}
	}

	// (b)+(c) The wrapper calls ARE disclosed (pre-feature behavior), and every
	// UnresolvedSpecOperation detail is the exact pre-descent message — no "; descended"
	// appendix anywhere. Byte-precision is the point, so the EnsureTemplate wrapper's
	// disclosure is asserted in FULL.
	var ensureTmpl *blindspots.BlindSpot
	discCount := 0
	for i := range off.BlindSpots {
		b := &off.BlindSpots[i]
		if b.Kind != blindspots.UnresolvedSpecOperation {
			continue
		}
		discCount++
		if strings.Contains(b.Detail, "; descended") {
			t.Errorf("followWrappers off must not append a descent outcome, got %q", b.Detail)
		}
		if strings.Contains(b.Detail, "Registrar).EnsureTemplate") {
			ensureTmpl = b
		}
	}
	if discCount == 0 {
		t.Fatal("expected the wrapper calls to be disclosed as UnresolvedSpecOperation (the pre-descent behavior)")
	}
	if ensureTmpl == nil {
		t.Fatal("expected the EnsureTemplate wrapper call to be disclosed")
	}
	const wantDetail = "call to (*example.com/wrapclientlib/eventbus.Registrar).EnsureTemplate " +
		"in declared openapi-client package example.com/wrapclientlib/eventbus matched no spec operation; " +
		"the outbound call cannot be named from the spec " +
		"(a client helper/transport, or generator drift that dropped an operation)"
	if ensureTmpl.Detail != wantDetail {
		t.Errorf("pre-descent disclosure detail =\n  %q\nwant exactly\n  %q", ensureTmpl.Detail, wantDetail)
	}

	// (d) The direct generated call is STILL named via=openapi-client: labeling keys on the
	// callee name, never its (unwidened) body.
	var directEdge *Edge
	for i := range off.Edges {
		if off.Edges[i].To == wrapDirectEdgeTo {
			directEdge = &off.Edges[i]
		}
	}
	if directEdge == nil {
		t.Fatalf("missing the direct generated-method boundary edge %q", wrapDirectEdgeTo)
	}
	if directEdge.Via != openapi.Via {
		t.Errorf("direct edge %q: via = %q, want %q", directEdge.To, directEdge.Via, openapi.Via)
	}

	// (e) Byte-identity across two independent builds under the off pipeline.
	b1, err := off.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	off2, err := Build(res, "", WithOpenAPI(lab))
	if err != nil {
		t.Fatal(err)
	}
	b2, err := off2.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatal("followWrappers-off graph is not byte-identical across two builds")
	}
}
