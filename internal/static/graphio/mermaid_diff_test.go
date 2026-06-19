package graphio

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/render"
)

// TestMermaidDiffGolden locks the rewire diff (branch-good → branch-skip) as a
// committed, fenced view, so a renderer change to the diff path shows up as a
// reviewable .md diff exactly like the per-graph callgraph goldens. Rebased by
// -update (shared with the call-graph golden harness) and by regen.sh.
func TestMermaidDiffGolden(t *testing.T) {
	base := loadGraph(t, "../../../testdata/groundwork/goldens/layeredsvc.branch-good.graph.json")
	branch := loadGraph(t, "../../../testdata/groundwork/goldens/layeredsvc.branch-skip.graph.json")
	got := render.Fence(MermaidDiff(base, branch, MermaidOptions{MaxTier: 2}))

	mdPath := "../../../testdata/groundwork/goldens/layeredsvc.rewire.callgraph-diff.md"
	if *updateGolden {
		if err := os.WriteFile(mdPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("missing golden %s; run with -update: %v", mdPath, err)
	}
	if string(want) != got {
		t.Errorf("%s is stale (run -update to rebase):\n%s", mdPath, firstDiff(string(want), got))
	}
}

func loadGraph(t *testing.T, path string) *Graph {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var g Graph
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return &g
}

// TestMermaidDiffRewire is the headline case the design must get right: a function
// (GetUserFast) that PERSISTS while one of its calls is rewired — the edge to the
// app layer removed, a new edge to the store layer added. The persisting node and
// its still-referenced targets must read as unchanged (kept), never as removed, so
// a reviewer can tell "lost a call" from "was deleted".
func TestMermaidDiffRewire(t *testing.T) {
	base := loadGraph(t, "../../../testdata/groundwork/goldens/layeredsvc.branch-good.graph.json")
	branch := loadGraph(t, "../../../testdata/groundwork/goldens/layeredsvc.branch-skip.graph.json")

	out := MermaidDiff(base, branch, MermaidOptions{MaxTier: 2})
	t.Logf("\n%s", out) // surfaced with `go test -run TestMermaidDiffRewire -v`

	mustContain := []string{
		"flowchart LR",
		`subgraph legend`,
		// the rewired caller persists — neutral kept class, no +/− prefix:
		`handler_Server_GetUserFast["handler.Server.GetUserFast"]:::kept`,
		// the new call to the store layer is an added (thick) edge:
		` ==>|`,
		// the dropped call to the app layer is a removed (dotted, labeled) edge:
		`-.->|− removed|`,
		// colored linkStyles for both directions of change:
		"stroke:#1a9d1a", // added green
		"stroke:#cc3333", // removed red
	}
	for _, w := range mustContain {
		if !strings.Contains(out, w) {
			t.Errorf("diff output missing %q\n--- full ---\n%s", w, out)
		}
	}

	// The persisting targets must NOT be styled as removed nodes: GetProfile is
	// still called elsewhere on the branch, so it stays kept even though the edge
	// to it was removed. This is the exact ambiguity the encoding must avoid.
	if strings.Contains(out, `app.Service.GetProfile"]:::removed`) ||
		strings.Contains(out, `GetProfile"]:::removed`) {
		t.Errorf("GetProfile persists on the branch; it must not be rendered as a removed node:\n%s", out)
	}
}

// TestMermaidDiffAddedEffect proves a NEW boundary effect (the review-critical case
// — a PR that adds a DB write) surfaces as an added, recolored effect node while
// keeping its shape.
func TestMermaidDiffAddedEffect(t *testing.T) {
	base := &Graph{
		Nodes: []Node{{FQN: "example.com/s/h.Handle", Tier: 1}},
	}
	branch := &Graph{
		Nodes: []Node{{FQN: "example.com/s/h.Handle", Tier: 1}},
		Edges: []Edge{{From: "example.com/s/h.Handle", To: "boundary:db DELETE accounts", Tier: 1, Boundary: "outbound-sync"}},
	}
	out := MermaidDiff(base, branch, MermaidOptions{MaxTier: 2})

	if !strings.Contains(out, `[("＋ db DELETE accounts")]:::added`) {
		t.Errorf("a newly-added DB effect must render as an added cylinder:\n%s", out)
	}
	// The caller is unchanged and must read as kept, not as part of the delta.
	if !strings.Contains(out, `h.Handle"]:::kept`) {
		t.Errorf("unchanged caller should be kept:\n%s", out)
	}
}

func TestMermaidDiffDeterministic(t *testing.T) {
	base := loadGraph(t, "../../../testdata/groundwork/goldens/layeredsvc.branch-good.graph.json")
	branch := loadGraph(t, "../../../testdata/groundwork/goldens/layeredsvc.branch-skip.graph.json")
	first := MermaidDiff(base, branch, MermaidOptions{MaxTier: 2})
	for i := 0; i < 6; i++ {
		if got := MermaidDiff(base, branch, MermaidOptions{MaxTier: 2}); got != first {
			t.Fatalf("MermaidDiff not deterministic on run %d", i)
		}
	}
}
