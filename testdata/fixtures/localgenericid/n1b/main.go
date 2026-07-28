// Witness n1b: the DECLARED RESIDUAL of the design. ONE generic function
// declares ONE function-local type, and the program instantiates that generic
// twice. Both sink[L] instances therefore come from one syntactic declaration —
// one source position — and L's structure does not mention X, so the two
// instantiations are structurally identical as well. Neither position nor
// structure can tell the instances apart, and no refinement of either ever will.
//
// This fixture MUST keep failing closed. If a change makes `flowmap graph`
// succeed here, two distinct *ssa.Function were silently collapsed into one node
// and every absence proof downstream now covers a function the analysis never
// examined. See "Residual undecided classes" in
// docs/superpowers/specs/2026-07-26-local-generic-type-identity-design.md.
package main

import "fmt"

func sink[T any](v T) { fmt.Println(v) }

func gen[X any](x X) {
	type L struct{ A int }
	sink(L{})
	fmt.Println(x)
}

func main() {
	gen(1)
	gen("s")
}
