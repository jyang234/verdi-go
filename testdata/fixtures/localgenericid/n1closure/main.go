// Witness n1closure: the local type is declared inside a CLOSURE nested in a
// generic function, not directly in the generic function's own body. The
// enclosing anonymous function has no type parameters of its own, yet go/ssa
// builds one copy of it per instantiation of gen, so the two sink[L] instances
// come from one declaration of L with identical structure — the residual class,
// reached through a body a blast radius written as "a generic function declares
// L" does not name.
//
// See "The uninstantiated-body sub-case" in
// docs/superpowers/specs/2026-07-26-local-generic-type-identity-design.md.
package main

import "fmt"

func sink[T any](v T) { fmt.Println(v) }

func gen[X any](x X) {
	f := func() {
		type L struct{ A int }
		sink(L{})
	}
	f()
	fmt.Println(x)
}

func main() {
	gen(1)
	gen("s")
}
