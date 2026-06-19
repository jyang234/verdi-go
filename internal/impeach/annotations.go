package impeach

import (
	"sort"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// AnnotationWitness pairs a blind-spot annotation with the corpus evidence that
// corroborates the SEAM it explains: the observed effects static lost at the
// annotation's site. It witnesses that a real boundary effect is severed there —
// NOT that the annotation's prose is accurate. The machine cannot read prose; it
// can only confirm that the seam the note explains hides a behaviorally-observed
// effect, and disclose what that effect was so a human judges the note against it.
// Audit-only: nothing here is a verdict input (annotations are disclosure-only,
// and a witnessed annotation is still disclosure — a stronger one).
type AnnotationWitness struct {
	Annotation graph.Annotation `json:"annotation"`
	Effects    []EffectWitness  `json:"effects"` // the corpus effects severed at this site, sorted
}

// EffectWitness is one observed effect static lost at the annotated site, with the
// downgrade-ladder verdict that conveys how strong the corroboration is (an
// IMPEACHMENT is firm; a CAPTURE-UNTRUSTED candidate is weak, low-fidelity evidence).
type EffectWitness struct {
	Effect  string `json:"effect"`  // the join key, e.g. "PUBLISH loan.approved" / "db DELETE ledger"
	Verdict string `json:"verdict"` // the candidate's ladder verdict — the strength of the corroboration
	Flow    string `json:"flow"`    // the flow it was observed in
}

// WitnessAnnotations corroborates annotations against corpus candidates. An
// annotation is WITNESSED when a candidate's localized severance site equals the
// annotation's site: the corpus observed an effect that static lost exactly at the
// seam the annotation explains. Site equality is the join — the SAME localization
// the impeach lens already prints (Witness.Severance.Site) — so corroboration and
// disclosure cannot drift. The result is deterministic: annotations are emitted in
// (Site, Kind) order and each witnessed annotation's effects in (Effect, Verdict,
// Flow) order, so the rendered audit is byte-identical across runs.
//
// It reads candidates and annotations and decides nothing: no gate, no count, no
// verdict change. A witnessed annotation is a stronger disclosure, never a proof —
// the corpus confirms the seam hides a real effect, not that the note describes it.
func WitnessAnnotations(candidates []Witness, anns []graph.Annotation) (witnessed []AnnotationWitness, unwitnessed []graph.Annotation) {
	bySite := map[string][]Witness{}
	for _, w := range candidates {
		if w.Severance != nil && w.Severance.Site != "" {
			bySite[w.Severance.Site] = append(bySite[w.Severance.Site], w)
		}
	}

	sorted := append([]graph.Annotation(nil), anns...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Site != sorted[j].Site {
			return sorted[i].Site < sorted[j].Site
		}
		return sorted[i].Kind < sorted[j].Kind
	})

	for _, a := range sorted {
		ws := bySite[a.Site]
		if len(ws) == 0 {
			unwitnessed = append(unwitnessed, a)
			continue
		}
		effs := make([]EffectWitness, 0, len(ws))
		for _, w := range ws {
			effs = append(effs, EffectWitness{Effect: w.Effect, Verdict: w.Verdict, Flow: w.Observed.Flow})
		}
		sort.Slice(effs, func(i, j int) bool {
			if effs[i].Effect != effs[j].Effect {
				return effs[i].Effect < effs[j].Effect
			}
			if effs[i].Verdict != effs[j].Verdict {
				return effs[i].Verdict < effs[j].Verdict
			}
			return effs[i].Flow < effs[j].Flow
		})
		witnessed = append(witnessed, AnnotationWitness{Annotation: a, Effects: effs})
	}
	return witnessed, unwitnessed
}
