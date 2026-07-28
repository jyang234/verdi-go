// Package q2 declares two function-local types inside GENERIC functions, each
// forwarding to the shared two-parameter generic mm.P.
//
// Its file is named local.go so its basename matches q1's, and its two `type L`
// declarations sit at the same byte offsets as q1's (66 and 126). See q1's
// package comment: the alignment is the witness, and a test asserts it.
package q2
