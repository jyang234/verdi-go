package review

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/groundwork/fitness"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
	"github.com/jyang234/golang-code-graph/internal/groundwork/setutil"
)

// RouteIODelta is one route whose external write surface changed base→branch —
// the per-route attribution the global io_effects diff cannot give. The
// load-bearing case is the LOST write: a route that reached an effect in base
// and no longer does, while the global effect set is unchanged because another
// route still reaches it — invisible to every other section. The disappearance
// of a write that was there is deterministic in a way the absence of a write
// that never was cannot be.
//
// A nil Base side means the route is new on the branch; a nil Branch side means
// it was removed (one-sided rows exist so a renamed route shows its counts —
// the rename itself already fires a breaking entrypoint contract change).
type RouteIODelta struct {
	Route   string       `json:"route"`
	Base    *RouteIOSide `json:"base,omitempty"`
	Branch  *RouteIOSide `json:"branch,omitempty"`
	Added   []string     `json:"added,omitempty"`
	Removed []string     `json:"removed,omitempty"`
}

// RouteIOSide is one side's write count and the frontier it was counted over.
// A blind frontier means the count is a lower bound — a delta against a blind
// side may be the graph's knowledge shifting, not the code's behavior.
type RouteIOSide struct {
	Writes   int    `json:"writes"`
	Frontier string `json:"frontier"` // "resolved" | "blind"
}

func routeSide(r fitness.RouteIO) *RouteIOSide {
	f := "resolved"
	if r.Blind {
		f = "blind"
	}
	return &RouteIOSide{Writes: len(r.Writes), Frontier: f}
}

// routeIODeltas diffs the per-route write surface. Counts are distinct static
// write targets, not runtime volume (an N+1 is the same target either way),
// which is why this section discloses every delta rather than gating on any
// threshold. Routes present on one side only get a row when they carry writes.
func routeIODeltas(p *policy.Policy, baseIx, branchIx *graph.Index) []RouteIODelta {
	baseRW := fitness.RouteWrites(p, baseIx)
	branchRW := fitness.RouteWrites(p, branchIx)

	var out []RouteIODelta
	for route, b := range baseRW {
		br, ok := branchRW[route]
		if !ok {
			if len(b.Writes) > 0 {
				out = append(out, RouteIODelta{Route: route, Base: routeSide(b), Removed: b.Writes})
			}
			continue
		}
		added, removed := diffStrings(b.Writes, br.Writes)
		if len(added) == 0 && len(removed) == 0 {
			continue
		}
		out = append(out, RouteIODelta{
			Route: route, Base: routeSide(b), Branch: routeSide(br),
			Added: added, Removed: removed,
		})
	}
	for route, br := range branchRW {
		if _, ok := baseRW[route]; !ok && len(br.Writes) > 0 {
			out = append(out, RouteIODelta{Route: route, Branch: routeSide(br), Added: br.Writes})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Route < out[j].Route })
	return out
}

// diffStrings returns the elements only in b (added) and only in a (removed);
// both inputs are sorted, both outputs are sorted.
func diffStrings(a, b []string) (added, removed []string) {
	as, bs := setutil.StringSet(a), setutil.StringSet(b)
	for _, s := range b {
		if !as[s] {
			added = append(added, s)
		}
	}
	for _, s := range a {
		if !bs[s] {
			removed = append(removed, s)
		}
	}
	return added, removed
}

// renderRouteIO renders the per-route section grouped by effect, not by route:
// when one change puts a write behind 40 routes, "db INSERT audit_log now
// behind 40 routes" is the signal, not 40 rows of noise. The JSON section
// stays complete per route. A route counted over a blind frontier carries the
// marker — a delta against a blind side may be the graph's knowledge shifting,
// not the code's behavior.
func renderRouteIO(rows []RouteIODelta) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🚏 Route write surface changed (%d route(s))\n", len(rows))

	added, removed := map[string][]string{}, map[string][]string{}
	var newRoutes, goneRoutes []string
	for _, r := range rows {
		name := fitness.ShortName(r.Route)
		switch {
		case r.Base == nil:
			newRoutes = append(newRoutes, fmt.Sprintf("%s: %d write(s)%s", name, r.Branch.Writes, blindTag(r.Branch)))
		case r.Branch == nil:
			goneRoutes = append(goneRoutes, fmt.Sprintf("%s: %d write(s)%s", name, r.Base.Writes, blindTag(r.Base)))
		default:
			for _, e := range r.Added {
				added[e] = append(added[e], name+blindTag(r.Branch))
			}
			for _, e := range r.Removed {
				removed[e] = append(removed[e], name+blindTag(r.Base))
			}
		}
	}
	for _, e := range setutil.SortedKeys(added) {
		fmt.Fprintf(&b, "- + %s: now behind %s\n", e, strings.Join(added[e], ", "))
	}
	for _, e := range setutil.SortedKeys(removed) {
		fmt.Fprintf(&b, "- - %s: no longer reached from %s\n", e, strings.Join(removed[e], ", "))
	}
	for _, r := range newRoutes {
		fmt.Fprintf(&b, "- + route %s\n", r)
	}
	for _, r := range goneRoutes {
		fmt.Fprintf(&b, "- - route %s\n", r)
	}
	return b.String()
}

func blindTag(s *RouteIOSide) string {
	if s.Frontier == "blind" {
		return " (frontier blind)"
	}
	return ""
}

// newWriteTargets is the effect ratchet: write-effect labels present on the
// branch, absent from the base, and not covered by the policy's allow-list.
// Labels are diffed as sets over the whole graph (not the edge delta), so a
// new edge to an already-written target is correctly not "new surface" — that
// per-route movement is routeIODeltas' job.
func newWriteTargets(p *policy.Policy, base, branch *graph.Graph) []string {
	labels := func(g *graph.Graph) map[string]bool {
		m := map[string]bool{}
		for _, e := range g.Edges {
			if fitness.IsWrite(e) {
				m[strings.TrimPrefix(e.To, "boundary:")] = true
			}
		}
		return m
	}
	baseL := labels(base)
	var out []string
	for l := range labels(branch) {
		if !baseL[l] && !p.EffectRatchet.Allows(l) {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
}
