package graphio

import (
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/render"
)

// --- Layer 1: edge-label escaping (via/decoration text) ---

func TestLayerEdgeLabelEscaping(t *testing.T) {
	// A crafted Via with the edge-label delimiter, an angle bracket, and a newline.
	g := &Graph{Algo: "rta",
		Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}, {FQN: "a.C", Tier: 1}},
		Edges: []Edge{
			{From: "a.A", To: "a.B", Tier: 2, Via: "x|y<z>"},                                             // solid via
			{From: "a.A", To: "a.C", Tier: 2, Concurrent: true, Via: "p|q"},                              // dashed via
			{From: "a.B", To: "boundary:bus PUBLISH e", Tier: 1, Boundary: "outbound-async", Via: "r|s"}, // async effect via
		},
	}
	assertValidMermaid(t, g.Mermaid(MermaidOptions{MaxTier: 2}))
	// And in the diff renderer (added/kept edges carry decoration).
	assertValidMermaid(t, MermaidDiff(&Graph{Algo: "rta"}, g, MermaidOptions{MaxTier: 2}))
}

// --- Layer 2a: diff linkStyle index ↔ edge alignment ---

func TestLayerDiffLinkStyleAlignment(t *testing.T) {
	// A diff whose union contains edges that get SKIPPED (dangling endpoints)
	// interspersed with drawn added/removed/kept edges. The linkStyle indices must
	// stay aligned with the actually-emitted edges, or the colors land on wrong edges.
	base := &Graph{Algo: "rta",
		Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}},
		Edges: []Edge{{From: "a.A", To: "a.B"}, {From: "a.A", To: "a.GONE"}}, // dangling in base
	}
	branch := &Graph{Algo: "rta",
		Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}, {FQN: "a.C", Tier: 1}},
		Edges: []Edge{{From: "a.A", To: "a.B"}, {From: "a.A", To: "a.C"}, {From: "a.A", To: "a.GHOST"}}, // a.C added, GHOST dangling
	}
	out := MermaidDiff(base, branch, MermaidOptions{MaxTier: 2})
	assertValidMermaid(t, out)

	// Count emitted edges and collect linkStyle indices; every index must reference an
	// emitted edge (0-based), or a color lands on the wrong edge / a nonexistent one.
	emitted := 0
	var indices []string
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "linkStyle "):
			// "linkStyle 0,1,2 stroke:..." — field [1] is the comma-joined index list.
			if f := strings.Fields(trimmed); len(f) >= 2 {
				indices = append(indices, strings.Split(f[1], ",")...)
			}
		case strings.Contains(trimmed, "-->"), strings.Contains(trimmed, "==>"), strings.Contains(trimmed, ".->"):
			emitted++
		}
	}
	for _, s := range indices {
		n, ok := atoiSafe(s)
		if !ok {
			t.Errorf("non-numeric linkStyle index %q", s)
			continue
		}
		if n < 0 || n >= emitted {
			t.Errorf("linkStyle index %d out of range (only %d edges emitted):\n%s", n, emitted, out)
		}
	}
}

func atoiSafe(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// --- Layer 2b: diff attribute-change visibility (third `changed` class) ---

func TestLayerDiffAttributeChange(t *testing.T) {
	// A surviving NODE whose tier drifts AND a surviving (from,to) EDGE that gains a
	// concurrent record — the two attribute-only drifts a presence-keyed diff used to miss.
	base := &Graph{Algo: "rta",
		Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 2}},
		Edges: []Edge{{From: "a.A", To: "a.B"}},
	}
	branch := &Graph{Algo: "rta",
		Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}}, // B: tier 2→1
		Edges: []Edge{{From: "a.A", To: "a.B", Concurrent: true}},   // A→B became concurrent
	}
	out := MermaidDiff(base, branch, MermaidOptions{MaxTier: 2})
	assertValidMermaid(t, out)

	// BEHAVIOR PIN (was a KNOWN LIMITATION until Phase 2): an attribute-only change on a
	// SURVIVING element now renders as the third `changed` class instead of a silent grey
	// kept edge — the node recolors and names its tier drift, the edge keeps its (solid)
	// diff-state shape but carries a Δ label naming the changed attribute. The diff is now
	// the authority on attribute change, in lockstep with the JSON delta it derives from
	// (parity-guarded by TestDiffMermaidJSONParity).
	mustContain := []string{
		`a_B["Δ a.B tier 2→1"]:::changed`,     // node tier drift: recolored + named
		`a_A -->|Δ concurrent ∅→go| a_B`,      // edge attr drift: kept-shape arrow + Δ label
		"stroke:#d98e00",                      // the changed linkStyle color
		`lg_changed["Δ changed (tier/attr)"]`, // the fourth legend entry
	}
	for _, w := range mustContain {
		if !strings.Contains(out, w) {
			t.Errorf("diff must flag the attribute change as `changed`, missing %q:\n%s", w, out)
		}
	}
	// It must NOT fall back to the old silent kept edge (the limitation this phase closed).
	if strings.Contains(out, "a_A -->|go| a_B") {
		t.Errorf("an attribute change must no longer render as a plain kept edge:\n%s", out)
	}
}

// --- Layer 3: render.Fence cannot be broken by a backtick label ---

func TestLayerFenceBacktickSafe(t *testing.T) {
	g := &Graph{Algo: "rta", Nodes: []Node{{FQN: "a.X```evil", Tier: 1}}}
	fenced := render.Fence(g.Mermaid(MermaidOptions{MaxTier: 2}))
	// The fence must open and close exactly once: no interior line is a bare ``` fence
	// (labels can't contain newlines, so a backtick run can never start its own line).
	opens := 0
	for _, ln := range strings.Split(fenced, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "```") {
			opens++
		}
	}
	if opens != 2 {
		t.Errorf("Fence must have exactly one open+close, got %d fence lines:\n%s", opens, fenced)
	}
}

// --- Layer 4: legend id reservation vs a colliding node name ---

func TestLayerLegendIDCollision(t *testing.T) {
	// A node whose short name sanitizes to a reserved legend id must NOT merge with the
	// legend box; the reserve + unique-suffix must keep them distinct.
	base := &Graph{Algo: "rta"}
	branch := &Graph{Algo: "rta", Nodes: []Node{{FQN: "lg.added", Tier: 1}, {FQN: "lg.kept", Tier: 1}}}
	out := MermaidDiff(base, branch, MermaidOptions{MaxTier: 2})
	assertValidMermaid(t, out)
	// The legend's own nodes are still the labelled boxes, not the graph's nodes.
	if !strings.Contains(out, `lg_added["＋ added"]`) || !strings.Contains(out, `lg_kept["unchanged"]`) {
		t.Errorf("legend boxes must survive a colliding node name:\n%s", out)
	}
}

// --- Layer 5: routeMatches / resolveRoot adversarial routes ---

func TestLayerRouteMatchesAdversarial(t *testing.T) {
	cases := []struct {
		name, query string
		want        bool
	}{
		{"GET /a/b", "GET /b", true},       // segment-suffix (leaf pattern)
		{"GET /ab", "GET /b", false},       // NOT a segment suffix
		{"GET /a/b", "GET /x/b", false},    // differing earlier segment
		{"GET /a", "POST /a", false},       // method differs
		{"GET  /a", "GET /a", true},        // extra space in method split
		{"topic.name", "topic.name", true}, // topic exact
		{"topic.name", "name", false},      // topic substring is not a match
		{"GET /a/b/c", "GET /b/c", true},   // multi-segment suffix
		{"GET /a/b/c", "GET /a/c", false},  // non-contiguous
	}
	for _, c := range cases {
		if got := routeMatches(c.name, c.query); got != c.want {
			t.Errorf("routeMatches(%q,%q)=%v want %v", c.name, c.query, got, c.want)
		}
	}
}

// TestLayerEmptyBaseDiff pins the generalizability fix: a NEW service has an empty
// base graph, and the all-added diff is correct — it must render (not refuse) and
// disclose the empty base so the "everything added" reading is unambiguous.
func TestLayerEmptyBaseDiff(t *testing.T) {
	branch := &Graph{Algo: "rta",
		Nodes: []Node{{FQN: "newsvc.Handler", Tier: 1}, {FQN: "newsvc.store", Tier: 2}},
		Edges: []Edge{{From: "newsvc.Handler", To: "newsvc.store", Tier: 2}},
	}
	out := MermaidDiff(&Graph{Algo: "rta"}, branch, MermaidOptions{MaxTier: 2})
	assertValidMermaid(t, out)
	if !strings.Contains(out, "base graph is empty") {
		t.Errorf("empty-base diff must disclose the empty base:\n%s", out)
	}
	if !strings.Contains(out, ":::added") {
		t.Errorf("empty-base diff must show the branch as added:\n%s", out)
	}
}
