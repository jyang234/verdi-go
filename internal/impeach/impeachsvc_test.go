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
	r := Audit("impeachsvc", ix, []*ir.CanonicalTrace{purge, create}, Provenance{})

	// Determinism over the real corpus.
	if r.Digest != Audit("impeachsvc", ix, []*ir.CanonicalTrace{create, purge}, Provenance{}).Digest {
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
	// The genuine contradiction is found, but this real corpus carries no code
	// identity (the trace model has no commit stamp yet, §14-D), so the ladder
	// caps it at VERSION-SKEW — fail-closed, not promoted to a bare IMPEACHMENT on
	// a graph it cannot prove the trace ran. The promotion is exercised separately
	// (TestImpeachsvcLadderPromotesWithProvenance) once the substrate is supplied.
	if w.Verdict != DowngradeVersionSkew {
		t.Errorf("Verdict = %q, want %q (real corpus has no code identity)", w.Verdict, DowngradeVersionSkew)
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
	r := Audit("impeachsvc", graph.NewIndex(g), []*ir.CanonicalTrace{loadTrace(t, impeachTraceLoanCreate)}, Provenance{})
	if len(r.Candidates) != 0 {
		t.Errorf("discovered route alone produced candidates: %+v", r.Candidates)
	}
}

// TestImpeachsvcLadderPromotesWithProvenance is the end-to-end IMPEACHMENT proof:
// the SAME real graph + real captured traces that downgrade to VERSION-SKEW above
// promote to a true IMPEACHMENT once the capture-side substrate the ladder needs
// is supplied — a graph stamped with the deployed commit and a matching
// production capture identity. Every rung clears: static asserts a real negative,
// the graph is the stamped code the trace ran, the effect is in the one-source DB
// vocabulary, it is on impeachsvc's own spans, and the capture is production. This
// is the §10 go/no-go top rung — the ladder can reach IMPEACHMENT, so it is not a
// detector that only ever downgrades.
func TestImpeachsvcLadderPromotesWithProvenance(t *testing.T) {
	g, err := graph.LoadFile(impeachsvcGraph)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	// Simulate the capture-side substrate Phase 1 budgets but does not yet ship
	// (§14-D): a deployed-commit stamp on the graph and the matching trace identity.
	const commit = "deadbeefcafe"
	g.Stamp = commit
	ix := graph.NewIndex(g)

	purge := loadTrace(t, impeachTraceAdminPurge)
	create := loadTrace(t, impeachTraceLoanCreate)
	prov := Provenance{TraceIdentity: commit, Capture: CaptureProduction}
	r := Audit("impeachsvc", ix, []*ir.CanonicalTrace{purge, create}, prov)

	if r.Digest != Audit("impeachsvc", ix, []*ir.CanonicalTrace{create, purge}, prov).Digest {
		t.Error("report not order-independent under provenance")
	}
	if len(r.Candidates) != 1 {
		t.Fatalf("want exactly 1 candidate, got %d: %+v", len(r.Candidates), r.Candidates)
	}
	w := r.Candidates[0]
	if w.Verdict != VerdictImpeachment {
		t.Fatalf("Verdict = %q, want %q; ladder = %+v", w.Verdict, VerdictImpeachment, w.Rungs)
	}
	// The ladder is recorded WHOLE and every rung cleared (Passed == benign
	// explanation ruled out): an IMPEACHMENT is exactly "all five rungs passed".
	if len(w.Rungs) != 5 {
		t.Fatalf("want the full 5-rung ladder recorded, got %d: %+v", len(w.Rungs), w.Rungs)
	}
	wantOrder := []string{RungStaticAssertsNoPath, RungCodeIdentity, RungLabel, RungServiceScope, RungCaptureFidelity}
	for i, rung := range w.Rungs {
		if rung.Name != wantOrder[i] {
			t.Errorf("rung %d = %q, want %q (ladder must be in §4 order)", i, rung.Name, wantOrder[i])
		}
		if !rung.Passed {
			t.Errorf("rung %q did not pass on the promotion path: %s", rung.Name, rung.Evidence)
		}
	}
	// Provenance is recorded in the report header (the numerator's identity, §5).
	if r.TraceIdentity != commit || r.CaptureProvenance != CaptureProduction {
		t.Errorf("report dropped provenance: identity=%q capture=%q", r.TraceIdentity, r.CaptureProvenance)
	}
}
