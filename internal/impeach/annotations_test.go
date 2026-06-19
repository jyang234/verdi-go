package impeach

import (
	"reflect"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

func sev(site string) *Severance { return &Severance{Site: site, Kind: SeveranceSeveredEmitter} }

func gradeAt(gs []GradedAnnotation, site string) (GradedAnnotation, bool) {
	for _, g := range gs {
		if g.Annotation.Site == site {
			return g, true
		}
	}
	return GradedAnnotation{}, false
}

// TestGradeAnnotations pins the corpus grading: an unclaimed annotation at a
// witnessed site is WITNESSED with the observed effect (and its ladder verdict)
// carried through; an annotation at a site with no candidate is UNWITNESSED.
func TestGradeAnnotations(t *testing.T) {
	candidates := []Witness{
		{Effect: "PUBLISH loan.approved", Verdict: VerdictImpeachment, Observed: Observation{Flow: "approve"}, Severance: sev("svc.Send")},
		{Effect: "db DELETE ledger", Verdict: DowngradeCaptureUntrusted, Observed: Observation{Flow: "wipe"}, Severance: sev("svc.Send")},
		{Effect: "no severance", Verdict: VerdictImpeachment, Observed: Observation{Flow: "y"}, Severance: nil},
	}
	anns := []graph.Annotation{
		{Site: "svc.Send", Kind: "ExternalBoundaryCall", Note: "POSTs to acme"},
		{Site: "svc.Quiet", Kind: "reflect", Note: "reflection here"},
	}

	g := GradeAnnotations(candidates, anns)

	send, _ := gradeAt(g, "svc.Send")
	if send.Grade != AnnotationWitnessed {
		t.Fatalf("unclaimed annotation at a witnessed site should be WITNESSED, got %s", send.Grade)
	}
	// Both effects severed at the site carried through, sorted by (Effect, …); the
	// low-fidelity verdict is preserved so corroboration strength stays visible.
	if len(send.Effects) != 2 || send.Effects[0].Effect != "PUBLISH loan.approved" || send.Effects[1].Verdict != DowngradeCaptureUntrusted {
		t.Errorf("effects not collected/sorted with verdicts: %+v", send.Effects)
	}
	quiet, _ := gradeAt(g, "svc.Quiet")
	if quiet.Grade != AnnotationUnwitnessed {
		t.Errorf("annotation with no candidate at its site should be UNWITNESSED, got %s", quiet.Grade)
	}
}

// TestGradeAnnotationsClaim pins the structured-claim grading: a claim matching an
// observed effect is CONFIRMED; a claim the witnessed site did not observe is
// UNCONFIRMED (never "false" — a sample is not exhaustive); a claim at an
// unwitnessed site is UNWITNESSED.
func TestGradeAnnotationsClaim(t *testing.T) {
	candidates := []Witness{
		{Effect: "PUBLISH loan.approved", Verdict: VerdictImpeachment, Observed: Observation{Flow: "f"}, Severance: sev("svc.Send")},
	}
	mk := func(site, claim string) GradedAnnotation {
		g := GradeAnnotations(candidates, []graph.Annotation{{Site: site, Kind: "ExternalBoundaryCall", Note: "n", Claim: claim}})
		return g[0]
	}

	if got := mk("svc.Send", "PUBLISH loan.approved").Grade; got != AnnotationConfirmed {
		t.Errorf("matching claim should be CONFIRMED, got %s", got)
	}
	// Trimming only — the key itself is matched exactly (fail-closed: no fuzzy confirm).
	if got := mk("svc.Send", "  PUBLISH loan.approved  ").Grade; got != AnnotationConfirmed {
		t.Errorf("trimmed claim should still be CONFIRMED, got %s", got)
	}
	if got := mk("svc.Send", "PUBLISH something.else").Grade; got != AnnotationUnconfirmed {
		t.Errorf("claim not among observed effects should be UNCONFIRMED, got %s", got)
	}
	if got := mk("svc.Nowhere", "PUBLISH loan.approved").Grade; got != AnnotationUnwitnessed {
		t.Errorf("claim at an unwitnessed site should be UNWITNESSED, got %s", got)
	}
}

// TestGradeAnnotationsDeterministic pins byte-identical output regardless of
// candidate/annotation arrival order — the audit and its digest must not move.
func TestGradeAnnotationsDeterministic(t *testing.T) {
	c1 := []Witness{
		{Effect: "PUBLISH a", Verdict: VerdictImpeachment, Observed: Observation{Flow: "f1"}, Severance: sev("svc.Z")},
		{Effect: "PUBLISH b", Verdict: VerdictImpeachment, Observed: Observation{Flow: "f2"}, Severance: sev("svc.A")},
	}
	a1 := []graph.Annotation{
		{Site: "svc.Z", Kind: "ExternalBoundaryCall", Note: "z"},
		{Site: "svc.A", Kind: "ExternalBoundaryCall", Note: "a"},
	}
	g1 := GradeAnnotations(c1, a1)
	g2 := GradeAnnotations([]Witness{c1[1], c1[0]}, []graph.Annotation{a1[1], a1[0]})
	if !reflect.DeepEqual(g1, g2) {
		t.Fatalf("order-dependent output:\n %+v\n %+v", g1, g2)
	}
	if g1[0].Annotation.Site != "svc.A" || g1[1].Annotation.Site != "svc.Z" {
		t.Errorf("annotations not in (Site) order: %+v", g1)
	}
}
