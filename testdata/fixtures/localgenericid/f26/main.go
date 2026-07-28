// Witness f26: two PACKAGES whose source files share a basename (local.go) and
// whose function-local declarations sit at ALIGNED byte offsets. A1 reaches E2
// and A2 reaches E1 — the cross-wiring is what makes the two pooled site sets
// equal, since one instance draws (q1 offset a, q2 offset b) and the other draws
// (q1 offset b, q2 offset a).
//
// The retired local-sites/v1 suffix pooled, sorted and deduplicated those sites
// into one alphabet that no longer recorded WHICH PACKAGE each site came from,
// so both mm.P[q1.L q2.L] instances carried the byte-identical set
// [local.go:<a>, local.go:<b>]. The collision is masked on the shipped binary
// only because q2's locals are declared inside GENERIC functions and the retired
// nil-Parent predicate dropped them; correcting that predicate makes it live.
package main

import "example.com/f26/q1"

func main() {
	q1.A1()
	q1.A2()
}
