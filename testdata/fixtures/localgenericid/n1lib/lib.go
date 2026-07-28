// Witness n1lib: the residual class reached with the generic instantiated ONCE,
// under rta and vta — NOT only under --algo cha.
//
// This unit is a library: it has no main, no HTTP handler and no declared
// entrypoint, so root discovery falls back to the exported surface and roots at
// every exported function. Gen is exported and generic, so its UNINSTANTIATED
// body is a root, and the one instantiation Use creates is reached separately.
// go/ssa therefore supplies two distinct sink[L] built from the SAME single
// declaration of L with the SAME structure — the declared residual class, with a
// generic that is instantiated exactly once.
//
// The fixture exists to keep the refusal diagnostic honest. A message that
// asserts "this program instantiates the enclosing generic more than once", or
// that offers "instantiate it only once" as a remedy, is false here under every
// algorithm — and the analyzer cannot tell this shape from n1b's, so it must not
// claim either. See "The uninstantiated-body sub-case" in
// docs/superpowers/specs/2026-07-26-local-generic-type-identity-design.md.
package n1lib

import "fmt"

func sink[T any](v T) { fmt.Println(v) }

// Gen declares a function-local type whose structure does not mention X.
func Gen[X any](x X) {
	type L struct{ A int }
	sink(L{})
	fmt.Println(x)
}

// Use instantiates Gen exactly once.
func Use() { Gen(1) }
