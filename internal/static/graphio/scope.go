package graphio

import (
	"golang.org/x/tools/go/ssa"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	cg "github.com/jyang234/golang-code-graph/internal/static/callgraph"
	"github.com/jyang234/golang-code-graph/internal/static/features"
)

// firstPartyScope is the set of all reachable first-party functions, EXCLUDING
// package initializers. init is seeded into RTA so addresses taken during package
// initialization (the registration idiom) resolve dispatch — but init ordering is
// runtime plumbing, not service behavior, so rendering it would add an init node per
// package plus init→init edges to every graph (pure noise that no consumer reads;
// the obligations layer handles init-time obligations separately). The recovered
// dispatch edges are between real route-reachable functions, so excluding init from
// the rendered graph keeps the recovery while leaving the service graph clean. The
// edgeOf init-callee guard is the backstop should init ever re-enter scope.
func firstPartyScope(res *analyze.Result) map[*ssa.Function]bool {
	s := make(map[*ssa.Function]bool)
	for _, n := range res.Graph.Nodes {
		if features.IsPackageInit(n.Func) {
			continue
		}
		if res.Program.IsFirstParty(n.Func.Pkg) {
			s[n.Func] = true
		}
	}
	return s
}

// rootFuncSet is the set of root functions, for tier purposes (roots are entries).
func rootFuncSet(res *analyze.Result) map[*ssa.Function]bool {
	s := make(map[*ssa.Function]bool)
	for _, r := range res.Roots.Roots {
		s[r.Func] = true
	}
	return s
}

// rootByName returns the root function registered under name (a route or topic).
func rootByName(res *analyze.Result, name string) *ssa.Function {
	for _, r := range res.Roots.Roots {
		if r.Name == name {
			return r.Func
		}
	}
	return nil
}

// reachableFirstParty returns the first-party functions reachable from root,
// pruning the traversal at the boundary (it does not descend into dependencies).
func reachableFirstParty(res *analyze.Result, root *ssa.Function) map[*ssa.Function]bool {
	seen := make(map[*ssa.Function]bool)
	start := res.Graph.Node(root)
	if start == nil {
		return seen
	}
	visited := map[*cg.Node]bool{start: true}
	queue := []*cg.Node{start}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		seen[n.Func] = true
		for _, e := range n.Out {
			c := e.Callee
			if visited[c] || !res.Program.IsFirstParty(c.Func.Pkg) {
				continue
			}
			visited[c] = true
			queue = append(queue, c)
		}
	}
	return seen
}

// fallible reports whether fn returns or propagates an error.
func fallible(fn *ssa.Function) bool { return features.Fallible(fn) }
