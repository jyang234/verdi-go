package facts

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/obligations"
)

// TestObligationVerdictParity pins the obligation-verdict vocabulary the consumer
// (this package's ObligationStatus* constants, which fitness's checkObligations
// and the assert `obligation` claim both classify through) to the producer's
// constants (internal/static/obligations.Status). The two are DELIBERATELY
// decoupled — the graph JSON is the interface, so groundwork decodes the strings
// independently rather than importing the producer — and every consumer of
// ClassifyObligationStatus fails closed on an unknown status. But per CLAUDE.md
// the parity of a value applied in two places must be pinned by a DIRECT named
// test (the schemadrift TestVerbParity pattern), not left to an indirect fixture
// golden: a producer that renamed "CANT-PROVE" to "CANT_PROVE" would silently turn
// every consumer match into a fall-through, and a golden might not exercise that
// verdict. This test is that tripwire for RENAMES — see the closing note for what
// it does not catch. It imports the producer constant ONLY here, in a test,
// keeping the production decoupling.
//
// It lives beside the constants rather than beside checkObligations because the
// vocabulary moved here: facts is now the single owner both judges read.
func TestObligationVerdictParity(t *testing.T) {
	cases := []struct {
		consumer string
		producer obligations.Status
	}{
		{ObligationStatusViolated, obligations.Violated},
		{ObligationStatusCantProve, obligations.CantProve},
		{ObligationStatusUnmatched, obligations.Unmatched},
		{ObligationStatusSatisfied, obligations.Satisfied},
	}
	for _, c := range cases {
		if c.consumer != string(c.producer) {
			t.Errorf("obligation verdict drift: consumer %q != producer %q — the graph-JSON status vocabulary diverged; every consumer would silently stop matching it", c.consumer, string(c.producer))
		}
	}

	// The cases above are a HAND-MAINTAINED enumeration, and this test cannot
	// close that gap: the producer package exports its statuses as loose constants
	// with no list to range over, so a newly added Status is invisible here until
	// someone adds a row. What the tripwire does catch is the likelier failure — a
	// RENAMED status, where the consumer constant silently stops matching and every
	// verdict falls through to the "not understood" caution. Widening it to true
	// completeness needs an exported status list on the producer side; that is a
	// change to internal/static/obligations, not to this test.
}
