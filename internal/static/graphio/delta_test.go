package graphio

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/canonjson"
	"github.com/jyang234/golang-code-graph/internal/render"
)

// TestAttrTupleMirrorsEdgeIdentity pins the parity between attrTuple (delta.go) and Edge:
// record identity for a (from,to) pair is the FULL attribute tuple (Phase 0), so attrTuple
// must carry EVERY Edge field except the pair key (From,To). If a new identity-bearing Edge
// field is added and not folded into attrTuple, pairTuples would silently merge two distinct
// records and a multiplicity change would read as a false "unchanged" — the fail-open this
// phase closes. Fails loudly on drift, the test half of the one-source-of-truth rule.
func TestAttrTupleMirrorsEdgeIdentity(t *testing.T) {
	edgeAttrs := map[string]bool{}
	et := reflect.TypeOf(Edge{})
	for i := 0; i < et.NumField(); i++ {
		if n := et.Field(i).Name; n != "From" && n != "To" { // From,To are the pair KEY, not attributes
			edgeAttrs[strings.ToLower(n)] = true
		}
	}
	tupleAttrs := map[string]bool{}
	tt := reflect.TypeOf(attrTuple{})
	for i := 0; i < tt.NumField(); i++ {
		tupleAttrs[strings.ToLower(tt.Field(i).Name)] = true
	}
	if !reflect.DeepEqual(edgeAttrs, tupleAttrs) {
		t.Fatalf("attrTuple fields %v must mirror Edge's non-key attribute fields %v exactly — a new "+
			"Edge field must be folded into attrTuple (delta.go) or pairTuples silently merges records", tupleAttrs, edgeAttrs)
	}
}

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
// independently (the fork this phase deliberately avoids) shows up here as a count OR a
// per-label (identity) mismatch — the label multiset closes the gap where a spurious pair
// and a missed pair of equal count would net out.
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

	// IDENTITY, not just cardinality: the multiset of Δ EDGE labels the mermaid draws must
	// equal the multiset the JSON delta implies, so a spurious pair + a missed pair of equal
	// count cannot pass. Each label is edgeDeltaLabel(a pair's changes), escaped as the
	// renderer escapes it.
	byPair := map[ekey][]EdgeChange{}
	for _, ec := range d.EdgesChanged {
		k := ekey{ec.From, ec.To}
		byPair[k] = append(byPair[k], ec)
	}
	wantLabels := map[string]int{}
	for _, cs := range byPair {
		wantLabels[changedPrefix+edgeLabelSafe(edgeDeltaLabel(cs))]++
	}
	gotLabels := map[string]int{}
	for _, ln := range strings.Split(out, "\n") {
		if i := strings.Index(ln, "-->|Δ "); i >= 0 {
			rest := ln[i+len("-->|"):]
			if j := strings.Index(rest, "|"); j >= 0 {
				gotLabels[rest[:j]]++
			}
		}
	}
	if !reflect.DeepEqual(wantLabels, gotLabels) {
		t.Errorf("mermaid Δ-edge labels %v disagree with the JSON-derived labels %v", gotLabels, wantLabels)
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
	// The mermaid view derived from it is likewise stable across reordering. attrdrift now
	// carries a KEPT multi-record pair (Handler→Repo, plain+concurrent on both sides), whose
	// rendered decoration was an arrival-order last-writer before edgeIndex became canonical —
	// so this reordering check is a live guard on that fix, not a tautology.
	m1 := MermaidDiff(base, branch, MermaidOptions{MaxTier: 2})
	m2 := MermaidDiff(reversed(base), reversed(branch), MermaidOptions{MaxTier: 2})
	if m1 != m2 {
		t.Errorf("MermaidDiff not invariant under input reordering")
	}

	// The set→sorted-list repr paths are the most order-fragile: a multi-value field (JSON
	// []any) and the `records` fallback (record sets differ but no single field signature
	// does) both build their old/new from a map, so only their internal sort — not map
	// iteration — may decide order. Exercise each explicitly under reordering.
	fragile := []struct {
		name         string
		base, branch *Graph
	}{
		{
			name:   "multi-value via list",
			base:   &Graph{Algo: "rta", Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}}, Edges: []Edge{{From: "a.A", To: "a.B", Tier: 1, Via: "p"}}},
			branch: &Graph{Algo: "rta", Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}}, Edges: []Edge{{From: "a.A", To: "a.B", Tier: 1, Via: "y"}, {From: "a.A", To: "a.B", Tier: 1, Via: "x"}}},
		},
		{
			name:   "records fallback (record sets differ, no single field signature does)",
			base:   &Graph{Algo: "rta", Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}}, Edges: []Edge{{From: "a.A", To: "a.B", Tier: 1}, {From: "a.A", To: "a.B", Tier: 2, Concurrent: true}}},
			branch: &Graph{Algo: "rta", Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}}, Edges: []Edge{{From: "a.A", To: "a.B", Tier: 1, Concurrent: true}, {From: "a.A", To: "a.B", Tier: 2}}},
		},
	}
	for _, tc := range fragile {
		w, _ := canonjson.Marshal(Delta(tc.base, tc.branch))
		if g, _ := canonjson.Marshal(Delta(reversed(tc.base), reversed(tc.branch))); string(g) != string(w) {
			t.Errorf("%s: Delta not invariant under reordering:\n want %s\n  got %s", tc.name, w, g)
		}
		if MermaidDiff(tc.base, tc.branch, MermaidOptions{MaxTier: 2}) != MermaidDiff(reversed(tc.base), reversed(tc.branch), MermaidOptions{MaxTier: 2}) {
			t.Errorf("%s: MermaidDiff not invariant under reordering", tc.name)
		}
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
	// Attribute-changed EDGES too, so the disclosed count exercises the len(pairChanged) term,
	// not only the node-tier term: 150 pairs that became concurrent.
	for i := 0; i < 300; i += 2 {
		from := fmt.Sprintf("example.com/big.F%03d", i)
		to := fmt.Sprintf("example.com/big.F%03d", i+1)
		base.Edges = append(base.Edges, Edge{From: from, To: to, Tier: 1})
		branch.Edges = append(branch.Edges, Edge{From: from, To: to, Tier: 1, Concurrent: true})
	}
	out := MermaidDiff(base, branch, MermaidOptions{MaxNodes: 50})
	assertValidMermaid(t, out)
	// 300 tier-drifted nodes + 150 concurrent-flipped pairs = 450 changed elements disclosed.
	if !strings.Contains(out, "450 changed") {
		t.Errorf("over-cap summary must disclose the exact changed count (300 nodes + 150 edges = 450):\n%s", out)
	}
}

// TestDeltaEdgeAttributeCases pins the three edge-attribute paths the golden's scalar cases
// do not reach: a surviving pair whose boundary drifts, a multi-value (JSON array) field, and
// the `records` soundness fallback (record sets differ but no single field signature does).
func TestDeltaEdgeAttributeCases(t *testing.T) {
	n := []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}}
	changes := func(base, branch *Graph) []EdgeChange { return Delta(base, branch).EdgesChanged }

	// boundary drift on a surviving pair (outbound-sync → outbound-async).
	base := &Graph{Algo: "rta", Nodes: n, Edges: []Edge{{From: "a.A", To: "boundary:bus PUBLISH e", Tier: 1, Boundary: "outbound-sync"}}}
	branch := &Graph{Algo: "rta", Nodes: n, Edges: []Edge{{From: "a.A", To: "boundary:bus PUBLISH e", Tier: 1, Boundary: "outbound-async"}}}
	if cs := changes(base, branch); len(cs) != 1 || cs[0].Field != "boundary" || cs[0].Old != "outbound-sync" || cs[0].New != "outbound-async" {
		t.Errorf("boundary drift must report outbound-sync→outbound-async, got %+v", cs)
	}

	// multi-value via: branch carries two distinct via on one pair → sorted JSON array.
	base = &Graph{Algo: "rta", Nodes: n, Edges: []Edge{{From: "a.A", To: "a.B", Tier: 1, Via: "p"}}}
	branch = &Graph{Algo: "rta", Nodes: n, Edges: []Edge{{From: "a.A", To: "a.B", Tier: 1, Via: "y"}, {From: "a.A", To: "a.B", Tier: 1, Via: "x"}}}
	cs := changes(base, branch)
	if len(cs) != 1 || cs[0].Field != "via" || cs[0].Old != "p" {
		t.Fatalf("multi-via drift must be one via change old=\"p\", got %+v", cs)
	}
	if got, ok := cs[0].New.([]any); !ok || len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Errorf("via new must be the sorted list [x y], got %#v", cs[0].New)
	}
	if m := MermaidDiff(base, branch, MermaidOptions{MaxTier: 2}); !strings.Contains(m, "via p→[x,y]") {
		t.Errorf("mermaid must render the multi-via list p→[x,y]:\n%s", m)
	}

	// records fallback: record sets differ but every per-field signature is equal, so a bare
	// per-field diff would find NOTHING — the fallback must fire so the changed pair is not
	// silently invisible (the soundness guarantee).
	base = &Graph{Algo: "rta", Nodes: n, Edges: []Edge{{From: "a.A", To: "a.B", Tier: 1}, {From: "a.A", To: "a.B", Tier: 2, Concurrent: true}}}
	branch = &Graph{Algo: "rta", Nodes: n, Edges: []Edge{{From: "a.A", To: "a.B", Tier: 1, Concurrent: true}, {From: "a.A", To: "a.B", Tier: 2}}}
	if cs := changes(base, branch); len(cs) != 1 || cs[0].Field != "records" {
		t.Fatalf("a record-set difference with equal field signatures must fall back to a `records` change so the pair is never invisible, got %+v", cs)
	}
	if m := MermaidDiff(base, branch, MermaidOptions{MaxTier: 2}); !strings.Contains(m, "-->|Δ ") {
		t.Errorf("the fallback-changed pair must render as a Δ edge, not silently unchanged:\n%s", m)
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
	// An empty base is disclosed, not refused, AND the whole branch is enumerated as added
	// (the legitimate new-service delta) — not reported as an empty delta with a warning.
	nb := &Graph{Nodes: []Node{{FQN: "a.F", Tier: 1}, {FQN: "a.G", Tier: 1}}, Edges: []Edge{{From: "a.F", To: "a.G", Tier: 1}}}
	empty := Delta(&Graph{}, nb)
	if len(empty.Caveats) == 0 || !strings.Contains(empty.Caveats[0], "base graph is empty") {
		t.Errorf("empty-base delta must disclose the all-added reading, got %v", empty.Caveats)
	}
	if len(empty.NodesAdded) != 2 || len(empty.EdgesAdded) != 1 {
		t.Errorf("empty-base delta must enumerate the whole branch as added (2 nodes, 1 edge), got %d nodes, %d edges", len(empty.NodesAdded), len(empty.EdgesAdded))
	}
	if len(empty.NodesRemoved) != 0 || len(empty.EdgesRemoved) != 0 || len(empty.NodesChanged) != 0 || len(empty.EdgesChanged) != 0 {
		t.Errorf("empty-base delta must have nothing removed or changed, got %+v", empty)
	}
}
