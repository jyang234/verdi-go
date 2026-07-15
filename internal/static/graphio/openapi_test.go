package graphio

import (
	"bytes"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
	"github.com/jyang234/golang-code-graph/internal/static/openapi"
)

// oapiClientFixtureDir resolves the oapiclientsvc fixture from this file's location, so
// the test is independent of the caller's working directory (statictest.FixtureDir does
// the same for loansvc; this fixture is graphio-specific, so it is resolved here).
func oapiClientFixtureDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", "oapiclientsvc")
}

// The two operation labels the fixture's spec + client stand-in produce. All six
// generated-name shapes across both operations collapse to these two boundary edges
// (one per operationId), since edges dedup on their full attribute tuple.
const (
	oapiPOSTLabel = "boundary:event-bus POST /v1/publishers/{publisherId}/eventTypes/{eventType}/versions/{version}/events/{eventId}"
	oapiGETLabel  = "boundary:event-bus GET /v1/events/{eventId}"
)

// TestOpenAPIClientLabeling is the FR's core acceptance test: with --reclaim-openapi
// (WithOpenAPI) the generated-client calls are NAMED boundary:<peer> <METHOD> <template>
// (via=openapi-client) from the spec, the constructor call is DISCLOSED as an
// UnresolvedSpecOperation with its callee FQN, and the whole thing is byte-stable;
// WITHOUT it the graph carries no openapi footprint (strictly opt-in).
func TestOpenAPIClientLabeling(t *testing.T) {
	dir := oapiClientFixtureDir()
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

	// OFF: no openapi footprint at all — the strictly-opt-in contract.
	off, err := Build(res, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range off.Edges {
		if e.Via == openapi.Via || strings.HasPrefix(e.To, "boundary:event-bus") {
			t.Errorf("off build must carry no openapi edge, got %+v", e)
		}
	}
	for _, b := range off.BlindSpots {
		if b.Kind == blindspots.UnresolvedSpecOperation {
			t.Errorf("off build must carry no UnresolvedSpecOperation, got %+v", b)
		}
	}

	// ON: the two named boundary edges, each outbound-sync, tier 1, via=openapi-client,
	// rooted at the service handler (not the client's internal plumbing).
	on, err := Build(res, "", WithOpenAPI(lab))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{oapiPOSTLabel: false, oapiGETLabel: false}
	for _, e := range on.Edges {
		if _, want := seen[e.To]; !want {
			continue
		}
		seen[e.To] = true
		if e.Via != openapi.Via {
			t.Errorf("edge %q: via = %q, want %q", e.To, e.Via, openapi.Via)
		}
		if e.Boundary != "outbound-sync" {
			t.Errorf("edge %q: boundary = %q, want outbound-sync", e.To, e.Boundary)
		}
		if e.Tier != 1 {
			t.Errorf("edge %q: tier = %d, want 1", e.To, e.Tier)
		}
		if e.From != "example.com/oapiclientsvc.publish" {
			t.Errorf("edge %q: from = %q, want the service handler", e.To, e.From)
		}
	}
	for label, ok := range seen {
		if !ok {
			t.Errorf("missing expected boundary edge %q", label)
		}
	}

	// The client's INTERNAL plumbing edge (NewClientWithResponses' body reaching
	// NewCreateEventRequest, etc.) must NOT be relabeled: only the SERVICE→client edge
	// is named. So no via=openapi-client edge originates inside the client package.
	for _, e := range on.Edges {
		if e.Via == openapi.Via && strings.Contains(e.From, "/clients/eventbus.") {
			t.Errorf("client-internal edge must not be openapi-labeled, got %+v", e)
		}
	}

	// The constructor (a non-operation) is disclosed with its callee FQN, never labeled.
	var disc *blindspots.BlindSpot
	for i := range on.BlindSpots {
		if on.BlindSpots[i].Kind == blindspots.UnresolvedSpecOperation {
			if disc != nil {
				t.Fatalf("expected exactly one UnresolvedSpecOperation, got a second: %+v", on.BlindSpots[i])
			}
			disc = &on.BlindSpots[i]
		}
	}
	if disc == nil {
		t.Fatal("expected an UnresolvedSpecOperation disclosure for the constructor call")
	}
	if disc.Site != "example.com/oapiclientsvc.publish" {
		t.Errorf("disclosure site = %q, want the calling handler", disc.Site)
	}
	if !strings.Contains(disc.Detail, "example.com/oapiclientsvc/clients/eventbus.NewClientWithResponses") {
		t.Errorf("disclosure must name the callee FQN, got %q", disc.Detail)
	}

	// Byte-stable across independent builds (the prime directive).
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
		t.Fatal("openapi-labeled graph is not byte-identical across two builds")
	}

	// Golden (regenerate-and-diff): the committed graph is exactly Build(...).Marshal()
	// — tool-free like every committed graph golden — so a schema/label shift shows up
	// as a reviewable diff. Rebase with `go test ./internal/static/graphio -update`.
	assertGolden(t, filepath.Join("testdata", "oapiclientsvc.openapi.graph.json"), string(b1))
}
