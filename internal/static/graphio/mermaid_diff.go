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
// triple-cued: fill color, border style, and a +/−/Δ label prefix (so the diff
// survives greyscale printing and colorblindness).
//
// There are FOUR element states, not three: beyond added/removed/kept, a SURVIVING
// element whose ATTRIBUTES drifted is the `changed` class (amber, Δ) — a node whose
// tier moved, or a (from,to) pair whose record set changed (became concurrent, gained
// or lost a `via`, moved tier). It is DERIVED from the same graphio.Delta the JSON
// `flowmap graph --diff` emits (parity-tested), so the human view and the machine view
// cannot disagree on what changed. A changed edge keeps the kept ARROW SHAPE (shape is
// reserved for the add/remove/kept channel) and carries a Δ attribute label.
//
// It shares the base renderer's invariants through the same helpers (collectEmitsEffect,
// keepNode, edgeDecoration, the boundary/id/label helpers) so the two renderers cannot
// drift on "an effect emitter is never hidden", boundary shaping, or edge annotation.

import (
	"sort"
	"strconv"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/static/frontier"
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

// classFor maps a node/boundary state to its CSS class. A kept element gets the
// neutral grey, so the delta owns the whole color budget and a kept node can never
// be mistaken for a changed one.
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
// in Mermaid, but a node or edge that is part of the delta — added, removed, OR
// attribute-changed — is ALWAYS shown, whatever its tier: the diff must never hide the
// very thing it is diffing (a tier-changed node may itself now exceed MaxTier).
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

	// The attribute-aware delta is the ONE source of what changed; the mermaid `changed`
	// class is DERIVED from it (and parity-tested against the JSON delta) so the two views
	// cannot disagree on what changed. nodeChanged/pairChanged index it for the loops below.
	d := Delta(base, branch)
	nodeChanged := make(map[string]NodeChange, len(d.NodesChanged))
	for _, nc := range d.NodesChanged {
		nodeChanged[nc.FQN] = nc
	}
	pairChanged := map[ekey][]EdgeChange{}
	for _, ec := range d.EdgesChanged {
		pairChanged[ekey{ec.From, ec.To}] = append(pairChanged[ekey{ec.From, ec.To}], ec)
	}

	// A node that emits a boundary effect on EITHER side is never hidden (shared rule with
	// the base renderer). Force-show every delta-touched element so a change is never
	// collapsed away by tier folding: any endpoint of an added/removed OR attribute-changed
	// edge, plus any node whose own tier drifted (which may itself now sit above MaxTier).
	emitsEffect := collectEmitsEffect(base.Edges)
	for fqn := range collectEmitsEffect(branch.Edges) {
		emitsEffect[fqn] = true
	}
	allEdges := unionEdgeKeys(baseE, branchE)
	changedEndpoint := map[string]bool{}
	for _, k := range allEdges {
		_, inB := baseE[k]
		_, inBr := branchE[k]
		if stateOf(inB, inBr) == stKept {
			continue
		}
		changedEndpoint[k.from] = true
		if !isBoundary(k.to) {
			changedEndpoint[k.to] = true
		}
	}
	for k := range pairChanged {
		changedEndpoint[k.from] = true
		if !isBoundary(k.to) {
			changedEndpoint[k.to] = true
		}
	}
	for fqn := range nodeChanged {
		changedEndpoint[fqn] = true
	}
	// Boundary-target state, precomputed in two single passes (O(E)) rather than a
	// per-target rescan of both edge maps (the old O(boundary×edges)).
	boundaryState := boundaryTargetStates(baseE, branchE)

	ids := &idAlloc{used: map[string]bool{}}
	reserveLegendIDs(ids)

	nodeKeys := unionNodeKeys(baseN, branchN) // computed once, ranged twice below

	// Pass A: choose which first-party nodes to show, and assign ids in union order.
	// Only KEPT nodes are eligible for tier-hiding; an added/removed node is always
	// shown. Tally added/removed here so the over-cap summary reuses these counts
	// rather than recomputing the union.
	nodeID := map[string]string{}
	shown := map[string]bool{}
	hidden, added, removed := 0, 0, 0
	for _, fqn := range nodeKeys {
		st := nodeStateOf(fqn)
		switch st {
		case stAdded:
			added++
		case stRemoved:
			removed++
		}
		if st == stKept && !keepNode(pick(fqn), opts.MaxTier, emitsEffect, changedEndpoint) {
			hidden++
			continue
		}
		nodeID[fqn] = ids.get(frontier.ShortName(fqn))
		shown[fqn] = true
	}

	// Boundary effect nodes, once per label, in canonical edge order, with their diff
	// state from the precomputed map.
	type bnode struct {
		id, label, class string
		state            diffState
	}
	bIDs := map[string]string{}
	var bnodes []bnode
	seenBoundary := map[string]bool{}
	for _, k := range allEdges {
		if !isBoundary(k.to) || !shown[k.from] || seenBoundary[k.to] {
			continue
		}
		seenBoundary[k.to] = true
		label, class := boundaryShape(k.to)
		id := ids.get(class + "_" + label)
		bIDs[k.to] = id
		bnodes = append(bnodes, bnode{id: id, label: label, class: class, state: boundaryState[k.to]})
	}

	// Cap on the FULL drawn-node count (first-party union + boundary effects), not the
	// first-party count alone — a refactor-scale delta or a wide effect fan-out is the
	// hairball worst case. Summarize the delta (reusing the pass-A counts) instead.
	if opts.MaxNodes > 0 && len(shown)+len(bnodes) > opts.MaxNodes {
		return diffOverview(base, branch, opts, len(shown)+len(bnodes), added, removed, len(nodeChanged)+len(pairChanged))
	}

	var b strings.Builder
	writeDiffHeader(&b, base, branch)
	if hidden > 0 {
		b.WriteString("    %% " + plural(hidden, "unchanged node") +
			" above tier " + strconv.Itoa(opts.MaxTier) + " hidden; changed nodes are always shown\n")
	}
	writeLegend(&b)

	// First-party node declarations, in union order.
	for _, fqn := range nodeKeys {
		id, ok := nodeID[fqn]
		if !ok {
			continue
		}
		st := nodeStateOf(fqn)
		class := classFor(st)
		name := mermaidText(frontier.ShortName(fqn))
		label := prefixFor(st) + name
		// A kept node whose tier drifted is the third class: neutral kept grey would hide
		// the change, so it recolors to `changed` and names the drift (Δ … tier 2→1). Only
		// a KEPT node can be changed — an added/removed node already owns its own color.
		if nc, ok := nodeChanged[fqn]; ok && st == stKept {
			class = classChanged
			label = changedPrefix + name + " " + nc.Field + " " + renderDeltaVal(nc.Old) + "→" + renderDeltaVal(nc.New)
		}
		if pick(fqn).Fallible {
			label += " ⚠"
		}
		b.WriteString("    " + id + `["` + label + `"]:::` + class + "\n")
	}
	for _, bn := range bnodes {
		open, close := boundaryDelims(bn.class)
		// Color follows the DIFF state, never the effect kind: a kept effect node is
		// neutral grey (the delta owns the color budget), a changed one recolors. The
		// shape (cylinder/hexagon/stadium) still conveys db/bus/external. Using the
		// kind class here would also reference a classDef the diff palette never defines.
		b.WriteString("    " + bn.id + open + `"` + prefixFor(bn.state) + mermaidText(bn.label) + `"` + close + ":::" + classFor(bn.state) + "\n")
	}

	// Edges, in union order; collect link indices per state for linkStyle coloring.
	var idx int
	var addedIdx, removedIdx, keptIdx, changedIdx []int
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
		eBr, inBr := branchE[k]
		eBase, inBase := baseE[k]
		e := eBr
		if !inBr {
			e = eBase
		}
		st := stateOf(inBase, inBr)
		// A surviving pair whose attribute record SET drifted is the edge third class: draw
		// the kept-shape (solid) arrow so the persist/add/remove channel still reads "this
		// call stayed", but label it Δ with the changed attributes and recolor it changed.
		if changes, ok := pairChanged[k]; ok && st == stKept {
			b.WriteString("    " + diffEdgeChangedLine(fromID, toID, edgeDeltaLabel(changes)) + "\n")
			changedIdx = append(changedIdx, idx)
			idx++
			continue
		}
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
	writeLinkStyle(&b, changedIdx, changedLinkStyle)
	b.WriteString(diffClassDefs)
	return b.String()
}

// provenanceCaveats warns when base and branch were not built on the same substrate,
// so a reviewer never reads a substrate difference as a code change. groundwork's
// JSON comparison gates on this (review/provenance.go); the visual view must at least
// DISCLOSE it — a substrate mismatch silently painted as added/removed edges would be
// exactly the confidently-wrong delta the prime directive forbids. Empty Algo/Tool on
// either side (a committed, tool-stripped golden) is treated as "unrecorded", not a
// mismatch, so a golden-vs-golden diff stays caveat-free and byte-stable.
func provenanceCaveats(base, branch *Graph) []string {
	var out []string
	// An empty base renders the whole branch as added — correct for a NEW service, but
	// also what a wrong --diff base would produce. Disclose it so the reading is
	// unambiguous rather than refusing the legitimate new-service case.
	if len(base.Nodes) == 0 && len(base.Edges) == 0 {
		out = append(out, "base graph is empty — the whole branch shows as added (a new service, or a wrong --diff base?)")
	}
	if base.Algo != "" && branch.Algo != "" && base.Algo != branch.Algo {
		out = append(out, "algo differs (base "+base.Algo+" vs branch "+branch.Algo+
			"): edges differing only by analysis precision show as added/removed, not code changes")
	}
	if base.Tool != "" && branch.Tool != "" && base.Tool != branch.Tool {
		out = append(out, "producer tool differs (base "+base.Tool+" vs branch "+branch.Tool+
			"): 'same code → same graph' holds only within one tool version")
	}
	// A reclaimer (--reclaim / --reclaim-sql) ADDS provenance-tagged `via` edges that
	// a plain build never had; diffing a reclaimed branch against an un-reclaimed base
	// (or vice versa) paints those recovered edges as added/removed. The flag is not in
	// the graph, but its footprint is: any `via`-tagged edge. Disclose an asymmetry.
	if hasViaEdge(base) != hasViaEdge(branch) {
		out = append(out, "reclaimer state differs (one side carries provenance-tagged 'via' edges, "+
			"the other does not): reclaimer-recovered edges show as added/removed, not code changes")
	}
	return out
}

// hasViaEdge reports whether any edge carries a reclaimer `via` tag — the footprint
// of a --reclaim/--reclaim-sql build, used to flag a base↔branch reclaimer mismatch.
func hasViaEdge(g *Graph) bool {
	for _, e := range g.Edges {
		if e.Via != "" {
			return true
		}
	}
	return false
}

// writeDiffHeader emits the shared diff header — the `flowchart LR` line, the
// "base → branch" banner, and the provenance caveats — for both the full diff and the
// over-cap summary, so the substrate-mismatch disclosure (the honesty channel for a
// base↔branch skew) is emitted IDENTICALLY on both paths and cannot drift (CLAUDE.md:
// one source of truth).
func writeDiffHeader(b *strings.Builder, base, branch *Graph) {
	b.WriteString("flowchart LR\n")
	b.WriteString("    %% call-graph diff — base → branch (a view, never a gate)\n")
	for _, c := range provenanceCaveats(base, branch) {
		b.WriteString("    %% ⚠ " + comment(c) + "\n")
	}
}

// diffOverview renders the over-cap summary: a refactor-scale delta is an illegible
// hairball, so disclose the added/removed counts (reused from MermaidDiff's pass A —
// not recomputed) PLUS the changed count (nodes with a tier drift + attribute-changed
// pairs, from the Delta) and the provenance caveats, rather than drawing it. Disclosing
// the changed count is load-bearing: the third class must not vanish silently when the
// diagram truncates. A valid, deterministic single-node diagram. drawn is the full
// first-party+boundary node count.
func diffOverview(base, branch *Graph, opts MermaidOptions, drawn, added, removed, changed int) string {
	ids := &idAlloc{used: map[string]bool{}}
	var b strings.Builder
	writeDiffHeader(&b, base, branch)
	b.WriteString("    %% " + strconv.Itoa(drawn) + " nodes exceed the render cap (" +
		strconv.Itoa(opts.MaxNodes) + "); summarizing the delta instead\n")
	// The changed count rides alongside added/removed so a truncated delta never HIDES that
	// attributes drifted — silent truncation of the third class would be exactly the
	// fail-open (a semantic change invisible in the view) this phase closes.
	msg := "⚠ large delta — " + strconv.Itoa(added) + " added, " + strconv.Itoa(removed) +
		" removed, " + strconv.Itoa(changed) + " changed across " + strconv.Itoa(drawn) +
		" nodes. Too large to draw legibly; review the JSON diff (flowmap graph --diff) or raise --max-nodes."
	b.WriteString("    " + ids.get("toobig") + `["` + mermaidText(msg) + `"]` + "\n")
	b.WriteString(diffClassDefs)
	return b.String()
}

// boundaryTargetStates precomputes the diff state of every boundary target in two
// single passes over the edge maps, so the per-target lookup is O(1) instead of a
// rescan of both maps per boundary node. Iteration order is irrelevant: it only fills
// a membership map keyed by the target label.
func boundaryTargetStates(baseE, branchE map[ekey]Edge) map[string]diffState {
	inBase, inBranch := map[string]bool{}, map[string]bool{}
	for k := range baseE {
		if isBoundary(k.to) {
			inBase[k.to] = true
		}
	}
	for k := range branchE {
		if isBoundary(k.to) {
			inBranch[k.to] = true
		}
	}
	out := make(map[string]diffState, len(inBase)+len(inBranch))
	for to := range inBase {
		out[to] = stateOf(true, inBranch[to])
	}
	for to := range inBranch {
		if _, ok := out[to]; !ok {
			out[to] = stateOf(false, true)
		}
	}
	return out
}

// diffEdgeLine styles an edge by its delta state: added is a thick arrow, removed a
// dotted arrow with a "removed" label, kept a plain thin arrow. The arrow SHAPE
// differs per state so the delta survives with no color (the linkStyle colors are
// an enhancement, not the only signal). Concurrency/async/via decorations from the
// underlying edge are preserved on kept and added edges via the shared edgeDecoration.
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

// diffEdgeChangedLine renders a surviving pair whose attributes drifted: the kept-shape
// (solid) arrow — so the persist/add/remove channel still says "this call stayed" — with a
// Δ label naming the changed attributes. Color comes from the changed linkStyle; the SHAPE
// is deliberately the plain kept arrow, since arrow shape is reserved for the diff-state
// channel (a changed edge is neither an added nor a removed one). The label is data (a
// `via` value can carry anything), so it is edge-label-safe.
func diffEdgeChangedLine(from, to, delta string) string {
	return from + " -->|" + changedPrefix + edgeLabelSafe(delta) + "| " + to
}

// edgeDeltaLabel joins a pair's changed attributes into "field old→new; …" in the delta's
// canonical (from,to,field)-sorted order, so the label is deterministic across runs.
func edgeDeltaLabel(changes []EdgeChange) string {
	parts := make([]string, len(changes))
	for i, c := range changes {
		parts[i] = c.Field + " " + renderDeltaVal(c.Old) + "→" + renderDeltaVal(c.New)
	}
	return strings.Join(parts, "; ")
}

// renderDeltaVal renders one delta value for a human-readable label: the unset ∅ (nil, the
// JSON null), a scalar, or a bracketed list for a field that varies across a pair's records.
// A concurrent `true` shows as the diagram's concurrency word `go` (the vocabulary the kept
// edges already use), so a lost/gained concurrent mode reads consistently with the rest of
// the flowchart. It is the human twin of the JSON old/new — the two mean the same fact.
func renderDeltaVal(v any) string {
	switch x := v.(type) {
	case nil:
		return "∅"
	case bool:
		if x {
			return "go"
		}
		return "∅"
	case int:
		return strconv.Itoa(x)
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = renderDeltaVal(e)
		}
		return "[" + strings.Join(parts, ",") + "]"
	default:
		return ""
	}
}

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
		parts[i] = strconv.Itoa(n)
	}
	b.WriteString("    linkStyle " + strings.Join(parts, ",") + " " + style + "\n")
}

// reserveLegendIDs claims the fixed legend ids so the node allocator can never
// collide with them.
func reserveLegendIDs(a *idAlloc) {
	for _, id := range legendIDs {
		a.used[id] = true
	}
}

// legendIDs is the single source for the legend node ids: reserveLegendIDs claims
// them and writeLegend emits them, so the two cannot drift into a collision.
var legendIDs = []string{"lg_kept", "lg_added", "lg_removed", "lg_changed"}

func writeLegend(b *strings.Builder) {
	b.WriteString("    subgraph legend[\"legend — base → branch\"]\n")
	b.WriteString("        direction LR\n")
	b.WriteString("        " + legendIDs[0] + "[\"unchanged\"]:::kept\n")
	b.WriteString("        " + legendIDs[1] + "[\"＋ added\"]:::added\n")
	b.WriteString("        " + legendIDs[2] + "[\"− removed\"]:::removed\n")
	b.WriteString("        " + legendIDs[3] + "[\"Δ changed (tier/attr)\"]:::changed\n")
	b.WriteString("    end\n")
}

// classChanged / changedPrefix / changedLinkStyle are the third-class cues — an amber fill,
// a Δ text prefix, and an amber stroke — a colorway distinct from added-green, removed-red,
// and kept-grey, so an attribute drift on a SURVIVING element (a call that stayed but became
// concurrent, a node whose tier moved) reads as neither added nor removed. Triple-cued like
// the other states (fill + Δ glyph + label text), so it survives greyscale and colorblindness.
const (
	classChanged     = "changed"
	changedPrefix    = "Δ "
	changedLinkStyle = "stroke:#d98e00,stroke-width:2px"
)

const diffClassDefs = "    classDef kept fill:#f6f6f6,stroke:#aaaaaa,color:#444444\n" +
	"    classDef added fill:#e7f9e7,stroke:#1a9d1a,stroke-width:2px,color:#0a5d0a\n" +
	"    classDef removed fill:#fbeaea,stroke:#cc3333,stroke-width:2px,stroke-dasharray:5 3,color:#7d0a0a\n" +
	"    classDef changed fill:#fff6e5,stroke:#d98e00,stroke-width:2px,color:#7a4d00\n"
