// Witness n1method: the residual class reached through a METHOD OF A GENERIC
// TYPE, not through a generic function.
//
// The refused SSA function here is (Box[L]).Show[L]. Nothing in this program is
// a call to a generic function with L as its type argument — L becomes a type
// argument of the generic TYPE Box, and the SSA function whose discriminator
// roots reach L is Box's method. That is why the blast radius is stated as a
// MECHANISM in "The uninstantiated-body sub-case" and not as a list of syntactic
// shapes: an enumeration written around `g(L{})` with g a generic FUNCTION does
// not contain this program, yet flowmap refuses it.
//
// rta and vta graph it cleanly (one Show[L]); cha refuses it, because the
// whole-program set also holds gen's uninstantiated body and builds a second,
// distinct Show[L] from the same one declaration of L with the same structure.
package main

type Box[T any] struct{ V T }

func (b Box[T]) Show() {}

func gen[X any]() {
	type L struct{ A int }
	Box[L]{}.Show()
}

func main() { gen[int]() }
