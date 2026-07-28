// Witness rec: a RECURSIVE function-local named type. The encoder reserves a
// node's id before recursing into its underlying type, so R's back edge through
// *R resolves to an id that already exists: the encoding is finite, and it does
// not depend on where the walk entered the cycle. A walk that guarded cycles with
// an on-path stack instead would either diverge here or produce a
// path-dependent — and therefore non-canonical — encoding.
package main

import "fmt"

func sink[T any](v T) { fmt.Println(v) }

func use() {
	type R struct{ next *R }
	sink(R{})
}

func main() { use() }
