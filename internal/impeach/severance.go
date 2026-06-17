package impeach

import (
	"sort"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/canon/opkey"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/ir"
)

// Severance localization (plan §6), at the L0 resolution (§7 harness levels):
// entry+effect anchors only, no internal-span FQN reconciliation. It answers
// WHERE the static over-approximation lost the observed effect, by projecting the
// coarse observed (entry → effect) anchor pair onto the graph and finding the
// first hop static cannot reproduce. The result is a coarse Site plus the
// known/unknown frontier sort (§6) — disclosure-only at this phase, never enacted
// (the repair-writing loop is Phase 4, §8).
//
// L0 soundness (§7): the Site is COARSER than L1/L2 (it cannot point inside the
// handler cone), but it is never a GUESS — every anchor is a real graph node (or
// the entry registration literal), and the proof obligation below refuses to
// fabricate a seam where none exists. Precision is a dial; soundness is invariant.

// Severance kinds (§6, the "three flavors classified for free"). Each names the
// shape of the broken link the L0 walk found, so a reader knows what to repair
// without re-deriving it.
const (
	// SeveranceMissedRoot: the observed entry is not a discovered entrypoint, so
	// the graph models no root for this flow at all — the entry REGISTRATION site
	// is the seam (an unhinted router, a framework registration static did not
	// model). Site is the entry route literal; EntryDiscovered is false.
	SeveranceMissedRoot = "missed-root"

	// SeveranceSeveredEmitter: the entry IS a discovered root and the effect's
	// emitter IS a graph node, but no discovered root reaches it — the break is
	// upstream of the emitter, a dispatch seam somewhere on entry→emitter that L0
	// cannot resolve finer. Site is the entry function (the upstream anchor).
	SeveranceSeveredEmitter = "severed-emitter"

	// SeveranceUnmodeledEffect: the graph models NO emitter for the effect at all
	// (the ReachAbsent candidate), reached from a discovered entry — static could
	// not model or label the effect itself. Site is the entry function whose cone
	// the effect escaped.
	SeveranceUnmodeledEffect = "unmodeled-effect"

	// SeveranceNone: the proof obligation FAILED — the effect is statically
	// reproducible along the observed anchors, so the "unreachable" Claim was a
	// mis-read and there is no real contradiction to localize (§6). Recorded with
	// NO Site (never a fabricated seam); Phase 5 verdict integration must treat
	// this as non-impeaching. It cannot arise from a Phase-0 candidate by
	// construction (an emitter a discovered root reaches is CONFIRMED-LIVE, not a
	// candidate) — the rung is the explicit fail-closed guard if it ever does.
	SeveranceNone = "no-severance"
)

// Severance is the L0 localization attached to a candidate witness (§6): the
// coarse severance Site, the flavor of the break, and the known/unknown frontier
// sort. It is a pure function of (witness, graph) — every field resolves on
// intrinsic graph data (entrypoint join, emitter node, reachability, frontier
// markers), never on arrival order — so it rides the byte-identical report.
type Severance struct {
	// Site is the coarse L0 severance point: the entry registration literal (a
	// missed root), the entry function (a severed emitter / unmodeled effect), or
	// "" (the proof obligation failed — no fabricated seam). The repair-proposal
	// loop (§8) will target this; Phase 2 only records it.
	Site string `json:"site"`

	// Kind is the break flavor (SeveranceMissedRoot | SeveranceSeveredEmitter |
	// SeveranceUnmodeledEffect | SeveranceNone), classified for free from whether
	// the entry mapped and whether an emitter exists (§6).
	Kind string `json:"kind"`

	// FrontierKnown sorts the value (§6): true when a static frontier marker or a
	// disclosed blind spot already covers Site — behavior confirms a DISCLOSED seam
	// (a "the negative should have respected the frontier" bug). false is the
	// high-value discovery — an UNDISCLOSED blind spot static did not know it had.
	FrontierKnown bool `json:"frontier_known"`

	// Anchors is the ordered anchor chain the walk mapped onto the graph — at L0
	// the discovered entry function then the effect emitter, at L1 the precise node
	// chain entry→…→emitter resolved through the span map (§7). An entry that did
	// not map (missed root) is omitted, and an unmodeled effect contributes no
	// emitter, so the chain length itself is a signal (§6's EntryDiscovered /
	// missing-emitter distinction).
	Anchors []string `json:"anchors,omitempty"`

	// Level is the harness-investment level the Site was resolved at (§7): "L0"
	// (entry+effect anchors only — coarse but sound) or "L1" (precise, resolved
	// through `flowmap.fqn` tags on the causal path). Recorded as provenance: the
	// SAME report shape carries either, and a reader knows the Site's resolution.
	Level string `json:"level"`

	// AbsentFromGraph is the sharp L1 signal (§7): an internal span whose FQN tag
	// keys to a function the graph has NO node for — a directly localized missing
	// node, sharper than the walk. It is a WEAK HINT at L1 (it is sound only once
	// canonFQN ⊥-symmetry is fuzz-proven, §12.5), so it rides as disclosure beside
	// the walk's Site, never replacing it. "" when no internal tag was absent.
	AbsentFromGraph string `json:"absent_from_graph,omitempty"`
}

// Harness-investment levels (§7), recorded as Severance.Level provenance.
const (
	LevelL0 = "L0"
	LevelL1 = "L1"
)

// localize runs the severance walk (§6) over one candidate and returns its
// Severance plus whether the proof obligation HELD (a real contradiction exists).
// ok == false (Kind SeveranceNone) means the effect was statically reproducible
// from the observed entry — the Claim was a mis-read, so the caller must disclose
// a self-inconsistency rather than localize a seam (§6: never a fabricated Site).
//
// It resolves at the highest level the corpus supports (§7): when the candidate's
// causal path carries `flowmap.fqn` tags that map to graph nodes, the precise L1
// walk finds the first PATH NODE severed from every root — a node Site a repair
// can blind (the self-extinguish handle, §6/§8). Absent tags, it falls back to the
// coarse L0 walk (entry+effect anchors). The Site is sound at either level; the
// level only sets its resolution. nx and rootReach are built once per audit by the
// caller and shared across candidates.
//
// discovered is also returned so the caller can stamp Observation.EntryDiscovered
// without re-running the entrypoint join.
func localize(w Witness, ix *graph.Index, nx *nodeIndex, rootReach map[string]bool) (sev Severance, discovered, ok bool) {
	emitters := staticEmitters(ix, w.Effect)
	entryFn := mapEntry(ix, w.Observed.Entry)
	discovered = entryFn != ""

	if l1, hit, l1ok := localizeL1(w, ix, nx, rootReach, emitters, entryFn, discovered); hit {
		return l1, discovered, l1ok
	}

	switch {
	case !discovered:
		// The entry is not a graph root: the registration site is the seam,
		// regardless of whether an emitter is modeled (§6, EntryDiscovered=false).
		// The emitter (if any) is unreachable from every discovered root by the
		// candidate's construction, so the missed root is the real contradiction.
		sev = Severance{Site: w.Observed.Entry, Kind: SeveranceMissedRoot, Anchors: emitters, Level: LevelL0}
	case w.Claim.Reachability == ReachAbsent:
		// A discovered root, but the graph models no emitter at all — the effect
		// itself is unmodeled (§6, "break at the emitter" with no emitter node).
		sev = Severance{Site: entryFn, Kind: SeveranceUnmodeledEffect, Anchors: []string{entryFn}, Level: LevelL0}
	default:
		// A discovered root AND a modeled emitter, but no root reaches it. The
		// proof obligation: confirm THIS entry does not reach any emitter either.
		// It cannot, by construction (a reached emitter is CONFIRMED-LIVE), but the
		// search IS the verification — a reproducible effect here is a self-
		// inconsistency, not an impeachment (§6).
		reach := reachSetOf(ix, []string{entryFn})
		for _, e := range emitters {
			if reach[e] {
				return Severance{Site: "", Kind: SeveranceNone, Level: LevelL0}, discovered, false
			}
		}
		anchors := []string{entryFn}
		anchors = append(anchors, emitters...)
		sev = Severance{Site: entryFn, Kind: SeveranceSeveredEmitter, Anchors: anchors, Level: LevelL0}
	}

	sev.FrontierKnown = frontierKnown(ix, sev.Site)
	return sev, discovered, true
}

// localizeL1 attempts the precise §6/§7 walk over the candidate's causal span
// chain. hit is false when the corpus offers no L1 signal (no internal span maps
// to a node and none is absent-from-graph) — the caller then falls back to L0.
//
// The walk maps each internal span to a graph node and finds the FIRST mapped node
// that is (a) severed from every root and (b) able to reach an emitter — the
// outermost point on the OBSERVED path where static lost the effect. That node is
// the Site: blinding it puts the emitter in the disclosed-seam cone, so the
// candidate self-extinguishes (§6/§8). A sharp `absent-from-graph` tag (an
// internal node the graph lacks) rides along as a weak L1 hint (§7/§12.5),
// disclosed beside the Site, never replacing it.
func localizeL1(w Witness, ix *graph.Index, nx *nodeIndex, rootReach map[string]bool, emitters []string, entryFn string, discovered bool) (Severance, bool, bool) {
	// reachesEmitter: every node that can reach a modeled emitter (the emitters
	// included). A path node here whose forward cone holds the effect is a valid
	// blind-spot Site.
	reachesEmitter := map[string]bool{}
	for _, e := range append(ix.Reaching(emitters...), emitters...) {
		reachesEmitter[e] = true
	}

	var anchors []string
	absentHint := ""
	sawSignal := false
	severed := "" // the first path node severed from every root
	for _, s := range internalSpans(w.chain) {
		a := nx.mapInternal(s)
		switch a.Outcome {
		case MapMapped:
			sawSignal = true
			anchors = append(anchors, a.Node)
			if severed == "" && !rootReach[a.Node] && reachesEmitter[a.Node] {
				severed = a.Node
			}
		case MapAbsentFromGraph:
			sawSignal = true
			if absentHint == "" {
				absentHint = a.Tag
			}
		}
	}
	if !sawSignal {
		return Severance{}, false, false // no L1 signal — fall back to L0
	}

	// The proof obligation at L1: if a discovered root reaches every mapped anchor
	// AND an emitter (no severed node, no absent hint), the effect is reproducible
	// along the observed path — a self-inconsistency, not an impeachment (§6).
	if severed == "" && absentHint == "" {
		reproducible := discovered
		for _, n := range anchors {
			if !rootReach[n] {
				reproducible = false
				break
			}
		}
		if reproducible {
			return Severance{Site: "", Kind: SeveranceNone, Level: LevelL1}, true, false
		}
	}

	site := severed
	kind := SeveranceSeveredEmitter
	if site == "" {
		// No mapped severed node carries the effect (only an absent-from-graph hint,
		// or the chain mapped past the seam): fall to the coarse anchor — the entry
		// when discovered, else the registration literal (missed root).
		if discovered {
			site = entryFn
		} else {
			site = w.Observed.Entry
			kind = SeveranceMissedRoot
		}
	} else if !discovered {
		kind = SeveranceMissedRoot
	}

	chain := []string{}
	if discovered {
		chain = append(chain, entryFn)
	}
	chain = append(chain, anchors...)
	chain = append(chain, emitters...)

	return Severance{
		Site:            site,
		Kind:            kind,
		FrontierKnown:   frontierKnown(ix, site),
		Anchors:         chain,
		Level:           LevelL1,
		AbsentFromGraph: absentHint,
	}, true, true
}

// internalSpans is the causal chain with the entry (root) and effect (terminal)
// spans dropped — the spans whose FQN tags the L1 map anchors (§7's "internal"
// class). The entry maps via the route join and the effect via its label, so only
// the middle of the chain needs the tag.
func internalSpans(chain []*ir.CanonicalSpan) []*ir.CanonicalSpan {
	if len(chain) <= 2 {
		return nil
	}
	return chain[1 : len(chain)-1]
}

// staticEmitters returns the sorted, deduped first-party FQNs the graph models as
// emitting the effect key — the bus PUBLISH or DB op the key names. Empty when the
// graph models no emitter (a ReachAbsent candidate). Reused decoders (BusEffects /
// DBEffects) keep the boundary-label vocabulary with the schema owner, and the key
// reconciliation goes through the one-source DBEffectKey / bus op key, so an
// emitter is matched like-with-like (the same parity the join itself relies on).
func staticEmitters(ix *graph.Index, effect string) []string {
	seen := map[string]bool{}
	switch {
	case isDBKey(effect):
		dbEffs, _ := ix.DBEffects()
		for _, de := range dbEffs {
			if DBEffectKey(de.Op, de.Table) == effect {
				seen[de.From] = true
			}
		}
	case isBusKey(effect):
		busEffs, _ := ix.BusEffects()
		for _, be := range busEffs {
			if be.Op == graph.BusPublish && graph.BusPublish+" "+be.Event == effect {
				seen[be.From] = true
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

// mapEntry projects the observed entry op onto a discovered entrypoint and returns
// its handler FQN, or "" when the graph models no such root (a missed root, §6).
// The reconciliation is structural: the canonical entry op is "HTTP <METHOD>
// <route>" or "CONSUME <topic>" (opkey.Of for a server/consumer span), while an
// Entrypoint.Name is the bare "<METHOD> <route>" / "<topic>" — so the op key's
// prefix is stripped and the remainder matched against Name. It never guesses: an
// op with no recognized entry prefix, or no matching entrypoint, yields "".
func mapEntry(ix *graph.Index, entryOp string) string {
	var name string
	switch {
	case strings.HasPrefix(entryOp, opkey.HTTPPrefix):
		name = strings.TrimPrefix(entryOp, opkey.HTTPPrefix)
	case strings.HasPrefix(entryOp, opkey.ConsumePrefix):
		name = strings.TrimPrefix(entryOp, opkey.ConsumePrefix)
	default:
		return ""
	}
	for _, ep := range ix.Entrypoints() {
		if ep.Name == name && ep.Fn != "" {
			return ep.Fn
		}
	}
	return ""
}

// frontierKnown reports whether the graph already DISCLOSES a seam at site — a
// frontier marker (by site or reclaim owner) or a blind spot there. This is the
// §14-D markerAt lookup over the shipped frontier section (Index.Frontier) plus
// the blind-spot manifest. A known site means behavior confirms a seam static
// already admitted (lower value); an unknown site is the undisclosed blind spot
// the cell exists to discover (§3). The entry-literal Site of a missed root is
// never a graph node, so it is correctly unknown — an undisclosed missed root.
func frontierKnown(ix *graph.Index, site string) bool {
	if site == "" {
		return false
	}
	if len(ix.BlindSpotsAt(site)) > 0 {
		return true
	}
	if fr := ix.Frontier(); fr != nil {
		for _, m := range fr.Markers {
			if m.Site == site || m.Owner == site {
				return true
			}
		}
	}
	return false
}
