// Witness n1typemethod: the local type is declared inside a METHOD OF A GENERIC
// TYPE. Show declares no type parameters of its own, and no generic FUNCTION
// declares L — sink[T] is the callee that carries L into its roots, not the
// declaring body — yet Box's parameter makes the analyzer build Show's body once
// per instantiation of Box. So conjunct 1 of the blast radius is about a body the
// analyzer builds more than once, not about the `func f[X]` syntax.
//
// It is the companion of n1method, which is the other half of the same pair: in
// n1method a generic FUNCTION declares L and the refused function is a generic
// TYPE's method; here the generic type's method is what declares L.
package main

import "fmt"

func sink[T any](v T) { fmt.Println(v) }

type Box[T any] struct{ v T }

func (b Box[T]) Show() {
	type L struct{ A int }
	sink(L{})
	fmt.Println(b.v)
}

func main() {
	Box[int]{}.Show()
	Box[string]{}.Show()
}
