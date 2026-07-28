package q2

import "example.com/f26/mm"

func E1[T any]() {
	type L struct{ Z int }
	mm.P[T, L]()
}

func E2[T any]() {
	type L struct{ Z int }
	mm.P[T, L]()
}
