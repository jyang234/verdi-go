// Witness n1methodnocall: n1method with the method NEVER CALLED. It pins the
// word "builds" in the blast-radius mechanism against the weaker reading
// "calls".
//
// No line of this program invokes Show. It is built anyway: `fmt.Println(b)`
// converts Box[L] to an interface, which makes Box[L] a runtime type and puts
// its method set into the analyzed set. rta and vta build exactly one Show[L]
// and graph cleanly; cha also holds gen's UNINSTANTIATED body, builds a second
// distinct Show over the same one declaration of L with the same structure, and
// refuses.
//
// Call-reachability is therefore NOT the threshold; being BUILT into the
// analyzed set is. This fixture and n1typeonly are a controlled pair: they
// differ by exactly the `func (b Box[T]) Show() {}` line below, and that one
// line is the difference between a refusal and a clean graph under cha.
package main

import "fmt"

type Box[T any] struct{ V T }

// Show is never called anywhere in this program.
func (b Box[T]) Show() {}

func gen[X any]() {
	type L struct{ A int }
	var b Box[L]
	fmt.Println(b)
}

func main() { gen[int]() }
