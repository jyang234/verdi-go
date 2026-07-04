package graphio

import (
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
	"github.com/jyang234/golang-code-graph/internal/static/frontier"
)

func pressureCases() map[string]*Graph {
	return map[string]*Graph{
		"empty":             {Algo: "rta"},
		"self-loop":         {Algo: "rta", Nodes: []Node{{FQN: "a.A", Tier: 1}}, Edges: []Edge{{From: "a.A", To: "a.A", Tier: 2}}},
		"dangling-to":       {Algo: "rta", Nodes: []Node{{FQN: "a.A", Tier: 1}}, Edges: []Edge{{From: "a.A", To: "a.GHOST", Tier: 2}}},
		"dangling-from":     {Algo: "rta", Nodes: []Node{{FQN: "a.A", Tier: 1}}, Edges: []Edge{{From: "a.GHOST", To: "a.A", Tier: 2}}},
		"dup-edges":         {Algo: "rta", Nodes: []Node{{FQN: "a.A", Tier: 1}, {FQN: "a.B", Tier: 1}}, Edges: []Edge{{From: "a.A", To: "a.B"}, {From: "a.A", To: "a.B"}}},
		"only-special-fqn":  {Algo: "rta", Nodes: []Node{{FQN: "***", Tier: 1}}},
		"empty-fqn":         {Algo: "rta", Nodes: []Node{{FQN: "", Tier: 1}}},
		"quotes-backslash":  {Algo: "rta", Nodes: []Node{{FQN: `a."b"\c`, Tier: 1}}},
		"newline-tab":       {Algo: "rta", Nodes: []Node{{FQN: "a.\n\tB", Tier: 1}}},
		"unicode":           {Algo: "rta", Nodes: []Node{{FQN: "a.Café→世界🚀", Tier: 1}}},
		"shape-break-label": {Algo: "rta", Nodes: []Node{{FQN: `a.X"]:::evil`, Tier: 1}}},
		"generic-brackets":  {Algo: "rta", Nodes: []Node{{FQN: "a.Map[string][]*b.T", Tier: 1}}},
		"fqn-boundary-prefix": {Algo: "rta", Nodes: []Node{{FQN: "boundary:fake", Tier: 1}, {FQN: "x.A", Tier: 1}},
			Edges: []Edge{{From: "x.A", To: "boundary:fake", Tier: 1, Boundary: "outbound-sync"}}},
		"boundary-shared-dangling": {Algo: "rta",
			Nodes: []Node{{FQN: "a.A", Tier: 1}},
			Edges: []Edge{
				{From: "a.A", To: "boundary:db X", Tier: 1, Boundary: "outbound-sync"},
				{From: "a.GHOST", To: "boundary:db X", Tier: 1, Boundary: "outbound-sync"}, // dangling source, same target
			}},
		"boundary-label-evil": {Algo: "rta", Nodes: []Node{{FQN: "a.A", Tier: 1}},
			Edges: []Edge{{From: "a.A", To: `boundary:db EXEC "a"|b[c]{d}<e>`, Tier: 1, Boundary: "outbound-sync"}}},
		"blindspot-empty-site": {Algo: "rta", Nodes: []Node{{FQN: "a.A", Tier: 1}},
			BlindSpots: []blindspots.BlindSpot{{Kind: blindspots.Reflect, Site: ""}}},
		"frontier-weird": {Algo: "rta", Nodes: []Node{{FQN: "a.A", Tier: 1}},
			Frontier: &FrontierSection{Markers: []frontier.Marker{{Kind: `evil"<x>`, Bin: frontier.BinA, Site: "", Owner: ""}}}},
		"only-boundary-no-firstparty": {Algo: "rta",
			Edges: []Edge{{From: "a.A", To: "boundary:db X", Tier: 1, Boundary: "outbound-sync"}}},
		"long-fqn": {Algo: "rta", Nodes: []Node{{FQN: "a." + strings.Repeat("X", 5000), Tier: 1}}},
	}
}

// TestPressureRenderers throws pathological graphs at all three renderers and checks:
// no panic, valid Mermaid, and determinism (rendered twice, byte-identical).
func TestPressureRenderers(t *testing.T) {
	for name, g := range pressureCases() {
		t.Run(name, func(t *testing.T) {
			check := func(label, out string, out2 string) {
				if out != out2 {
					t.Errorf("[%s] not deterministic", label)
				}
				if err := validateMermaid(out); err != nil {
					t.Errorf("[%s] invalid Mermaid: %v\n%s", label, err, out)
				}
			}
			// whole-graph, capped and uncapped
			check("whole", g.Mermaid(MermaidOptions{MaxTier: 2}), g.Mermaid(MermaidOptions{MaxTier: 2}))
			check("whole-cap1", g.Mermaid(MermaidOptions{MaxTier: 2, MaxNodes: 1}), g.Mermaid(MermaidOptions{MaxTier: 2, MaxNodes: 1}))
			// diff vs empty, and vs self
			check("diff-vs-empty", MermaidDiff(&Graph{Algo: "rta"}, g, MermaidOptions{MaxTier: 2}), MermaidDiff(&Graph{Algo: "rta"}, g, MermaidOptions{MaxTier: 2}))
			check("diff-self", MermaidDiff(g, g, MermaidOptions{MaxTier: 2}), MermaidDiff(g, g, MermaidOptions{MaxTier: 2}))
			// rooted at first node FQN if any
			if len(g.Nodes) > 0 {
				if out, ok := g.MermaidRootedAt(g.Nodes[0].FQN, MermaidOptions{MaxTier: 2}); ok {
					check("rooted", out, mustRoot(g, g.Nodes[0].FQN))
				}
			}
			// Focus on EVERY endpoint (nodes ∪ edge endpoints): MermaidFocus must never
			// panic and must return either an error (fail closed — dangling endpoints,
			// unresolvable names) or valid, deterministic Mermaid. The endpoint universe is
			// exactly the resolvable set, so each name resolves to itself (a pathological FQN
			// resolves via the raw universe entry) without tripping ambiguity on this graph.
			eps := g.endpointUniverse()
			if len(eps) > 0 {
				out, err := g.MermaidFocus(eps, MermaidOptions{MaxTier: 2})
				if err == nil {
					again, _ := g.MermaidFocus(eps, MermaidOptions{MaxTier: 2})
					check("focus", out, again)
				} else if out != "" {
					t.Errorf("[focus] error path must render NOTHING, got:\n%s", out)
				}
			}
		})
	}
}

func mustRoot(g *Graph, root string) string {
	out, _ := g.MermaidRootedAt(root, MermaidOptions{MaxTier: 2})
	return out
}
