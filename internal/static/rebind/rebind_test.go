package rebind_test

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/callgraph"
	"github.com/jyang234/golang-code-graph/internal/static/graphio"
	"github.com/jyang234/golang-code-graph/internal/static/rebind"
)

func analyzeFixture(t *testing.T, name string) *analyze.Result {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", name)
	res, err := analyze.Analyze(dir, callgraph.Options{Algo: callgraph.AlgoVTA})
	if err != nil {
		t.Fatalf("analyze %s: %v", name, err)
	}
	return res
}

// reaches reports whether the forward reach of fqn in g crosses a boundary effect whose
// label contains marker.
func reaches(g *graphio.Graph, fqn, marker string) bool {
	seen := map[string]bool{fqn: true}
	for changed := true; changed; {
		changed = false
		for _, e := range g.Edges {
			if seen[e.From] && !seen[e.To] {
				seen[e.To] = true
				changed = true
			}
		}
	}
	for to := range seen {
		if strings.Contains(to, marker) {
			return true
		}
	}
	return false
}

const (
	cmdA = "(*example.com/unionsvc.CmdA).Handle"
	cmdB = "(*example.com/unionsvc.CmdB).Handle"
	leak = "(*example.com/unionsvc.LeakCmd).Handle"
	hub  = "example.com/unionsvc.invoke"
)

// Before --rebind, the context-insensitive runner unions every closure: CmdA appears to
// issue CmdB's write. After de-union each command reaches ONLY its own write — the
// over-report is removed in the precision-improving direction.
func TestRebindDeUnionsConfinedClosures(t *testing.T) {
	res := analyzeFixture(t, "unionsvc")
	g, err := graphio.Build(res, "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// Precondition: the union is present — CmdA reaches BOTH writes.
	if !reaches(g, cmdA, "table_a") || !reaches(g, cmdA, "table_b") {
		t.Fatalf("precondition: CmdA should reach the unioned table_a AND table_b before rebind")
	}

	added, removed := graphio.ApplyRebind(g, res)
	if added == 0 || removed == 0 {
		t.Fatalf("rebind must add and remove edges, got +%d -%d", added, removed)
	}
	taggedFound := false
	for _, e := range g.Edges {
		if e.Via == rebind.Via {
			taggedFound = true
		}
	}
	if !taggedFound {
		t.Error("rebound edges must carry their via=rebind provenance")
	}

	// After de-union: each confined command reaches only its OWN write.
	if !reaches(g, cmdA, "table_a") {
		t.Error("CmdA must still reach its own write table_a after rebind")
	}
	if reaches(g, cmdA, "table_b") {
		t.Error("CmdA must NOT reach CmdB's write table_b after de-union (the over-report removed)")
	}
	if !reaches(g, cmdB, "table_b") || reaches(g, cmdB, "table_a") {
		t.Error("CmdB must reach only its own write table_b after de-union")
	}
}

// The SOUNDNESS guard: LeakCmd's closure ESCAPES (passed to RunInTx AND the helper
// invoke), so its union must be KEPT. The adversarial regression is the gate flip: a
// must_not_reach(invoke → table_leak) gate measured VIOLATED→false-PASS when an early
// per-closure de-union wrongly dropped the escaped closure. Here the helper invoke MUST
// still reach table_leak after rebind, so the gate stays VIOLATED.
func TestRebindKeepsEscapedClosureUnion(t *testing.T) {
	res := analyzeFixture(t, "unionsvc")

	// The plan must not touch the escaped closure (no add, no remove naming LeakCmd).
	plan := rebind.Compute(res)
	for _, e := range plan.Add {
		if strings.Contains(e.From, "LeakCmd") || strings.Contains(e.To, "LeakCmd") {
			t.Errorf("escaped closure was de-unioned (add): %s -> %s", e.From, e.To)
		}
	}
	for _, r := range plan.Remove {
		if strings.Contains(r[1], "LeakCmd") {
			t.Errorf("escaped closure's union edge was removed: %s -> %s", r[0], r[1])
		}
	}

	g, err := graphio.Build(res, "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !reaches(g, hub, "table_leak") {
		t.Fatalf("precondition: the helper invoke should reach table_leak before rebind")
	}
	graphio.ApplyRebind(g, res)
	// The gate-flip repair: the helper still reaches the escaped write, so a
	// must_not_reach(invoke → table_leak) stays VIOLATED (no false absence).
	if !reaches(g, hub, "table_leak") {
		t.Error("after rebind the helper invoke must STILL reach table_leak — dropping it is a false absence (gate flip)")
	}
	// LeakCmd itself still reaches its write.
	if !reaches(g, leak, "table_leak") {
		t.Error("LeakCmd must still reach its own write")
	}
}

// Across every escape MODE — stored, returned, channel-sent, captured — the closure is
// not confined, so the pass abstains: the plan over escapemodessvc is EMPTY (no add, no
// remove). This is the conservatively-complete confinement guard exercised mode by mode.
func TestRebindAbstainsOnEveryEscapeMode(t *testing.T) {
	plan := rebind.Compute(analyzeFixture(t, "escapemodessvc"))
	if len(plan.Add) != 0 || len(plan.Remove) != 0 {
		t.Errorf("every closure here escapes; the de-union plan must be empty, got add=%v remove=%v", plan.Add, plan.Remove)
	}
}

// The plan and the applied graph must be byte-identical across repeated builds — the
// de-union is a pure function of the SSA (CLAUDE.md determinism). A non-deterministic
// add/remove order or a map-iteration leak would surface here.
func TestRebindDeterministic(t *testing.T) {
	want := rebind.Compute(analyzeFixture(t, "unionsvc"))
	for i := 0; i < 20; i++ {
		got := rebind.Compute(analyzeFixture(t, "unionsvc"))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("rebind plan not deterministic across runs:\n want %+v\n got  %+v", want, got)
		}
	}
}

// Opt-in / not-default: ApplyRebind is the only graph mutation that removes edges, so the
// default Build must leave the union intact (a committed-golden-preserving invariant).
func TestRebindIsOptInOnly(t *testing.T) {
	res := analyzeFixture(t, "unionsvc")
	g, err := graphio.Build(res, "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, e := range g.Edges {
		if e.Via == rebind.Via {
			t.Errorf("a default Build must not carry rebound edges: %+v", e)
		}
	}
	// The union edge is still present by default (CmdA over-reports until --rebind).
	if !reaches(g, cmdA, "table_b") {
		t.Error("default Build must keep the union (CmdA reaches table_b) — de-union is opt-in")
	}
}
