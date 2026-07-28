// Witness n1onedecl is n1twodecls with mkA and mkB collapsed into ONE generic
// function instantiated twice. Every other line is the same, and this one is
// REFUSED. In n1twodecls the two gen[L] instances are separated because mkA's L
// and mkB's L come from two declarations; here the two gen[M] instances come
// from ONE declaration of M instantiated twice, so nothing reaching gen's root
// differs and the pair collides.
//
// gen[M] is what the guard names, because it is the first duplicate in sorted
// node order. The sink[L M] pair one level down is the same class for the same
// reason: L is identical in both by construction, and M now encodes identically
// too.
//
// The two fixtures are a minimal pair for conjunct 5. n1twodecls proves that
// sharing a display FQN is not enough; this one proves the difference is the
// number of DECLARATIONS behind the differing root, not the number of type
// arguments and not the rendering.
package main

func sink[T any, U any](v T, u U) {}

func gen[X any](x X) {
	type L struct{ A int }
	sink(L{}, x)
}

func mk[Y any](y Y) {
	type M struct{ B int }
	gen(M{})
}

func main() { mk(1); mk("s") }
