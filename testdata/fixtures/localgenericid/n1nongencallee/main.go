// Witness n1nongencallee: n1b's exact shape except that the callee is NOT
// generic — `func sink(v any)` rather than `func sink[T any](v T)`. It pins the
// other half of the uninstantiated-body sub-case's precondition.
//
// gen is instantiated once and, under cha, its UNINSTANTIATED body is analyzed
// alongside that instantiation — the same open door n1 walks through. The
// refusal still does not fire, because passing L to a non-generic function
// instantiates nothing at L: there is one sink node for the whole program, so
// there is no pair of distinct *ssa.Function sharing a key. The refusal needs a
// GENERIC callee, so that two sink[L] exist.
//
// The cha node counts below keep the fixture armed: both `gen` (uninstantiated)
// and `gen[int]` must be present, proving the door is open and that the clean
// graph is due to the non-generic callee alone.
//
// This fixture exists because the design once claimed that under cha "ANY
// program containing `func f[X any]() { type L struct{ …no X… }; g(L{}) }` is
// refused", without requiring g to be generic. That is false, and this witness
// is why it cannot be written again. See "The uninstantiated-body sub-case" in
// docs/superpowers/specs/2026-07-26-local-generic-type-identity-design.md.
package main

import "fmt"

// sink is deliberately NOT generic. Making it `sink[T any](v T)` turns this
// fixture into the declared residual and it must then be refused under cha.
func sink(v any) { fmt.Println(v) }

func gen[X any](x X) {
	type L struct{ A int }
	sink(L{})
	fmt.Println(x)
}

func main() { gen(1) }
