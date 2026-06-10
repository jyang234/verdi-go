package fitness

import (
	"fmt"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// checkLayering enforces the declared layering: a call may stay within a layer or
// descend to the immediately-adjacent layer, but never skip a layer downward and
// never call upward. Edges out of a root package (the composition root, which
// constructs every layer) are exempt, as are explicitly allow-listed edges — so
// the gate fires only on the violations a human has not already reviewed.
//
// Only edges whose *both* endpoints belong to a declared layer are judged; a call
// into an unassigned helper package (codec, eventbus) is outside the layering
// model, not a violation. This is the "derive invariants from the measured
// baseline" discipline: the policy declares which packages are layers, and
// everything else is left alone.
func checkLayering(p *policy.Policy, ix *graph.Index, r *Result) {
	if p.Layering == nil {
		return
	}
	for _, from := range ix.Nodes() {
		fromPkg := PkgOf(from)
		if isRootPkg(p.Layering.Roots, fromPkg) {
			continue
		}
		fromLayer := p.LayerOf(fromPkg)
		if fromLayer == "" {
			continue
		}
		fromRank, _ := p.LayerRank(fromLayer)
		for _, to := range ix.Callees(from) {
			toLayer := p.LayerOf(PkgOf(to))
			if toLayer == "" || toLayer == fromLayer {
				continue
			}
			toRank, _ := p.LayerRank(toLayer)
			if toRank == fromRank+1 {
				continue // adjacent descent: the intended shape
			}
			if exempted(p.Layering.Allow, from, to) {
				continue
			}
			r.add(Finding{
				Rule:     "layering",
				Severity: Violation,
				Summary:  layeringSummary(fromLayer, toLayer, fromRank, toRank),
				From:     from,
				To:       to,
			})
		}
	}
}

// layeringSummary describes the broken relationship in layer terms.
func layeringSummary(fromLayer, toLayer string, fromRank, toRank int) string {
	if toRank > fromRank+1 {
		return fmt.Sprintf("%s → %s skips %d layer(s)", fromLayer, toLayer, toRank-fromRank-1)
	}
	return fmt.Sprintf("%s → %s calls upward", fromLayer, toLayer)
}

// isRootPkg reports whether pkg is one of the exact root package paths.
func isRootPkg(roots []string, pkg string) bool {
	for _, r := range roots {
		if pkg == r {
			return true
		}
	}
	return false
}

// exempted reports whether the edge from→to is allow-listed. From/To in an
// exception are prefixes, so an entry can name one edge, a type, or a package.
func exempted(allow []policy.Exception, from, to string) bool {
	for _, ex := range allow {
		if strings.HasPrefix(from, ex.From) && strings.HasPrefix(to, ex.To) {
			return true
		}
	}
	return false
}
