// Package reviewtriage is a PROTOTYPE human-reviewer triage surface: given the
// base and branch graphs of an MR, it partitions the *changed* functions into two
// zones for a human reviewer drowning in diff volume —
//
//   - VOUCHED: every path through the change is statically resolved, so the tool
//     can show the COMPLETE evidence (which entrypoints it is live behind, the exact
//     boundary-effect surface it can reach) and the reviewer can verify that evidence
//     against the code rather than re-derive it. "Don't take my word for it — here is
//     the proof, go check it."
//   - FOCUS: the change touches or sits behind a disclosed blind spot, so the tool
//     CANNOT give complete evidence. These are exactly where a reviewer's scarce
//     attention should go — both because the tool can't vouch and because a blind
//     spot is precisely where a hallucinated or poisoned understanding can hide.
//
// The zoning rule is deliberately tight (over-flagging FOCUS destroys the signal it
// exists to give):
//
//  1. FORWARD-ONLY. A change's trustworthiness is about what it can DO (its forward
//     cone to effects), not who can reach it. A blind spot in a CALLER does not make
//     the change unverifiable, so only forward-cone blind spots drive FOCUS; the
//     reverse-reach over-approximation (CoverOverApprox) merely qualifies the
//     "live behind" evidence.
//  2. SEVERITY-AWARE. The producer tags benign seams (a context.CancelFunc dispatch
//     and the like) "trivial"; those are set aside from the FOCUS decision — but
//     still disclosed, so a vouched change never silently over-claims completeness.
//  3. RANKED. Focus items are ordered by consequence (salience tier, then whether the
//     change can mutate state, then blast radius), so a reviewer with little time sees
//     the most consequential one first.
//
// PROTOTYPE scope/limits (ride with the report): the changed set is the set-based
// node/edge/effect delta (a new call *site* to an already-called target is not a new
// edge); the per-function evidence is a static blast radius (what the change COULD
// touch, not the route a given input takes); a VOUCHED change is vouched for STRUCTURE
// only (whether the resolved effects are the RIGHT ones is the reviewer's call); and a
// FOCUS blind spot is any in the forward cone, NOT yet distinguished by whether THIS
// MR introduced it (the planned three-state refinement).
package reviewtriage

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/impact"
	"github.com/jyang234/golang-code-graph/internal/groundwork/setutil"
	"github.com/jyang234/golang-code-graph/internal/sqlverb"
)

// ChangedFn is one changed function with the checkable evidence and, when the tool
// cannot fully vouch, the reasons why (empty Reasons ⇒ vouched). Deterministic: every
// field derives from sorted graph data.
type ChangedFn struct {
	FQN  string `json:"fqn"`
	Tier int    `json:"tier,omitempty"`

	// Evidence — the facts a reviewer can check against the code.
	Entrypoints     []string `json:"entrypoints,omitempty"`       // reverse-reach cover: the routes it is live behind
	CoverUpperBound bool     `json:"cover_upper_bound,omitempty"` // the cover crossed a reverse HighFanOut seam — context, NOT a focus reason (#1)
	Effects         []string `json:"effects,omitempty"`           // forward boundary effects it can reach (human-readable)

	// Reasons the tool cannot fully vouch (empty ⇒ vouched): forward-cone blind spots
	// of non-trivial severity, plus a forward HighFanOut over-approximation.
	Reasons           []string `json:"reasons,omitempty"`
	EffectsUpperBound bool     `json:"effects_upper_bound,omitempty"` // forward HighFanOut: the effect surface is an upper bound

	// BenignSeams are trivial-severity forward blind spots set aside from the FOCUS
	// decision (#2) but disclosed so a vouched change never over-claims completeness.
	BenignSeams []string `json:"benign_seams,omitempty"`
}

// Report is the two-zone triage of an MR's changed functions. Focus is ordered by
// consequence; Vouched stays in FQN order (it is the low-priority zone by definition).
type Report struct {
	BaseNodes   int         `json:"base_nodes"`
	BranchNodes int         `json:"branch_nodes"`
	Vouched     []ChangedFn `json:"vouched,omitempty"`
	Focus       []ChangedFn `json:"focus,omitempty"`
}

// Build computes the triage over the BRANCH graph (the post-merge reality the reviewer
// is judging). The FOCUS decision uses the FORWARD cone only (#1) and ignores
// trivial-severity seams (#2); the evidence (entrypoint cover, effect surface) comes
// from the full impact card. Focus is then ranked by consequence (#4).
func Build(base, branch *graph.Graph) Report {
	ix := graph.NewIndex(branch)
	tier := tierLookup(branch)
	var vouched, focus []ChangedFn
	for _, fqn := range changedFns(base, branch) {
		card := impact.ForNodes(ix, []string{fqn})                       // evidence: reverse cover + forward effects
		fwdBlind, fwdOver := impact.ForwardBlindSpots(ix, []string{fqn}) // decision: forward-only (#1)
		serious, benign := splitSeverity(fwdBlind)                       // set aside benign seams (#2)

		cf := ChangedFn{
			FQN:               fqn,
			Tier:              tier[fqn],
			Entrypoints:       card.Entrypoints,
			CoverUpperBound:   card.CoverOverApprox,
			Effects:           trimmedEffects(card.Effects),
			EffectsUpperBound: fwdOver,
			BenignSeams:       benignNotes(benign),
		}
		if len(serious) > 0 || fwdOver {
			cf.Reasons = seriousReasons(serious, fwdOver)
			focus = append(focus, cf)
		} else {
			vouched = append(vouched, cf)
		}
	}
	sortFocus(focus) // most consequential first (#4)
	return Report{
		BaseNodes:   len(base.Nodes),
		BranchNodes: len(branch.Nodes),
		Vouched:     vouched,
		Focus:       focus,
	}
}

// changedFns is the sorted set of branch functions the MR structurally moved: new
// functions, signature changes, and functions that gained an outgoing call or effect.
func changedFns(base, branch *graph.Graph) []string {
	baseSig := make(map[string]string, len(base.Nodes))
	for _, n := range base.Nodes {
		baseSig[n.FQN] = n.Sig
	}
	branchNode := make(map[string]bool, len(branch.Nodes))
	for _, n := range branch.Nodes {
		branchNode[n.FQN] = true
	}
	changed := map[string]bool{}
	for _, n := range branch.Nodes {
		if old, existed := baseSig[n.FQN]; !existed || old != n.Sig {
			changed[n.FQN] = true // new function, or its signature moved
		}
	}
	baseEdge := make(map[string]bool, len(base.Edges))
	for _, e := range base.Edges {
		baseEdge[e.From+"\x00"+e.To] = true
	}
	for _, e := range branch.Edges {
		// A function that gained a callee or a boundary effect changed behavior, even
		// if its own node is unchanged. Restrict to a real branch node so a synthetic
		// boundary endpoint is never mistaken for a changed function.
		if branchNode[e.From] && !baseEdge[e.From+"\x00"+e.To] {
			changed[e.From] = true
		}
	}
	return setutil.SortedKeys(changed)
}

// splitSeverity divides forward-cone blind spots into the focus-worthy (serious) and
// the producer-tagged-benign (trivial). Only Severity=="trivial" is benign; every
// other value — including the empty default that covers reflection, dynamic effects,
// and unresolved dispatch — is serious (#2 fails toward flagging, never hiding).
func splitSeverity(bs []graph.BlindSpot) (serious, benign []graph.BlindSpot) {
	for _, b := range bs {
		if b.Severity == "trivial" {
			benign = append(benign, b)
		} else {
			serious = append(serious, b)
		}
	}
	return serious, benign
}

// seriousReasons renders the focus reasons: each serious blind spot, then the forward
// over-approximation if present. The reverse-reach over-approximation is deliberately
// absent — it qualifies evidence, it does not make the change unverifiable (#1).
func seriousReasons(serious []graph.BlindSpot, effectsOverApprox bool) []string {
	var rs []string
	for _, b := range serious {
		rs = append(rs, blindReason(b))
	}
	if effectsOverApprox {
		rs = append(rs, "the reachable-effect surface is an UPPER BOUND — the forward reach crossed a shared dispatch seam (HighFanOut), so it may include effects of sibling code, not just this change")
	}
	return rs
}

// benignNotes renders the set-aside trivial seams as short disclosures, so a vouched
// change with a benign seam never claims a completeness it does not have.
func benignNotes(benign []graph.BlindSpot) []string {
	var out []string
	for _, b := range benign {
		site := b.Site
		if site == "" {
			site = "an undisclosed site"
		}
		out = append(out, fmt.Sprintf("%s at %s — producer-tagged trivial (a benign seam, e.g. a cancel-func dispatch)", b.Kind, site))
	}
	return out
}

// sortFocus orders the focus zone by consequence so scarce reviewer attention lands on
// the most consequential change first (#4): most-critical salience tier, then a change
// that can MUTATE state over a read-only one, then the larger blast radius, then FQN
// for a deterministic final tie-break.
func sortFocus(fs []ChangedFn) {
	sort.SliceStable(fs, func(i, j int) bool {
		if a, b := tierRank(fs[i].Tier), tierRank(fs[j].Tier); a != b {
			return a < b // lower tier number = more critical = first
		}
		if a, b := reachesMutating(fs[i].Effects), reachesMutating(fs[j].Effects); a != b {
			return a // a state-mutating change before a read-only one
		}
		if a, b := len(fs[i].Entrypoints), len(fs[j].Entrypoints); a != b {
			return a > b // bigger blast radius first
		}
		return fs[i].FQN < fs[j].FQN
	})
}

// tierRank orders salience tiers most-critical-first and sends the unset tier (0 —
// synthetic nodes, or graphs built before the field) to the back, so a real tier
// always outranks "unknown".
func tierRank(t int) int {
	if t <= 0 {
		return 1 << 30
	}
	return t
}

// reachesMutating is a RANKING-ONLY heuristic (no verdict rests on it): does the
// change's resolved effect surface include a write — a mutating SQL verb via the one
// shared sqlverb source, or a bus PUBLISH? A change that can mutate state outranks a
// read-only one when attention is scarce.
func reachesMutating(effects []string) bool {
	for _, e := range effects {
		// Labels are "db <OP> <table>", "bus PUBLISH <event>", "bus CONSUME <event>", …
		if f := strings.Fields(e); len(f) >= 2 && f[0] == "db" && sqlverb.Mutating(f[1]) {
			return true
		}
		if strings.Contains(e, "PUBLISH") {
			return true
		}
	}
	return false
}

// blindReason renders one blind spot as a reviewer-actionable sentence: what the tool
// cannot see, where, and the implicit thing to verify. Keyed on the disclosed Kind (the
// vocabulary flowmap emits); an unrecognized kind falls back to an honest generic rather
// than dropping the disclosure (fail loud).
func blindReason(b graph.BlindSpot) string {
	at := b.Site
	if at == "" {
		at = "an undisclosed site"
	}
	detail := ""
	if b.Detail != "" {
		detail = " (" + b.Detail + ")"
	}
	switch b.Kind {
	case "NonConstantBoundaryArg":
		return fmt.Sprintf("a boundary call with a NON-CONSTANT target at %s%s — the tool cannot tell which destination; verify the value", at, detail)
	case "UnresolvedDispatch", "UnresolvedCall":
		return fmt.Sprintf("a call through a function value the tool cannot resolve at %s%s — the actual callee, and what it does, is invisible here; verify it", at, detail)
	case "ConcurrentDispatch":
		return fmt.Sprintf("an unresolved goroutine dispatch at %s%s — concurrent behavior past this point is invisible to the tool", at, detail)
	case "DynamicEffect":
		return fmt.Sprintf("a DYNAMIC boundary effect at %s%s — the tool sees that an effect happens but not its full identity", at, detail)
	case "HighFanOut":
		return fmt.Sprintf("a dispatch site fanning to many possible targets at %s%s — the tool over-approximates here; confirm which target this change intends", at, detail)
	case "reflect":
		return fmt.Sprintf("reflection at %s%s — call structure here is invisible to static analysis", at, detail)
	case "unsafe", "cgo", "go:linkname":
		return fmt.Sprintf("%s at %s%s — bypasses the analyzable call graph", b.Kind, at, detail)
	case "ExternalBoundaryCall":
		return fmt.Sprintf("a call into a third-party package at %s%s — the tool cannot see inside it", at, detail)
	case "ImpeachmentSeam":
		return fmt.Sprintf("a behaviorally-proven blind spot at %s%s — runtime has shown this seam hides effects", at, detail)
	default:
		return fmt.Sprintf("%s at %s%s — the tool's view stops here", b.Kind, at, detail)
	}
}

// RenderMarkdown is the human-reviewer report: focus zone first (lead with where
// attention must go), then the vouched zone with its checkable evidence. A change with
// no structural movement yields an explicit "nothing to triage" rather than an empty
// page (silence is never a silent pass).
func (r Report) RenderMarkdown() string {
	var b strings.Builder
	v, f := len(r.Vouched), len(r.Focus)
	fmt.Fprintf(&b, "# MR review triage — where to spend your verification\n")
	fmt.Fprintf(&b, "_graph %d → %d nodes · %d changed function(s): %d need your eyes, %d the tool can vouch for_\n",
		r.BaseNodes, r.BranchNodes, v+f, f, v)

	if v+f == 0 {
		b.WriteString("\nNo structural change detected (body-only or no diff). The tool has nothing to triage here — that is not the same as \"safe\"; it means the change did not move the call graph, so verify behavior the usual way.\n")
		return b.String()
	}

	fmt.Fprintf(&b, "\n## ⚠️  Focus here — %d change(s) the tool CANNOT vouch for\n", f)
	if f > 0 {
		b.WriteString("_ordered by consequence: salience tier, then state-mutating, then blast radius_\n")
	} else {
		b.WriteString("_None — every changed path is statically resolved (benign seams aside). Still your call, but the tool has no blind spot to flag._\n")
	}
	for _, cf := range r.Focus {
		fmt.Fprintf(&b, "\n### %s%s\n", cf.FQN, tierTag(cf.Tier))
		b.WriteString("The tool cannot give you complete evidence here:\n")
		for _, reason := range cf.Reasons {
			fmt.Fprintf(&b, "- %s\n", reason)
		}
		writeEvidence(&b, cf, true)
	}

	fmt.Fprintf(&b, "\n## ✅ Vouched — %d change(s), fully resolved (check the evidence, don't take the tool's word)\n", v)
	if v == 0 {
		b.WriteString("_None — every changed function touches a blind spot above._\n")
	}
	for _, cf := range r.Vouched {
		fmt.Fprintf(&b, "\n### %s%s\n", cf.FQN, tierTag(cf.Tier))
		if len(cf.BenignSeams) == 0 {
			b.WriteString("Every path through this change is statically resolved — no dynamic dispatch, reflection, or opaque I/O on any reachable path. Evidence to verify against the code:\n")
		} else {
			b.WriteString("Statically resolved except a benign seam the producer tagged trivial; the effect surface is otherwise complete. Evidence to verify against the code:\n")
			for _, s := range cf.BenignSeams {
				fmt.Fprintf(&b, "- (set aside) %s\n", s)
			}
		}
		writeEvidence(&b, cf, false)
	}

	b.WriteString("\n---\n")
	b.WriteString("_Triage is the static MAP (what each change COULD touch), not the route a given input takes; and a vouched change is vouched for STRUCTURE only — whether the resolved effects are the RIGHT ones is your call. PROTOTYPE._\n")
	return b.String()
}

// writeEvidence prints the checkable facts of a change: the entrypoints it is live
// behind and the boundary-effect surface it reaches. partial marks the focus-zone case,
// where the same facts are a FLOOR (a blind spot may hide more), so the reviewer reads
// them as a floor, not the whole truth.
func writeEvidence(b *strings.Builder, cf ChangedFn, partial bool) {
	floor := ""
	if partial {
		floor = " (a FLOOR — the blind spot(s) above may hide more)"
	}

	coverNote := ""
	if cf.CoverUpperBound {
		coverNote = " ≤ (upper bound — reverse dispatch seam)"
	}
	if len(cf.Entrypoints) == 0 {
		fmt.Fprintf(b, "- live behind no discovered entrypoint%s\n", floor)
	} else {
		fmt.Fprintf(b, "- live behind %d entrypoint(s)%s%s:\n", len(cf.Entrypoints), coverNote, floor)
		for _, e := range cf.Entrypoints {
			fmt.Fprintf(b, "  - %s\n", e)
		}
	}

	switch {
	case len(cf.Effects) == 0 && !partial:
		b.WriteString("- reaches NO boundary effects — a pure/internal change (no DB, bus, or outbound I/O on any path)\n")
	case len(cf.Effects) == 0:
		b.WriteString("- no boundary effect resolved on the visible paths\n")
	default:
		surface := "the COMPLETE boundary-effect surface of this change"
		if partial {
			surface = "the boundary effects the tool CAN see"
		}
		fmt.Fprintf(b, "- reaches %d boundary effect(s) — %s%s:\n", len(cf.Effects), surface, floor)
		for _, e := range cf.Effects {
			fmt.Fprintf(b, "  - %s\n", e)
		}
	}
}

// tierTag is the salience badge beside a changed function (omitted for the unset tier).
func tierTag(t int) string {
	if t <= 0 {
		return ""
	}
	return fmt.Sprintf("  [tier %d]", t)
}

// tierLookup maps each branch function to its salience tier for ranking and display.
func tierLookup(g *graph.Graph) map[string]int {
	m := make(map[string]int, len(g.Nodes))
	for _, n := range g.Nodes {
		m[n.FQN] = n.Tier
	}
	return m
}

// trimmedEffects strips the internal "boundary:" prefix from each effect label for
// display — the same human-readable form the ground/triage cards use — keeping them
// sorted.
func trimmedEffects(effects []string) []string {
	out := make([]string, 0, len(effects))
	for _, e := range effects {
		out = append(out, strings.TrimPrefix(e, "boundary:"))
	}
	sort.Strings(out)
	return out
}
