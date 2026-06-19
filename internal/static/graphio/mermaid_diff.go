package graphio

// MermaidDiff renders the structural delta between two call graphs (base → branch)
// as a single Mermaid flowchart with the change colored in. It is the visual view
// of a comparison groundwork already performs at the JSON level — a VIEW, never a
// gate: the verdict stays with `groundwork review`, this only makes the delta
// legible to a human reviewer.
//
// The encoding is deliberately redundant so the delta is unmistakable, and — the
// design point — NODE state and EDGE state are independent, so "a function that
// still exists but lost one call" never reads the same as "a function that was
// removed". A kept node stays neutral grey even when a red removed-edge dangles off
// it; only a node that is GONE on the branch turns red. Every changed element is
// triple-cued: fill color, border style, and a +/− label prefix (so the diff
// survives greyscale printing and colorblindness).

import (
	"sort"
	"strings"
)

type diffState int

const (
	stKept diffState = iota
	stAdded
	stRemoved
)

func stateOf(inBase, inBranch bool) diffState {
	switch {
	case inBase && inBranch:
		return stKept
	case inBranch:
		return stAdded
	default:
		return stRemoved
	}
}

// classFor maps a node/boundary state to its CSS class. Kept nodes keep no diff
// class (callers apply the kind class instead); only changed nodes are recolored.
func classFor(s diffState) string {
	switch s {
	case stAdded:
		return "added"
	case stRemoved:
		return "removed"
	default:
		return "kept"
	}
}

// prefixFor is the leading glyph on a changed label: a third, text-only cue beside
// fill and border, so the state reads even with no color at all.
func prefixFor(s diffState) string {
	switch s {
	case stAdded:
		return "＋ "
	case stRemoved:
		return "− "
	default:
		return ""
	}
}

type ekey struct{ from, to string }

func nodeIndex(g *Graph) map[string]Node {
	m := make(map[string]Node, len(g.Nodes))
	for _, n := range g.Nodes {
		m[n.FQN] = n
	}
	return m
}

func edgeIndex(g *Graph) map[ekey]Edge {
	m := make(map[ekey]Edge, len(g.Edges))
	for _, e := range g.Edges {
		m[ekey{e.From, e.To}] = e
	}
	return m
}

// MermaidDiff renders base → branch. opts.MaxTier collapses unchanged plumbing as
// in Mermaid, but a node or edge that is part of the delta is ALWAYS shown,
// whatever its tier — the diff must never hide the very thing it is diffing.
func MermaidDiff(base, branch *Graph, opts MermaidOptions) string {
	baseN, branchN := nodeIndex(base), nodeIndex(branch)
	baseE, branchE := edgeIndex(base), edgeIndex(branch)

	// node FQN -> chosen Node (branch wins; a removed node survives only in base).
	pick := func(fqn string) Node {
		if n, ok := branchN[fqn]; ok {
			return n
		}
		return baseN[fqn]
	}
	nodeStateOf := func(fqn string) diffState {
		_, b := baseN[fqn]
		_, br := branchN[fqn]
		return stateOf(b, br)
	}

	// A first-party node that emits a boundary effect, on either side, is never
	// hidden — the diff must not drop an effect (the same soundness rule Mermaid
	// keeps). Likewise, any endpoint of a CHANGED edge is force-shown so the delta
	// is never collapsed away.
	emitsEffect := map[string]bool{}
	for _, e := range base.Edges {
		if isBoundary(e.To) {
			emitsEffect[e.From] = true
		}
	}
	for _, e := range branch.Edges {
		if isBoundary(e.To) {
			emitsEffect[e.From] = true
		}
	}
	allEdges := unionEdgeKeys(baseE, branchE)
	changedEndpoint := map[string]bool{}
	for _, k := range allEdges {
		_, b := baseE[k]
		_, br := branchE[k]
		if stateOf(b, br) == stKept {
			continue
		}
		changedEndpoint[k.from] = true
		if !isBoundary(k.to) {
			changedEndpoint[k.to] = true
		}
	}

	ids := &idAlloc{used: map[string]bool{}}
	reserveLegendIDs(ids)

	// Pass A: choose which first-party nodes to show, and assign ids in a stable
	// union order.
	nodeID := map[string]string{}
	shown := map[string]bool{}
	hidden := 0
	for _, fqn := range unionNodeKeys(baseN, branchN) {
		st := nodeStateOf(fqn)
		if st == stKept && opts.MaxTier > 0 && pick(fqn).Tier > opts.MaxTier &&
			!emitsEffect[fqn] && !changedEndpoint[fqn] {
			hidden++
			continue
		}
		nodeID[fqn] = ids.get(shortName(fqn))
		shown[fqn] = true
	}

	// Boundary effect nodes, once per label, with their own added/removed/kept
	// state taken from which side's edges reach them.
	type bnode struct {
		id, label, class string
		state            diffState
	}
	bIDs := map[string]string{}
	var bnodes []bnode
	boundaryState := func(to string) diffState {
		inB, inBr := false, false
		for k := range baseE {
			if k.to == to {
				inB = true
				break
			}
		}
		for k := range branchE {
			if k.to == to {
				inBr = true
				break
			}
		}
		return stateOf(inB, inBr)
	}
	for _, k := range allEdges {
		if !isBoundary(k.to) || !shown[k.from] {
			continue
		}
		if _, ok := bIDs[k.to]; ok {
			continue
		}
		label, class := boundaryShape(k.to)
		id := ids.get(class + "_" + label)
		bIDs[k.to] = id
		bnodes = append(bnodes, bnode{id: id, label: label, class: class, state: boundaryState(k.to)})
	}

	var b strings.Builder
	b.WriteString("flowchart LR\n")
	b.WriteString("    %% call-graph diff — base → branch (a view, never a gate)\n")
	if hidden > 0 {
		b.WriteString("    %% " + plural(hidden, "unchanged node") +
			" above tier " + itoa(opts.MaxTier) + " hidden; changed nodes are always shown\n")
	}
	writeLegend(&b)

	// First-party node declarations, in union order.
	for _, fqn := range unionNodeKeys(baseN, branchN) {
		id, ok := nodeID[fqn]
		if !ok {
			continue
		}
		st := nodeStateOf(fqn)
		label := prefixFor(st) + shortName(fqn)
		if pick(fqn).Fallible {
			label += " ⚠"
		}
		b.WriteString("    " + id + `["` + label + `"]:::` + nodeClass(st) + "\n")
	}
	for _, bn := range bnodes {
		open, close := boundaryDelims(bn.class)
		cls := bn.class
		if bn.state != stKept {
			cls = classFor(bn.state) // a changed effect node recolors; shape stays
		}
		b.WriteString("    " + bn.id + open + `"` + prefixFor(bn.state) + bn.label + `"` + close + ":::" + cls + "\n")
	}

	// Edges, in union order; collect link indices per state for linkStyle coloring.
	var idx int
	var addedIdx, removedIdx, keptIdx []int
	for _, k := range allEdges {
		var toID string
		if isBoundary(k.to) {
			id, ok := bIDs[k.to]
			if !ok {
				continue
			}
			toID = id
		} else {
			id, ok := nodeID[k.to]
			if !ok {
				continue
			}
			toID = id
		}
		fromID, ok := nodeID[k.from]
		if !ok {
			continue
		}
		e := branchE[k]
		if _, in := branchE[k]; !in {
			e = baseE[k]
		}
		st := stateOf(hasKey(baseE, k), hasKey(branchE, k))
		b.WriteString("    " + diffEdgeLine(fromID, toID, e, st) + "\n")
		switch st {
		case stAdded:
			addedIdx = append(addedIdx, idx)
		case stRemoved:
			removedIdx = append(removedIdx, idx)
		default:
			keptIdx = append(keptIdx, idx)
		}
		idx++
	}

	writeLinkStyle(&b, addedIdx, "stroke:#1a9d1a,stroke-width:3px")
	writeLinkStyle(&b, removedIdx, "stroke:#cc3333,stroke-width:2px")
	writeLinkStyle(&b, keptIdx, "stroke:#cccccc")
	b.WriteString(diffClassDefs)
	return b.String()
}

// nodeClass picks the class for a first-party node: a changed node uses its diff
// class (added/removed); an unchanged node is always the neutral kept grey, so the
// delta owns the whole color budget and a kept node can never be mistaken for a
// changed one. Fallibility is still flagged on kept nodes by a ⚠ glyph in the
// label — a quiet cue that does not compete with the red/green of the diff.
func nodeClass(s diffState) string {
	if s != stKept {
		return classFor(s)
	}
	return "kept"
}

// diffEdgeLine styles an edge by its delta state: added is a thick arrow, removed a
// dotted arrow with a "removed" label, kept a plain thin arrow. The arrow SHAPE
// differs per state so the delta survives with no color (the linkStyle colors are
// an enhancement, not the only signal). Concurrency/async/via decorations from the
// underlying edge are preserved on kept and added edges.
func diffEdgeLine(from, to string, e Edge, s diffState) string {
	switch s {
	case stAdded:
		text := "＋"
		if d := edgeDecoration(e); d != "" {
			text += " " + d
		}
		return from + " ==>|" + text + "| " + to
	case stRemoved:
		return from + " -.->|− removed| " + to
	default:
		if d := edgeDecoration(e); d != "" {
			return from + " -->|" + d + "| " + to
		}
		return from + " --> " + to
	}
}

// edgeDecoration is the non-diff annotation an edge carries: go/async for a
// concurrent or asynchronous hop, and the reclaimer `via` provenance.
func edgeDecoration(e Edge) string {
	var text string
	switch {
	case e.Concurrent:
		text = "go"
	case e.Boundary == "outbound-async":
		text = "async"
	}
	if e.Via != "" {
		if text != "" {
			text += "; "
		}
		text += "via " + e.Via
	}
	return text
}

func hasKey(m map[ekey]Edge, k ekey) bool { _, ok := m[k]; return ok }

func unionNodeKeys(a, b map[string]Node) []string {
	seen := map[string]bool{}
	var out []string
	for k := range a {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func unionEdgeKeys(a, b map[ekey]Edge) []ekey {
	seen := map[ekey]bool{}
	var out []ekey
	for k := range a {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].from != out[j].from {
			return out[i].from < out[j].from
		}
		return out[i].to < out[j].to
	})
	return out
}

func writeLinkStyle(b *strings.Builder, idx []int, style string) {
	if len(idx) == 0 {
		return
	}
	parts := make([]string, len(idx))
	for i, n := range idx {
		parts[i] = itoa(n)
	}
	b.WriteString("    linkStyle " + strings.Join(parts, ",") + " " + style + "\n")
}

// reserveLegendIDs claims the fixed legend ids so the node allocator can never
// collide with them.
func reserveLegendIDs(a *idAlloc) {
	for _, id := range []string{"lg_kept", "lg_added", "lg_removed"} {
		a.used[id] = true
	}
}

func writeLegend(b *strings.Builder) {
	b.WriteString("    subgraph legend[\"legend — base → branch\"]\n")
	b.WriteString("        direction LR\n")
	b.WriteString("        lg_kept[\"unchanged\"]:::kept\n")
	b.WriteString("        lg_added[\"＋ added\"]:::added\n")
	b.WriteString("        lg_removed[\"− removed\"]:::removed\n")
	b.WriteString("    end\n")
}

const diffClassDefs = "    classDef kept fill:#f6f6f6,stroke:#aaaaaa,color:#444444\n" +
	"    classDef added fill:#e7f9e7,stroke:#1a9d1a,stroke-width:2px,color:#0a5d0a\n" +
	"    classDef removed fill:#fbeaea,stroke:#cc3333,stroke-width:2px,stroke-dasharray:5 3,color:#7d0a0a\n"
