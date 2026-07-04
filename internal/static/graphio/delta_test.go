package graphio

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/canonjson"
	"github.com/jyang234/golang-code-graph/internal/render"
)

// TestDiffAttributeDriftGolden pins the attribute-aware diff over a hand-authored pair
// that exercises every class in one artifact: a node added (New) and removed (Old), a node
// whose TIER drifts (Service 2→1), an edge added and removed, an edge that became CONCURRENT
// (Handler→Service), a MULTIPLICITY change (Service→Repo lost its concurrent record while
// the plain one stayed), and a two-field boundary edge change (tier + via). Both renderings
// of the ONE comparison are locked: the mermaid `--diff` view and the JSON GraphDelta.
func TestDiffAttributeDriftGolden(t *testing.T) {
	base := loadGraph(t, "testdata/attrdrift.base.graph.json")
	branch := loadGraph(t, "testdata/attrdrift.branch.graph.json")

	md := render.Fence(MermaidDiff(base, branch, MermaidOptions{MaxTier: 2}))
	assertValidMermaid(t, md)
	assertGolden(t, "testdata/attrdrift.callgraph-diff.md", md)

	jb, err := canonjson.Marshal(Delta(base, branch))
	if err != nil {
		t.Fatalf("marshal delta: %v", err)
	}
	assertGolden(t, "testdata/attrdrift.delta.json", string(jb))
}

// TestDiffMermaidJSONParity is the one-source-of-truth guard: the mermaid `changed`
// classification is DERIVED from the same Delta the JSON emits, so the two must agree on
// exactly which pairs and nodes changed. A future refactor that re-derives one side
// independently (the fork this phase deliberately avoids) shows up here as a count mismatch.
func TestDiffMermaidJSONParity(t *testing.T) {
	base := loadGraph(t, "testdata/attrdrift.base.graph.json")
	branch := loadGraph(t, "testdata/attrdrift.branch.graph.json")
	d := Delta(base, branch)
	// MaxTier 0: draw everything, so no tier-hide can drop a changed element and skew a count.
	out := MermaidDiff(base, branch, MermaidOptions{MaxTier: 0})

	// Distinct changed PAIRS in the JSON (a pair with two changed fields is still one pair).
	pairs := map[ekey]bool{}
	for _, ec := range d.EdgesChanged {
		pairs[ekey{ec.From, ec.To}] = true
	}
	// Each changed edge renders exactly one "-->|Δ …|" line; the token is unique to them
	// (added use ==>|, removed use -.->|, kept use plain decorations — none begin "|Δ ").
	if got, want := strings.Count(out, "-->|Δ "), len(pairs); got != want {
		t.Errorf("mermaid renders %d changed edges, JSON delta reports %d changed pairs:\n%s", got, want, out)
	}

	// Changed NODE declarations carry :::changed; exclude the always-present legend entry.
	changedNodes := 0
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, ":::changed") && !strings.Contains(ln, "lg_changed") {
			changedNodes++
		}
	}
	if changedNodes != len(d.NodesChanged) {
		t.Errorf("mermaid renders %d changed nodes, JSON delta reports %d:\n%s", changedNodes, len(d.NodesChanged), out)
	}

	// Added/removed/changed are disjoint classes: a pair the delta calls changed must not
	// also appear as added or removed (that would be a double-count / contradictory verdict).
	addedRemoved := map[ekey]bool{}
	for _, e := range d.EdgesAdded {
		addedRemoved[ekey{e.From, e.To}] = true
	}
	for _, e := range d.EdgesRemoved {
		addedRemoved[ekey{e.From, e.To}] = true
	}
	for k := range pairs {
		if addedRemoved[k] {
			t.Errorf("pair %v is both changed and added/removed — classes must be disjoint", k)
		}
	}
}

// TestDeltaDeterministic pins byte-identical output across repeated runs (Go randomizes map
// iteration per run) AND across input reordering — the determinism the whole gating model
// rests on. The canon fuzz-corpus discipline, sized to fit.
func TestDeltaDeterministic(t *testing.T) {
	base := loadGraph(t, "testdata/attrdrift.base.graph.json")
	branch := loadGraph(t, "testdata/attrdrift.branch.graph.json")
	want, err := canonjson.Marshal(Delta(base, branch))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Repeated runs must be byte-identical despite per-run map-iteration shuffling.
	for i := 0; i < 6; i++ {
		got, _ := canonjson.Marshal(Delta(base, branch))
		if string(got) != string(want) {
			t.Fatalf("Delta not deterministic on run %d", i)
		}
	}
	// And invariant under input order: reversing nodes/edges on both sides must not move a byte.
	if got, _ := canonjson.Marshal(Delta(reversed(base), reversed(branch))); string(got) != string(want) {
		t.Errorf("Delta not invariant under input reordering:\n want %s\n  got %s", want, got)
	}
	// The mermaid view derived from it is likewise stable across reordering.
	m1 := MermaidDiff(base, branch, MermaidOptions{MaxTier: 2})
	m2 := MermaidDiff(reversed(base), reversed(branch), MermaidOptions{MaxTier: 2})
	if m1 != m2 {
		t.Errorf("MermaidDiff not invariant under input reordering")
	}
}

func reversed(g *Graph) *Graph {
	out := *g
	out.Nodes = append([]Node{}, g.Nodes...)
	out.Edges = append([]Edge{}, g.Edges...)
	for i, j := 0, len(out.Nodes)-1; i < j; i, j = i+1, j-1 {
		out.Nodes[i], out.Nodes[j] = out.Nodes[j], out.Nodes[i]
	}
	for i, j := 0, len(out.Edges)-1; i < j; i, j = i+1, j-1 {
		out.Edges[i], out.Edges[j] = out.Edges[j], out.Edges[i]
	}
	return &out
}

// TestDiffOverCapDisclosesChanged proves the over-cap summary never SILENTLY drops the third
// class: a changed-heavy delta that blows the node cap must still report the changed count,
// so truncation discloses rather than hides an attribute drift.
func TestDiffOverCapDisclosesChanged(t *testing.T) {
	base := &Graph{Algo: "rta"}
	branch := &Graph{Algo: "rta"}
	for i := 0; i < 300; i++ {
		fqn := fmt.Sprintf("example.com/big.F%03d", i)
		base.Nodes = append(base.Nodes, Node{FQN: fqn, Tier: 2})
		branch.Nodes = append(branch.Nodes, Node{FQN: fqn, Tier: 1}) // every node's tier drifts
	}
	out := MermaidDiff(base, branch, MermaidOptions{MaxNodes: 50})
	assertValidMermaid(t, out)
	if !strings.Contains(out, "300 changed") {
		t.Errorf("over-cap summary must disclose the exact changed count (300):\n%s", out)
	}
}

// TestDeltaMultiplicityAndCounting pins the two subtle counting rules a set-based diff got
// wrong: (1) a pair is compared on its whole RECORD SET, so losing one of two records is a
// change a single-record comparison would miss; (2) added/removed pairs count once over
// UNIQUE (from,to), regardless of how many records the pair carried.
func TestDeltaMultiplicityAndCounting(t *testing.T) {
	// (1) {plain, concurrent} → {plain}: the concurrent record was dropped.
	base := &Graph{Algo: "rta",
		Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}},
		Edges: []Edge{{From: "a.A", To: "a.B"}, {From: "a.A", To: "a.B", Concurrent: true}},
	}
	branch := &Graph{Algo: "rta",
		Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}},
		Edges: []Edge{{From: "a.A", To: "a.B"}},
	}
	d := Delta(base, branch)
	if len(d.EdgesAdded)+len(d.EdgesRemoved) != 0 {
		t.Fatalf("a multiplicity change is not an add/remove: %+v", d)
	}
	if len(d.EdgesChanged) != 1 || d.EdgesChanged[0].Field != "concurrent" ||
		d.EdgesChanged[0].Old != true || d.EdgesChanged[0].New != nil {
		t.Fatalf("multiplicity drop must be concurrent true→null, got %+v", d.EdgesChanged)
	}

	// (2) a pair present on ONE side with two records is one removed pair, not two.
	base2 := &Graph{Algo: "rta",
		Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}},
		Edges: []Edge{{From: "a.A", To: "a.B"}, {From: "a.A", To: "a.B", Concurrent: true}},
	}
	branch2 := &Graph{Algo: "rta", Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}}}
	d2 := Delta(base2, branch2)
	if len(d2.EdgesRemoved) != 1 {
		t.Errorf("a two-record pair present only in base is ONE removed pair, got %d: %+v", len(d2.EdgesRemoved), d2.EdgesRemoved)
	}
}

// TestDeltaProvenanceCaveats confirms the delta reuses the mermaid diff's substrate-skew
// disclosure verbatim (one source of truth) and echoes each side's provenance structurally,
// so a cross-algo delta can never read a precision difference as a code change.
func TestDeltaProvenanceCaveats(t *testing.T) {
	base := &Graph{Algo: "rta", Tool: "flowmap-1", Nodes: []Node{{FQN: "a.F", Tier: 1}}}
	branch := &Graph{Algo: "vta", Tool: "flowmap-2", Nodes: []Node{{FQN: "a.F", Tier: 1}}}
	d := Delta(base, branch)
	if d.Base.Algo != "rta" || d.Branch.Algo != "vta" || d.Base.Tool != "flowmap-1" || d.Branch.Tool != "flowmap-2" {
		t.Errorf("delta must echo each side's provenance: %+v", d)
	}
	joined := strings.Join(d.Caveats, "\n")
	if !strings.Contains(joined, "algo differs") || !strings.Contains(joined, "producer tool differs") {
		t.Errorf("cross-substrate delta must disclose the skew caveats, got %v", d.Caveats)
	}
	// An empty base is disclosed, not refused (a new service is a legitimate all-added delta).
	if empty := Delta(&Graph{}, branch); len(empty.Caveats) == 0 || !strings.Contains(empty.Caveats[0], "base graph is empty") {
		t.Errorf("empty-base delta must disclose the all-added reading, got %v", empty.Caveats)
	}
}
