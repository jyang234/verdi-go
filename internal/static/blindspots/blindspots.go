// Package blindspots detects where the static analysis is blind at the service
// boundary, so a reviewer never operates on false completeness (static-extractor
// spec §7). The boundary subset of this manifest is part of the GATED artifact:
// if a PR introduces a dynamically-named publish or an unresolved dispatch at the
// boundary, the manifest changes and routes to a human — the one genuine hole
// becomes a tracked fact instead of a silent miss.
//
// The headline category is NonConstantBoundaryArg: a publish or outbound call
// whose target (event name, or peer/method/route) is not a string constant, so
// the effect is real but cannot be named.
package blindspots

import (
	"sort"

	"golang.org/x/tools/go/ssa"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/features"
)

// Kind is a blind-spot category.
type Kind string

const (
	// NonConstantBoundaryArg is a publish/RPC with a non-literal target — the
	// gated, reviewable hole.
	NonConstantBoundaryArg Kind = "NonConstantBoundaryArg"
	// UnresolvedDispatch is a registration whose handler could not be resolved.
	UnresolvedDispatch Kind = "UnresolvedDispatch"
	// Reflect is reflective code, invisible to the call graph.
	Reflect Kind = "reflect"
)

// BlindSpot is one disclosed gap. Fields are JSON-tagged for the gated artifact.
type BlindSpot struct {
	Kind   Kind   `json:"kind"`
	Site   string `json:"site"`
	Detail string `json:"detail"`
}

// Detect returns the boundary blind spots reachable in the analyzed program,
// sorted and de-duplicated for deterministic, gated output.
func Detect(res *analyze.Result, hints *features.HintSet) []BlindSpot {
	var out []BlindSpot

	// Unresolved handler registrations, surfaced by root discovery.
	for _, bs := range res.Roots.BlindSpots {
		out = append(out, BlindSpot{
			Kind:   UnresolvedDispatch,
			Site:   bs.Registrar,
			Detail: bs.Detail,
		})
	}

	// Boundary calls with non-constant targets, in reachable first-party code.
	for _, n := range res.Graph.Nodes {
		fn := n.Func
		if !res.Program.IsFirstParty(fn.Pkg) {
			continue
		}
		site := fn.RelString(nil)
		for _, e := range n.Out {
			callee := e.Callee.Func
			switch {
			case hints.IsPublish(callee):
				if !constStrings(e.Site, 1) {
					out = append(out, BlindSpot{
						Kind:   NonConstantBoundaryArg,
						Site:   site,
						Detail: "publish event name is not a string constant; the published event cannot be named statically",
					})
				}
			case hints.IsHTTP(callee):
				if !constStrings(e.Site, 3) {
					out = append(out, BlindSpot{
						Kind:   NonConstantBoundaryArg,
						Site:   site,
						Detail: "outbound peer/method/route is not a string constant; the dependency cannot be named statically",
					})
				}
			case pkgPathOf(callee) == "reflect":
				out = append(out, BlindSpot{
					Kind:   Reflect,
					Site:   site,
					Detail: "reflective call; downstream edges are invisible to the static call graph",
				})
			}
		}
	}

	return dedupSort(out)
}

// constStrings reports whether the call site supplies at least n string arguments
// and the first n are all constants — the condition for naming the boundary
// effect.
func constStrings(site ssa.CallInstruction, n int) bool {
	args := features.StringArgs(site)
	if len(args) < n {
		return false
	}
	for i := 0; i < n; i++ {
		if _, ok := features.ConstString(args[i]); !ok {
			return false
		}
	}
	return true
}

func pkgPathOf(fn *ssa.Function) string {
	if fn == nil || fn.Pkg == nil || fn.Pkg.Pkg == nil {
		return ""
	}
	return fn.Pkg.Pkg.Path()
}

func dedupSort(in []BlindSpot) []BlindSpot {
	seen := make(map[BlindSpot]bool, len(in))
	out := make([]BlindSpot, 0, len(in))
	for _, b := range in {
		if seen[b] {
			continue
		}
		seen[b] = true
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Site != b.Site {
			return a.Site < b.Site
		}
		return a.Detail < b.Detail
	})
	return out
}
