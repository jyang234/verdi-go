// Witness n1c: n1b's shape, but the local type's STRUCTURE varies with the
// enclosing type parameter — L is struct{A int} in one instantiation and
// struct{A string} in the other. Position alone still cannot separate the two
// sink[L] instances (one declaration, one site); the structural component of the
// encoding is what does it.
//
// This is the fixture that fails if a Basic node is encoded by kind alone: a
// definition of "kind plus children by id" does not separate basic:int from
// basic:string, and the two keys would be byte-identical.
package main

import "fmt"

func sink[T any](v T) { fmt.Println(v) }

func gen[X any](x X) {
	type L struct{ A X }
	sink(L{A: x})
}

func main() {
	gen(1)
	gen("s")
}
