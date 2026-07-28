// Witness n1zeroinst: n1b's exact shape with ZERO instantiations — main never
// calls gen. It pins the lower bound of the uninstantiated-body sub-case.
//
// Under cha the whole program is analyzed, so gen's UNINSTANTIATED body is in
// the set and it does build sink[L]. But it is the ONLY source of sink[L]: with
// no instantiation there is no second one to collide with, so the analysis
// graphs cleanly. Zero instantiations is precisely the case that CANNOT trip the
// refusal, and the cha node count below (exactly one sink[L], not zero) is what
// keeps that honest — a fixture where the door were simply shut would prove
// nothing.
//
// Under rta and vta gen is unreachable from main, so nothing here is analyzed at
// all and there is likewise nothing to refuse.
//
// This fixture exists because the design once claimed that under cha "ANY
// program containing this shape is refused — even when it is never instantiated
// at all". That is false, and this witness is why it cannot be written again.
// See "The uninstantiated-body sub-case" in
// docs/superpowers/specs/2026-07-26-local-generic-type-identity-design.md.
package main

import "fmt"

func sink[T any](v T) { fmt.Println(v) }

// gen has n1b's body exactly. Nothing instantiates it.
func gen[X any](x X) {
	type L struct{ A int }
	sink(L{})
	fmt.Println(x)
}

func main() { fmt.Println("gen is never instantiated") }
