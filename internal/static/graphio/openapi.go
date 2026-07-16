package graphio

import (
	"fmt"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
	cg "github.com/jyang234/golang-code-graph/internal/static/callgraph"
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
//
// Wrapper descent (classify.openapiClients[i].followWrappers): when the callee's package
// opted in, a callee matching no operation is DESCENDED (descendWrapper — the SAME walk
// edgeOf uses, so naming and disclosure cannot disagree) before it is disclosed. On a
// COMPLETE walk the outcome mirrors edgeOf's naming exactly: exactly one operation reached
// means edgeOf already NAMED the edge, so the disclosure is skipped (a named edge is not
// also a blind spot — the agreement invariant); zero or multiple stays disclosed, the
// descent outcome appended to Detail. An INCOMPLETE walk (a depth-cap truncation or a
// bodiless un-widened hop — edgeOf then refuses to name it, by construction) is ALWAYS
// disclosed, even at exactly one operation, with an honest incomplete-walk detail naming
// what was found and why it cannot be trusted: the label set is only a lower bound, so a
// lone label is not proof the edge names exactly one op. Without the opt-in, Detail is
// byte-identical to the pre-descent message.
func openapiBlindSpots(res *analyze.Result, lab *openapi.Labeler) []blindspots.BlindSpot {
	var out []blindspots.BlindSpot
	seen := map[[2]string]bool{}
	for _, n := range res.Graph.Nodes {
		fn := n.Func
		if !res.Program.IsFirstPartyFunc(fn) || features.IsPackageInit(fn) {
			continue
		}
		// A spliced synthetic wrapper ($bound/$thunk/promotion) is never a renderable caller:
		// edgeOf's caller universe (firstPartyScope) excludes it so a plumbing "$bound" name
		// never appears in a verdict, and a disclosure Site must be a caller FQN a reviewer can
		// see — the SAME site convention. Skip it here so the two caller universes stay in
		// parity (a Site the manifest could never render is not an honest disclosure).
		if isSplicedWrapper(fn) {
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
			detail := "call to " + calleeFQN + " in declared openapi-client package " +
				features.EffectivePkgPath(callee) +
				" matched no spec operation; the outbound call cannot be named from the spec " +
				"(a client helper/transport, or generator drift that dropped an operation)"
			// Wrapper descent (followWrappers): the callee matched no generated-name shape,
			// but its package opted into descending its hand-written wrappers. Run the SAME
			// walk edgeOf runs — descendWrapper is the single source of truth, so naming and
			// disclosure can never disagree. On a COMPLETE walk, exactly one operation reached
			// => edgeOf NAMED the edge (via=openapi-client-wrapper), so SKIP the disclosure
			// (the agreement invariant: a named edge is never also a blind spot); zero or
			// multiple stays disclosed with the descent outcome appended — never guess between
			// candidates. An INCOMPLETE walk (depth-cap or bodiless hop) is ALWAYS disclosed,
			// even at one operation: edgeOf refuses to name it (its label set is only a lower
			// bound), so it is never a named edge and must not be silently dropped here. When
			// the package did not opt in, detail is BYTE-IDENTICAL to the pre-descent message
			// (the acceptance criterion). Pass e.Callee (the *cg.Node) so the walk has the
			// out-edges, not just the function.
			if lab.FollowWrappers(callee) {
				r := descendWrapper(lab, e.Callee)
				switch {
				case !r.complete():
					detail += incompleteWalkDetail(r) // lower-bound labels: disclose, never name
				case len(r.labels) == 1:
					continue // complete + one op => named by edgeOf; do not double-disclose
				case len(r.labels) == 0:
					detail += fmt.Sprintf("; descended %d declared-package function(s) and found 0 operations", r.visited)
				default:
					detail += fmt.Sprintf("; descended %d declared-package function(s) and found %d ambiguous operations (%s)", r.visited, len(r.labels), strings.Join(r.labels, "; "))
				}
			}
			out = append(out, blindspots.BlindSpot{
				Kind:   blindspots.UnresolvedSpecOperation,
				Site:   site,
				Detail: detail,
			})
		}
	}
	return out
}

// wrapperDescentDepthCap bounds descendWrapper's BFS through chained declared-package
// wrappers. 8 is generous: a real client's wrapper-over-generated-method layering is one
// or two hops, never eight. Unlike graphio's spliceDepthCap — which PANICS, because a
// synthetic splice chain that deep is a broken invariant and dropping the edge would fail
// OPEN for an absence proof — overflow here just STOPS that branch and MARKS the walk
// INCOMPLETE (descentResult.depthCapHit).
//
// The marking is load-bearing: raw truncation is NOT safe on its own, because the naming
// rule (exactly one label => name) is NON-monotonic in label-set size. Cutting the second,
// deeper operation of a genuinely 2-operation wrapper leaves ONE label behind — which the
// naming rule would read as a proven single-op edge and NAME, a confidently-wrong silent
// edge plus a suppressed disclosure (CLAUDE.md tenets 2 and 4). So an over-deep chain is
// not "fewer candidates, still sound"; the leftover label is a LOWER BOUND the walk must
// not name. edgeOf names only on a COMPLETE walk (complete() true), and the disclosure
// fires unconditionally on an incomplete one, which is what makes overflow push solely
// toward unresolved/disclosed. That is fail-closed, so it must NOT panic.
const wrapperDescentDepthCap = 8

// descentResult is descendWrapper's outcome. labels and visited are the walk's findings;
// the remaining fields record WHY the walk could not see the whole wrapper subtree, so a
// caller can tell a COMPLETE walk (every declared-package hop entered) from an INCOMPLETE
// one whose label set is only a LOWER BOUND. Every field is a deterministic function of the
// (sorted) call graph: labels and bodilessPkgs are sorted+deduped, visited and depthCapHit
// are order-independent.
type descentResult struct {
	labels       []string // sorted, deduped operation labels ("<peer> <METHOD> <template>") reached
	visited      int      // distinct declared-package functions whose body was walked, including start
	depthCapHit  bool     // a to-be-enqueued callee was cut by wrapperDescentDepthCap — a deeper chain is unseen
	bodilessPkgs []string // sorted, deduped declared packages a bodiless (un-widened) hop dead-ended in
}

// complete reports whether the walk entered every declared-package hop it reached: no
// depth-cap truncation and no bodiless dead-end. Only a complete walk's label set is EXACT
// — an incomplete walk may hide operations past the cut, so its single label is not proof
// the edge names exactly one operation, and the naming gate must refuse it (see the
// wrapperDescentDepthCap doc for why a lone leftover label is unsafe).
func (r descentResult) complete() bool { return !r.depthCapHit && len(r.bodilessPkgs) == 0 }

// descendWrapper walks outward from start — a callee in a declared client package that
// matched no generated-name shape (a hand-written wrapper, under followWrappers) —
// breadth-first over the already-sorted Node.Out (callgraph.finalize sorts every node's
// out-edges on intrinsic keys — callee FQN, caller FQN, site position — so no re-sort is
// needed and the walk is deterministic), restricted to functions in declared client
// packages. It returns a descentResult: the sorted, de-duplicated operation labels
// ("<peer> <METHOD> <template>", no "boundary:" prefix) reached, the count of DISTINCT
// declared-package functions whose body was walked (including start), and — the reason data
// that lets the caller refuse to name a walk it could not finish — whether the depth cap
// truncated a hop and the sorted, deduped declared packages a bodiless hop dead-ended in.
//
// Three deliberate rules:
//   - A reached OPERATION (Label ok) is recorded and NOT enqueued: an operation is a
//     leaf. Descending into a generated operation's internals (NewXxxRequest,
//     http.NewRequest) could only re-find the same op or conflate it with a sibling —
//     never add a distinct wrapper target — so stopping there keeps the label set honest.
//   - A callee NOT in a declared client package is never enqueued: descent across an
//     undeclared intermediate package is out of scope. An edge is named only when the
//     whole wrapper chain lives inside declared client packages.
//   - A to-be-enqueued callee cut by the depth cap, or a declared-package non-operation
//     callee with NO SSA body (Blocks == nil — its package was declared but not widened,
//     so followWrappers is unset on ITS own hint), makes the walk INCOMPLETE: an operation
//     may hide past the cut, so the recorded label set is only a LOWER BOUND. An
//     ALREADY-enqueued callee skipped as a revisit is NOT incompleteness (the first visit
//     walks it). complete() folds the two signals; the caller must not name an incomplete
//     walk.
//
// Determinism holds by construction: sorted Out + FIFO queue + sorted label/package output.
func descendWrapper(oapi *openapi.Labeler, start *cg.Node) descentResult {
	if oapi == nil || start == nil || start.Func == nil {
		return descentResult{}
	}
	type item struct {
		node  *cg.Node
		depth int
	}
	// enqueued is the visited set: start plus every declared-package callee ever queued.
	// Only InDeclaredPackage callees are ever enqueued, and start is InDeclaredPackage
	// (both callers gate on it), so each dequeue is one distinct declared-package function.
	enqueued := map[*cg.Node]bool{start: true}
	queue := []item{{start, 0}}
	labelSet := map[string]bool{}
	bodilessSet := map[string]bool{}
	var res descentResult
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		res.visited++
		for _, e := range cur.node.Out {
			callee := e.Callee
			if callee == nil || callee.Func == nil || !oapi.InDeclaredPackage(callee.Func) {
				continue // restricted to declared client packages
			}
			if label, ok := oapi.Label(callee.Func); ok {
				labelSet[label] = true // a reached operation is a leaf: record, do not descend
				continue
			}
			if enqueued[callee] {
				continue // a revisit is not incompleteness — the first visit already walks it
			}
			if cur.depth+1 > wrapperDescentDepthCap {
				res.depthCapHit = true // a deeper chain was truncated: the walk is incomplete
				continue
			}
			if callee.Func.Blocks == nil {
				// A declared-package non-operation callee with no SSA body: its package was
				// declared but not widened (no followWrappers on ITS own hint), so descent
				// cannot enter it and an operation may hide behind it. Record the dead-end
				// package and do not enqueue — the walk is incomplete (fail-closed).
				bodilessSet[features.EffectivePkgPath(callee.Func)] = true
				continue
			}
			enqueued[callee] = true
			queue = append(queue, item{callee, cur.depth + 1})
		}
	}
	res.labels = sortedKeys(labelSet)          // intrinsic order — deterministic output
	res.bodilessPkgs = sortedKeys(bodilessSet) // intrinsic order — deterministic output
	return res
}

// incompleteWalkDetail renders the descent-outcome append for an INCOMPLETE walk — one the
// depth cap truncated or that dead-ended at a bodiless un-widened hop. Unlike the two
// complete-walk appends it fires even at exactly one operation: an incomplete walk's label
// set is a LOWER BOUND (an operation may lie past the cut), so a lone label is NOT proof the
// edge names exactly one op, and naming it would be a confidently-wrong silent edge (CLAUDE.md
// tenets 2 and 4). It states the visited count, the found-operation count and sorted op list
// (when non-empty), and every reason the walk stopped short — the depth cap (with its value)
// and/or the sorted bodiless package(s) that need followWrappers on their own hint. A pure
// function of the deterministic descentResult, so it is byte-stable run to run.
func incompleteWalkDetail(r descentResult) string {
	found := fmt.Sprintf("found %d operation(s)", len(r.labels))
	if len(r.labels) > 0 {
		found += " (" + strings.Join(r.labels, "; ") + ")"
	}
	var reasons []string
	if r.depthCapHit {
		reasons = append(reasons, fmt.Sprintf("the descent hit the depth cap of %d, so a deeper wrapper chain is unresolved", wrapperDescentDepthCap))
	}
	if len(r.bodilessPkgs) > 0 {
		reasons = append(reasons, "it dead-ended at bodiless function(s) in the declared package(s) "+
			strings.Join(r.bodilessPkgs, ", ")+", which need followWrappers on their own hint to be descended")
	}
	return fmt.Sprintf("; descended %d declared-package function(s) and %s, but the walk is INCOMPLETE so the edge is not named: %s",
		r.visited, found, strings.Join(reasons, "; "))
}
