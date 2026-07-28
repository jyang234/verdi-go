package graphio

import (
	"slices"
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

	// Two assertions in one string: the count is per FUNCTION (4, not the 6 records those
	// four functions carry), AND the fold is disclosed rather than left for the reader to
	// discover by differencing this note against the graph.
	const want = "%% 4 first-party nodes (6 instance records) above tier 2 hidden as plumbing"
	if !strings.Contains(out, want) {
		t.Errorf("hidden-plumbing note must count distinct hidden functions and disclose the records (%q)\n%s", want, out)
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

// TestMermaidFocusPinNoteCountsDistinctFunctions pins the --focus rescue note against the
// same duplicate records. The note both COUNTS and LISTS the pinned-into-view nodes, and
// a reader checks it against the boxes on screen: three records behind one plumbing-tier
// display FQN draw exactly ONE box, so a per-record note reports "3 pinned node(s) …
// result]; result]; result]" for the single function actually rescued.
func TestMermaidFocusPinNoteCountsDistinctFunctions(t *testing.T) {
	const dup = "example.com/dupsvc.report[example.com/dupsvc.result]"
	g := duplicateFQNGraph()
	// MaxTier 2 with the tier-3 dup focused: it is kept ONLY by the pin, which is exactly
	// the condition the rescue note discloses.
	out, err := g.MermaidFocus([]string{dup}, MermaidOptions{MaxTier: 2})
	if err != nil {
		t.Fatalf("MermaidFocus(%q): %v", dup, err)
	}
	assertValidMermaid(t, out)

	if got := declaredNodeIDs(out); len(got) != 1 {
		t.Fatalf("focus render declared %d node ids %v, want 1 (three records, one box)\n%s", len(got), got, out)
	}
	// Counted and listed ONCE per function (not once per record), with the three records
	// behind that single box disclosed rather than dropped.
	const want = "%% 1 pinned node(s) (3 instance records) above tier 2 (plumbing); pinned into view: dupsvc.result]"
	if !strings.Contains(out, want) {
		t.Errorf("rescue note must count the distinct rescued function once and disclose its records (%q)\n%s", want, out)
	}
}

// dupFQN is the display FQN duplicateFQNGraph carries three node records for.
const dupFQN = "example.com/dupsvc.report[example.com/dupsvc.result]"

// singleRecordGraph is duplicateFQNGraph's duplicate-free twin: the same five functions,
// one record each. Every disclosure this file pins must be SILENT over it — a suffix or
// caveat on a graph that collapsed nothing is noise that trains a reader to skip the
// channel, and it would change the bytes of every existing golden.
func singleRecordGraph() *Graph {
	g := duplicateFQNGraph()
	var nodes []Node
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		if seen[n.FQN] {
			continue
		}
		seen[n.FQN] = true
		nodes = append(nodes, n)
	}
	g.Nodes = nodes
	return g
}

// divergentDuplicateGraph is duplicateFQNGraph with its three same-FQN records made to
// DISAGREE (distinct tiers). It is the case where the diff's per-FQN representative
// choice actually DISCARDS information, as opposed to the byte-identical records
// duplicateFQNGraph carries, where the collapse is lossless.
func divergentDuplicateGraph() *Graph {
	g := duplicateFQNGraph()
	nodes := append([]Node(nil), g.Nodes...)
	tier := 1
	for i, n := range nodes {
		if n.FQN == dupFQN {
			nodes[i].Tier = tier
			tier++
		}
	}
	g.Nodes = nodes
	return g
}

// TestMermaidDisclosesCollapsedRecordCount pins the diagram's own disclosure of the fold
// the declaration loop performs. Drawing one box per display FQN is right, but a reader
// counting five boxes against the graph's seven nodes[] entries has no way to learn where
// the other two went — a clean number over a silent collapse is the laundered unknown
// tenet 3 forbids. The note names both numbers.
func TestMermaidDisclosesCollapsedRecordCount(t *testing.T) {
	out := duplicateFQNGraph().Mermaid(MermaidOptions{MaxTier: 0}) // 0 = draw everything
	assertValidMermaid(t, out)

	const want = "%% 5 first-party nodes (7 instance records) kept: several node records share one display FQN"
	if !strings.Contains(out, want) {
		t.Errorf("render must disclose the record fold behind its box count (%q)\n%s", want, out)
	}

	// The same note must ride the over-cap OVERVIEW, which draws an entry-point index
	// instead of the graph: truncation must not take the honesty channel with it. Its
	// wording ("kept", "render as one box") is what keeps it true on a path that draws no
	// node box at all.
	over := duplicateFQNGraph().Mermaid(MermaidOptions{MaxTier: 0, MaxNodes: 2})
	assertValidMermaid(t, over)
	if !strings.Contains(over, "exceed the render cap") {
		t.Fatalf("expected the over-cap index render\n%s", over)
	}
	if !strings.Contains(over, want) {
		t.Errorf("the over-cap index must keep the record disclosure (%q)\n%s", want, over)
	}
}

// TestMermaidRecordDisclosureSilentWithoutDuplicates is the other half: a graph that
// collapsed nothing must say nothing, so every duplicate-free diagram is byte-identical
// to one produced before the disclosure existed.
func TestMermaidRecordDisclosureSilentWithoutDuplicates(t *testing.T) {
	for _, tier := range []int{0, 2} {
		out := singleRecordGraph().Mermaid(MermaidOptions{MaxTier: tier})
		assertValidMermaid(t, out)
		if strings.Contains(out, "instance records") {
			t.Errorf("MaxTier %d: a duplicate-free graph must carry no record disclosure\n%s", tier, out)
		}
	}
}

// TestRollupDisclosesCollapsedRecords pins the component view's disclosure. `nodes`
// counting distinct functions is correct, but a consumer summing it against the graph's
// nodes[] array reads an unexplainable 5-vs-7 without the caveat; the caveats channel is
// where every sibling artifact already puts exactly this kind of disclosure.
func TestRollupDisclosesCollapsedRecords(t *testing.T) {
	r := duplicateFQNGraph().RollupByPackage()
	if len(r.Caveats) != 1 {
		t.Fatalf("rollup carried %d caveats %v, want exactly 1", len(r.Caveats), r.Caveats)
	}
	const want = "`nodes` counts DISTINCT functions, not node records — example.com/dupsvc 5 nodes (7 instance records)"
	if !strings.Contains(r.Caveats[0], want) {
		t.Errorf("rollup caveat = %q, must contain %q", r.Caveats[0], want)
	}
	if clean := singleRecordGraph().RollupByPackage(); len(clean.Caveats) != 0 {
		t.Errorf("a duplicate-free rollup must carry no caveat, got %v", clean.Caveats)
	}
}

// TestDiffDisclosesDivergentRecordsOnly pins requirement (2) of the honesty rule and its
// negative half together: the diff keys on display FQN and compares ONE representative
// per FQN, so a group whose records DISAGREE loses the others' attributes and must be
// disclosed — while a group of byte-identical records loses nothing and must stay silent.
// Both the machine artifact (GraphDelta.Caveats) and the human one (the flowchart header)
// carry it, because both read the same diffCaveats list.
func TestDiffDisclosesDivergentRecordsOnly(t *testing.T) {
	const want = "branch graph: 1 display FQN carries node records that DISAGREE (" + dupFQN +
		"): the diff keys on FQN and compares ONE canonical representative per FQN, so the other records' attributes are not compared"

	base, branch := duplicateFQNGraph(), divergentDuplicateGraph()
	d := Delta(base, branch)
	if !slices.Contains(d.Caveats, want) {
		t.Errorf("Delta must disclose the divergent same-FQN collapse\n got: %v\nwant: %q", d.Caveats, want)
	}
	// The base side's three records are byte-identical: nothing is discarded there, so it
	// earns no caveat. Exactly one disclosure, naming the branch.
	if n := len(d.Caveats); n != 1 {
		t.Errorf("Delta carried %d caveats %v, want exactly 1 (the branch side only)", n, d.Caveats)
	}

	diagram := MermaidDiff(base, branch, MermaidOptions{MaxTier: 0})
	assertValidMermaid(t, diagram)
	if !strings.Contains(diagram, "%% ⚠ "+want) {
		t.Errorf("the flowchart header must disclose the same collapse as the JSON delta\n%s", diagram)
	}

	// Lossless collapse: identical records on both sides, no caveat on either artifact.
	quiet := Delta(base, base)
	if len(quiet.Caveats) != 0 {
		t.Errorf("byte-identical duplicate records discard nothing and must not be caveated, got %v", quiet.Caveats)
	}
	if q := MermaidDiff(base, base, MermaidOptions{MaxTier: 0}); strings.Contains(q, "DISAGREE") {
		t.Errorf("byte-identical duplicate records must not be caveated in the flowchart\n%s", q)
	}
}

// TestDivergentFQNs pins the detector directly, including the completeness claim its doc
// makes: comparing every record against the group's FIRST catches a group of three where
// only the LAST member differs — the arrangement a scan that only compared neighbours
// would miss.
func TestDivergentFQNs(t *testing.T) {
	const fqn = "svc.f"
	a := Node{FQN: fqn, Sig: "func()", Tier: 1}
	b := Node{FQN: fqn, Sig: "func()", Tier: 2}
	other := Node{FQN: "svc.g", Sig: "func()", Tier: 1}
	cases := []struct {
		name  string
		nodes []Node
		want  []string
	}{
		{"no duplicates", []Node{a, other}, nil},
		{"identical duplicates", []Node{a, a, a, other}, nil},
		{"trailing member differs", []Node{a, a, b}, []string{fqn}},
		{"leading member differs", []Node{b, a, a}, []string{fqn}},
		{"middle member differs", []Node{a, b, a}, []string{fqn}},
		{"two divergent groups", []Node{a, b, other, {FQN: "svc.g", Sig: "func()", Tier: 3}},
			[]string{"svc.f", "svc.g"}},
		{"empty graph", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := divergentFQNs(&Graph{Nodes: tc.nodes}); !slices.Equal(got, tc.want) {
				t.Errorf("divergentFQNs = %v, want %v", got, tc.want)
			}
		})
	}
}
