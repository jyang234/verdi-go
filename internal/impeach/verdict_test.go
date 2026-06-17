package impeach

import (
	"bytes"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
	"github.com/jyang234/golang-code-graph/ir"
)

// impeachFixture builds the canonical Phase-5 setup: a severed DB DELETE emitter
// the graph models but no root reaches, observed reaching from the HTTP entry, with
// production provenance and a matching code stamp — so the lone candidate clears
// every ladder rung and is a true IMPEACHMENT, the input the verdict layer integrates.
func impeachFixture(t *testing.T) (*graph.Index, Report, Provenance) {
	t.Helper()
	ix := graph.NewIndex(&graph.Graph{
		Stamp:       "sha1",
		Nodes:       []graph.Node{{FQN: "svc.handler"}, {FQN: "svc.orphan"}},
		Edges:       []graph.Edge{{From: "svc.orphan", To: "boundary:db DELETE ledger", Boundary: "outbound-sync"}},
		Entrypoints: []graph.Entrypoint{{Kind: "http", Name: "POST /x", Fn: "svc.handler"}},
	})
	tr := &ir.CanonicalTrace{Flow: "POST /x", Service: "svc", Root: &ir.CanonicalSpan{
		Op: "HTTP POST /x", Kind: ir.KindServer,
		Children: []ir.ChildGroup{{Members: []*ir.CanonicalSpan{
			{Op: "DB postgres DELETE ledger", Kind: ir.KindClient},
		}}},
	}}
	prov := Provenance{TraceIdentity: "sha1", Capture: CaptureProduction}
	r := Audit("svc", ix, []*ir.CanonicalTrace{tr}, prov)
	if len(r.Candidates) != 1 || r.Candidates[0].Verdict != VerdictImpeachment {
		t.Fatalf("fixture is not a clean IMPEACHMENT: %+v", r.Candidates)
	}
	return ix, r, prov
}

// TestResolveWitnessedBreachIsViolated is the §13-crack-#1 fix: an impeachment
// whose witnessed (Entry, Effect) falls under a must_not_reach rule — the entry
// binds `from`, the effect binds `to` — is upgraded to VIOLATED, the behaviorally-
// confirmed breach, never laundered to a passing CANT-PROVE.
func TestResolveWitnessedBreachIsViolated(t *testing.T) {
	ix, r, _ := impeachFixture(t)
	rules := []policy.ReachRule{{
		Name: "no-delete-from-web", From: []string{"svc.handler"}, To: []string{"boundary:db DELETE ledger"},
	}}
	res := Resolve(r, ix, rules, OriginCommitted)
	w := res.Candidates[0]
	if w.Verdict != VerdictViolated {
		t.Fatalf("Verdict = %q, want %q (witnessed must_not_reach breach)", w.Verdict, VerdictViolated)
	}
	if len(w.Claim.Rules) != 1 || w.Claim.Rules[0] != "no-delete-from-web" {
		t.Errorf("Claim.Rules = %v, want [no-delete-from-web]", w.Claim.Rules)
	}
}

// TestResolveBareImpeachmentRecordsRulesNotViolated is the carve-out's other side:
// the effect binds the rule's `to` but the entry is OUTSIDE its `from`, so no
// from→to path was witnessed. It stays a BARE impeachment (records the dependent
// rule it downgrades, never VIOLATED) — over-claiming VIOLATED here would be a
// false BLOCK.
func TestResolveBareImpeachmentRecordsRulesNotViolated(t *testing.T) {
	ix, r, _ := impeachFixture(t)
	rules := []policy.ReachRule{{
		Name: "no-delete-from-cron", From: []string{"svc.cronjob"}, To: []string{"boundary:db DELETE ledger"},
	}}
	res := Resolve(r, ix, rules, OriginCommitted)
	w := res.Candidates[0]
	if w.Verdict != VerdictImpeachment {
		t.Errorf("Verdict = %q, want bare %q (entry outside the rule's from)", w.Verdict, VerdictImpeachment)
	}
	if len(w.Claim.Rules) != 1 || w.Claim.Rules[0] != "no-delete-from-cron" {
		t.Errorf("Claim.Rules = %v, want the dependent rule recorded", w.Claim.Rules)
	}
}

// TestResolveUnrelatedRuleIsNotDependent confirms a rule whose `to` does not bind
// the effect is neither recorded nor a breach — the match is real, not blanket.
func TestResolveUnrelatedRuleIsNotDependent(t *testing.T) {
	ix, r, _ := impeachFixture(t)
	rules := []policy.ReachRule{{
		Name: "no-publish", From: []string{"svc.handler"}, To: []string{"boundary:bus PUBLISH other.event"},
	}}
	res := Resolve(r, ix, rules, OriginCommitted)
	w := res.Candidates[0]
	if w.Verdict != VerdictImpeachment || len(w.Claim.Rules) != 0 {
		t.Errorf("unrelated rule bound the effect: verdict=%q rules=%v", w.Verdict, w.Claim.Rules)
	}
}

// TestGateBlockersFencedToCommittedCorpus is the §13-crack-#2 prime-directive
// fence: the SAME witnessed VIOLATED blocks on a committed corpus but yields NO
// gate blocker on a live corpus — a gate fed by run-varying traffic would be
// non-deterministic, so live is audit-only by construction.
func TestGateBlockersFencedToCommittedCorpus(t *testing.T) {
	ix, r, _ := impeachFixture(t)
	rules := []policy.ReachRule{{
		Name: "no-delete-from-web", From: []string{"svc.handler"}, To: []string{"boundary:db DELETE ledger"},
	}}

	committed := Resolve(r, ix, rules, OriginCommitted).GateBlockers()
	if len(committed) != 1 || committed[0].Verdict != VerdictViolated {
		t.Fatalf("committed corpus: want 1 VIOLATED blocker, got %+v", committed)
	}

	live := Resolve(r, ix, rules, OriginLive).GateBlockers()
	if len(live) != 0 {
		t.Errorf("live corpus must never gate (non-deterministic): got %+v", live)
	}
}

// TestGateBlockersZeroOriginIsAuditOnly pins the fail-closed default: the zero
// CorpusOrigin (OriginLive) never gates, so an unset origin cannot silently move a
// verdict.
func TestGateBlockersZeroOriginIsAuditOnly(t *testing.T) {
	ix, r, _ := impeachFixture(t)
	rules := []policy.ReachRule{{Name: "x", From: []string{"svc.handler"}, To: []string{"boundary:db DELETE ledger"}}}
	var zero CorpusOrigin
	if got := Resolve(r, ix, rules, zero).GateBlockers(); len(got) != 0 {
		t.Errorf("zero origin gated: %+v", got)
	}
}

// TestGateBlockersBareImpeachmentOnlyBlocksUnderRequireProof: a bare impeachment
// downgrades its dependent proof SATISFIED→CANT-PROVE. That blocks the gate ONLY
// for a require_proof rule (fails closed on an unprovable invariant); a default
// rule downgrades to an advisory CANT-PROVE that discloses without blocking.
func TestGateBlockersBareImpeachmentOnlyBlocksUnderRequireProof(t *testing.T) {
	ix, r, _ := impeachFixture(t)

	advisory := []policy.ReachRule{{Name: "soft", From: []string{"svc.cronjob"}, To: []string{"boundary:db DELETE ledger"}}}
	if got := Resolve(r, ix, advisory, OriginCommitted).GateBlockers(); len(got) != 0 {
		t.Errorf("advisory bare impeachment blocked the gate: %+v", got)
	}

	strict := []policy.ReachRule{{Name: "hard", From: []string{"svc.cronjob"}, To: []string{"boundary:db DELETE ledger"}, RequireProof: true}}
	got := Resolve(r, ix, strict, OriginCommitted).GateBlockers()
	if len(got) != 1 || got[0].Verdict != VerdictImpeachment || got[0].Rule != "hard" {
		t.Errorf("require_proof bare impeachment did not block: %+v", got)
	}
}

// TestResolveNeverTouchesDowngrades: a witness that did NOT reach IMPEACHMENT (a
// ladder downgrade) is not a sound impeachment, so the verdict layer leaves it
// untouched and never gates on it — fail closed.
func TestResolveNeverTouchesDowngrades(t *testing.T) {
	ix, _, _ := impeachFixture(t)
	// Drop provenance ⇒ the candidate caps at VERSION-SKEW (a downgrade).
	tr := &ir.CanonicalTrace{Flow: "POST /x", Service: "svc", Root: &ir.CanonicalSpan{
		Op: "HTTP POST /x", Kind: ir.KindServer,
		Children: []ir.ChildGroup{{Members: []*ir.CanonicalSpan{{Op: "DB postgres DELETE ledger", Kind: ir.KindClient}}}},
	}}
	r := Audit("svc", ix, []*ir.CanonicalTrace{tr}, Provenance{})
	if r.Candidates[0].Verdict == VerdictImpeachment {
		t.Fatal("fixture should be a downgrade without provenance")
	}
	rules := []policy.ReachRule{{Name: "no-del", From: []string{"svc.handler"}, To: []string{"boundary:db DELETE ledger"}}}
	res := Resolve(r, ix, rules, OriginCommitted)
	if res.Candidates[0].Verdict != r.Candidates[0].Verdict {
		t.Errorf("a downgrade was re-classified: %q -> %q", r.Candidates[0].Verdict, res.Candidates[0].Verdict)
	}
	if len(res.GateBlockers()) != 0 {
		t.Errorf("a downgrade gated: %+v", res.GateBlockers())
	}
}

// TestWitnessFromSurfaceExcludesEmitters pins the §13 soundness invariant (#12):
// the effect emitter is the terminus of the localized anchor chain, and the
// from-surface subtracts exactly the emitters — so a must_not_reach `from` pattern
// can never bind the emitting function and mint the tautology "the emitter reaches
// the effect it emits" as a witnessed VIOLATED.
func TestWitnessFromSurfaceExcludesEmitters(t *testing.T) {
	ix, r, _ := impeachFixture(t)
	w := r.Candidates[0]
	emitters := staticEmitters(ix, w.Effect)
	if len(emitters) == 0 {
		t.Fatal("fixture should model an emitter")
	}
	if w.Severance == nil {
		t.Fatal("an impeachment must carry a severance")
	}
	// The emitter IS in the anchor chain (its terminus) — the invariant the
	// subtraction relies on.
	emitterInAnchors := false
	for _, a := range w.Severance.Anchors {
		for _, e := range emitters {
			if a == e {
				emitterInAnchors = true
			}
		}
	}
	if !emitterInAnchors {
		t.Fatalf("emitter absent from anchors %v; the strip-emitters invariant assumes the emitter is the chain terminus", w.Severance.Anchors)
	}
	// ...but the from-surface strips it.
	surface := witnessFromSurface(ix, w)
	for _, s := range surface {
		for _, e := range emitters {
			if s == e {
				t.Errorf("from-surface %v includes emitter %q: a tautological from→its-own-effect breach could be minted", surface, e)
			}
		}
	}
	// The discovered entry handler (a genuine causal ancestor) survives.
	found := false
	for _, s := range surface {
		if s == "svc.handler" {
			found = true
		}
	}
	if !found {
		t.Errorf("from-surface %v dropped the entry handler (the real causal ancestor)", surface)
	}
}

// TestResolveLivePreservesDisclosure pins that the live-corpus gate FENCE (no
// blocker — non-deterministic traffic must never gate) does NOT suppress the
// DISCLOSURE (#21): the witnessed breach is still upgraded to VIOLATED in the
// report so observe-first surfaces it. The fence is on gating, not on telling the
// truth — silently dropping the verdict on a live corpus would be a hidden
// disclosure, the worst outcome under the prime directive.
func TestResolveLivePreservesDisclosure(t *testing.T) {
	ix, r, _ := impeachFixture(t)
	rules := []policy.ReachRule{{
		Name: "no-delete-from-web", From: []string{"svc.handler"}, To: []string{"boundary:db DELETE ledger"},
	}}
	res := Resolve(r, ix, rules, OriginLive)
	if len(res.GateBlockers()) != 0 {
		t.Fatalf("live corpus gated (must be audit-only): %+v", res.GateBlockers())
	}
	if res.Candidates[0].Verdict != VerdictViolated {
		t.Errorf("live disclosure lost: verdict = %q, want %q recorded in the report", res.Candidates[0].Verdict, VerdictViolated)
	}
}

// TestResolveDeterministic: the resolved report's digest is a pure function of its
// inputs and independent of must_not_reach rule order (the dependent set is sorted).
func TestResolveDeterministic(t *testing.T) {
	ix, r, _ := impeachFixture(t)
	a := []policy.ReachRule{
		{Name: "a", From: []string{"svc.cronjob"}, To: []string{"boundary:db DELETE ledger"}},
		{Name: "b", From: []string{"svc.cronjob"}, To: []string{"boundary:db DELETE ledger"}},
	}
	b := []policy.ReachRule{a[1], a[0]} // reversed order
	da := Resolve(r, ix, a, OriginCommitted).Digest
	db := Resolve(r, ix, b, OriginCommitted).Digest
	if da == "" || da != db {
		t.Errorf("digest not order-independent: %q vs %q", da, db)
	}
	// And byte-identical across repeated runs.
	if !bytes.Equal([]byte(da), []byte(Resolve(r, ix, a, OriginCommitted).Digest)) {
		t.Error("digest not reproducible across runs")
	}
}
