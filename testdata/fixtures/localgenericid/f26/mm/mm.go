// Package mm holds the two-parameter generic both intermediaries instantiate.
package mm

import "fmt"

// P is instantiated as P[q1.L, q2.L] from BOTH q2.E1 and q2.E2, so its two
// instances share one rendered display FQN and are separated only by the
// discriminator.
func P[A any, B any]() { fmt.Println("P") }
