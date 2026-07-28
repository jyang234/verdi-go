// Witness n1fqndiffers is a NEGATIVE witness, and it is the program that
// falsified the third formulation of the blast radius. All four of the conjuncts
// that formulation listed hold: gen declares the function-local L; L reaches
// sink's discriminator roots; the analyzed set builds sink from each instance of
// gen's body (sink[L X], sink[L int], sink[L string] under cha); and L's
// structure does not depend on X. It graphs cleanly under every algorithm.
//
// The reason is the SECOND type argument. x carries the enclosing type
// parameter's instantiation into sink's roots, so the two functions do not share
// a display FQN — and panicOnDuplicateSortKey short-circuits on
// `prev.FQN != cur.FQN` and never consults the discriminator at all.
//
// The minimal repair of this program is `sink(L{}, 0)`, which is refused. See
// n1twodecls for why sharing the FQN is still not enough.
package main

func sink[T any, U any](v T, u U) {}

func gen[X any](x X) {
	type L struct{ A int }
	sink(L{}, x)
}

func main() { gen(1); gen("s") }
