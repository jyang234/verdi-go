package graphio

import (
	"strings"
	"testing"
)

// duplicateFQNGraph mirrors the shape testdata/fixtures/localtypeargsvc produces: a
// generic instantiated at three FUNCTION-LOCAL types collapses to ONE display FQN, so
// the graph legally carries three byte-identical node records for it (the local generic
// type identity design bars deduping them in sortGraph/serialization — every instance
// must survive). The package therefore declares 5 functions but emits 7 node records.
func duplicateFQNGraph() *Graph {
	const pkg = "example.com/dupsvc"
	const dup = pkg + ".report[" + pkg + ".result]"
	inst := Node{
		FQN: dup, Sig: "func main.report[T any](v T) string", Tier: 3,
		Package: pkg, File: "main.go", Line: 5, EndLine: 7,
	}
	fn := func(name string, tier, line int) Node {
		return Node{
			FQN: pkg + "." + name, Sig: "func main." + name + "() string", Tier: tier,
			Package: pkg, File: "main.go", Line: line, EndLine: line,
		}
	}
	return &Graph{
		Algo: "vta",
		Nodes: []Node{
			fn("alpha", 3, 9),
			fn("beta", 3, 10),
			fn("gamma", 3, 11),
			fn("main", 1, 13),
			inst, inst, inst,
		},
		Edges: []Edge{
			{From: pkg + ".alpha", To: dup, Tier: 3},
			{From: pkg + ".beta", To: dup, Tier: 3},
			{From: pkg + ".gamma", To: dup, Tier: 3},
			{From: pkg + ".main", To: pkg + ".alpha", Tier: 3},
			{From: pkg + ".main", To: pkg + ".beta", Tier: 3},
			{From: pkg + ".main", To: pkg + ".gamma", Tier: 3},
		},
	}
}

// declaredNodeIDs returns each node id declared by a rendered flowchart, in emission
// order and WITH repeats, so a test can prove a duplicate declaration rather than only
// that the set of ids is right.
func declaredNodeIDs(diagram string) []string {
	var out []string
	for _, ln := range strings.Split(diagram, "\n") {
		trimmed := strings.TrimSpace(ln)
		switch {
		case trimmed == "", strings.HasPrefix(trimmed, "%%"),
			strings.HasPrefix(trimmed, "classDef "), strings.HasPrefix(trimmed, "linkStyle "),
			strings.HasPrefix(trimmed, "subgraph "), strings.HasPrefix(trimmed, "flowchart"),
			trimmed == "direction LR", trimmed == "end":
			continue
		case strings.Contains(trimmed, "-->"), strings.Contains(trimmed, "==>"),
			strings.Contains(trimmed, ".->"):
			continue
		}
		if id := leadingID(trimmed); id != "" && strings.Contains(trimmed, `"`) {
			out = append(out, id)
		}
	}
	return out
}

// TestMermaidDeclaresDuplicateFQNRecordsOnce pins the render against the duplicate node
// RECORD the graph is required to keep: the diagram draws one box per FQN, so three
// records for one display FQN must still produce exactly ONE declaration. A repeated
// declaration is not a second box — Mermaid collapses it — so the render would silently
// disagree with the graph it claims to draw.
func TestMermaidDeclaresDuplicateFQNRecordsOnce(t *testing.T) {
	g := duplicateFQNGraph()
	out := g.Mermaid(MermaidOptions{MaxTier: 0}) // 0 = draw everything, nothing tier-hidden
	assertValidMermaid(t, out)

	seen := map[string]int{}
	for _, id := range declaredNodeIDs(out) {
		seen[id]++
	}
	if len(seen) != 5 {
		t.Errorf("declared %d distinct node ids, want 5 (the package's 5 functions)\n%s", len(seen), out)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("node id %q declared %d times, want exactly 1\n%s", id, n, out)
		}
	}
}

// TestMermaidHiddenPlumbingCountsDistinctFunctions pins the hidden-plumbing note against
// the same duplicate records: the note is a number a human acts on ("how much did the
// default view collapse"), and what the render collapses is FUNCTIONS, not records. With
// three records behind one hidden FQN, counting records would claim 6 hidden for the 4
// functions actually folded away.
func TestMermaidHiddenPlumbingCountsDistinctFunctions(t *testing.T) {
	g := duplicateFQNGraph()
	out := g.Mermaid(MermaidOptions{MaxTier: 2}) // alpha/beta/gamma/report are tier 3
	assertValidMermaid(t, out)

	const want = "%% 4 first-party nodes above tier 2 hidden as plumbing"
	if !strings.Contains(out, want) {
		t.Errorf("hidden-plumbing note must count distinct hidden functions (%q)\n%s", want, out)
	}
}

// TestRollupCountsDistinctFunctionsPerPackage pins the component rollup against the same
// duplicate records: `nodes` is read as "this package declares N functions", so it counts
// distinct FQNs. Counting records reports 7 for a package that declares 5.
func TestRollupCountsDistinctFunctionsPerPackage(t *testing.T) {
	r := duplicateFQNGraph().RollupByPackage()
	if len(r.Components) != 1 {
		t.Fatalf("rollup produced %d components, want 1", len(r.Components))
	}
	if got := r.Components[0].Nodes; got != 5 {
		t.Errorf("component nodes = %d, want 5 (distinct FQNs; 7 records collapse to 5 functions)", got)
	}
}
