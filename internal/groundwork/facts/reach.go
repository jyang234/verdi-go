package facts

import (
	"sort"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
)

const dynamicEffectKind = "DynamicEffect"

type boundaryEffect struct {
	From    string
	To      string
	Dynamic bool
}

// EvaluateReach binds source and target selectors, then finds the first
// deterministic shortest path. A concrete path dominates blindness across all
// sources; otherwise blindness dominates a visible absence proof.
//
// UnboundFrom/UnboundTo name the individually dead selectors in every state; the
// ReachUnbound state itself still means a whole FAMILY bound nothing.
func EvaluateReach(ix *graph.Index, from, to []string) ReachResult {
	if ix == nil {
		// A nil index binds nothing, so every selector is dead — the same value the
		// per-selector probe would produce, without dereferencing the index.
		return ReachResult{
			State:       ReachUnbound,
			UnboundFrom: canonicalStrings(from),
			UnboundTo:   canonicalStrings(to),
		}
	}

	boundFrom := BindSources(ix, from)
	boundTo := BindTargets(ix, to)
	return evaluateReachBindings(ix, boundFrom, boundTo,
		deadSelectors(ix, from, BindSources), deadSelectors(ix, to, BindTargets))
}

// EvaluateReachBoundSources evaluates source identities that a compatibility
// caller has already bound. It does not name-expand those identities again;
// target selectors retain normal rich-selector binding. A supplied identity the
// graph does not carry is recorded in UnboundFrom, per identity.
func EvaluateReachBoundSources(ix *graph.Index, from, to []string) ReachResult {
	if ix == nil {
		return ReachResult{
			State:       ReachUnbound,
			UnboundFrom: canonicalStrings(from),
			UnboundTo:   canonicalStrings(to),
		}
	}
	return evaluateReachBindings(ix, boundIdentities(ix, from), BindTargets(ix, to),
		deadSelectors(ix, from, boundIdentities), deadSelectors(ix, to, BindTargets))
}

// boundIdentities is the selectorBinder for already-bound source identities: an
// identity binds itself when the graph carries it as a node, and nothing
// otherwise. No name expansion happens here.
func boundIdentities(ix *graph.Index, values []string) []string {
	var bound []string
	for _, value := range canonicalStrings(values) {
		if ix.Has(value) {
			bound = append(bound, value)
		}
	}
	return bound
}

// evaluateReachBindings decides the state from the bound FAMILIES and carries
// the already-computed per-selector dead sets through unchanged.
func evaluateReachBindings(
	ix *graph.Index,
	boundFrom []string,
	boundTo []string,
	deadFrom []string,
	deadTo []string,
) ReachResult {
	result := ReachResult{
		From: boundFrom, To: boundTo,
		UnboundFrom: deadFrom, UnboundTo: deadTo,
	}
	if len(boundFrom) == 0 || len(boundTo) == 0 {
		result.State = ReachUnbound
		return result
	}

	targets := make(map[string]bool, len(boundTo))
	for _, target := range boundTo {
		targets[target] = true
	}
	effects := effectsByOwner(ix)

	var firstBlind *BlindWitness
	for _, source := range boundFrom {
		search := searchFrom(ix, source, targets, effects)
		if search.path != nil {
			result.State = ReachFound
			result.Paths = []PathWitness{*search.path}
			return result
		}
		if firstBlind == nil && search.blind != nil {
			firstBlind = search.blind
		}
	}
	if firstBlind != nil {
		result.State = ReachBlind
		result.Blind = firstBlind
		return result
	}
	result.State = ReachAbsent
	return result
}

type sourceSearch struct {
	path  *PathWitness
	blind *BlindWitness
}

// searchFrom walks one source exactly once and selects its witness in CONE
// order — the source, then every reachable function in canonical FQN order.
// Every reachable function is considered before any boundary target; only when
// no function matches does the effect surface decide, and there the first owner
// in that same cone order that carries a matching effect wins (see
// coneMatchingEffect for the within-owner tie-break). BFS distance orders
// nothing: the parent map exists SOLELY to reconstruct a deterministic shortest
// path to whatever the cone order selected.
func searchFrom(
	ix *graph.Index,
	source string,
	targets map[string]bool,
	effects map[string][]boundaryEffect,
) sourceSearch {
	parent := map[string]string{source: ""}
	current := []string{source}
	for len(current) > 0 {
		current = nextLevel(ix, current, parent)
	}

	cone := canonicalCone(source, parent)
	for _, fn := range cone[1:] {
		if targets[fn] {
			return sourceSearch{path: &PathWitness{
				From: source,
				To:   fn,
				Path: reconstructPath(parent, source, fn),
			}}
		}
	}
	if effect, ok := coneMatchingEffect(cone, targets, effects); ok {
		path := reconstructPath(parent, source, effect.From)
		path = append(path, effect.To)
		return sourceSearch{path: &PathWitness{
			From: source,
			To:   effect.To,
			Path: path,
		}}
	}

	return sourceSearch{blind: blindForCone(ix, source, cone, effects)}
}

func nextLevel(ix *graph.Index, current []string, parent map[string]string) []string {
	var next []string
	for _, from := range current {
		for _, to := range ix.Callees(from) {
			if _, seen := parent[to]; seen {
				continue
			}
			parent[to] = from
			next = append(next, to)
		}
	}
	return next
}

func canonicalCone(source string, parent map[string]string) []string {
	reachable := make([]string, 0, len(parent)-1)
	for fn := range parent {
		if fn != source {
			reachable = append(reachable, fn)
		}
	}
	sort.Strings(reachable)
	return append([]string{source}, reachable...)
}

// coneMatchingEffect picks the boundary effect that witnesses a source's reach.
// Owners are visited in cone order and the FIRST owner carrying a match wins:
// the cone is already sorted, so that cross-owner precedence is a pure function
// of the graph's content and needs no further canonicalization.
//
// Within one owner the choice IS input-sensitive — graph.Load sorts neither the
// node list nor the edge list — so "the first matching edge" would move with
// producer emission order. Each owner's slice arrives sorted by (To, From) from
// canonicalBoundaryEffectValues, so the first match in it is that owner's
// canonical minimum. This within-owner canonicalization is a deliberate
// post-extraction correction, pinned by the fitness characterization subtest
// "canonical effect within one owner over reversed declaration order".
func coneMatchingEffect(
	cone []string,
	targets map[string]bool,
	effects map[string][]boundaryEffect,
) (boundaryEffect, bool) {
	for _, owner := range cone {
		for _, effect := range effects[owner] {
			if targets[effect.To] {
				return effect, true
			}
		}
	}
	return boundaryEffect{}, false
}

func reconstructPath(parent map[string]string, source, target string) []string {
	var reversed []string
	for current := target; ; current = parent[current] {
		reversed = append(reversed, current)
		if current == source {
			break
		}
	}
	path := make([]string, len(reversed))
	for i := range reversed {
		path[len(reversed)-1-i] = reversed[i]
	}
	return path
}

// BlindFrontier classifies a caller-provided reachable cone and effect surface.
// It is the compatibility boundary for fitness evaluators not yet migrated to a
// complete typed fact family.
func BlindFrontier(
	ix *graph.Index,
	from string,
	cone []string,
	effects []graph.Edge,
) *BlindWitness {
	if ix == nil {
		return nil
	}
	coneValues := canonicalStrings(cone)
	if len(coneValues) > 0 && from != "" {
		for i, fn := range coneValues {
			if fn == from {
				reordered := make([]string, 0, len(coneValues))
				reordered = append(reordered, fn)
				reordered = append(reordered, coneValues[:i]...)
				reordered = append(reordered, coneValues[i+1:]...)
				coneValues = reordered
				break
			}
		}
	}
	canonicalEffects := canonicalBoundaryEffectValues(effects)
	byOwner := make(map[string][]boundaryEffect)
	for _, effect := range canonicalEffects {
		byOwner[effect.From] = append(byOwner[effect.From], effect)
	}
	return blindForCone(ix, from, coneValues, byOwner)
}

// blindForCone preserves the legacy precedence ACROSS sites: the cone is walked
// source-first then lexicographically, function-site evidence beats package-site
// evidence at each function, and dynamic effects are consulted only after every
// function and package site in the cone is visible — including the dynamic
// effects of the source itself before any callee's.
//
// What it deliberately does NOT preserve is the choice WITHIN one site. The
// legacy probe returned the first non-disclosure spot in the manifest and the
// first dynamic edge in the edge list; graph.Load sorts neither, so both moved
// with producer emission order for a graph that is semantically identical. Both
// are now the canonical minimum over that site's candidates — a declared
// post-extraction correction required by the shuffle-invariance contract, on the
// precedent the concurrent path already set. blindSpotsAt owns the first;
// the dynamic loop below owns the second.
func blindForCone(
	ix *graph.Index,
	from string,
	cone []string,
	effects map[string][]boundaryEffect,
) *BlindWitness {
	for _, fn := range cone {
		if candidates := blindSpotsAt(ix, from, fn, BlindAtFunction); len(candidates) > 0 {
			witness := candidates[0]
			return &witness
		}
		// PackageOf is probed unconditionally, including the "" it returns for an
		// FQN that does not parse: graph.Load accepts both an unparsable node FQN
		// and a blind spot recorded at the empty site, so skipping the probe there
		// would drop a real blind frontier and turn a fail-closed abstention into a
		// silent absence proof.
		candidates := blindSpotsAt(ix, from, PackageOf(fn), BlindInPackage)
		if len(candidates) > 0 {
			witness := candidates[0]
			return &witness
		}
	}

	// Cone order across owners: the FIRST function in the cone that makes a
	// dynamic effect is the site, and only its own effects are ranked. Collecting
	// every dynamic effect in the cone and sorting them globally would let a
	// callee outrank the source purely on its FQN, which is not the precedence
	// this walk is documented (and relied on) to have.
	for _, fn := range cone {
		var dynamic []BlindWitness
		for _, effect := range effects[fn] {
			if effect.Dynamic {
				dynamic = append(dynamic, BlindWitness{
					From:     from,
					Site:     fn,
					Kind:     dynamicEffectKind,
					Detail:   effect.To,
					Location: BlindAtDynamicEffect,
				})
			}
		}
		// Within the site, the canonical minimum — see the declared correction in
		// this function's doc.
		if dynamic = canonicalBlindWitnesses(dynamic); len(dynamic) > 0 {
			witness := dynamic[0]
			return &witness
		}
	}
	return nil
}

func canonicalBlindWitnesses(witnesses []BlindWitness) []BlindWitness {
	sortBlindWitnesses(witnesses)
	if len(witnesses) == 0 {
		return witnesses
	}
	n := 1
	for _, witness := range witnesses[1:] {
		if witness == witnesses[n-1] {
			continue
		}
		witnesses[n] = witness
		n++
	}
	return witnesses[:n]
}

// blindSpotsAt returns one site's non-disclosure blind spots in canonical
// (Kind, Site, Detail, Location) order, so a caller taking [0] takes the site's
// canonical minimum.
//
// That minimum is a DECLARED post-extraction correction, not the legacy
// behavior: the pre-extraction probe returned the first non-disclosure spot in
// the manifest, and graph.Load does not sort BlindSpots, so the named spot
// tracked producer emission order and churned the base-vs-branch diff for a
// graph that is semantically identical. The shuffle-invariance contract (equal
// ReachResult values over shuffled nodes, edges and blind spots) requires an
// intrinsic key here; the concurrent path declared and pinned the same
// correction first. Pinned by the fitness characterization subtest "canonical
// blind spot within one site over adversarial manifest order". Which SITE is
// selected is untouched — that stays cone order, see blindForCone.
func blindSpotsAt(
	ix *graph.Index,
	from string,
	site string,
	location BlindLocation,
) []BlindWitness {
	var result []BlindWitness
	for _, spot := range ix.BlindSpotsAt(site) {
		if blindspots.Kind(spot.Kind).IsDisclosureOnlyFrontier() {
			continue
		}
		result = append(result, BlindWitness{
			From:     from,
			Site:     site,
			Kind:     spot.Kind,
			Detail:   spot.Detail,
			Location: location,
		})
	}
	return canonicalBlindWitnesses(result)
}

func sortBlindWitnesses(witnesses []BlindWitness) {
	sort.Slice(witnesses, func(i, j int) bool {
		left, right := witnesses[i], witnesses[j]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Site != right.Site {
			return left.Site < right.Site
		}
		if left.Detail != right.Detail {
			return left.Detail < right.Detail
		}
		return left.Location < right.Location
	})
}

func effectsByOwner(ix *graph.Index) map[string][]boundaryEffect {
	result := make(map[string][]boundaryEffect)
	for _, effect := range canonicalBoundaryEffectValues(ix.Edges()) {
		result[effect.From] = append(result[effect.From], effect)
	}
	return result
}

func canonicalBoundaryEdges(ix *graph.Index) []graph.Edge {
	var edges []graph.Edge
	for _, edge := range ix.Edges() {
		if edge.IsBoundary() {
			edges = append(edges, edge)
		}
	}
	sortBoundaryEdges(edges)
	if len(edges) == 0 {
		return edges
	}
	n := 1
	for _, edge := range edges[1:] {
		if edge == edges[n-1] {
			continue
		}
		edges[n] = edge
		n++
	}
	return edges[:n]
}

func sortBoundaryEdges(edges []graph.Edge) {
	sort.SliceStable(edges, func(i, j int) bool {
		left, right := edges[i], edges[j]
		if left.From != right.From {
			return left.From < right.From
		}
		if left.To != right.To {
			return left.To < right.To
		}
		if left.Boundary != right.Boundary {
			return left.Boundary < right.Boundary
		}
		if left.Tier != right.Tier {
			return left.Tier < right.Tier
		}
		if left.Concurrent != right.Concurrent {
			return !left.Concurrent
		}
		return left.Via < right.Via
	})
}

func canonicalBoundaryEffectValues(edges []graph.Edge) []boundaryEffect {
	var effects []boundaryEffect
	for _, edge := range edges {
		if edge.IsBoundary() {
			effects = append(effects, boundaryEffect{
				From: edge.From, To: edge.To, Dynamic: edge.IsDynamic(),
			})
		}
	}
	sortBoundaryEffects(effects)
	if len(effects) == 0 {
		return effects
	}
	n := 1
	for _, effect := range effects[1:] {
		if effect == effects[n-1] {
			continue
		}
		effects[n] = effect
		n++
	}
	return effects[:n]
}

func sortBoundaryEffects(effects []boundaryEffect) {
	sort.Slice(effects, func(i, j int) bool {
		if effects[i].To != effects[j].To {
			return effects[i].To < effects[j].To
		}
		return effects[i].From < effects[j].From
	})
}
