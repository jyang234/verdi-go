package graphio_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/graphio"
)

func passthroughFixture(t *testing.T) *analyze.Result {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", "sqlpassthroughsvc")
	res, err := analyze.Analyze(dir)
	if err != nil {
		t.Fatalf("analyze sqlpassthroughsvc: %v", err)
	}
	return res
}

// dbEdges returns the set of (From, "db …" label) pairs and the Via on each, so a
// test can assert WHICH node a DB effect is attributed to — the whole point of the
// §19 re-attribution.
type dbEdge struct {
	from  string
	label string
	via   string
}

func dbEdges(g *graphio.Graph) []dbEdge {
	var out []dbEdge
	for _, e := range g.Edges {
		if label, ok := cutDBLabel(e.To); ok {
			out = append(out, dbEdge{from: e.From, label: label, via: e.Via})
		}
	}
	return out
}

func cutDBLabel(to string) (string, bool) {
	const p = "boundary:db "
	if len(to) > len(p) && to[:len(p)] == p {
		return to[len(p):], true
	}
	return "", false
}

func hasEdge(edges []dbEdge, fromContains, label, via string) bool {
	for _, e := range edges {
		if e.label == label && e.via == via && contains(e.from, fromContains) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Without --reclaim-sql the helper's sink stays opaque and is attributed to the
// HELPER, not the callers — the default build is unchanged.
func TestPassthroughOffByDefault(t *testing.T) {
	g, err := graphio.Build(passthroughFixture(t), "")
	if err != nil {
		t.Fatal(err)
	}
	edges := dbEdges(g)
	if !hasEdge(edges, "execByID", "ExecContext", "") {
		t.Errorf("default build must leave the opaque sink at the helper execByID; got %+v", edges)
	}
	// No caller should carry a re-attributed classified DB write by default.
	for _, e := range edges {
		if e.via == "sql-passthrough" {
			t.Errorf("default build must carry no passthrough provenance; got %+v", e)
		}
	}
}

// With --reclaim-sql the helper's opaque sink is dropped and each caller carries its
// own recovered verb (Tier B), the finite-table caller fans out (Tier C), and the
// dynamic-verb caller keeps an opaque effect re-homed at it (soundness).
func TestPassthroughReattributesPerCaller(t *testing.T) {
	g, err := graphio.Build(passthroughFixture(t), "", graphio.WithSQLFold())
	if err != nil {
		t.Fatal(err)
	}
	edges := dbEdges(g)

	// Tier B: per-caller verbs, all tagged sql-passthrough.
	for _, want := range []struct{ from, label string }{
		{"DeleteEventType", "DELETE event_types"},
		{"InsertEventTypeVersion", "INSERT event_type_versions"},
		{"UpdateEventTypeVersion", "UPDATE event_type_versions"},
	} {
		if !hasEdge(edges, want.from, want.label, "sql-passthrough") {
			t.Errorf("Tier B: want %s -> db %s (via sql-passthrough); got %+v", want.from, want.label, edges)
		}
	}

	// Tier C: the finite-constant table set fans out into one edge per target, at
	// the caller (resolution composes with the helper hop).
	for _, label := range []string{"DELETE publishers", "DELETE subscribers"} {
		if !hasEdge(edges, "DeleteParticipant", label, "sql-passthrough") {
			t.Errorf("Tier C: want DeleteParticipant -> db %s (via sql-passthrough); got %+v", label, edges)
		}
	}

	// Soundness: the dynamic-verb caller keeps an opaque effect re-homed at IT — the
	// effect is preserved, never dropped.
	if !hasEdge(edges, "ExecRaw", "ExecContext", "sql-passthrough") {
		t.Errorf("soundness: an unrecoverable forwarded statement must re-home opaque at the caller; got %+v", edges)
	}

	// The helper's own sink edge must be gone (every caller now carries the effect).
	for _, e := range edges {
		if contains(e.from, "execByID") {
			t.Errorf("the pass-through helper's sink edge must be re-attributed away; got %+v", e)
		}
	}
}

// Per-caller fail closed (§19): a caller that invokes the helper indirectly (its arg
// slot is unmappable) re-homes only its OWN effect as opaque, and must not collapse
// re-attribution for the directly-resolved callers. Under the old whole-helper abort
// this indirect call left every sibling's verb unrecovered.
func TestPassthroughPerCallerFailClosed(t *testing.T) {
	g, err := graphio.Build(passthroughFixture(t), "", graphio.WithSQLFold())
	if err != nil {
		t.Fatal(err)
	}
	edges := dbEdges(g)
	// The indirect caller's effect is preserved opaque, homed at the caller.
	if !hasEdge(edges, "IndirectCaller", "ExecContext", "sql-passthrough") {
		t.Errorf("indirect caller must re-home opaque at itself; got %+v", edges)
	}
	// A direct sibling is still classified despite the indirect caller existing.
	if !hasEdge(edges, "DeleteEventType", "DELETE event_types", "sql-passthrough") {
		t.Errorf("a direct caller must stay classified despite an unmappable sibling; got %+v", edges)
	}
	// And the helper edge is still fully re-attributed away (no out-of-scope caller).
	for _, e := range edges {
		if contains(e.from, "execByID") {
			t.Errorf("helper sink edge must be re-attributed away; got %+v", e)
		}
	}
}

// Determinism: the re-attributed build is a pure function of the SSA — byte-identical
// across repeated builds (CLAUDE.md determinism-test rule for a new label path).
func TestPassthroughDeterministic(t *testing.T) {
	res := passthroughFixture(t)
	first, err := graphio.Build(res, "", graphio.WithSQLFold())
	if err != nil {
		t.Fatal(err)
	}
	a, err := first.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		g, err := graphio.Build(res, "", graphio.WithSQLFold())
		if err != nil {
			t.Fatal(err)
		}
		b, err := g.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Fatalf("re-attributed build is not byte-identical across runs (iteration %d)", i)
		}
	}
}
