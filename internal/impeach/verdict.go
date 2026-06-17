package impeach

import (
	"sort"

	"github.com/jyang234/golang-code-graph/internal/groundwork/fitness"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// Verdict integration (plan §9) — the LAST, gated step, and the one the pressure
// test (§13) found two prime-directive cracks in. It invents no new gate: it
// reuses the existing three-valued machinery (SATISFIED / VIOLATED / CANT-PROVE,
// §14-C) and feeds it from a fixed, committed corpus. Two corrections are
// load-bearing and encoded structurally here rather than as guidelines:
//
//   - A witnessed policy breach is a VIOLATED, not a downgrade (§13 crack #1).
//     An impeachment carries a POSITIVE witness — the effect was observed reaching
//     from that entry. If that (Entry, Effect) falls under a must_not_reach rule,
//     the breach demonstrably happened, so it is surfaced as VIOLATED and the gate
//     fails — never laundered to a passing CANT-PROVE.
//   - Only the committed corpus may reach a gate (§13 crack #2). A report over live
//     traffic is not byte-identical run-to-run, so a gate fed by it would be
//     non-deterministic — the prime directive's cardinal sin. The committed/live
//     distinction is a TYPE (CorpusOrigin), and GateBlockers is empty on a live
//     corpus by construction: a live resolution structurally cannot move a gate.

// CorpusOrigin distinguishes the corpus a resolution was computed over. It is a
// soundness invariant carried as a type, not a convenience flag: the gate-facing
// GateBlockers reads it and a live origin yields no blockers, so traffic can never
// move a deterministic gate (§9/§11). The zero value is OriginLive — fail closed,
// so an unset origin is audit-only, never silently gate-eligible.
type CorpusOrigin int

const (
	// OriginLive is the fail-closed zero value: a corpus of live/unknown
	// provenance (current traffic). Audit-only — its verdicts are disclosed but
	// NEVER block a gate, because a live corpus is not reproducible run-to-run.
	OriginLive CorpusOrigin = iota
	// OriginCommitted is a reviewed, committed corpus — the *.golden.json behavior
	// corpus (§14-B): byte-identical run-to-run, so its verdicts MAY reach a gate.
	OriginCommitted
)

// Resolution is the gate-facing Phase-5 artifact: a Report whose IMPEACHMENT
// witnesses have been integrated against the policy's must_not_reach rules — a
// witnessed breach upgraded to VIOLATED, each bare impeachment recording (in
// Claim.Rules) the SATISFIED proofs it downgrades to CANT-PROVE — plus the origin
// fence. It is a pure function of (report, graph, rules, origin); the embedded
// Report carries a recomputed byte-identical digest over the integrated verdicts.
type Resolution struct {
	Report
	Origin CorpusOrigin `json:"-"` // gate-eligibility, not structural content (excluded from the digest)

	// rules is retained so GateBlockers can read each dependent rule's RequireProof
	// without re-deriving the binding. Unexported: it never serializes.
	rules []policy.ReachRule
}

// Resolve integrates a Phase-0..3 report's IMPEACHMENT verdicts against the
// must_not_reach rules (§9). For each impeachment it records the dependent rules
// (those whose `to` binds the effect) in Claim.Rules, and upgrades the verdict to
// VIOLATED when the witnessed entry also binds such a rule's `from` — the
// behaviorally-confirmed breach the cell must never launder (§13 crack #1).
//
// Only IMPEACHMENT witnesses integrate: a downgrade (VERSION-SKEW, CAPTURE-
// UNTRUSTED, …) is not a sound impeachment, so it never touches a verdict (fail
// closed). The result is a pure function of its inputs — all matching goes through
// the ONE shared matcher (fitness.MatchesAny) and the ONE effect-parity source
// (effectEmitters), so it can never diverge from the matcher the gates themselves
// use, and the recomputed digest is byte-identical across runs.
func Resolve(r Report, ix *graph.Index, rules []policy.ReachRule, origin CorpusOrigin) Resolution {
	cand := make([]Witness, len(r.Candidates))
	copy(cand, r.Candidates)
	for i := range cand {
		w := &cand[i]
		if w.Verdict != VerdictImpeachment {
			continue // only a sound impeachment integrates; a downgrade never gates
		}
		surface := effectMatchSurface(ix, w.Effect)
		entryFn := mapEntry(ix, w.Observed.Entry)
		dep := map[string]bool{}
		violated := false
		for _, rule := range rules {
			if !matchesAnyOf(surface, rule.To) {
				continue // this rule's `to` does not bind the impeached effect
			}
			dep[rule.Name] = true
			// VIOLATED only when the witnessed entry handler binds the rule's
			// `from`: then a from→to path the rule forbids was OBSERVED. A missed
			// root (entryFn == "") cannot bind a `from`, so it stays a bare
			// impeachment — fail closed, never a false VIOLATED on an unmappable entry.
			if entryFn != "" && fitness.MatchesAny(entryFn, rule.From) {
				violated = true
			}
		}
		w.Claim.Rules = sortedKeys(dep)
		if violated {
			w.Verdict = VerdictViolated
		}
	}
	out := r
	out.Candidates = cand
	out.Digest = ""
	out.Digest = canonicalDigest(out)
	return Resolution{Report: out, Origin: origin, rules: rules}
}

// GateFinding is one gate-blocking witness with the precise reason it blocks —
// the bridge to the review layer's BLOCK (§14-C). It carries the verdict and the
// must_not_reach rule so the disclosure names exactly which invariant the
// behavioral witness broke or unproved.
type GateFinding struct {
	Effect  string `json:"effect"`
	Flow    string `json:"flow"`
	Entry   string `json:"entry"`
	Verdict string `json:"verdict"` // VIOLATED | IMPEACHMENT
	Rule    string `json:"rule"`    // the must_not_reach rule whose proof this breaks/unproves
	Reason  string `json:"reason"`
}

// GateBlockers returns the witnesses that fail a gate. It is EMPTY unless the
// resolution came from a COMMITTED corpus (§9 crack #2): a live corpus is
// audit-only, so its verdicts are disclosed (in Candidates) but can never move a
// deterministic gate. A witness blocks when:
//
//   - it is VIOLATED — a behaviorally-confirmed must_not_reach breach (§9); or
//   - it is a bare IMPEACHMENT whose dependent rule sets require_proof: the proof
//     the rule relied on is downgraded SATISFIED→CANT-PROVE, and require_proof
//     fails closed on a rule it can no longer prove (mirrors fitness's reach gate).
//
// A bare impeachment of a non-require_proof rule downgrades to an ADVISORY
// CANT-PROVE (disclosed, not blocking) — exactly fitness's default for an
// unprovable must_not_reach. Observe-first holds: every verdict is in Candidates
// as disclosure regardless of origin; only this gate-facing subset blocks.
func (res Resolution) GateBlockers() []GateFinding {
	if res.Origin != OriginCommitted {
		return nil // live corpus ⇒ audit-only; a non-deterministic gate is refused
	}
	reqProof := map[string]bool{}
	for _, rule := range res.rules {
		if rule.RequireProof {
			reqProof[rule.Name] = true
		}
	}
	var out []GateFinding
	for _, w := range res.Candidates {
		switch w.Verdict {
		case VerdictViolated:
			out = append(out, GateFinding{
				Effect: w.Effect, Flow: w.Observed.Flow, Entry: w.Observed.Entry,
				Verdict: VerdictViolated, Rule: firstReqOrAny(w.Claim.Rules, reqProof),
				Reason: "behaviorally-confirmed must_not_reach breach: the forbidden effect was observed reaching from this entry",
			})
		case VerdictImpeachment:
			for _, name := range w.Claim.Rules {
				if reqProof[name] {
					out = append(out, GateFinding{
						Effect: w.Effect, Flow: w.Observed.Flow, Entry: w.Observed.Entry,
						Verdict: VerdictImpeachment, Rule: name,
						Reason: "impeachment downgrades a require_proof must_not_reach proof to CANT-PROVE (fails closed)",
					})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return lessGateFinding(out[i], out[j]) })
	return out
}

// effectMatchSurface is the set of strings a must_not_reach `to` pattern may bind
// to claim this effect: the emitter FQNs (a rule naming the emitting function) AND
// the raw boundary labels (a rule naming the boundary effect, e.g.
// "boundary:db DELETE ledger"). Derived from the ONE parity source (effectEmitters)
// so it can never disagree with the severance walk on what emits the effect.
func effectMatchSurface(ix *graph.Index, effect string) []string {
	seen := map[string]bool{}
	for _, e := range effectEmitters(ix, effect) {
		if e.From != "" {
			seen[e.From] = true
		}
		if e.Label != "" {
			seen[e.Label] = true
		}
	}
	return sortedKeys(seen)
}

// matchesAnyOf reports whether ANY candidate string binds any pattern, via the
// shared boundary-aware matcher (fitness.MatchesAny). Used to test whether a
// rule's `to` binds any of the effect's match-surface strings.
func matchesAnyOf(candidates, patterns []string) bool {
	for _, c := range candidates {
		if fitness.MatchesAny(c, patterns) {
			return true
		}
	}
	return false
}

// firstReqOrAny returns the first require_proof dependent rule (the one that
// actually drives the block), or the first rule otherwise — so a VIOLATED finding
// names the most consequential invariant it breaks. Names are pre-sorted.
func firstReqOrAny(names []string, reqProof map[string]bool) string {
	for _, n := range names {
		if reqProof[n] {
			return n
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

func lessGateFinding(a, b GateFinding) bool {
	if a.Effect != b.Effect {
		return a.Effect < b.Effect
	}
	if a.Flow != b.Flow {
		return a.Flow < b.Flow
	}
	if a.Entry != b.Entry {
		return a.Entry < b.Entry
	}
	if a.Verdict != b.Verdict {
		return a.Verdict < b.Verdict
	}
	return a.Rule < b.Rule
}

// sortedKeys is the deterministic key list of a set — intrinsic order, never map
// iteration (§5/§10 determinism).
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
