package facts

import (
	"sort"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// EvaluatePassThrough proves whether every visible source-to-target path enters
// a waypoint. An unbound waypoint is disclosed but treated as removing no
// nodes, so standing fitness can preserve its legacy finding while caller-
// supplied claims can reject the same incomplete binding.
func EvaluatePassThrough(ix *graph.Index, in PassThroughInput) PassThroughResult {
	if ix == nil {
		return PassThroughResult{
			State:          PassThroughUnbound,
			UnboundFrom:    canonicalStrings(in.From),
			UnboundTo:      canonicalStrings(in.To),
			UnboundThrough: canonicalStrings(in.Through),
		}
	}

	result := PassThroughResult{
		From:    BindSources(ix, in.From),
		To:      BindTargets(ix, in.To),
		Through: BindFunctions(ix, in.Through),
	}
	if len(result.From) == 0 {
		result.UnboundFrom = canonicalStrings(in.From)
	}
	if len(result.To) == 0 {
		result.UnboundTo = canonicalStrings(in.To)
	}
	if len(result.Through) == 0 {
		result.UnboundThrough = canonicalStrings(in.Through)
	}
	if len(result.UnboundFrom) > 0 || len(result.UnboundTo) > 0 {
		result.State = PassThroughUnbound
		return result
	}

	through := make(map[string]bool, len(result.Through))
	for _, fn := range result.Through {
		through[fn] = true
	}
	targets := make(map[string]bool, len(result.To))
	for _, target := range result.To {
		targets[target] = true
	}

	bypassByPair := make(map[[2]string]PathWitness)
	var firstBlind *BlindWitness
	for _, source := range result.From {
		if through[source] {
			continue
		}
		cone, parent := GuardedWalk(ix, source, result.Through)
		coneSet := make(map[string]bool, len(cone))
		for _, fn := range cone {
			coneSet[fn] = true
			if fn == source || !targets[fn] || allowedPair(in.Allow, source, fn) {
				continue
			}
			recordBypass(bypassByPair, PathWitness{
				From: source,
				To:   fn,
				Path: reconstructPath(parent, source, fn),
			})
		}

		var coneEffects []graph.Edge
		for _, edge := range canonicalBoundaryEdges(ix) {
			if !coneSet[edge.From] {
				continue
			}
			coneEffects = append(coneEffects, edge)
			if !targets[edge.To] || allowedPair(in.Allow, source, edge.To) {
				continue
			}
			path := reconstructPath(parent, source, edge.From)
			path = append(path, edge.To)
			recordBypass(bypassByPair, PathWitness{
				From: source,
				To:   edge.To,
				Path: path,
			})
		}
		if firstBlind == nil {
			firstBlind = BlindFrontier(ix, source, cone, coneEffects)
		}
	}

	result.Bypasses = sortedBypasses(bypassByPair)
	if len(result.Bypasses) > 0 {
		result.State = PassThroughBypassed
		return result
	}
	if firstBlind != nil {
		result.State = PassThroughBlind
		result.Blind = firstBlind
		return result
	}
	result.State = PassThroughGuarded
	return result
}

// GuardedWalk is a deterministic forward BFS that never enters a waypoint.
// It is exported narrowly for the fitness proposal adapter, which consumes the
// same cone while later fact families are migrated.
func GuardedWalk(ix *graph.Index, from string, through []string) (cone []string, parent map[string]string) {
	parent = map[string]string{from: ""}
	current := []string{from}
	for len(current) > 0 {
		var next []string
		for _, cur := range current {
			for _, candidate := range ix.Callees(cur) {
				if _, seen := parent[candidate]; seen || matchesAny(candidate, through) {
					continue
				}
				parent[candidate] = cur
				next = append(next, candidate)
			}
		}
		current = next
	}
	cone = make([]string, 0, len(parent))
	for fn := range parent {
		cone = append(cone, fn)
	}
	sort.Strings(cone)
	return cone, parent
}

func allowedPair(allow []AllowPair, from, to string) bool {
	for _, pair := range allow {
		fromMatches := pair.From == "" || matchesAny(from, []string{pair.From})
		toMatches := pair.To == "" || matchesAny(to, []string{pair.To})
		if fromMatches && toMatches {
			return true
		}
	}
	return false
}

func recordBypass(byPair map[[2]string]PathWitness, witness PathWitness) {
	key := [2]string{witness.From, witness.To}
	current, ok := byPair[key]
	if !ok || pathLess(witness.Path, current.Path) {
		byPair[key] = witness
	}
}

func pathLess(left, right []string) bool {
	if len(left) != len(right) {
		return len(left) < len(right)
	}
	for i := range left {
		if left[i] != right[i] {
			return left[i] < right[i]
		}
	}
	return false
}

func sortedBypasses(byPair map[[2]string]PathWitness) []PathWitness {
	result := make([]PathWitness, 0, len(byPair))
	for _, witness := range byPair {
		result = append(result, witness)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].From != result[j].From {
			return result[i].From < result[j].From
		}
		return result[i].To < result[j].To
	})
	if len(result) == 0 {
		return nil
	}
	return result
}
