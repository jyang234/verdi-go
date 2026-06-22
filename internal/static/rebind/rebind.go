// Package rebind is flowmap's EXPERIMENTAL, opt-in de-union pass. It is the ONLY pass
// that REMOVES call edges — the soundness-dangerous direction — so it is gated heavily
// and is never on by default.
//
// The problem it addresses is the over-report dual of the TxClosure under-report. A
// shared runner invokes a passed callback at a single site — `RunInTx`'s `fn(exec)` —
// and because the call graph is context-insensitive that site resolves to EVERY closure
// that flows to the runner across all callers. So eight commands that each hand the
// runner their own writing closure each appear to reach the UNION of all eight writes
// (CmdA looks like it issues CmdB's INSERT). De-unioning ADDs each command's precise
// enclosing-fn→own-closure edge and REMOVEs the shared runner→closure union edges, so a
// command reaches only its own write.
//
// Removing the union edge is sound ONLY when the closure is CONFINED to the one runner
// call we rebind: its sole use in its parent is that call argument. Any escape — the
// closure passed to a second invoker (a helper), stored, returned, sent on a channel, or
// captured into another closure — means the closure may be invoked on another path, so
// removing the union edge would drop a real can-reach edge (a false absence: a
// must_not_reach gate could flip VIOLATED→false-PASS). On ANY such escape the pass
// ABSTAINS and keeps the full union. The guard is conservatively complete because a
// closure cannot leave its parent except as a value operand of some instruction, and
// confined() rejects every operand use that is not the single rebind call.
package rebind

import (
	"sort"

	"golang.org/x/tools/go/ssa"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/reclaim"
)

// Via attributes an added edge to the rebind pass (provenance, like reclaim's Via).
const Via = "rebind"

// Edge is a precise command→closure edge to ADD (FQNs in RelString form).
type Edge struct{ From, To string }

// Plan is the de-union mutation: the precise edges to ADD and the union edges to REMOVE.
// Both are sorted, so a Plan is a deterministic function of the SSA.
type Plan struct {
	Add    []Edge      // enclosing-fn → confined closure
	Remove [][2]string // (runner, closure) union edges to drop
}

// Compute returns the de-union Plan for res: for every closure passed to a static
// callee that DIRECTLY invokes the corresponding parameter (so the graph carries a
// concrete runner→closure union edge) AND that is CONFINED to that single call, it adds
// enclosing→closure and removes runner→closure. A closure that escapes its parent keeps
// its union (it contributes neither an add nor a remove).
func Compute(res *analyze.Result) Plan {
	var plan Plan
	addSeen := map[[2]string]bool{}
	remSeen := map[[2]string]bool{}
	for _, n := range res.Graph.Nodes {
		f := n.Func
		if f == nil {
			continue
		}
		for _, b := range f.Blocks {
			for _, instr := range b.Instrs {
				call, ok := instr.(ssa.CallInstruction)
				if !ok {
					continue
				}
				common := call.Common()
				runner := common.StaticCallee()
				if runner == nil {
					continue
				}
				for i, arg := range common.Args {
					closure := reclaim.ClosureFn(arg)
					if closure == nil {
						continue
					}
					// The runner must carry a concrete runner→closure union edge (it
					// invokes the parameter directly), and the closure must be confined to
					// THIS call — else removing the union edge would be unsound.
					if !reclaim.DirectlyInvokesParam(runner, i) || !confined(arg, call) {
						continue
					}
					add := [2]string{n.FQN, closure.RelString(nil)}
					if !addSeen[add] {
						addSeen[add] = true
						plan.Add = append(plan.Add, Edge{From: add[0], To: add[1]})
					}
					rem := [2]string{runner.RelString(nil), closure.RelString(nil)}
					if !remSeen[rem] {
						remSeen[rem] = true
						plan.Remove = append(plan.Remove, rem)
					}
				}
			}
		}
	}
	sort.Slice(plan.Add, func(i, j int) bool {
		if plan.Add[i].From != plan.Add[j].From {
			return plan.Add[i].From < plan.Add[j].From
		}
		return plan.Add[i].To < plan.Add[j].To
	})
	sort.Slice(plan.Remove, func(i, j int) bool {
		if plan.Remove[i][0] != plan.Remove[j][0] {
			return plan.Remove[i][0] < plan.Remove[j][0]
		}
		return plan.Remove[i][1] < plan.Remove[j][1]
	})
	return plan
}

// confined reports whether the closure value c is used in its parent ONLY as an argument
// of the single call we are rebinding — i.e. every referrer of c is that call. Any other
// referrer (a second invoker, a Store, a Return, a Send, a capture into another closure)
// is an escape: the closure may be invoked on another path, so its union edge must be
// kept. A nil referrer list (no uses at all) is not confined to a rebind either.
func confined(c ssa.Value, call ssa.CallInstruction) bool {
	refs := c.Referrers()
	if refs == nil || len(*refs) == 0 {
		return false
	}
	for _, r := range *refs {
		if r != ssa.Instruction(call) {
			return false
		}
	}
	return true
}
