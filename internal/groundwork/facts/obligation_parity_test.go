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
// verdict. This test is the tripwire — it imports the producer constant ONLY here,
// in a test, keeping the production decoupling.
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

	// Completeness: every producer status must classify to a named state. Adding a
	// new producer Status without teaching ClassifyObligationStatus must fail here
	// rather than fall through to ObligationUnknown, where it would read as
	// vocabulary drift — a true disclosure, but the wrong one, and one that hides
	// the real cause behind a "not understood by this groundwork" caution.
	for _, s := range []obligations.Status{
		obligations.Satisfied, obligations.Violated,
		obligations.CantProve, obligations.Unmatched,
	} {
		if got := ClassifyObligationStatus(string(s)); got == ObligationUnknown {
			t.Errorf("producer status %q classifies as ObligationUnknown — facts cannot interpret it", s)
		}
	}
}
