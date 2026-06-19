package impeach

import (
	"reflect"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

func sev(site string) *Severance { return &Severance{Site: site, Kind: SeveranceSeveredEmitter} }

// TestWitnessAnnotations pins the corroboration join: an annotation is witnessed
// iff a candidate's severance site equals the annotation's site, the observed
// effect (with its ladder verdict) is carried through, and an annotation with no
// candidate at its site is unwitnessed.
func TestWitnessAnnotations(t *testing.T) {
	candidates := []Witness{
		{Effect: "PUBLISH loan.approved", Verdict: VerdictImpeachment, Observed: Observation{Flow: "approve"}, Severance: sev("svc.Send")},
		{Effect: "db DELETE ledger", Verdict: DowngradeCaptureUntrusted, Observed: Observation{Flow: "wipe"}, Severance: sev("svc.Send")},
		{Effect: "PUBLISH other", Verdict: VerdictImpeachment, Observed: Observation{Flow: "x"}, Severance: sev("svc.Elsewhere")},
		{Effect: "no severance", Verdict: VerdictImpeachment, Observed: Observation{Flow: "y"}, Severance: nil},
	}
	anns := []graph.Annotation{
		{Site: "svc.Send", Kind: "ExternalBoundaryCall", Note: "POSTs to acme", By: "dev@x"},
		{Site: "svc.Quiet", Kind: "reflect", Note: "reflection here", By: "dev@x"},
	}

	witnessed, unwitnessed := WitnessAnnotations(candidates, anns)

	if len(witnessed) != 1 || witnessed[0].Annotation.Site != "svc.Send" {
		t.Fatalf("expected svc.Send witnessed, got %+v", witnessed)
	}
	// Both effects severed at svc.Send carried through, sorted by (Effect, …); the
	// low-fidelity verdict is preserved so the corroboration's strength is visible.
	effs := witnessed[0].Effects
	if len(effs) != 2 || effs[0].Effect != "PUBLISH loan.approved" || effs[1].Effect != "db DELETE ledger" {
		t.Fatalf("effects not collected/sorted: %+v", effs)
	}
	if effs[1].Verdict != DowngradeCaptureUntrusted {
		t.Errorf("verdict strength dropped: %+v", effs[1])
	}
	// svc.Elsewhere has a candidate but no annotation; svc.Quiet has an annotation
	// but no candidate → unwitnessed.
	if len(unwitnessed) != 1 || unwitnessed[0].Site != "svc.Quiet" {
		t.Fatalf("expected svc.Quiet unwitnessed, got %+v", unwitnessed)
	}
}

// TestWitnessAnnotationsDeterministic pins byte-identical output regardless of
// candidate/annotation arrival order — the audit and its digest must not move.
func TestWitnessAnnotationsDeterministic(t *testing.T) {
	c1 := []Witness{
		{Effect: "PUBLISH a", Verdict: VerdictImpeachment, Observed: Observation{Flow: "f1"}, Severance: sev("svc.Z")},
		{Effect: "PUBLISH b", Verdict: VerdictImpeachment, Observed: Observation{Flow: "f2"}, Severance: sev("svc.A")},
	}
	c2 := []Witness{c1[1], c1[0]} // reversed
	a1 := []graph.Annotation{
		{Site: "svc.Z", Kind: "ExternalBoundaryCall", Note: "z"},
		{Site: "svc.A", Kind: "ExternalBoundaryCall", Note: "a"},
	}
	a2 := []graph.Annotation{a1[1], a1[0]} // reversed

	w1, u1 := WitnessAnnotations(c1, a1)
	w2, u2 := WitnessAnnotations(c2, a2)
	if !reflect.DeepEqual(w1, w2) || !reflect.DeepEqual(u1, u2) {
		t.Fatalf("order-dependent output:\n %+v\n %+v", w1, w2)
	}
	// Sorted by site: svc.A before svc.Z.
	if w1[0].Annotation.Site != "svc.A" || w1[1].Annotation.Site != "svc.Z" {
		t.Errorf("annotations not in (Site) order: %+v", w1)
	}
}
