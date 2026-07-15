package graphio

import (
	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
	"github.com/jyang234/golang-code-graph/internal/static/features"
	"github.com/jyang234/golang-code-graph/internal/static/openapi"
)

// openapiBlindSpots discloses every first-party call into a DECLARED OpenAPI-client
// package whose callee matched no spec operation — the honesty channel for the opt-in
// --reclaim-openapi labeler (CLAUDE.md tenet 3). The labeler NAMES the operation calls
// it resolves (boundary:<peer> <METHOD> <template>) and discloses the ones it cannot
// HERE, with the callee FQN, so a client helper/transport call — or generator drift
// that dropped an operation the code still calls — is a tracked fact rather than a
// silently-unnamed edge or a guessed label.
//
// It walks the SAME reachable first-party nodes blindspots.Detect does (IsFirstPartyFunc,
// skipping package inits), so its scope matches the rest of the manifest. A caller that
// is ITSELF inside a declared client package is skipped: the client's own internal
// plumbing (New<Op>Request calling fmt.Sprintf / http.NewRequest) is not the service's
// outbound surface, and disclosing it would bury the genuine service→client gaps. Site
// is the caller FQN (the convention every other blind spot follows); Detail names the
// callee FQN and its package, so a reviewer sees exactly which generated-client function
// fell through. Deterministic: the walk order is res.Graph.Nodes (FQN-sorted), a repeated
// (caller, callee) pair is deduped here, and the result is sorted through the one
// canonical blindspots comparator by the caller in Build.
func openapiBlindSpots(res *analyze.Result, lab *openapi.Labeler) []blindspots.BlindSpot {
	var out []blindspots.BlindSpot
	seen := map[[2]string]bool{}
	for _, n := range res.Graph.Nodes {
		fn := n.Func
		if !res.Program.IsFirstPartyFunc(fn) || features.IsPackageInit(fn) {
			continue
		}
		// The client's own internal calls are not the service's outbound edges: skip a
		// caller inside a declared client package so its plumbing is neither labeled
		// (edgeOf applies the same guard) nor disclosed here.
		if lab.InDeclaredPackage(fn) {
			continue
		}
		site := fn.RelString(nil)
		for _, e := range n.Out {
			callee := e.Callee.Func
			if callee == nil || !lab.InDeclaredPackage(callee) {
				continue
			}
			if _, ok := lab.Label(callee); ok {
				continue // a resolved operation is NAMED (edgeOf), not disclosed
			}
			calleeFQN := callee.RelString(nil)
			key := [2]string{site, calleeFQN}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, blindspots.BlindSpot{
				Kind: blindspots.UnresolvedSpecOperation,
				Site: site,
				Detail: "call to " + calleeFQN + " in declared openapi-client package " +
					features.EffectivePkgPath(callee) +
					" matched no spec operation; the outbound call cannot be named from the spec " +
					"(a client helper/transport, or generator drift that dropped an operation)",
			})
		}
	}
	return out
}
