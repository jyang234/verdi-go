// Witness f3b: the f3a swap routed through a generic container, so the collision
// also surfaces on an instance with ONE type argument (report[box[result, result]]).
// A per-argument sort of the site set cannot repair this shape: both roles are
// collapsed into a single rendered argument before the discriminator ever sees
// them, and only the position of each declaration INSIDE that argument's type
// graph separates the two instances.
package main

import "fmt"

type box[A any, B any] struct {
	L A
	R B
}

func mk[A any, B any](a A, b B) box[A, B] { return box[A, B]{L: a, R: b} }

func report[T any](v T) { fmt.Println(v) }

func use() {
	type result struct{ A int }
	var outer result
	f := func(v result) {
		type result struct{ A int }
		var inner result
		report(mk(v, inner))
		report(mk(inner, v))
	}
	f(outer)
}

func main() { use() }
