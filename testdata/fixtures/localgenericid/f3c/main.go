// Witness f3c: the f3a swap through a result struct with REPEATED roles,
// struct{X A; Y B; Z A}. This is the shape a position encoding performed under a
// GLOBAL boolean seen-set cannot separate: the second occurrence of A at role Z
// is skipped as already-seen, so struct{X A; Y B; Z A} and struct{X B; Y A; Z B}
// record the same visit sequence. The numbered-DAG encoding records every child
// by id at every position, so the repeat is kept.
package main

import "fmt"

func mkS[A any, B any](a A, b B) struct {
	X A
	Y B
	Z A
} {
	return struct {
		X A
		Y B
		Z A
	}{X: a, Y: b, Z: a}
}

func use() {
	type result struct{ A int }
	var outer result
	f := func(v result) {
		type result struct{ A int }
		var inner result
		fmt.Println(mkS(v, inner))
		fmt.Println(mkS(inner, v))
	}
	f(outer)
}

func main() { use() }
