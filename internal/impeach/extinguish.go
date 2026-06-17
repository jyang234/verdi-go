package impeach

import (
	"github.com/jyang234/golang-code-graph/internal/groundwork/fitness"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
	"github.com/jyang234/golang-code-graph/ir"
)

// The per-repair acceptance gate (plan §6/§8, §13 crack #4). A loop whose repairs
// don't extinguish their own impeachments is the thing to refuse to ship, so a
// proposed repair is accepted only when, AFTER (hypothetically) ratifying it, the
// regenerated verdicts satisfy a MONOTONIC criterion:
//
//  1. the TARGET impeachment extinguishes (blinding its Site makes the effect
//     seam-reachable ⇒ static abstains there ⇒ it is no longer a candidate); and
//  2. NO proof is newly created — no must_not_reach (or other) finding that held
//     before the repair disappears after it.
//
// (2) is the §13-crack-#4 correction: blinding a site downgrades MANY proofs
// (SATISFIED→CANT-PROVE) and may extinguish SEVERAL impeachments, so the test is
// emphatically NOT "the candidate count drops by exactly one." It is monotonic:
// proofs may only WEAKEN. Because a blind spot adds disclosure without removing any
// edge, fitness's reachable paths are untouched and its proofs can only turn
// unprovable — so a finding DISAPPEARING (a newly-minted proof) is an anomaly that
// fails the gate. A mislocalized Site fails (1): it does not blind the real seam,
// so the target persists and announces the mislocalization (§6 self-extinguish).

// ExtinguishResult records the gate's decision and WHY, so a rejected repair is
// actionable (mislocalized? created a proof?) without re-running the regeneration.
type ExtinguishResult struct {
	OK              bool   // both criteria held: accept the repair
	Extinguished    bool   // criterion 1: the target is no longer a candidate
	Monotonic       bool   // criterion 2: no finding (proof) was newly created
	Reason          string // the failure, when !OK
	NewProofWitness string // the finding that disappeared, when !Monotonic
}

// SelfExtinguishes runs the per-repair acceptance gate (§6/§8) for a proposed
// blind-spot repair against (graph, target, corpus, provenance, policy). It is a
// pure function: it builds the hypothetically-ratified graph in-memory
// (ix.WithBlindSpots — the original is never mutated), re-runs the audit and the
// fitness reach gate over it, and checks the monotonic criterion. Only a
// RepairBlindSpot is gated here; a RepairReclaimer is never auto-proposed (§8) and
// is refused outright (it fabricates reachability, a different and unsound-by-
// default acceptance question).
func SelfExtinguishes(ix *graph.Index, repair *ProposedRepair, target Witness, traces []*ir.CanonicalTrace, prov Provenance, p *policy.Policy, service string) ExtinguishResult {
	if repair == nil || repair.Kind != RepairBlindSpot || repair.Site == "" {
		return ExtinguishResult{Reason: "no blind-spot repair to gate (reclaimers are never auto-proposed, §8)"}
	}

	// Findings BEFORE the repair: every proof/disclosure the policy makes today.
	before := findingKeys(p, ix)

	// The hypothetically-ratified graph: the seam disclosed blind at Site. Built on
	// a copy, so ix is untouched — this is a dry run, never an enactment.
	repaired := ix.WithBlindSpots(graph.BlindSpot{
		Kind:   BlindSpotKindImpeachment,
		Site:   repair.Site,
		Detail: repair.Detail,
	})

	// Criterion 1: the target extinguishes. Re-audit over the repaired graph; the
	// target is identified by its intrinsic witness identity (effect, flow, entry,
	// op, path), so a still-present same-identity candidate means the repair did not
	// blind the real seam (mislocalized, §6).
	r2 := Audit(service, repaired, traces, prov)
	extinguished := !candidatePresent(r2, target)

	// Criterion 2: monotonic — no finding that held before is gone after. Blinding
	// only weakens proofs, so a disappeared finding is a newly-created proof, which
	// must never happen (and would mean the regeneration is unsound).
	after := findingKeys(p, repaired)
	lost := ""
	for k := range before {
		if !after[k] {
			lost = k
			break
		}
	}
	monotonic := lost == ""

	res := ExtinguishResult{Extinguished: extinguished, Monotonic: monotonic, NewProofWitness: lost}
	switch {
	case !extinguished:
		res.Reason = "target impeachment did NOT extinguish: the blind spot at " + repair.Site + " does not cover the witnessed seam (mislocalized, §6)"
	case !monotonic:
		res.Reason = "repair created a new proof (a finding disappeared): " + lost + " — proofs may only weaken (§6 monotonic)"
	default:
		res.OK = true
	}
	return res
}

// findingKeys is the set of finding identities the policy produces over ix —
// every Violation and Caution (a proven-absent rule produces no finding, so the
// SET shrinking means a proof was newly created). Reuses fitness.Check (the ONE
// reach/obligation evaluator) so the monotonic gate reads exactly the findings the
// real gate does, never a re-implementation that could drift.
func findingKeys(p *policy.Policy, ix *graph.Index) map[string]bool {
	out := map[string]bool{}
	if p == nil {
		return out
	}
	for _, f := range fitness.Check(p, ix).Findings {
		out[f.Key()] = true
	}
	return out
}

// candidatePresent reports whether r still carries a candidate with target's
// intrinsic identity — the same tuple lessWitness orders on (effect, flow,
// service, entry, op, causal path). Identity is content, not pointer: the repaired
// audit produces fresh witnesses, so the match must be structural.
func candidatePresent(r Report, target Witness) bool {
	for _, w := range r.Candidates {
		if sameWitness(w, target) {
			return true
		}
	}
	return false
}

// sameWitness is structural witness identity for the extinguish check — the same
// intrinsic fields lessWitness tie-breaks on, including the causal path signature
// (two paths to one effect are distinct witnesses, §5/Phase 3).
func sameWitness(a, b Witness) bool {
	return a.Effect == b.Effect &&
		a.Observed.Flow == b.Observed.Flow &&
		a.Observed.Service == b.Observed.Service &&
		a.Observed.Entry == b.Observed.Entry &&
		a.Observed.Op == b.Observed.Op &&
		pathSig(a.chain) == pathSig(b.chain)
}
