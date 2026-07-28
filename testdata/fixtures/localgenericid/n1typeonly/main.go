// Witness n1typeonly: the THIRD negative witness. The function-local type L IS
// a type argument of a generic the analyzed set instantiates twice — Box[L]
// exists for gen's uninstantiated body and again for gen[int] — and the program
// still graphs cleanly under all three algorithms.
//
// Box has NO METHODS, so no SSA function is built at L: nothing carries L in its
// type arguments and nothing has an L-bearing receiver, so no two functions can
// share a key. The refusal needs the local type to reach the DISCRIMINATOR ROOTS
// of an ssa.Function, not merely to appear as a type argument of an instantiated
// generic type. That is the clause a shape-list phrasing of the blast radius
// ("L becomes a type argument of something instantiated twice") gets wrong, and
// this fixture is the counterexample to it.
//
// KEEPING IT ARMED, two ways. The `fmt.Println(b)` below converts Box[L] to an
// interface, so Box[L] is a runtime type and its method set — empty — is in the
// analyzed set; without it the fixture would prove only that an unused value is
// unanalyzed. And Box must stay METHODLESS: adding one method, even one that is
// never called, turns this program into witness n1methodnocall, which cha
// refuses. The two fixtures differ by exactly that one line. The cha node counts
// asserted for this one show the door is open: both `gen` (uninstantiated) and
// `gen[int]` are in the analyzed set.
package main

import "fmt"

// Box deliberately declares no methods. See KEEPING IT ARMED above.
type Box[T any] struct{ V T }

func gen[X any]() {
	type L struct{ A int }
	var b Box[L]
	fmt.Println(b)
}

func main() { gen[int]() }
