package graphio

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/config"
	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
	"github.com/jyang234/golang-code-graph/internal/static/openapi"
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
