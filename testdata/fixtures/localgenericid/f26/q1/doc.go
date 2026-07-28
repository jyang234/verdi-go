// Package q1 declares two function-local types in NON-generic functions and
// hands each to a different generic intermediary in q2.
//
// Its file is named local.go so its basename matches q2's, and the `// pad`
// comments above A1 and A2 are BYTE PADDING: their exact lengths place q1's two
// `type L` declarations at the same byte offsets as q2's (66 and 126). Editing
// either file without re-aligning the offsets disarms the witness, so a test
// asserts the alignment rather than trusting the comments.
package q1
