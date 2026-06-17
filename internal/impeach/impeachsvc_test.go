package impeach

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/ir"
)

// The impeachsvc fixture is a CONTROLLED environment for the impeachment cell: a
// real service whose graph the real analyzer produces, plus traces the real
// harness captures. Its admin route is registered through a custom (unhinted)
// router, so its DB DELETE is a MISSED ROOT — present in the graph, reachable
// from no discovered entrypoint, and DISCLOSED BY NOTHING (no blind spot, no
// frontier marker). That is the genuine analyzer blind spot the cell must catch,
// and the seam this whole fixture exists to manufacture inside a boundary we
// control independent of the test.
const (
	impeachsvcGraph        = "testdata/impeachsvc.graph.json"
	impeachTraceAdminPurge = "../../testdata/fixtures/impeachsvc/flows/testdata/flows/delete_admin_ledger.golden.json"
	impeachTraceLoanCreate = "../../testdata/fixtures/impeachsvc/flows/testdata/flows/post_loan.golden.json"
)

// TestImpeachsvcCatchesUndisclosedMissedRoot is the end-to-end proof: the real
// graph + the two real captured traces, joined, yield EXACTLY ONE impeachment —
// the DB DELETE on the missed admin route — while the discovered route's DB
// INSERT is CONFIRMED-LIVE (neither impeached nor an unobserved gap). This is the
// cell catching a real, undisclosed false-negative end to end, not a synthetic
// stub.
func TestImpeachsvcCatchesUndisclosedMissedRoot(t *testing.T) {
	g, err := graph.LoadFile(impeachsvcGraph)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	ix := graph.NewIndex(g)

	// Precondition: the seam is genuinely UNDISCLOSED. If a future analyzer learns
	// to disclose this (a frontier marker or blind spot at the missed route), the
	// cell would correctly downgrade it to RECLAIMED-LIVE — so this assertion is
	// what keeps the fixture honest about what it is testing.
	if bs := ix.BlindSpots(); len(bs) != 0 {
		t.Fatalf("fixture no longer undisclosed: graph carries blind spots %+v", bs)
	}
	if fr := ix.Frontier(); fr != nil && len(fr.Markers) != 0 {
		t.Fatalf("fixture no longer undisclosed: graph carries frontier markers %+v", fr.Markers)
	}

	purge := loadTrace(t, impeachTraceAdminPurge)
	create := loadTrace(t, impeachTraceLoanCreate)
	r := Audit("impeachsvc", ix, []*ir.CanonicalTrace{purge, create})

	// Determinism over the real corpus.
	if r.Digest != Audit("impeachsvc", ix, []*ir.CanonicalTrace{create, purge}).Digest {
		t.Error("report not order-independent over the real corpus")
	}

	if len(r.Candidates) != 1 {
		t.Fatalf("want exactly 1 impeachment candidate, got %d: %+v", len(r.Candidates), r.Candidates)
	}
	w := r.Candidates[0]
	if w.Effect != "db DELETE ledger" {
		t.Errorf("Effect = %q, want %q", w.Effect, "db DELETE ledger")
	}
	if w.Claim.Reachability != ReachUnreachable {
		t.Errorf("Reachability = %q, want %q (named effect, no discovered route reaches it)", w.Claim.Reachability, ReachUnreachable)
	}
	if w.Verdict != VerdictCandidate {
		t.Errorf("Verdict = %q, want %q", w.Verdict, VerdictCandidate)
	}
	// The witness carries the runtime counterexample: the entry it was reached
	// from (the missed admin route) and the enriched observed op (with DB system).
	if w.Observed.Entry != "HTTP DELETE /admin/ledger" {
		t.Errorf("Observed.Entry = %q, want the missed admin route", w.Observed.Entry)
	}
	if w.Observed.Op != "DB postgres DELETE ledger" {
		t.Errorf("Observed.Op = %q, want the enriched DB op", w.Observed.Op)
	}

	// The discovered route's INSERT must be CONFIRMED-LIVE: observed AND reachable,
	// so it is neither impeached nor reported as an unexercised coverage gap.
	for _, c := range r.Candidates {
		if c.Effect == "db INSERT loans" {
			t.Error("the discovered route's effect was wrongly impeached")
		}
	}
	for _, gap := range r.CoverageGaps {
		if gap == "db INSERT loans" {
			t.Error("the discovered route's effect read as unobserved despite being driven")
		}
	}
}

// TestImpeachsvcDiscoveredRouteAloneIsClean is the discrimination control: the
// discovered route's flow ON ITS OWN yields zero candidates. The cell fires on
// the missed route, not on the sound one.
func TestImpeachsvcDiscoveredRouteAloneIsClean(t *testing.T) {
	g, err := graph.LoadFile(impeachsvcGraph)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	r := Audit("impeachsvc", graph.NewIndex(g), []*ir.CanonicalTrace{loadTrace(t, impeachTraceLoanCreate)})
	if len(r.Candidates) != 0 {
		t.Errorf("discovered route alone produced candidates: %+v", r.Candidates)
	}
}
