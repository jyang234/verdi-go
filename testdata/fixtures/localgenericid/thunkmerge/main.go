// Witness thunkmerge: the FABRICATED-EDGE witness for callgraph.mergeKey.
//
// Both `local` types render as `example.com/thunkmerge.local` — a function-local
// name carries no lexical scope into a type string — so the two method-expression
// thunks `local.QueryContext` share a display FQN AND a wrapped Object (the
// interface method emb.QueryContext). Before features.receiverType existed, a
// $thunk's discriminator root set was empty (its receiver is its first PARAMETER,
// not Signature.Recv()), so InstanceDiscriminator returned "" and mergeKey admitted
// both — collapsing two distinct *ssa.Function into ONE node, with no refusal and
// no message.
//
// The two are NOT byte-identical: `emb` sits at field #0 in one() and #1 in two(),
// so the spilled-receiver selection is `&t0.emb [#0]` against `&t0.emb [#1]`.
//
// The damage is on the POSITIVE pole. The merged node's out-edges are the UNION of
// the two thunks', so under vta — which sees that only A ever flows into one()'s
// local and only B into two()'s — the emitted graph gained
//
//	one -> (B).QueryContext        fabricated
//	two -> (A).QueryContext        fabricated
//
// A union never DELETES a callee, so no absence proof (PROVEN / NO-FLOW / NEVER /
// "no path" / "covered") could flip; what it costs is precision and every blast
// radius computed from these edges. rta and cha resolve the embedded interface
// call to every implementer anyway, so their six edges are sound
// over-approximation and are unchanged by the fix — vta is the algorithm that can
// see the difference, and TestLocalGenericIdentityThunkMergeDoesNotFabricateEdges
// asserts all three.
package main

import "fmt"

type emb interface{ QueryContext() string }

type A struct{}

func (A) QueryContext() string { return "A" }

type B struct{}

func (B) QueryContext() string { return "B" }

// one's local embeds emb at field #0.
func one() string {
	type local struct{ emb }
	f := local.QueryContext
	return f(local{A{}})
}

// two's local embeds emb at field #1. The padding field is load-bearing: it is
// what makes the two merged bodies differ, so the merge was a real behavioral
// collapse and not a harmless dedup of identical code.
func two() string {
	type local struct {
		pad int
		emb
	}
	f := local.QueryContext
	return f(local{0, B{}})
}

func main() { fmt.Println(one(), two()) }
