package graphio

import (
	"fmt"
	"sort"
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
// edgeOf uses, so naming and disclosure cannot disagree) before it is disclosed. Exactly
// one operation reached means edgeOf already NAMED the edge, so the disclosure is skipped
// entirely (a named edge is not also a blind spot — the agreement invariant); zero or
// multiple stays disclosed, with the descent outcome appended to Detail so the reviewer
// sees why the wrapper could not be named. Without the opt-in, Detail is byte-identical
// to the pre-descent message.
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
			detail := "call to " + calleeFQN + " in declared openapi-client package " +
				features.EffectivePkgPath(callee) +
				" matched no spec operation; the outbound call cannot be named from the spec " +
				"(a client helper/transport, or generator drift that dropped an operation)"
			// Wrapper descent (followWrappers): the callee matched no generated-name shape,
			// but its package opted into descending its hand-written wrappers. Run the SAME
			// walk edgeOf runs — descendWrapper is the single source of truth, so naming and
			// disclosure can never disagree. Exactly one operation reached => edgeOf NAMED
			// the edge (via=openapi-client-wrapper), so SKIP the disclosure (the agreement
			// invariant: a named edge is never also a blind spot). Zero or multiple stays
			// disclosed with the descent outcome appended — never guess between candidates.
			// When the package did not opt in, detail is BYTE-IDENTICAL to the pre-descent
			// message (the acceptance criterion). Pass e.Callee (the *cg.Node) so the walk
			// has the out-edges, not just the function.
			if lab.FollowWrappers(callee) {
				visited, labels := descendWrapper(lab, e.Callee)
				switch len(labels) {
				case 1:
					continue // named by edgeOf; do not double-disclose a named edge
				case 0:
					detail += fmt.Sprintf("; descended %d declared-package function(s) and found 0 operations", visited)
				default:
					detail += fmt.Sprintf("; descended %d declared-package function(s) and found %d ambiguous operations (%s)", visited, len(labels), strings.Join(labels, "; "))
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
// OPEN for an absence proof — overflow here just STOPS the branch. That can only push the
// result toward "fewer operations reached" (unresolved / disclosed), never fabricate a
// label: descent NAMES an edge only on exactly one operation found, so a truncated walk
// can only lose candidates, never invent one. That is fail-closed, so it must NOT panic.
const wrapperDescentDepthCap = 8

// descendWrapper walks outward from start — a callee in a declared client package that
// matched no generated-name shape (a hand-written wrapper, under followWrappers) —
// breadth-first over the already-sorted Node.Out (callgraph.finalize sorts every node's
// out-edges on intrinsic keys — callee FQN, caller FQN, site position — so no re-sort is
// needed and the walk is deterministic), restricted to functions in declared client
// packages. It returns the count of DISTINCT declared-package functions visited
// (including start) and the sorted, de-duplicated operation labels
// ("<peer> <METHOD> <template>", no "boundary:" prefix) reached.
//
// Two deliberate rules, both per the FR:
//   - A reached OPERATION (Label ok) is recorded and NOT enqueued: an operation is a
//     leaf. Descending into a generated operation's internals (NewXxxRequest,
//     http.NewRequest) could only re-find the same op or conflate it with a sibling —
//     never add a distinct wrapper target — so stopping there keeps the label set honest.
//   - A callee NOT in a declared client package is never enqueued: descent across an
//     undeclared intermediate package is out of scope for this FR. An edge is named only
//     when the whole wrapper chain lives inside declared client packages.
//
// Overflow past wrapperDescentDepthCap stops that branch (fail-closed; see the cap's doc).
// Determinism holds by construction: sorted Out + FIFO queue + sorted label output.
func descendWrapper(oapi *openapi.Labeler, start *cg.Node) (visited int, labels []string) {
	if oapi == nil || start == nil || start.Func == nil {
		return 0, nil
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
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		visited++
		for _, e := range cur.node.Out {
			callee := e.Callee
			if callee == nil || callee.Func == nil || !oapi.InDeclaredPackage(callee.Func) {
				continue // restricted to declared client packages
			}
			if label, ok := oapi.Label(callee.Func); ok {
				labelSet[label] = true // a reached operation is a leaf: record, do not descend
				continue
			}
			if enqueued[callee] || cur.depth+1 > wrapperDescentDepthCap {
				continue
			}
			enqueued[callee] = true
			queue = append(queue, item{callee, cur.depth + 1})
		}
	}
	labels = make([]string, 0, len(labelSet))
	for l := range labelSet {
		labels = append(labels, l)
	}
	sort.Strings(labels) // intrinsic order — deterministic output
	return visited, labels
}
