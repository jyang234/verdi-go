package impeach

import (
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// The discovery loop's proposal half (plan §8). The loop PROPOSES; a human
// ratifies — it never silently rewrites the soundness substrate (the same model as
// `init` proposing + a CODEOWNER committing). This file builds the proposal; the
// per-repair acceptance gate is extinguish.go.

// Repair kinds (§5/§8). The fail-toward-abstain gradient lives in which one the
// loop will auto-propose: only the always-sound blind_spot.
const (
	// RepairBlindSpot discloses the severance Site as a blind spot — ALWAYS sound:
	// it makes static abstain at the seam (NEVER→CANT-PROVE), so its worst failure
	// is over-abstention, never a fabricated reachability. The loop's default.
	RepairBlindSpot = "blind_spot"

	// RepairReclaimer adds a recovered edge — the PRECISE repair, but it fabricates
	// reachability, so a wrong edge mints a false VIOLATED. It needs human
	// ratification and is NEVER auto-proposed by the loop (§8); which severance bins
	// are reclaimer-safe is deferred to the reclaimer framework (§12.4).
	RepairReclaimer = "reclaimer"
)

// BlindSpotKindImpeachment is the graph.BlindSpot.Kind a ratified impeachment
// repair carries — a disclosed seam discovered by behavior, distinct from the
// statically-detected kinds (reflect, HighFanOut, …). It is also the ratchet
// allow-list entry's Kind, so the co-update (§8) matches the repair exactly.
const BlindSpotKindImpeachment = "ImpeachmentSeam"

// ProposedRepair mirrors blindspots.BlindSpot / frontier.Marker on purpose (§5):
// ratification is "move this proposed blind spot into the graph," not a
// translation (one source of truth for the blind-spot shape). It is a PROPOSAL —
// never enacted here.
type ProposedRepair struct {
	Kind   string `json:"kind"`             // RepairBlindSpot (default) | RepairReclaimer
	Site   string `json:"site"`             // the severance point (§6)
	Detail string `json:"detail,omitempty"` // why it is proposed (the witness)
}

// ProposeRepair builds the Phase-4 proposed substrate change for an impeached
// witness (§8): a DISCLOSED BLIND SPOT at the severance Site, always. The
// fail-toward-abstain gradient (§8): disclosing a blind spot is always safe
// (abstain), whereas a reclaimer is the precise repair that demands a provably-real
// edge — so the loop prefers disclosure and NEVER auto-proposes a reclaimer.
//
// Returns nil when there is no sound Site to repair: a witness that did not reach
// an impeaching verdict (only IMPEACHMENT/VIOLATED carry a repair, §5), or whose
// severance localized to no seam (SeveranceNone — the §6 proof obligation failed,
// so there is nothing to disclose without fabricating one).
func ProposeRepair(w Witness) *ProposedRepair {
	if w.Verdict != VerdictImpeachment && w.Verdict != VerdictViolated {
		return nil
	}
	if w.Severance == nil || w.Severance.Site == "" || w.Severance.Kind == SeveranceNone {
		return nil
	}
	return &ProposedRepair{
		Kind:   RepairBlindSpot,
		Site:   w.Severance.Site,
		Detail: "behavioral impeachment of " + w.Effect + " witnessed on flow " + w.Observed.Flow,
	}
}

// RatchetEntry is the blind-spot-ratchet allow-list entry a ratified repair
// co-commits in the SAME reviewed act (§8, §13 crack #6). Adding a blind spot is
// exactly what blind_spot_ratchet gates as "a new blind spot," so a ratification
// that disclosed the seam WITHOUT allow-listing it would trip that sibling gate.
// Keyed (Kind, Site) identically to the blind spot the repair adds, with the
// impeachment witness as its Reason — a reviewed, intentional disclosure, not drift.
func RatchetEntry(repair *ProposedRepair, w Witness) policy.BlindSpotException {
	return policy.BlindSpotException{
		Kind:   BlindSpotKindImpeachment,
		Site:   repair.Site,
		Reason: "ratified behavioral impeachment: " + w.Effect + " observed on flow " + w.Observed.Flow,
	}
}
