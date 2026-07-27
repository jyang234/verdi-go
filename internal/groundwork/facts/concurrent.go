package facts

import (
	"sort"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/setutil"
	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
)

// ConcurrentState is the closed outcome of evaluating a target family against
// the graph's rule-independent concurrent surface.
type ConcurrentState uint8

const (
	// ConcurrentUnbound means the target selectors bind no graph identity, so
	// concurrent reachability cannot be evaluated.
	ConcurrentUnbound ConcurrentState = iota
	// ConcurrentHit means at least one target is on the concurrent surface.
	ConcurrentHit
	// ConcurrentClean means no target is on a fully visible concurrent surface.
	ConcurrentClean
	// ConcurrentBlind means no target was found, but the concurrent surface is
	// incomplete.
	ConcurrentBlind
)

// ConcurrentWitness identifies one target on the concurrent surface. From is
// empty for a spawned or cone function target.
type ConcurrentWitness struct {
	From string
	To   string
}

// ConcurrentResult carries target bindings and canonical decisive evidence.
//
// UnboundTo is a PER-SELECTOR dead set with the same meaning as ReachResult's:
// it names the target selectors that bind nothing on their own, in every state,
// sorted and de-duplicated. State == ConcurrentUnbound still means the whole
// target family bound nothing.
type ConcurrentResult struct {
	State     ConcurrentState
	To        []string
	Hits      []ConcurrentWitness
	Blind     *BlindWitness
	UnboundTo []string
}

// ConcurrentSurface is the rule-independent graph surface entered through
// concurrent edges. Its direct edges, internal cone, effects, and decisive
// blindness are canonicalized once and reused by every rule or claim.
type ConcurrentSurface struct {
	ix      *graph.Index
	direct  []graph.Edge
	cone    []string
	effects []graph.Edge
	blind   *BlindWitness
}

// BuildConcurrentSurface constructs the complete rule-independent concurrent
// surface once. Only concurrent internal targets that are graph nodes become
// seeds; direct concurrent boundary edges remain terminal targets.
func BuildConcurrentSurface(ix *graph.Index) ConcurrentSurface {
	if ix == nil {
		return ConcurrentSurface{}
	}

	seedSet := make(map[string]bool)
	var direct []graph.Edge
	for _, edge := range canonicalBoundaryEdges(ix) {
		if edge.Concurrent {
			direct = append(direct, edge)
		}
	}
	for _, edge := range ix.Edges() {
		if edge.Concurrent && !edge.IsBoundary() && ix.Has(edge.To) {
			seedSet[edge.To] = true
		}
	}

	seeds := setutil.SortedKeys(seedSet)
	coneSet := setutil.StringSet(seeds)
	for _, fn := range ix.Reachable(seeds...) {
		coneSet[fn] = true
	}
	cone := setutil.SortedKeys(coneSet)
	effects := concurrentConeEffects(ix, cone)

	return ConcurrentSurface{
		ix:      ix,
		direct:  direct,
		cone:    cone,
		effects: effects,
		blind:   concurrentBlindWitness(ix, cone, effects, direct),
	}
}

// Evaluate binds targets and evaluates them against the precomputed surface. A
// concrete hit dominates every blind witness, including a matching dynamically
// named direct concurrent boundary.
//
// UnboundTo names the individually dead selectors in every state; the
// ConcurrentUnbound state itself still means the whole target family bound
// nothing.
func (s ConcurrentSurface) Evaluate(to []string) ConcurrentResult {
	if s.ix == nil {
		// A nil index binds nothing, so every selector is dead.
		return ConcurrentResult{
			State:     ConcurrentUnbound,
			UnboundTo: canonicalStrings(to),
		}
	}

	boundTo := BindTargets(s.ix, to)
	result := ConcurrentResult{
		To:        boundTo,
		UnboundTo: deadSelectors(s.ix, to, BindTargets),
	}
	if len(boundTo) == 0 {
		result.State = ConcurrentUnbound
		return result
	}

	targets := setutil.StringSet(boundTo)
	var hits []ConcurrentWitness
	for _, edge := range s.direct {
		if targets[edge.To] {
			hits = append(hits, ConcurrentWitness{From: edge.From, To: edge.To})
		}
	}
	for _, fn := range s.cone {
		if targets[fn] {
			hits = append(hits, ConcurrentWitness{To: fn})
		}
	}
	for _, edge := range s.effects {
		if targets[edge.To] {
			hits = append(hits, ConcurrentWitness{From: edge.From, To: edge.To})
		}
	}
	result.Hits = canonicalConcurrentWitnesses(hits)
	if len(result.Hits) > 0 {
		result.State = ConcurrentHit
		return result
	}
	if s.blind != nil {
		result.State = ConcurrentBlind
		witness := *s.blind
		result.Blind = &witness
		return result
	}
	result.State = ConcurrentClean
	return result
}

func concurrentConeEffects(ix *graph.Index, cone []string) []graph.Edge {
	coneSet := setutil.StringSet(cone)
	var effects []graph.Edge
	for _, edge := range canonicalBoundaryEdges(ix) {
		if coneSet[edge.From] {
			effects = append(effects, edge)
		}
	}
	return effects
}

// concurrentBlindWitness layers blindness in the legacy fail-closed order:
// cone frontier, dynamic direct concurrent boundary, then graph-wide unresolved
// concurrent dispatch. Each input is canonicalized before selecting evidence.
//
// That canonicalization is a deliberate correction to the pre-extraction probe,
// not a faithful copy of it. graph.Load sorts neither the edge list nor the
// blind-spot manifest, so the old probe named whichever dynamic edge or
// ConcurrentDispatch spot the producer emitted first: WHICH blind site a caution
// disclosed moved with input order on a semantically identical graph (tenet 1).
// Only the representative changes — a blind surface stays blind either way, so
// the correction cannot flip a verdict. Pinned by the two "canonical ... over
// shuffled input" cases in fitness's TestConcurrentCharacterization.
func concurrentBlindWitness(
	ix *graph.Index,
	cone []string,
	effects []graph.Edge,
	direct []graph.Edge,
) *BlindWitness {
	from := ""
	if len(cone) > 0 {
		from = cone[0]
	}
	if witness := BlindFrontier(ix, from, cone, effects); witness != nil {
		return witness
	}
	for _, edge := range direct {
		if edge.IsDynamic() {
			return &BlindWitness{
				Site:     edge.From,
				Kind:     dynamicEffectKind,
				Detail:   edge.To,
				Location: BlindAtConcurrentBoundary,
			}
		}
	}

	spots := append([]graph.BlindSpot(nil), ix.BlindSpots()...)
	graph.SortBlindSpots(spots)
	for _, spot := range spots {
		if blindspots.Kind(spot.Kind) == blindspots.ConcurrentDispatch {
			return &BlindWitness{
				Site:     spot.Site,
				Kind:     spot.Kind,
				Detail:   spot.Detail,
				Location: BlindAtConcurrentDispatch,
			}
		}
	}
	return nil
}

func canonicalConcurrentWitnesses(values []ConcurrentWitness) []ConcurrentWitness {
	sort.Slice(values, func(i, j int) bool {
		if values[i].From != values[j].From {
			return values[i].From < values[j].From
		}
		return values[i].To < values[j].To
	})
	if len(values) == 0 {
		return nil
	}
	n := 1
	for _, value := range values[1:] {
		if value == values[n-1] {
			continue
		}
		values[n] = value
		n++
	}
	return values[:n]
}
