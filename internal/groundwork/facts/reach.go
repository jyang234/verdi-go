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

// searchFrom walks one source exactly once. It preserves legacy evidence
// precedence by considering every reachable function in canonical FQN order
// before boundary targets; the BFS parent map still reconstructs a deterministic
// shortest path to the selected function. With no function hit, boundary
// targets retain distance order and their canonical label/owner tie-break.
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
		if len(next) > 0 {
			levels = append(levels, next)
		}
		current = next
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
	for _, owners := range levels {
		if effect, ok := firstMatchingEffect(owners, targets, effects); ok {
			path := reconstructPath(parent, source, effect.From)
			path = append(path, effect.To)
			return sourceSearch{path: &PathWitness{
				From: source,
				To:   effect.To,
				Path: path,
			}}
		}
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

// blindForCone preserves the legacy structural precedence: source then
// lexicographic reachable functions, function-site evidence before package-site
// evidence at each function, and dynamic effects only after every function and
// package site is visible.
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

	var dynamic []BlindWitness
	for _, fn := range cone {
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
	}
	dynamic = canonicalBlindWitnesses(dynamic)
	if len(dynamic) > 0 {
		witness := dynamic[0]
		return &witness
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
