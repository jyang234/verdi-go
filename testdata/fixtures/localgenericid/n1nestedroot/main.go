// Witness n1nestedroot: L is NOT a discriminator root. sink's single type
// argument is []L, so L is reached one edge below the root, and the refusal
// fires all the same — the encoding walks the whole type graph under each root
// (localTypeGraph), it does not read the roots themselves. A conjunct 2 written
// as "L IS one of that function's type arguments, or its receiver" does not
// contain this program.
//
// *L, chan L, map[string]L, func(L) and Wrap[L] were all confirmed to behave the
// same way; the slice is the shortest of them.
package main

import "fmt"

func sink[T any](v T) { fmt.Println(v) }

func gen[X any](x X) {
	type L struct{ A int }
	sink([]L{})
	fmt.Println(x)
}

func main() {
	gen(1)
	gen("s")
}
