// Witness n1recv: the residual class reached through the RECEIVER root rather
// than through a type argument. It is n2's promoted-method wrapper crossed with
// n1b's generic enclosing function.
//
// The refused SSA function is the promotion wrapper over the function-local
// receiver `result`, which carries NO type arguments at all — its discriminator
// root is the receiver (see "Wrapper discrimination"). A blast-radius sentence
// written only about type arguments does not contain this program, yet flowmap
// refuses it, which is why the mechanism names both roots.
//
// rta and vta graph it cleanly; cha refuses it, because the whole-program set
// also holds gen's uninstantiated body and builds a second, distinct wrapper
// over the same one declaration of `result` with the same structure.
package main

import "fmt"

type ifc interface{ QueryContext() string }

type emb struct{}

func (emb) QueryContext() string { return "A" }

// gen returns its function-local type as an interface, which is what makes
// go/ssa build a promotion wrapper over the local receiver.
func gen[X any]() ifc {
	type result struct{ emb }
	return result{}
}

func main() { fmt.Println(gen[int]().QueryContext()) }
