// Package callgraph builds a deterministic call graph for one service unit from
// its discovered roots. RTA from the discovered roots is the default — precise
// enough for services and cheap in CI; VTA refines interface-dense code, and CHA
// is the rootless-library fallback. The chosen algorithm and its caveats are
// recorded so a reviewer knows the soundness/precision trade-off that was made.
//
// The graph is the raw reachable structure: every reachable function is a node,
// every resolved call is an edge. Projecting it to the first-party-plus-typed-
// boundary view is a later stage; this package's job is to produce that structure
// deterministically — nodes sorted by fully-qualified name, each node's edges
// sorted likewise — so regenerating it yields byte-identical output.
package callgraph

import (
	"fmt"
	"go/types"
	"sort"

	xcg "golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/cha"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/callgraph/vta"
	"golang.org/x/tools/go/ssa"

	"github.com/jyang234/golang-code-graph/internal/static/features"
	"github.com/jyang234/golang-code-graph/internal/static/roots"
	"github.com/jyang234/golang-code-graph/internal/static/ssabuild"
)

// Algo is a call-graph construction algorithm.
type Algo string

const (
	AlgoRTA Algo = "rta" // Rapid Type Analysis from roots (default)
	AlgoVTA Algo = "vta" // Variable Type Analysis, RTA-seeded (refine)
	AlgoCHA Algo = "cha" // Class Hierarchy Analysis, rootless (fallback)
)

// Options selects the construction algorithm. The zero value means RTA.
type Options struct {
	Algo Algo
}

// Node is one reachable function.
type Node struct {
	FQN  string
	Func *ssa.Function
	Out  []*Edge // outgoing edges, sorted
	In   []*Edge // incoming edges, sorted
}

// Edge is one resolved call from Caller to Callee. Site is the call instruction
// (nil for synthetic edges), which later stages map to an AST position.
type Edge struct {
	Caller *Node
	Callee *Node
	Site   ssa.CallInstruction
}

// Graph is the deterministic reachable call graph.
type Graph struct {
	Algo    Algo
	Caveats []string
	Nodes   []*Node // sorted by FQN
	byFunc  map[*ssa.Function]*Node
	// wrapperNodes collapses the interchangeable synthetic wrappers go/ssa mints per
	// use-site to one node (see mergeKey); transient, populated only during construction.
	wrapperNodes map[wrapperKey]*Node
}

// Build constructs the call graph for prog rooted at rs per opt. When RTA is
// requested but no roots were discovered (a library with no resolvable entry
// points), it falls back to CHA and records the substitution as a caveat.
func Build(prog *ssabuild.Program, rs *roots.Result, opt Options) (*Graph, error) {
	algo := opt.Algo
	if algo == "" {
		algo = AlgoRTA
	}
	rootFns := rs.Funcs()
	var caveats []string
	if (algo == AlgoRTA || algo == AlgoVTA) && len(rootFns) == 0 {
		caveats = append(caveats, fmt.Sprintf("no roots discovered; fell back from %s to cha (sound but over-approximate)", algo))
		algo = AlgoCHA
	}

	var x *xcg.Graph
	switch algo {
	case AlgoRTA:
		x = rta.Analyze(rootFns, true).CallGraph
		caveats = append(caveats, fmt.Sprintf("rta from %d discovered root(s)", rs.DiscoveredRootCount()))
	case AlgoVTA:
		seed := rta.Analyze(rootFns, true).CallGraph
		x = vta.CallGraph(reachableSet(seed), seed)
		caveats = append(caveats, fmt.Sprintf("vta refined over rta from %d discovered root(s)", rs.DiscoveredRootCount()))
	case AlgoCHA:
		x = cha.CallGraph(prog.Prog)
		caveats = append(caveats, "cha over whole program; dynamic dispatch over-approximated")
	default:
		return nil, fmt.Errorf("callgraph: unknown algorithm %q", opt.Algo)
	}
	if len(rs.BlindSpots) > 0 {
		caveats = append(caveats, fmt.Sprintf("%d unresolved registration(s) recorded as blind spots", len(rs.BlindSpots)))
	}

	g := fromX(x, algo, caveats)
	return g, nil
}

// reachableSet returns the functions reachable in x, as VTA's analysis scope.
func reachableSet(x *xcg.Graph) map[*ssa.Function]bool {
	set := make(map[*ssa.Function]bool, len(x.Nodes))
	for fn := range x.Nodes {
		if fn != nil {
			set[fn] = true
		}
	}
	return set
}

// fromX converts an x/tools call graph into the deterministic Graph, dropping the
// synthetic root node (nil function) while keeping every real reachable function.
func fromX(x *xcg.Graph, algo Algo, caveats []string) *Graph {
	g := &Graph{
		Algo:         algo,
		Caveats:      caveats,
		byFunc:       make(map[*ssa.Function]*Node),
		wrapperNodes: make(map[wrapperKey]*Node),
	}

	for fn := range x.Nodes {
		if fn != nil {
			g.node(fn)
		}
	}
	// One edge per call site is preserved — a function calling the same callee
	// both normally and under `go` is two semantically distinct edges, and feature
	// extraction reads each site's concurrency context. Edges are keyed by (caller
	// node, callee node, source position): two instructions can share a position (a
	// generic body duplicated across instantiations, or the tail call inside two
	// merged wrappers), and collapsing those keeps position a total tiebreaker for
	// ordering, so the edge list is fully deterministic. Both endpoints are keyed by
	// NODE, not raw *ssa.Function, so edges from/into merged wrapper nodes (see
	// mergeKey) de-duplicate across the SSA functions that collapsed into one node.
	type edgeKey struct {
		caller *Node
		callee *Node
		pos    int
	}
	seen := make(map[edgeKey]bool)
	for _, xn := range x.Nodes {
		if xn.Func == nil {
			continue
		}
		caller := g.node(xn.Func)
		for _, e := range xn.Out {
			if e.Callee == nil || e.Callee.Func == nil {
				continue
			}
			callee := g.node(e.Callee.Func)
			k := edgeKey{caller, callee, sitePos(e.Site)}
			if seen[k] {
				continue
			}
			seen[k] = true
			edge := &Edge{Caller: caller, Callee: callee, Site: e.Site}
			caller.Out = append(caller.Out, edge)
			callee.In = append(callee.In, edge)
		}
	}
	g.finalize()
	return g
}

// node returns the graph node for fn, creating it on first use. A mergeable synthetic
// wrapper (see mergeKey) shares ONE node with every other use-site copy of the same
// wrapper: the first copy creates the node, later copies alias to it. Every raw
// *ssa.Function that reaches here is recorded in byFunc, so a later g.Node(fn) lookup
// by any copy still resolves to the shared node.
func (g *Graph) node(fn *ssa.Function) *Node {
	if n, ok := g.byFunc[fn]; ok {
		return n
	}
	if key, ok := mergeKey(fn); ok {
		if n, ok := g.wrapperNodes[key]; ok {
			g.byFunc[fn] = n // alias this copy to the already-built merged node
			return n
		}
		n := g.newNode(fn)
		g.wrapperNodes[key] = n
		return n
	}
	return g.newNode(fn)
}

// newNode builds and registers a fresh node for fn. The merge class of a wrapper is
// byte-identical (same wrapped Object, RelString, and body), so which copy iteration
// happens to reach here first — and thus becomes the node's Func — does not change the
// node's FQN or its edges: map-iteration order over x.Nodes does not leak into output.
func (g *Graph) newNode(fn *ssa.Function) *Node {
	n := &Node{FQN: fn.RelString(nil), Func: fn}
	g.byFunc[fn] = n
	g.Nodes = append(g.Nodes, n)
	return n
}

// wrapperKey is the identity of a mergeable synthetic wrapper: its display FQN (which
// carries the wrapper KIND — $bound vs $thunk — and the full receiver display,
// including a generic receiver's type arguments) plus the wrapped method object. Two
// wrappers with an equal key are byte-identical, so they share one node.
type wrapperKey struct {
	fqn string
	obj types.Object
}

// mergeKey returns fn's wrapper identity and true when fn is one of the receiver-less
// method-value / method-expression forwarders go/ssa mints fresh per use-site.
// createBound (MethodVal, "$bound") and createThunk (MethodExpr, "$thunk") in x/tools
// go/ssa build a NEW wrapper at every occurrence and never cache it, so K uses of one
// method M yield K distinct *ssa.Function that are byte-identical — same wrapped
// Object, same Synthetic kind, same RelString, same pos (obj.Pos()) — differing only
// in pointer identity. They collide on BOTH the display FQN and the
// InstanceDiscriminator, the exact tie finalize() cannot order, and pos is identical
// too, so no tie-break could separate them: the sound resolution is to MERGE the
// interchangeable copies, not to fabricate an order between identical things.
//
// The class is deliberately restricted to receiver-less forwarders
// (Signature.Recv() == nil) — exactly bound+thunk, the two UNCACHED kinds. Everything
// else is excluded ON PURPOSE so an unproven collision fails LOUD at finalize() rather
// than being silently merged (CLAUDE.md: fail closed; soundness is asymmetric): a
// promotion/interface wrapper carries a receiver and is cached by go/ssa (it never
// duplicates), and a generic INSTANCE (TypeArgs != 0) has a real per-instantiation
// body. This is the intentionally NARROWER dedup subset of graphio.isSplicedWrapper's
// spliced-wrapper set (which keys on Pkg == nil for the different splice-vs-render
// question); the two are NOT folded into one predicate because they answer different
// questions.
//
// Merge safety rests on the KEY, not on Object being unique: two wrappers merge only
// when they share RelString(nil) AND the wrapped Object. RelString carries the full
// receiver display, so wrappers over different instantiations of one generic method —
// (*Store[int]).Get$bound vs (*Store[string]).Get$bound, whose Object() is the SHARED
// origin generic (taint.go:75, features.EffectivePkgPath) — get DIFFERENT keys and do
// not merge. A receiver-less forwarder with equal RelString and equal Object is
// byte-identical, so the merge cannot collapse two behaviorally distinct functions.
func mergeKey(fn *ssa.Function) (wrapperKey, bool) {
	if fn == nil || fn.Synthetic == "" || fn.Object() == nil ||
		len(fn.TypeArgs()) != 0 || fn.Signature == nil || fn.Signature.Recv() != nil {
		return wrapperKey{}, false
	}
	return wrapperKey{fqn: fn.RelString(nil), obj: fn.Object()}, true
}

// finalize sorts nodes and each node's edges into canonical order. The node sort
// tie-breaks on features.InstanceDiscriminator because a generic instance's FQN
// (fn.RelString) is documented non-unique: an FQN-only comparator over the
// map-iteration-ordered node set is nondeterministic on such a tie (M-20). The one
// FQN+discriminator collision go/ssa is known to produce — interchangeable synthetic
// $bound/$thunk wrappers minted per use-site — is merged upstream in node() (see
// mergeKey). A collision surviving to here is therefore either genuinely un-orderable
// or an unrecognized synthetic class outside that proven-identical merge set; either
// way, fail loudly rather than emit a run-varying order (determinism before
// convenience), so an unproven duplicate trips this guard instead of being silently
// merged.
func (g *Graph) finalize() {
	sort.Slice(g.Nodes, func(i, j int) bool {
		a, b := g.Nodes[i], g.Nodes[j]
		if a.FQN != b.FQN {
			return a.FQN < b.FQN
		}
		ka, kb := features.InstanceDiscriminator(a.Func), features.InstanceDiscriminator(b.Func)
		if ka != kb {
			return ka < kb
		}
		if a.Func != b.Func {
			panic(fmt.Sprintf("callgraph: two distinct functions share sort key %q (discriminator %q) — cannot order deterministically", a.FQN, ka))
		}
		return false
	})
	for _, n := range g.Nodes {
		sort.Slice(n.Out, func(i, j int) bool { return edgeLess(n.Out[i], n.Out[j]) })
		sort.Slice(n.In, func(i, j int) bool { return edgeLess(n.In[i], n.In[j]) })
	}
}

// edgeLess orders edges by callee then caller then call-site position, a total
// order over the de-duplicated edge set.
func edgeLess(a, b *Edge) bool {
	if a.Callee.FQN != b.Callee.FQN {
		return a.Callee.FQN < b.Callee.FQN
	}
	if a.Caller.FQN != b.Caller.FQN {
		return a.Caller.FQN < b.Caller.FQN
	}
	return sitePos(a.Site) < sitePos(b.Site)
}

// Lookup returns the node for the function with the given FQN, or nil.
func (g *Graph) Lookup(fqn string) *Node {
	for _, n := range g.Nodes {
		if n.FQN == fqn {
			return n
		}
	}
	return nil
}

// Node returns the node for fn, or nil if it is not in the graph.
func (g *Graph) Node(fn *ssa.Function) *Node { return g.byFunc[fn] }

// Reachable reports whether any reachable node's FQN equals fqn.
func (g *Graph) Reachable(fqn string) bool { return g.Lookup(fqn) != nil }

// sitePos is a stable ordering key for a (possibly nil) call site.
func sitePos(site ssa.CallInstruction) int {
	if site == nil {
		return -1
	}
	return int(site.Pos())
}
