package fitness

import (
	"fmt"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
	"github.com/jyang234/golang-code-graph/internal/groundwork/setutil"
)

// checkIOBudget caps the external *write* effects reachable from a single route
// — the side-effect-blowout guard. Each structural entrypoint (Sources) is judged
// independently, EXCEPT the composition root (main), which is an entrypoint but
// not a route and whose startup writes (migrations, seeding) must not be charged
// against a per-route budget. Reads (DB SELECT, outbound GET, bus consume) do not
// count, only mutations (DB INSERT/UPDATE/DELETE, bus PUBLISH, outbound
// POST/PUT/PATCH/DELETE). "Route" is approximated by "non-root entrypoint"; the
// boundary contract refines it to named HTTP routes and bus consumers.
func checkIOBudget(p *policy.Policy, ix *graph.Index, r *Result) {
	if p.IOBudget == nil {
		return
	}
	max := p.IOBudget.MaxWritesPerRoute
	routes := RouteWrites(p, ix)
	for _, src := range setutil.SortedKeys(routes) {
		writes := routes[src].Writes
		if len(writes) > max {
			r.add(Finding{
				Rule:     "io_budget",
				Severity: Violation,
				Summary:  fmt.Sprintf("%s reaches %d write(s) over a budget of %d: %s", ShortName(src), len(writes), max, strings.Join(writes, ", ")),
				From:     src,
			})
		}
	}
}

// RouteIO is one route's external write surface: the sorted distinct write
// targets (sans "boundary:") reachable from it, and whether the route's cone
// touches a blind spot — in which case Writes is a lower bound, not a count.
type RouteIO struct {
	Writes []string
	Blind  bool
}

// RouteWrites computes the write surface of every route (non-root entrypoint),
// with checkIOBudget's exact semantics — one computation, shared with the
// review artifact's per-route delta section so the two surfaces can never
// disagree about what a route writes.
func RouteWrites(p *policy.Policy, ix *graph.Index) map[string]RouteIO {
	roots := p.RootPackages()
	out := map[string]RouteIO{}
	for _, src := range ix.Sources() {
		if isRootPkg(roots, PkgOf(src)) {
			continue // the composition root (main) is an entrypoint but not a route
		}
		cone := append([]string{src}, ix.Reachable(src)...)
		effects := ix.Effects(cone...)
		writes := map[string]bool{}
		for _, e := range effects {
			if IsWrite(e) {
				writes[strings.TrimPrefix(e.To, "boundary:")] = true
			}
		}
		_, blind := frontierBlindSiteWith(ix, cone, effects)
		out[src] = RouteIO{Writes: setutil.SortedKeys(writes), Blind: blind}
	}
	return out
}

// IsWrite reports whether a boundary effect mutates external state. The effect
// label is "<system> <op> <target>": db with a mutating SQL verb, bus PUBLISH, or
// an outbound HTTP call with a mutating method. It is shared with the review
// surface, which classifies the same effects in an MR's I/O-effect section.
func IsWrite(e graph.Edge) bool {
	if !e.IsBoundary() {
		return false
	}
	f := strings.Fields(strings.TrimPrefix(e.To, "boundary:"))
	if len(f) < 2 {
		return false
	}
	op := strings.ToUpper(f[1])
	switch f[0] {
	case "db":
		switch op {
		case "INSERT", "UPDATE", "DELETE", "UPSERT", "MERGE", "REPLACE":
			return true
		default: // SELECT and other reads
			return false
		}
	case "bus":
		return op == "PUBLISH"
	default: // outbound HTTP: "<peer> <METHOD> <route>"
		switch op {
		case "POST", "PUT", "PATCH", "DELETE":
			return true
		default: // GET, HEAD, OPTIONS
			return false
		}
	}
}
