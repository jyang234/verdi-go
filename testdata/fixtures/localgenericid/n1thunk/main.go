// Witness n1thunk: the residual class reached through a $thunk's receiver.
//
// It is n1recv with the promotion wrapper replaced by a method EXPRESSION. A
// thunk carries no type arguments and no Signature receiver at all — its receiver
// is its first parameter — so before features.receiverType existed its
// discriminator was "" and callgraph.mergeKey admitted it. That is the severity
// cliff this witness pins: the same program shape is a loud, disclosed refusal
// when go/ssa produces a promotion wrapper (n1recv, exit 2) and was a SILENT merge
// when it produced a thunk (exit 0). One shape, two answers, no message.
//
// rta and vta graph it cleanly; cha refuses it, because the whole-program set also
// holds gen's uninstantiated body and builds a second, distinct thunk over the
// same one declaration of `local` with the same structure.
package main

import "fmt"

type emb struct{}

func (emb) QueryContext() string { return "Q" }

// gen takes a method expression on its function-local type, which is what makes
// go/ssa mint a $thunk whose first parameter is that local type.
func gen[X any]() string {
	type local struct{ emb }
	f := local.QueryContext
	return f(local{})
}

func main() { fmt.Println(gen[int]()) }
