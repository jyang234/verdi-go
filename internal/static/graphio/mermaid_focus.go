package graphio

// MermaidFocus renders the INDUCED subgraph over a set of resolved focus names: the
// named nodes and every edge whose BOTH endpoints are named — nothing reachable-but-
// unnamed, nothing dropped. It is the render-time-scoped sibling of MermaidRootedAt
// (which scopes to one handler's forward reach); both scope an UNSCOPED graph at render
// time so the Frontier/blind-spot disclosure channels are present, and both prune those
// channels through the shared filterDisclosures helper. Absence of an edge in the output
// is a GRAPH FACT (the analysis records no call between two named nodes), not an omission
// — which is why it fails closed rather than ever rendering a partial focus.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/fqnres"
)

// MermaidFocus renders the induced subgraph over the resolved focus names — exactly
// those nodes and every edge with BOTH endpoints in the set. g must be UNSCOPED (Build
// with entry == ""), so the Frontier/blind-spot disclosure channels are present.
// Fail-closed: an UNRESOLVED or AMBIGUOUS name (or a regex matching nothing) is an error
// carrying the sorted candidates — never a partial render, since a silently dropped
// focus node would be a lie about the induced subgraph (CLAUDE.md tenet 2).
func (g *Graph) MermaidFocus(names []string, opts MermaidOptions) (string, error) {
	universe := g.endpointUniverse()

	// Resolve every name FIRST and abort on any failure, before building the sub-graph:
	// the drawn set must be EXACTLY what was asked for, so one bad name fails the whole
	// render rather than silently narrowing it (fail closed, loudly).
	focus := map[string]bool{}
	for _, name := range names {
		res, err := fqnres.Resolve(name, universe)
		if err != nil {
			// Regex compile failure — surface it verbatim (it names the bad pattern).
			return "", fmt.Errorf("--focus %s", err.Error())
		}
		if len(res.Matches) == 0 {
			if res.IsRegex {
				// A regex that selects nothing is a typo, not a legal empty set: rendering
				// without it would lie about the induced set (decision 4 of the spec). The
				// ZERO-MATCH label distinguishes it from a plain name's UNRESOLVED; the
				// quoting is the shared fqnres convention.
				return "", fmt.Errorf("--focus: ZERO-MATCH: %s matches no node/endpoint", fqnres.QuoteSingle(name))
			}
			return "", fmt.Errorf("--focus: %s", fqnres.UnresolvedDetail(name, "node/endpoint"))
		}
		if !res.IsRegex && res.Ambiguous {
			return "", fmt.Errorf("--focus: %s", fqnres.AmbiguousDetail(name, res.Matches))
		}
		for _, m := range res.Matches {
			focus[m] = true
		}
	}

	// A focus member that is neither a boundary endpoint NOR a first-party node with a
	// Node record — an edge from/to string that resolved via the endpoint universe but
	// has no node of its own (a dangling edge endpoint). It gets no node id, so the base
	// renderer draws no box for it and SILENTLY DROPS the edges it induces (uncounted, no
	// disclosure) — exactly the partial render this function promises never to produce.
	// Fail closed: name every such dangling member and refuse (CLAUDE.md tenet 2).
	nodeFQN := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeFQN[n.FQN] = true
	}
	var dangling []string
	for name := range focus {
		if !isBoundary(name) && !nodeFQN[name] {
			dangling = append(dangling, name)
		}
	}
	if len(dangling) > 0 {
		sort.Strings(dangling)
		return "", fmt.Errorf(
			"--focus: %d focus name(s) resolve to edge endpoints with no node record in this graph (dangling): %s — cannot render an honest induced subgraph",
			len(dangling), strings.Join(dangling, ", "))
	}

	// Nodes = g.Nodes filtered to the focus set, canonical order preserved (no
	// reachability walk). Edges = every record with BOTH endpoints in the focus set;
	// all records of a kept pair are kept (a sync+concurrent pair renders both arrows —
	// the multiplicity is information, Phase 0). g.Nodes/g.Edges are already canonically
	// sorted by Build, so iterating them (never the focus map) keeps output deterministic.
	sub := &Graph{
		// Scope label is the RAW CLI names in input order — deterministic because it is
		// input, and it reads back exactly what the user asked for.
		Entrypoint: "focus: " + strings.Join(names, ", "),
		Algo:       g.Algo,
	}
	for _, n := range g.Nodes {
		if focus[n.FQN] {
			sub.Nodes = append(sub.Nodes, n)
		}
	}
	for _, e := range g.Edges {
		if focus[e.From] && focus[e.To] {
			sub.Edges = append(sub.Edges, e)
		}
	}
	// Canonicalize the induced Nodes/Edges on their intrinsic fields (sortGraph — the
	// same total order Build uses), so the render is a pure function of the induced SET,
	// byte-identical no matter what order g's nodes/edges arrived in (CLAUDE.md: ordering
	// resolves on intrinsic, run-independent data, never arrival order). g is already a
	// canonically-sorted Build output, so this is a no-op for a well-formed graph.
	sortGraph(sub)

	// Prune the disclosure channels to the focus set and disclose what that dropped —
	// the SAME logic --root uses (filterDisclosures), so the two views cannot drift.
	notes := g.filterDisclosures(sub, focus, "the focus set")

	// A boundary endpoint exists only on edges; when it joins the focus set but no
	// FOCUSED caller reaches it, no induced edge names it and the base renderer draws
	// nothing (boundary nodes render only as the target of an edge from a shown source).
	// That is a silent hole in the drawn set — disclose it rather than drop it.
	notes = append(notes, boundaryFocusNotes(sub, focus)...)

	// Every focus-set FQN joins the force-keep set, so an isolated focus node (no induced
	// edge) still renders as a lone box — its isolation IS the finding. pinRoot stays
	// empty: focus discloses isolation as visible lone boxes, not as the root note.
	// focusScoped steers the over-cap index advice to "narrow the --focus set" (--root is
	// refused under --focus).
	opts.pinNodes = focus
	opts.focusScoped = true
	return sub.mermaid(opts, notes), nil
}

// endpointUniverse returns the sorted, deduped set of node FQNs ∪ every edge from/to
// string — the resolvable-name universe. It applies the SAME rule as claims.EndpointUniverse
// (internal/groundwork/claims/claims.go), so `--focus` and `groundwork assert` resolve a
// name against the same set: one resolver, both features. TestFocusResolverParityWithAssert
// pins this against claims' PRODUCTION constructor (production vs production). Boundary
// pseudo-nodes ("boundary:db SELECT loans") occur only as edge endpoints and are focusable.
// Collect-and-sort routes through the package's own sortedKeys (rollup.go), its "ONE home
// of the idiom" (CLAUDE.md: one source of truth).
func (g *Graph) endpointUniverse() []string {
	set := make(map[string]bool, len(g.Nodes)+2*len(g.Edges))
	for _, n := range g.Nodes {
		set[n.FQN] = true
	}
	for _, e := range g.Edges {
		set[e.From] = true
		set[e.To] = true
	}
	return sortedKeys(set)
}

// boundaryFocusNotes returns the disclosure note for boundary endpoints in the focus
// set that no induced edge draws — a focused boundary target no focused caller reaches,
// which the renderer would otherwise drop silently. It reads the ALREADY-COMPUTED induced
// set (sub.Edges — every edge with both endpoints in focus): a boundary endpoint is drawn
// iff some induced edge targets it. Reading sub.Edges rather than re-deriving
// focus[from]&&focus[to] over g.Edges keeps the induction rule in ONE place (CLAUDE.md:
// one source of truth). The note is endpoint-truthful — it counts undrawn ENDPOINTS, not
// "focus names" (a single regex can resolve to several, and a name may ALSO draw nodes).
// The undrawn list is sorted so the note is deterministic.
func boundaryFocusNotes(sub *Graph, focus map[string]bool) []string {
	drawn := map[string]bool{}
	for _, e := range sub.Edges {
		if isBoundary(e.To) {
			drawn[e.To] = true
		}
	}
	var undrawn []string
	for name := range focus {
		if isBoundary(name) && !drawn[name] {
			undrawn = append(undrawn, name)
		}
	}
	if len(undrawn) == 0 {
		return nil
	}
	sort.Strings(undrawn)
	return []string{fmt.Sprintf(
		"%d boundary endpoint(s) in the focus set have no induced edge — not drawn: %s",
		len(undrawn), strings.Join(undrawn, ", "))}
}
