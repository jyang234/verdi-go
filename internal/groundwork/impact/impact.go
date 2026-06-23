// Package impact assembles the incident-triage card: given suspect functions,
// the bidirectional blast radius an incident responder needs — which
// entrypoints are implicated (reverse reach), which external effects are in
// play (forward reach), and where the graph's own knowledge stops being sound
// (blind spots on any traversed path). It is the IT-0 engine from the
// incident-triage plan: pure composition over the existing graph index, a pure
// function of (graph, suspects), no policy and no verdict — the card is
// evidence, not judgment.
//
// The honest limits ride with the card: a static blast radius is the MAP (what
// the suspects COULD touch), not the route actually taken. It scopes the hunt;
// the incident's own trace locates the divergence (flowmap behavior ingest).
package impact

import (
	"fmt"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/groundwork/fitness"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/setutil"
)

// Card is the triage evidence for one suspect set. Every field is sorted and
// derived from the graph alone, so identical inputs render identical cards.
type Card struct {
	Suspects    []string           `json:"suspects"`
	Entrypoints []string           `json:"entrypoints,omitempty"` // implicated routes: entrypoint cover of the suspects
	Callers     []string           `json:"callers,omitempty"`     // reverse reach (who can be affected upstream)
	Effects     []string           `json:"effects,omitempty"`     // boundary effects reachable from the suspects
	BlindSpots  []graph.BlindSpot  `json:"blind_spots,omitempty"` // gaps on any traversed path — where the card's claims stop being sound
	Annotations []graph.Annotation `json:"annotations,omitempty"` // human/AI context on those blind spots (disclosure only)

	// CoverOverApprox marks the implicated-entrypoint set as an upper bound: the
	// reverse reach passed through a HighFanOut dispatch seam, which fans every
	// caller onto every implementation. The entrypoints are "≤", not a count.
	CoverOverApprox bool `json:"cover_over_approx,omitempty"`

	// EffectsOverApprox marks the reachable-effect set as an upper bound: the FORWARD
	// reach passed through a HighFanOut dispatch seam (a shared higher-order runner
	// fanning onto every closure that flows to it), so the effects may include
	// sibling-closure effects past the seam, not just the suspects'. The dual of
	// CoverOverApprox; the CLI `reach` lens discloses the same signal.
	EffectsOverApprox bool `json:"effects_over_approx,omitempty"`

	// Fault marks the what-if framing: the suspects are HYPOTHESIZED to be
	// failing, and the card reads as fault propagation — entrypoints degraded,
	// effects that may not have happened. Same evidence, same determinism; only
	// the question differs ("what if these fail" vs "what are these").
	Fault bool `json:"fault,omitempty"`

	// The partial-effect answer (IT-3), fault mode only: external effects that
	// can have already happened when a suspect faults — read off flowmap's
	// effect_order facts. Certainly = the effect dominates the fault site (it
	// happened on ANY path reaching the fault); Possibly = a path exists where
	// it happened first. This is the inconsistent-state lead the responder is
	// after: "loan.approved published, charge failed".
	PossiblyCommitted  []string `json:"possibly_committed,omitempty"`
	CertainlyCommitted []string `json:"certainly_committed,omitempty"`
}

// ForFault assembles the what-if card: mark the suspects as failing and read
// the blast radius off the same index walks. For an event symptom the resolver
// already returns both the publishers (who should emit) and the consumer
// registrars (who is now starved), so both directions of "event T missing"
// are in the suspect set.
func ForFault(ix *graph.Index, fqns []string) Card {
	c := ForNodes(ix, fqns)
	c.Fault = true

	// Partial effects: a fact applies when its fallible callee is one of the
	// hypothesized-failing suspects — the effect named by the fact may already
	// be committed when that call faults.
	suspect := setutil.StringSet(c.Suspects)
	possibly, certainly := map[string]bool{}, map[string]bool{}
	for _, f := range ix.EffectOrder() {
		if !suspect[f.Callee] {
			continue
		}
		entry := fmt.Sprintf("%s — before the fault at %s in %s", f.Effect, f.CalleeSite, fitness.ShortName(f.Fn))
		if f.Always {
			certainly[entry] = true
		} else {
			possibly[entry] = true
		}
	}
	// Certainly dominates possibly: an effect proven committed before the fault on
	// one path must not also be listed as merely possible. Two effect_order facts
	// for the same effect/site/fn differing only in Always would otherwise place
	// the identical entry string in both buckets — a self-contradicting card.
	for entry := range certainly {
		delete(possibly, entry)
	}
	c.PossiblyCommitted = setutil.SortedKeys(possibly)
	c.CertainlyCommitted = setutil.SortedKeys(certainly)
	return c
}

// gatherBlindSpots collects, deduped and sorted, the function- and package-level
// blind spots at each node in `nodes`, plus a synthesized DynamicEffect for each
// dynamic boundary effect in `dynEffects`. The two inputs let a caller scope the node
// walk — the bidirectional blast radius (ForNodes) or the forward cone alone
// (ForwardBlindSpots) — while keeping the dedup key AND the dynamic-effect synthesis
// identical, so the two surfaces can never disagree on what a blind spot IS (one
// source of truth, CLAUDE.md). The DynamicEffect Detail drops the internal "boundary:"
// prefix — the same human-readable form ground.go emits, so every surface renders (and
// diffs) the identical blind spot the same way.
func gatherBlindSpots(ix *graph.Index, nodes []string, dynEffects []graph.Edge) []graph.BlindSpot {
	var blind []graph.BlindSpot
	seen := map[string]bool{}
	add := func(bs []graph.BlindSpot) {
		for _, b := range bs {
			// Detail is part of the identity: two DynamicEffect blind spots at the same
			// site with different effect labels are distinct disclosures (Detail is rendered).
			k := b.Kind + "\x00" + b.Site + "\x00" + b.Detail
			if !seen[k] {
				seen[k] = true
				blind = append(blind, b)
			}
		}
	}
	for _, fn := range nodes {
		add(ix.BlindSpotsAt(fn))
		add(ix.BlindSpotsAt(fitness.PkgOf(fn)))
	}
	for _, e := range dynEffects {
		if e.IsDynamic() {
			add([]graph.BlindSpot{{Kind: "DynamicEffect", Site: e.From, Detail: strings.TrimPrefix(e.To, "boundary:")}})
		}
	}
	graph.SortBlindSpots(blind)
	return blind
}

// ForwardBlindSpots returns the blind spots on the FORWARD cone of fqns (the seeds
// plus everything they can reach) — where the tool's view of what these functions can
// DO becomes incomplete — and whether that cone crosses a HighFanOut seam (so the
// reachable-effect surface is an upper bound). Unlike ForNodes, which gathers blind
// spots over the bidirectional blast radius for incident triage, this is the
// forward-only set a review surface needs: a change's trustworthiness is about what it
// can cause, not who can reach it — a blind spot in a CALLER does not make the change
// itself unverifiable. Deterministic (sorted, intrinsic).
func ForwardBlindSpots(ix *graph.Index, fqns []string) (blind []graph.BlindSpot, effectsOverApprox bool) {
	forward := ix.Reachable(fqns...)
	cone := setutil.StringSet(fqns)
	for _, fn := range forward {
		cone[fn] = true
	}
	coneSorted := setutil.SortedKeys(cone)
	return gatherBlindSpots(ix, coneSorted, ix.Effects(coneSorted...)), ix.CrossesHighFanOut(coneSorted)
}

// ForNodes assembles the card for a set of suspect function FQNs.
func ForNodes(ix *graph.Index, fqns []string) Card {
	suspects := setutil.SortedKeys(setutil.StringSet(fqns))

	callers := ix.Reaching(suspects...)
	// Entrypoint cover for the whole suspect set in one pass: a source covers
	// a suspect iff it reaches one (or is one) — Sources ∩ (callers ∪ suspects).
	// Per-suspect EntrypointCover would redo a full reverse BFS per suspect.
	inCover := setutil.StringSet(callers)
	for _, s := range suspects {
		inCover[s] = true
	}
	entry := map[string]bool{}
	for _, src := range ix.Sources() {
		if inCover[src] {
			entry[src] = true
		}
	}

	forward := ix.Reachable(suspects...)
	cone := setutil.StringSet(suspects)
	for _, fn := range forward {
		cone[fn] = true
	}
	coneSorted := setutil.SortedKeys(cone)
	coneEffects := ix.Effects(coneSorted...) // gathered once: the effects section AND the dynamic probe read it
	effects := map[string]bool{}
	for _, e := range coneEffects {
		effects[e.To] = true
	}

	// Blind spots over the bidirectional blast radius: function- and package-level
	// disclosures on any traversed node (callers and forward cone), plus a
	// synthesized DynamicEffect for each dynamic boundary effect in the forward cone
	// — the frontier where the card's reachability claims are no longer sound.
	traversed := setutil.StringSet(callers)
	for fn := range cone {
		traversed[fn] = true
	}
	blind := gatherBlindSpots(ix, setutil.SortedKeys(traversed), coneEffects)
	// Annotation context for those blind spots, collected once per (Site, Kind) in
	// the sorted order so the card is deterministic and a seam with several blind
	// spots does not repeat its shared annotation.
	annot := ix.DistinctAnnotationsAt(blind)

	return Card{
		Suspects:    suspects,
		Entrypoints: setutil.SortedKeys(entry),
		Callers:     callers,
		Effects:     setutil.SortedKeys(effects),
		BlindSpots:  blind,
		Annotations: annot,
		// The implicated-entrypoint count is an upper bound iff the reverse reach
		// to those entrypoints fanned out through a HighFanOut dispatch seam; the
		// reachable-effect set is an upper bound iff the FORWARD cone did.
		CoverOverApprox:   ix.CrossesHighFanOut(callers),
		EffectsOverApprox: ix.CrossesHighFanOut(coneSorted),
	}
}
