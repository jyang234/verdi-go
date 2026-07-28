// Witness n1twodecls is the NEGATIVE witness that falsified the repair proposed
// for n1fqndiffers — "add a fifth conjunct requiring the two built functions to
// share a display FQN". Here they DO share one: both are
// sink[example.com/n1twodecls.L example.com/n1twodecls.L], because mkA's L and
// mkB's L are distinct types that RENDER identically. All four original
// conjuncts hold and the FQNs are equal, and the program still graphs cleanly.
//
// The discriminator separates them on the second root: the two locals are
// produced from two DIFFERENT declarations, so their (file, byte offset) sites
// differ. Sharing an FQN is therefore necessary and not sufficient; what the
// refusal needs is that nothing reaching the roots differs between the two
// instances. n1onedecl is this program with mkA and mkB collapsed into one
// generic function — one declaration, two instantiations — and it is refused.
package main

func sink[T any, U any](v T, u U) {}

func gen[X any](x X) {
	type L struct{ A int }
	sink(L{}, x)
}

func mkA() {
	type L struct{ B int }
	gen(L{})
}

func mkB() {
	type L struct{ B int }
	gen(L{})
}

func main() { mkA(); mkB() }
