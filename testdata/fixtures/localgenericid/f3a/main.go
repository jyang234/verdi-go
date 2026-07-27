// Witness f3a: two DISTINCT function-local types that render to the same string
// are supplied to one two-parameter generic in both orders. The func literal
// shadows the outer `result`, so the outer type survives only as the parameter
// type `v` and ordinary inference supplies both types as type arguments either
// way round. Both declarations sit in NON-generic scopes, so both contribute a
// site under the retired local-sites/v1 suffix too — this witness is independent
// of the nil-Parent predicate bug.
//
// The two locals are deliberately STRUCTURALLY IDENTICAL, so nothing but the
// positional role of each declaration can tell pair[outer, inner] from
// pair[inner, outer]. The retired pooled, sorted, deduplicated site set recorded
// which sites exist and never which site sits in which role, so both instances
// carried the same two sites and shared one sort key.
package main

import "fmt"

func pair[A any, B any](a A, b B) { fmt.Println(a, b) }

func use() {
	type result struct{ A int }
	var outer result
	f := func(v result) {
		type result struct{ A int }
		var inner result
		pair(v, inner)
		pair(inner, v)
	}
	f(outer)
}

func main() { use() }
