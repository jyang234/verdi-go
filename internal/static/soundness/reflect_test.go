package soundness

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/fitness"
)

// TestReflectDispatchAbstainsAtTheSeam is probe #1 of the static-checker audit: the
// coverage counterpart to probe #2. Where dispatchsvc exercises the NEW UnresolvedCall
// disclosure (a func-value call resolved to no callee), reflectsvc exercises the
// EXISTING reflect disclosure and confirms the dangerous case was already sound: a
// must_not_reach whose only path to the effect runs through a reflect.Value.Call
// abstains (noPathFound → Caution) instead of laundering the unseen edge into a
// provenAbsent.
//
// This seam is irreducible — no roots fix (rooting init(), the probe-#2 companion)
// recovers a reflective call — so the probe is STABLE across both fixes: the reflect
// case must stay abstained forever. The control proves the SAME effect is a Violation
// when a static path reaches it, so the abstain is the reflect seam, not an absent
// target.
func TestReflectDispatchAbstainsAtTheSeam(t *testing.T) {
	ix := indexFixture(t, "reflectsvc")

	handle := nodeWithSuffix(ix, "reflectsvc.Handle")
	report := nodeWithSuffix(ix, "reflectsvc.Report")
	if handle == "" || report == "" {
		t.Fatalf("fixture entrypoints missing: Handle=%q Report=%q", handle, report)
	}

	// purge IS in the graph (its address is taken by reflect.ValueOf, so the function
	// is reachable and its DELETE label binds) — but no call EDGE reaches it from
	// Handle, so the abstain below is a real noPathFound at the reflect seam.
	if nodeWithSuffix(ix, "reflectsvc.purge") == "" {
		t.Fatal("purge missing from graph; the DELETE label would not bind and the safeguard would mask the seam")
	}

	// THE DISCLOSURE: the reflect.Value.Call hop is flagged on Handle's cone.
	cone := append([]string{handle}, ix.Reachable(handle)...)
	if !coneHasBlindSpot(ix, cone) {
		t.Error("expected a reflect blind spot on Handle's cone; the seam is silent")
	}

	// THE ABSTAIN: must_not_reach over the reflect hop is a Caution (noPathFound), not
	// a silent provenAbsent. (The policy Service name is inert here — the rule binds on
	// From/To patterns — so the shared dispatchsvc helper applies unchanged.)
	got := mustNotReachFindings(ix, handle)
	if len(got) != 1 || got[0].Severity != fitness.Caution {
		t.Fatalf("Handle must_not_reach = %+v, want one Caution (noPathFound at the reflect seam)", got)
	}

	// CONTROL: the SAME effect, reached directly, is a Violation.
	ctrl := mustNotReachFindings(ix, report)
	if len(ctrl) != 1 || ctrl[0].Severity != fitness.Violation {
		t.Fatalf("control failed: must_not_reach(Report, DELETE) = %+v, want one Violation", ctrl)
	}
}
