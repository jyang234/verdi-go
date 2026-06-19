package impeach

import (
	"sort"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// AnnotationGrade is the corpus's verdict on one blind-spot annotation. It grades
// the SEAM and (when the annotation carries a structured Claim) the claim — never
// the note's prose, which the machine cannot read. Every grade is disclosure: none
// closes the blind spot, traverses the seam, or feeds a verdict.
type AnnotationGrade string

const (
	// AnnotationUnwitnessed: no observed effect was severed at the site — the
	// corpus does not reach this seam, so the note stands but is unverified.
	AnnotationUnwitnessed AnnotationGrade = "UNWITNESSED"
	// AnnotationWitnessed: an annotation with NO structured claim whose site the
	// corpus corroborates — a real effect is severed there. The seam is behaviorally
	// real; the prose is still the human's to judge against the disclosed effect.
	AnnotationWitnessed AnnotationGrade = "WITNESSED"
	// AnnotationConfirmed: an annotation whose structured Claim matches an effect
	// observed severed at the site — the specific assertion is corroborated.
	AnnotationConfirmed AnnotationGrade = "CONFIRMED"
	// AnnotationUnconfirmed: a claimed annotation whose site IS witnessed but whose
	// claimed effect is NOT among those observed. Asymmetric on purpose (tenet 4): a
	// sample's silence is not proof of absence, so this is "not corroborated, look",
	// never "false". The observed effects are disclosed so the discrepancy is legible.
	AnnotationUnconfirmed AnnotationGrade = "UNCONFIRMED"
)

// GradedAnnotation is one annotation with its corpus grade and the observed effects
// severed at its site (empty when UNWITNESSED). Audit-only — a stronger disclosure,
// never a proof.
type GradedAnnotation struct {
	Annotation graph.Annotation `json:"annotation"`
	Grade      AnnotationGrade  `json:"grade"`
	Effects    []EffectWitness  `json:"effects,omitempty"`
}

// EffectWitness is one observed effect static lost at the annotated site, with the
// downgrade-ladder verdict that conveys how strong the corroboration is (an
// IMPEACHMENT is firm; a CAPTURE-UNTRUSTED candidate is weak, low-fidelity evidence).
type EffectWitness struct {
	Effect  string `json:"effect"`  // the join key, e.g. "PUBLISH loan.approved" / "db DELETE ledger"
	Verdict string `json:"verdict"` // the candidate's ladder verdict — the strength of the corroboration
	Flow    string `json:"flow"`    // the flow it was observed in
}

// GradeAnnotations grades each annotation against the corpus candidates. The join
// is site equality against the candidate localization the impeach lens already
// prints (Witness.Severance.Site) — the SAME rule, so grading and the candidate
// disclosure cannot drift. When an annotation carries a structured Claim, the grade
// also compares the claimed effect key (exact, trimmed) against the observed effects
// at the site: CONFIRMED on a match, UNCONFIRMED when the seam is witnessed but the
// claim is not among the observed effects (never "false" — a sample is not
// exhaustive). An unclaimed annotation is WITNESSED/UNWITNESSED as before.
//
// Deterministic: annotations in (Site, Kind) order, each one's effects in (Effect,
// Verdict, Flow) order, so the rendered audit is byte-identical across runs. It
// reads and decides nothing — no gate, no count, no verdict change.
func GradeAnnotations(candidates []Witness, anns []graph.Annotation) []GradedAnnotation {
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

	out := make([]GradedAnnotation, 0, len(sorted))
	for _, a := range sorted {
		ws := bySite[a.Site]
		if len(ws) == 0 {
			out = append(out, GradedAnnotation{Annotation: a, Grade: AnnotationUnwitnessed})
			continue
		}
		effs := make([]EffectWitness, 0, len(ws))
		claimMatched := false
		claim := strings.TrimSpace(a.Claim)
		for _, w := range ws {
			effs = append(effs, EffectWitness{Effect: w.Effect, Verdict: w.Verdict, Flow: w.Observed.Flow})
			if claim != "" && w.Effect == claim {
				claimMatched = true
			}
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
		var grade AnnotationGrade
		switch {
		case claim == "":
			grade = AnnotationWitnessed
		case claimMatched:
			grade = AnnotationConfirmed
		default:
			grade = AnnotationUnconfirmed
		}
		out = append(out, GradedAnnotation{Annotation: a, Grade: grade, Effects: effs})
	}
	return out
}
