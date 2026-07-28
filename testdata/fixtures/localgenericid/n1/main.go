// Witness n1: the local type L is declared INSIDE a generic function and is
// reached only after instantiation. go/types re-creates such a TypeName with a
// nil Parent(), which the retired `obj.Parent() != nil` conjunct read as
// "package scope" — so both sink[L] instances contributed no site at all and
// collided on a bare prefix with no suffix whatsoever.
//
// Two DIFFERENT generic functions declare L here, so the two declarations are
// genuinely distinguishable by position; this is the shape the corrected
// predicate repairs. The undecidable sibling (ONE generic instantiated twice) is
// witness n1b.
package main

import "fmt"

func sink[T any](v T) { fmt.Println(v) }

func genA[X any](x X) {
	type L struct{ A int }
	sink(L{})
	fmt.Println(x)
}

func genB[X any](x X) {
	type L struct{ A int }
	sink(L{})
	fmt.Println(x)
}

func main() {
	genA(1)
	genB("s")
}
