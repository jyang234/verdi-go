package graphio

// MermaidRootedAt renders the per-entrypoint flowchart: the subgraph forward-
// reachable from one handler, scoped at RENDER time over an UNSCOPED graph. This is
// the honest alternative to scoping the graph at BUILD time (graphio.Build with an
// entry): a build-scoped graph drops the Frontier section ("rides unscoped builds
// only"), so a build-scoped per-handler view shows blind spots but silently omits
// frontier markers. Rendering from the unscoped graph keeps BOTH disclosure
// channels in the per-handler view, so a reviewer reading one handler's diagram
// sees the same "where the analysis goes dark" markers the whole-graph view shows.
//
// It returns ok=false if root resolves to no handler and no node — fail closed
// rather than render an empty or whole-graph diagram under a misleading scope label.

import (
	"strings"

	"github.com/jyang234/golang-code-graph/internal/static/frontier"
)

// MermaidRootedAt resolves root (an entry-point route/topic name, or a function
// FQN) to a handler, then renders everything forward-reachable from it. g must be
// an UNSCOPED graph (graphio.Build with entry==""), so its Frontier and Entrypoints
// sections are populated.
func (g *Graph) MermaidRootedAt(root string, opts MermaidOptions) (string, bool) {
	rootFn, ok := g.resolveRoot(root)
	if !ok {
		return "", false
	}

	reach := g.forwardReach(rootFn)

	// Build a sub-graph of only the reachable structure, then reuse the whole
	// renderer. Edges are kept when their source is reachable; because forwardReach
	// is a forward closure, a first-party target of a kept edge is reachable too, and
	// a boundary target rides along with its reachable source.
	sub := &Graph{
		Entrypoint: root,
		Algo:       g.Algo,
	}
	for _, n := range g.Nodes {
		if reach[n.FQN] {
			sub.Nodes = append(sub.Nodes, n)
		}
	}
	for _, e := range g.Edges {
		if reach[e.From] {
			sub.Edges = append(sub.Edges, e)
		}
	}

	// Blind spots are attributed to a first-party site FQN; keep those whose site is
	// in reach. A package-level site (reflect/unsafe, no owning function) cannot be
	// attributed to one handler's reach, so it is pruned here and disclosed — it
	// still rides the whole-graph view.
	droppedBlind := 0
	for _, b := range g.BlindSpots {
		if reach[b.Site] {
			sub.BlindSpots = append(sub.BlindSpots, b)
		} else {
			droppedBlind++
		}
	}

	// Frontier markers are attributed to an owning function (Owner) or a site FQN.
	// Keep a marker when either falls in reach; a marker keyed only on an effect
	// label (e.g. "bus PUBLISH <dynamic>") with no reachable owner is pruned.
	var markers []frontier.Marker
	if g.Frontier != nil {
		for _, m := range g.Frontier.Markers {
			if reach[m.Owner] || reach[m.Site] {
				markers = append(markers, m)
			}
		}
		if len(markers) > 0 {
			sub.Frontier = &FrontierSection{Markers: markers}
		}
	}

	var notes []string
	if droppedBlind > 0 {
		notes = append(notes, plural(droppedBlind, "package-level blind spot")+
			" not attributable to this handler's reach shown only in the whole-graph view")
	}
	return sub.mermaid(opts, notes), true
}

// resolveRoot maps a user-supplied root to a handler FQN. It tries, in order: an
// exact entry-point Name match, an exact node FQN, an exact entry-point Fn, then a
// segment-wise route match (so "POST /loan-application" resolves a route the router
// mounted under a prefix, the same loose match triage uses). The first hit wins;
// determinism holds because Entrypoints and Nodes are canonically sorted.
func (g *Graph) resolveRoot(root string) (string, bool) {
	for _, e := range g.Entrypoints {
		if e.Name == root {
			return e.Fn, true
		}
	}
	for _, n := range g.Nodes {
		if n.FQN == root {
			return n.FQN, true
		}
	}
	for _, e := range g.Entrypoints {
		if e.Fn == root {
			return e.Fn, true
		}
	}
	for _, e := range g.Entrypoints {
		if routeMatches(e.Name, root) {
			return e.Fn, true
		}
	}
	return "", false
}

// routeMatches reports whether a query route names the same endpoint as an
// entry-point Name, comparing method (if present) and path segment-wise so a
// leaf-pattern entry ("/{id}/status") matches a fuller query and vice versa. It is
// deliberately permissive — a per-handler VIEW, never a gate — but anchored on the
// method and the final segments so unrelated routes do not collide.
func routeMatches(name, query string) bool {
	nm, np := splitRoute(name)
	qm, qp := splitRoute(query)
	if nm != "" && qm != "" && !strings.EqualFold(nm, qm) {
		return false
	}
	if np == qp {
		return true
	}
	return strings.HasSuffix(np, qp) || strings.HasSuffix(qp, np)
}

// splitRoute separates an optional leading HTTP method from the path of a route
// name ("POST /loan-application" -> "POST", "/loan-application"). A topic with no
// method ("payment.settled") returns an empty method and the whole string as path.
func splitRoute(name string) (method, path string) {
	name = strings.TrimSpace(name)
	if i := strings.IndexByte(name, ' '); i >= 0 {
		return name[:i], strings.TrimSpace(name[i+1:])
	}
	return "", name
}

// forwardReach returns the set of first-party FQNs reachable forward from rootFn
// over the call edges (boundary targets are leaves, not first-party nodes, so they
// are reached via their edge rather than added to this set). The traversal is a
// plain BFS over a deterministic adjacency built from the sorted Edges.
func (g *Graph) forwardReach(rootFn string) map[string]bool {
	adj := map[string][]string{}
	for _, e := range g.Edges {
		if isBoundary(e.To) {
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
	}
	reach := map[string]bool{rootFn: true}
	queue := []string{rootFn}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range adj[cur] {
			if !reach[next] {
				reach[next] = true
				queue = append(queue, next)
			}
		}
	}
	return reach
}
