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
func EvaluateReach(ix *graph.Index, from, to []string) ReachResult {
	if ix == nil {
		return ReachResult{
			State:       ReachUnbound,
			UnboundFrom: canonicalStrings(from),
			UnboundTo:   canonicalStrings(to),
		}
	}

	boundFrom := BindSources(ix, from)
	boundTo := BindTargets(ix, to)
	return evaluateReachBindings(ix, boundFrom, boundTo, from, to)
}

// EvaluateReachBoundSources evaluates source identities that a compatibility
// caller has already bound. It does not name-expand those identities again;
// target selectors retain normal rich-selector binding.
func EvaluateReachBoundSources(ix *graph.Index, from, to []string) ReachResult {
	if ix == nil {
		return ReachResult{
			State:       ReachUnbound,
			UnboundFrom: canonicalStrings(from),
			UnboundTo:   canonicalStrings(to),
		}
	}
	var boundFrom []string
	for _, source := range canonicalStrings(from) {
		if ix.Has(source) {
			boundFrom = append(boundFrom, source)
		}
	}
	boundTo := BindTargets(ix, to)
	return evaluateReachBindings(ix, boundFrom, boundTo, from, to)
}

func evaluateReachBindings(
	ix *graph.Index,
	boundFrom []string,
	boundTo []string,
	fromSelectors []string,
	toSelectors []string,
) ReachResult {
	result := ReachResult{From: boundFrom, To: boundTo}
	if len(boundFrom) == 0 || len(boundTo) == 0 {
		result.State = ReachUnbound
		if len(boundFrom) == 0 {
			result.UnboundFrom = canonicalStrings(fromSelectors)
		}
		if len(boundTo) == 0 {
			result.UnboundTo = canonicalStrings(toSelectors)
		}
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

// searchFrom walks one source by BFS distance. At each terminal distance it
// considers function targets before boundary targets; sorted adjacency fixes
// parent selection for competing shortest paths.
func searchFrom(
	ix *graph.Index,
	source string,
	targets map[string]bool,
	effects map[string][]boundaryEffect,
) sourceSearch {
	parent := map[string]string{source: ""}
	levels := [][]string{{source}}
	current := []string{source}

	for len(current) > 0 {
		next := nextLevel(ix, current, parent)
		for _, fn := range next {
			if targets[fn] {
				return sourceSearch{path: &PathWitness{
					From: source,
					To:   fn,
					Path: reconstructPath(parent, source, fn),
				}}
			}
		}
		if effect, ok := firstMatchingEffect(current, targets, effects); ok {
			path := reconstructPath(parent, source, effect.From)
			path = append(path, effect.To)
			return sourceSearch{path: &PathWitness{
				From: source,
				To:   effect.To,
				Path: path,
			}}
		}
		if len(next) > 0 {
			levels = append(levels, next)
		}
		current = next
	}

	return sourceSearch{blind: blindForLevels(ix, source, levels, effects)}
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

func firstMatchingEffect(
	owners []string,
	targets map[string]bool,
	effects map[string][]boundaryEffect,
) (boundaryEffect, bool) {
	var candidates []boundaryEffect
	for _, owner := range owners {
		for _, effect := range effects[owner] {
			if targets[effect.To] {
				candidates = append(candidates, effect)
			}
		}
	}
	sortBoundaryEffects(candidates)
	if len(candidates) == 0 {
		return boundaryEffect{}, false
	}
	return candidates[0], true
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
	canonicalCone := canonicalStrings(cone)
	if len(canonicalCone) > 0 && from != "" {
		for i, fn := range canonicalCone {
			if fn == from {
				reordered := make([]string, 0, len(canonicalCone))
				reordered = append(reordered, fn)
				reordered = append(reordered, canonicalCone[:i]...)
				reordered = append(reordered, canonicalCone[i+1:]...)
				canonicalCone = reordered
				break
			}
		}
	}
	canonicalEffects := canonicalBoundaryEffectValues(effects)
	byOwner := make(map[string][]boundaryEffect)
	for _, effect := range canonicalEffects {
		byOwner[effect.From] = append(byOwner[effect.From], effect)
	}
	return blindForLevels(ix, from, [][]string{canonicalCone}, byOwner)
}

func blindForLevels(
	ix *graph.Index,
	from string,
	levels [][]string,
	effects map[string][]boundaryEffect,
) *BlindWitness {
	for _, level := range levels {
		var candidates []BlindWitness
		for _, fn := range level {
			candidates = append(candidates, blindSpotsAt(ix, from, fn, BlindAtFunction)...)
			if pkg := PackageOf(fn); pkg != "" {
				candidates = append(candidates, blindSpotsAt(ix, from, pkg, BlindInPackage)...)
			}
			for _, effect := range effects[fn] {
				if effect.Dynamic {
					candidates = append(candidates, BlindWitness{
						From:     from,
						Site:     fn,
						Kind:     dynamicEffectKind,
						Detail:   effect.To,
						Location: BlindAtDynamicEffect,
					})
				}
			}
		}
		sortBlindWitnesses(candidates)
		if len(candidates) > 0 {
			witness := candidates[0]
			return &witness
		}
	}
	return nil
}

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
	return result
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
	sort.Slice(edges, func(i, j int) bool {
		left, right := edges[i], edges[j]
		if left.To != right.To {
			return left.To < right.To
		}
		if left.From != right.From {
			return left.From < right.From
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
