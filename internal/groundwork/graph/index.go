package graph

import "sort"

// Index is a queryable view of a Graph: node lookup, forward/reverse adjacency
// over the first-party function subgraph, the boundary-effect surface, and the
// blind-spot frontier. It is built once and is read-only thereafter, so it is
// safe to share across the surfaces that consume it.
//
// Reachability is computed over function-to-function edges only. Boundary edges
// (DB/bus/outbound) are sinks: they are not traversed, but they are recorded so a
// caller can ask "what external effects does X reach" in one pass.
type Index struct {
	g     *Graph
	nodes map[string]Node
	out   map[string][]Edge // function-to-function edges, keyed by caller FQN
	in    map[string][]Edge // function-to-function edges, keyed by callee FQN
	effOf map[string][]Edge // boundary edges, keyed by the first-party FQN that makes them
	blind map[string][]BlindSpot
}

// NewIndex builds an Index over g. The graph is retained by reference; callers
// must not mutate it afterwards.
func NewIndex(g *Graph) *Index {
	ix := &Index{
		g:     g,
		nodes: make(map[string]Node, len(g.Nodes)),
		out:   make(map[string][]Edge),
		in:    make(map[string][]Edge),
		effOf: make(map[string][]Edge),
		blind: make(map[string][]BlindSpot),
	}
	for _, n := range g.Nodes {
		ix.nodes[n.FQN] = n
	}
	for _, e := range g.Edges {
		switch {
		case e.IsBoundary():
			ix.effOf[e.From] = append(ix.effOf[e.From], e)
		case ix.isNode(e.To):
			ix.out[e.From] = append(ix.out[e.From], e)
			ix.in[e.To] = append(ix.in[e.To], e)
		default:
			// An edge to a non-boundary target that is not a known node: a call
			// into something outside the emitted scope. flowmap does not normally
			// emit these in the full view, but tolerate them by ignoring for
			// reachability — they are neither a traversable function nor an effect.
		}
	}
	for _, b := range g.BlindSpots {
		ix.blind[b.Site] = append(ix.blind[b.Site], b)
	}
	return ix
}

func (ix *Index) isNode(fqn string) bool { _, ok := ix.nodes[fqn]; return ok }

// Has reports whether fqn is a function node in the graph.
func (ix *Index) Has(fqn string) bool { return ix.isNode(fqn) }

// Node returns the node for fqn, if present.
func (ix *Index) Node(fqn string) (Node, bool) { n, ok := ix.nodes[fqn]; return n, ok }

// Nodes returns every function FQN, sorted.
func (ix *Index) Nodes() []string {
	out := make([]string, 0, len(ix.nodes))
	for fqn := range ix.nodes {
		out = append(out, fqn)
	}
	sort.Strings(out)
	return out
}

// Callees returns the direct first-party callees of fqn, sorted and de-duplicated.
func (ix *Index) Callees(fqn string) []string {
	return targets(ix.out[fqn], func(e Edge) string { return e.To })
}

// Callers returns the direct first-party callers of fqn, sorted and de-duplicated.
func (ix *Index) Callers(fqn string) []string {
	return targets(ix.in[fqn], func(e Edge) string { return e.From })
}

// Reachable returns every function transitively reachable from any seed by
// following call edges forward (the seeds' downstream / dependency cone),
// excluding the seeds themselves unless a seed is reachable from another. The
// result is sorted.
func (ix *Index) Reachable(seeds ...string) []string {
	return ix.walk(seeds, func(fqn string) []string { return ix.Callees(fqn) })
}

// Reaching returns every function that can transitively reach any seed by
// following call edges backward (who breaks if a seed changes). The result is
// sorted, excluding the seeds themselves unless one reaches another.
func (ix *Index) Reaching(seeds ...string) []string {
	return ix.walk(seeds, func(fqn string) []string { return ix.Callers(fqn) })
}

// walk is the shared transitive-closure BFS for Reachable/Reaching. Seeds are
// enqueued but not placed in the result set unless re-discovered through an edge,
// so a seed only appears if it is genuinely (re)reachable — e.g. via a cycle.
func (ix *Index) walk(seeds []string, next func(string) []string) []string {
	seen := make(map[string]bool)
	var queue []string
	for _, s := range seeds {
		for _, t := range next(s) {
			if !seen[t] {
				seen[t] = true
				queue = append(queue, t)
			}
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, t := range next(cur) {
			if !seen[t] {
				seen[t] = true
				queue = append(queue, t)
			}
		}
	}
	out := make([]string, 0, len(seen))
	for fqn := range seen {
		out = append(out, fqn)
	}
	sort.Strings(out)
	return out
}

// Sources returns the function nodes with no first-party caller — the structural
// entry points of the graph (mains, HTTP handlers and bus consumers resolved
// through dynamic dispatch, exported library functions). This is the graph-only
// approximation of "entrypoint"; flowmap's boundary contract can later refine
// these with their route/topic names. The result is sorted.
func (ix *Index) Sources() []string {
	out := make([]string, 0)
	for fqn := range ix.nodes {
		if len(ix.in[fqn]) == 0 {
			out = append(out, fqn)
		}
	}
	sort.Strings(out)
	return out
}

// EntrypointCover returns the Sources from which fqn is transitively reachable —
// the entry points the function is live behind. The result is sorted.
func (ix *Index) EntrypointCover(fqn string) []string {
	reaching := make(map[string]bool)
	for _, r := range ix.Reaching(fqn) {
		reaching[r] = true
	}
	var out []string
	for _, s := range ix.Sources() {
		if s == fqn || reaching[s] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// Effects returns the boundary effects (DB/bus/outbound edges) made directly by
// any function in fqns. Order follows the input; callers wanting the full
// downstream effect surface of a function pass Reachable(fqn) plus fqn.
func (ix *Index) Effects(fqns ...string) []Edge {
	var out []Edge
	for _, fqn := range fqns {
		out = append(out, ix.effOf[fqn]...)
	}
	return out
}

// BlindSpotsAt returns the blind spots recorded at a site (an FQN or a package
// path).
func (ix *Index) BlindSpotsAt(site string) []BlindSpot { return ix.blind[site] }

// BlindSpots returns the whole graph-completeness blind-spot manifest.
func (ix *Index) BlindSpots() []BlindSpot { return ix.g.BlindSpots }

// targets projects edges through pick, de-duplicates, and sorts.
func targets(edges []Edge, pick func(Edge) string) []string {
	seen := make(map[string]bool, len(edges))
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		t := pick(e)
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}
