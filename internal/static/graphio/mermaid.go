package graphio

// Mermaid renders the non-gated call-graph view as a Mermaid flowchart — the
// human-readable sibling of Marshal's JSON, the way render.Mermaid is the
// human-readable sibling of a trace golden. It is a pure, deterministic function
// of the Graph (itself already canonically sorted by Build), so renderer drift
// never pollutes a gate: like the JSON view this is "what can happen" for human
// understanding, never a verdict.
//
// Where the sequence diagram shows one observed flow, this shows the static
// over-approximation: every first-party call reachable from the scope, the typed
// boundary effects (DB / bus / external) as shaped leaf nodes, and — the thing no
// behavioral diagram can offer — the blind spots and frontier markers as explicit
// terminal nodes, so a reviewer sees exactly where the analysis STOPS seeing
// instead of mistaking an incomplete graph for a complete one (CLAUDE.md fail
// closed / self-honesty about blind spots).

import "strings"

// MermaidOptions tunes the flowchart render.
type MermaidOptions struct {
	// MaxTier, when > 0, hides first-party nodes whose salience tier EXCEEDS it —
	// the low-salience plumbing (telemetry, compute-only closures) that buries the
	// decisions and effects a reviewer is looking for. The hide is sound-by-omission
	// only: a node that EMITS a boundary edge, or that is the source of a shown call,
	// is never hidden, so the diagram never silently drops an effect; the count of
	// what was hidden is disclosed in a header comment. The zero value (0) hides
	// nothing — the library renders the complete graph by default, and the CLI opts
	// into denoising for humans.
	MaxTier int
}

// Mermaid renders g as a Mermaid flowchart per opts. The output begins with
// "flowchart LR\n" and ends with a newline, and is byte-identical across runs for
// a given (g, opts).
func (g *Graph) Mermaid(opts MermaidOptions) string {
	ids := &idAlloc{used: map[string]bool{}}

	// A node that emits a boundary effect is load-bearing: hiding it would drop the
	// effect from the diagram, which the trust model forbids. Collect those first so
	// the tier filter can never reach them.
	emitsEffect := map[string]bool{}
	for _, e := range g.Edges {
		if isBoundary(e.To) {
			emitsEffect[e.From] = true
		}
	}

	// Pass A: assign ids to the first-party nodes we will show.
	nodeID := make(map[string]string, len(g.Nodes))
	shown := make(map[string]bool, len(g.Nodes))
	hidden := 0
	for _, n := range g.Nodes {
		if opts.MaxTier > 0 && n.Tier > opts.MaxTier && !emitsEffect[n.FQN] {
			hidden++
			continue
		}
		nodeID[n.FQN] = ids.get(shortName(n.FQN))
		shown[n.FQN] = true
	}

	// Boundary effect nodes, created once per distinct label, in edge order (sorted
	// by Build) and only when their source is shown.
	type bnode struct {
		id, label, class string
	}
	bIDs := map[string]string{}
	var bnodes []bnode
	for _, e := range g.Edges {
		if !isBoundary(e.To) || !shown[e.From] {
			continue
		}
		if _, ok := bIDs[e.To]; ok {
			continue
		}
		label, class := boundaryShape(e.To)
		id := ids.get(class + "_" + label)
		bIDs[e.To] = id
		bnodes = append(bnodes, bnode{id: id, label: label, class: class})
	}

	// Disclosure nodes: graph-completeness blind spots and frontier markers, the
	// honesty channel. Each hangs off the first-party node it is attributed to (its
	// site / owner) when that node is shown, else stands alone — but it is ALWAYS
	// drawn, so the diagram cannot launder a blind spot into clean reachability.
	type disc struct {
		id, label, from string // from = source node id, or "" for a standalone
	}
	var discs []disc
	for _, b := range g.BlindSpots {
		id := ids.get("blind_" + string(b.Kind))
		discs = append(discs, disc{id: id, label: "⊥ " + string(b.Kind) + "<br/>blind spot", from: nodeID[b.Site]})
	}
	if g.Frontier != nil {
		for _, m := range g.Frontier.Markers {
			id := ids.get("frontier_" + m.Kind)
			label := "⌖ " + m.Kind + "<br/>frontier " + string(m.Bin)
			discs = append(discs, disc{id: id, label: label, from: nodeID[m.Owner]})
		}
	}

	var b strings.Builder
	b.WriteString("flowchart LR\n")
	scope := g.Entrypoint
	if scope == "" {
		scope = "whole graph"
	}
	b.WriteString("    %% static call graph — scope: " + comment(scope) + "; algo: " + comment(g.Algo) + "\n")
	if hidden > 0 {
		b.WriteString("    %% " + plural(hidden, "first-party node") +
			" above tier " + itoa(opts.MaxTier) + " hidden as plumbing; pass --show-plumbing to include\n")
	}

	// First-party node declarations, in canonical Node order.
	for _, n := range g.Nodes {
		id, ok := nodeID[n.FQN]
		if !ok {
			continue
		}
		label := shortName(n.FQN)
		if n.Fallible {
			label += " ⚠"
		}
		decl := "    " + id + `["` + label + `"]`
		if n.Fallible {
			decl += ":::fallible"
		}
		b.WriteString(decl + "\n")
	}
	for _, bn := range bnodes {
		open, close := boundaryDelims(bn.class)
		b.WriteString("    " + bn.id + open + `"` + bn.label + `"` + close + ":::" + bn.class + "\n")
	}
	for _, d := range discs {
		b.WriteString("    " + d.id + `(["` + d.label + `"]):::blind` + "\n")
	}

	// Edges, in canonical Edge order. A boundary edge draws to its effect node; a
	// first-party edge draws only when both endpoints are shown.
	for _, e := range g.Edges {
		if isBoundary(e.To) {
			to, ok := bIDs[e.To]
			if !ok {
				continue
			}
			b.WriteString("    " + edgeLine(nodeID[e.From], to, e) + "\n")
			continue
		}
		from, okF := nodeID[e.From]
		to, okT := nodeID[e.To]
		if !okF || !okT {
			continue
		}
		b.WriteString("    " + edgeLine(from, to, e) + "\n")
	}
	// Disclosure attachments: a dashed link from the attributed node to its blind
	// spot / frontier marker, so the reviewer reads it as "here the graph goes dark".
	for _, d := range discs {
		if d.from == "" {
			continue
		}
		b.WriteString("    " + d.from + " -. blind .-> " + d.id + "\n")
	}

	b.WriteString(classDefs)
	return b.String()
}

const classDefs = "    classDef fallible stroke:#c44,stroke-width:2px\n" +
	"    classDef db fill:#eef,stroke:#66a\n" +
	"    classDef bus fill:#efe,stroke:#5a5\n" +
	"    classDef external fill:#fef6e8,stroke:#c93\n" +
	"    classDef blind fill:#fde,stroke:#c33,stroke-dasharray:3 3\n"

// isBoundary reports whether a To label is a typed boundary effect node rather
// than a first-party callee. Build emits these with the "boundary:" prefix.
func isBoundary(to string) bool { return strings.HasPrefix(to, "boundary:") }

// boundaryShape returns the display label (the part after "boundary:") and the
// CSS class keyed off the effect kind: bus, db, or an external host.
func boundaryShape(to string) (label, class string) {
	label = strings.TrimPrefix(to, "boundary:")
	switch {
	case strings.HasPrefix(label, "bus "):
		return label, "bus"
	case strings.HasPrefix(label, "db "):
		return label, "db"
	default:
		return label, "external"
	}
}

// boundaryDelims gives the Mermaid node-shape delimiters per effect class: bus a
// hexagon, db a cylinder, external a stadium — mirroring render.SystemGraph so the
// two flowcharts read with one visual vocabulary.
func boundaryDelims(class string) (open, close string) {
	switch class {
	case "bus":
		return "{{", "}}"
	case "db":
		return "[(", ")]"
	default:
		return "([", "])"
	}
}

// edgeLine renders one edge. Concurrent (`go`) and outbound-async hops are dashed
// to set them apart from synchronous calls (the flowchart analogue of the sequence
// renderer's `--)` async arrow); a reclaimed edge carries its `via` provenance as
// the link label so a reviewer can see which seam-reclaimer recovered it.
func edgeLine(from, to string, e Edge) string {
	dashed := e.Concurrent || e.Boundary == "outbound-async"
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
	if dashed {
		if text == "" {
			return from + " -.-> " + to
		}
		return from + " -. " + text + " .-> " + to
	}
	if text == "" {
		return from + " --> " + to
	}
	return from + " -->|" + text + "| " + to
}

// shortName trims a fully-qualified name to a readable label: the package leaf,
// the receiver type, and the method/function — dropping the module-path prefix and
// the SSA pointer/paren noise. It is lossy by design (two functions of the same
// name in different deep packages could collide), but the id allocator keeps the
// Mermaid ids distinct, so only the human-facing label collapses.
func shortName(fqn string) string {
	s := strings.TrimPrefix(fqn, "(*")
	s = strings.TrimPrefix(s, "(")
	if i := strings.LastIndex(s, "/"); i >= 0 {
		s = s[i+1:]
	}
	return strings.ReplaceAll(s, ")", "")
}

// comment sanitizes a string for use inside a Mermaid %% comment: newlines would
// terminate the comment line and corrupt the diagram, so they are folded to spaces.
func comment(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " ")
}

// idAlloc hands out Mermaid-safe, collision-free node ids from human seeds,
// deterministically: the same sequence of seeds always yields the same ids.
type idAlloc struct{ used map[string]bool }

func (a *idAlloc) get(seed string) string {
	base := sanitizeID(seed)
	id := base
	for i := 2; a.used[id]; i++ {
		id = base + "_" + itoa(i)
	}
	a.used[id] = true
	return id
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	id := b.String()
	if id == "" {
		id = "n"
	}
	if id[0] >= '0' && id[0] <= '9' {
		id = "n" + id
	}
	return id
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func plural(n int, noun string) string {
	s := itoa(n) + " " + noun
	if n != 1 {
		s += "s"
	}
	return s
}
