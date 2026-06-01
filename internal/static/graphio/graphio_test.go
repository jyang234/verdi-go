package graphio_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/graphio"
	"github.com/jyang234/golang-code-graph/internal/static/statictest"
)

func analyzeFixture(t *testing.T) *analyze.Result {
	t.Helper()
	res, err := statictest.Analyze()
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	return res
}

// TestGraphIncludesDBEdges is the complement of the boundary contract's DB
// exclusion: the non-gated graph DOES show DB operations.
func TestGraphIncludesDBEdges(t *testing.T) {
	g, err := graphio.Build(analyzeFixture(t), "")
	if err != nil {
		t.Fatal(err)
	}
	var dbEdges []string
	for _, e := range g.Edges {
		if strings.HasPrefix(e.To, "boundary:db ") {
			dbEdges = append(dbEdges, e.To)
		}
	}
	if len(dbEdges) == 0 {
		t.Fatal("graph view should include DB boundary edges")
	}
	// The SQL op and table should be extracted, e.g. "boundary:db SELECT applicants".
	var sawTable bool
	for _, e := range dbEdges {
		if strings.Contains(e, "applicants") || strings.Contains(e, "ledger") {
			sawTable = true
		}
	}
	if !sawTable {
		t.Errorf("DB edges did not resolve a table: %v", dbEdges)
	}
}

func TestGraphHasFirstPartyNodesWithSignatures(t *testing.T) {
	g, err := graphio.Build(analyzeFixture(t), "")
	if err != nil {
		t.Fatal(err)
	}
	byFQN := map[string]graphio.Node{}
	for _, n := range g.Nodes {
		byFQN[n.FQN] = n
	}
	create, ok := byFQN["(*example.com/loansvc/internal/handler.App).Create"]
	if !ok {
		t.Fatal("handler.App.Create node missing")
	}
	if !strings.Contains(create.Sig, "ResponseWriter") {
		t.Errorf("Create node signature looks wrong: %q", create.Sig)
	}
	if create.Tier != 1 {
		t.Errorf("Create (an entry handler) tier = %d, want 1", create.Tier)
	}
}

// TestEntryScoping checks --entry narrows the graph: the POST flow must exclude a
// function reachable only from the consumer.
func TestEntryScoping(t *testing.T) {
	res := analyzeFixture(t)
	full, err := graphio.Build(res, "")
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := graphio.Build(res, "POST /loan-application")
	if err != nil {
		t.Fatal(err)
	}
	if len(scoped.Nodes) >= len(full.Nodes) {
		t.Errorf("scoped graph (%d nodes) should be smaller than full (%d)", len(scoped.Nodes), len(full.Nodes))
	}
	if scoped.Entrypoint != "POST /loan-application" {
		t.Errorf("entrypoint = %q", scoped.Entrypoint)
	}
	for _, n := range scoped.Nodes {
		if strings.Contains(n.FQN, "MarkPaid") {
			t.Error("MarkPaid (reached only via the consumer) leaked into the POST scope")
		}
	}
}

func TestEntryNotFound(t *testing.T) {
	_, err := graphio.Build(analyzeFixture(t), "DELETE /nonexistent")
	if err == nil {
		t.Fatal("expected an error for an unknown entry point")
	}
}

func TestGraphDeterministic(t *testing.T) {
	res := analyzeFixture(t)
	g1, err := graphio.Build(res, "")
	if err != nil {
		t.Fatal(err)
	}
	g2, err := graphio.Build(res, "")
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := g1.Marshal()
	b2, _ := g2.Marshal()
	if !bytes.Equal(b1, b2) {
		t.Error("graph view is not deterministic across builds")
	}
}
