package impeach

import (
	"reflect"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/ir"
)

// passingCase is the canonical scenario where EVERY ladder rung clears, so a
// single targeted perturbation isolates exactly one downgrade. The graph is
// stamped with a commit, the supplied identity matches it, the effect is in the
// one-source DB vocabulary, the span is the audited service's own, and the
// capture is production.
func passingCase() (Witness, *graph.Index, string, Provenance) {
	const commit = "c1"
	w := Witness{
		Effect:   "db DELETE ledger",
		Claim:    Claim{Reachability: ReachUnreachable},
		Observed: Observation{Flow: "f", Service: "svc", Entry: "HTTP DELETE /x", Op: "DB postgres DELETE ledger"},
	}
	ix := graph.NewIndex(&graph.Graph{Stamp: commit})
	return w, ix, "svc", Provenance{TraceIdentity: commit, Capture: CaptureProduction}
}

// TestLadderAllPassIsImpeachment pins the apex: when every benign explanation is
// ruled out, the verdict is IMPEACHMENT and the full ordered ladder is recorded.
func TestLadderAllPassIsImpeachment(t *testing.T) {
	w, ix, svc, prov := passingCase()
	rungs, verdict := classify(w, ix, svc, prov)
	if verdict != VerdictImpeachment {
		t.Fatalf("verdict = %q, want %q; ladder = %+v", verdict, VerdictImpeachment, rungs)
	}
	want := []string{RungStaticAssertsNoPath, RungCodeIdentity, RungLabel, RungServiceScope, RungCaptureFidelity}
	if len(rungs) != len(want) {
		t.Fatalf("want %d rungs, got %d: %+v", len(want), len(rungs), rungs)
	}
	for i, r := range rungs {
		if r.Name != want[i] {
			t.Errorf("rung %d = %q, want %q", i, r.Name, want[i])
		}
		if !r.Passed {
			t.Errorf("rung %q did not pass: %s", r.Name, r.Evidence)
		}
	}
}

// TestLadderRungInIsolation drives each rung to fail ALONE (every other rung still
// clears) and asserts the verdict is that rung's specific downgrade. Because the
// verdict is the FIRST failing rung, an isolated failure proves the rung→downgrade
// mapping and the ordering at once.
func TestLadderRungInIsolation(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*Witness, *Provenance, *string)
		failRung string
		want     string
	}{
		{"static-abstains", func(w *Witness, _ *Provenance, _ *string) { w.Claim.Reachability = "blind" }, RungStaticAssertsNoPath, DowngradeNotAContradiction},
		{"identity-unestablished", func(_ *Witness, p *Provenance, _ *string) { p.TraceIdentity = "" }, RungCodeIdentity, DowngradeVersionSkew},
		{"identity-skew", func(_ *Witness, p *Provenance, _ *string) { p.TraceIdentity = "other" }, RungCodeIdentity, DowngradeVersionSkew},
		{"label-opaque", func(w *Witness, _ *Provenance, _ *string) { w.Effect = "mystery effect" }, RungLabel, DowngradeLabelMismatch},
		{"foreign-service", func(w *Witness, _ *Provenance, _ *string) { w.Observed.Service = "other" }, RungServiceScope, DowngradeCrossService},
		{"synthetic-capture", func(_ *Witness, p *Provenance, _ *string) { p.Capture = CaptureSynthetic }, RungCaptureFidelity, DowngradeCaptureUntrusted},
		{"capture-unrecorded", func(_ *Witness, p *Provenance, _ *string) { p.Capture = "" }, RungCaptureFidelity, DowngradeCaptureUntrusted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, ix, svc, prov := passingCase()
			c.mutate(&w, &prov, &svc)
			rungs, verdict := classify(w, ix, svc, prov)
			if verdict != c.want {
				t.Fatalf("verdict = %q, want %q; ladder = %+v", verdict, c.want, rungs)
			}
			// The whole ladder is recorded regardless of where it failed (§4).
			if len(rungs) != 5 {
				t.Fatalf("ladder not recorded whole: %d rungs", len(rungs))
			}
			// Exactly the targeted rung failed; the rest cleared (it is isolated).
			for _, r := range rungs {
				if r.Name == c.failRung && r.Passed {
					t.Errorf("targeted rung %q unexpectedly passed", r.Name)
				}
				if r.Name != c.failRung && !r.Passed {
					t.Errorf("collateral failure on rung %q: %s", r.Name, r.Evidence)
				}
			}
		})
	}
}

// TestLadderRecordedWholeOnEarlyFailure proves the ladder does not short-circuit:
// a failure at rung 2 (code-identity) still evaluates and records rungs 3–5, so a
// partial rule-out ("only code-identity is missing — label/scope/capture all
// clear") is actionable from the recorded ladder alone (§4).
func TestLadderRecordedWholeOnEarlyFailure(t *testing.T) {
	w, ix, svc, prov := passingCase()
	prov.TraceIdentity = "" // fail rung 2
	rungs, verdict := classify(w, ix, svc, prov)
	if verdict != DowngradeVersionSkew {
		t.Fatalf("verdict = %q, want %q", verdict, DowngradeVersionSkew)
	}
	byName := map[string]Rung{}
	for _, r := range rungs {
		byName[r.Name] = r
	}
	if byName[RungCodeIdentity].Passed {
		t.Error("rung 2 should have failed")
	}
	for _, later := range []string{RungLabel, RungServiceScope, RungCaptureFidelity} {
		r, ok := byName[later]
		if !ok {
			t.Errorf("downstream rung %q was not recorded — ladder short-circuited", later)
			continue
		}
		if !r.Passed {
			t.Errorf("downstream rung %q recorded as failed; want cleared: %s", later, r.Evidence)
		}
	}
}

// TestLadderDeterministic pins that classify is a pure function: the same inputs
// yield byte-identical ladders (the cross-cutting P-determinism requirement, §10).
func TestLadderDeterministic(t *testing.T) {
	w, ix, svc, prov := passingCase()
	a, va := classify(w, ix, svc, prov)
	b, vb := classify(w, ix, svc, prov)
	if va != vb || !reflect.DeepEqual(a, b) {
		t.Errorf("classify not deterministic: (%q,%+v) != (%q,%+v)", va, a, vb, b)
	}
}

// TestLadderDistributionHealthy is the Phase-1 go/no-go measurement (§10): on a
// corpus with no caller-supplied provenance — the real state today (§14-D) — the
// rung distribution is downgrade-DOMINATED and impeachments are zero, the healthy
// signal. A ladder that minted impeachments here would be "too credulous". The
// promotion path (a genuine candidate reaching IMPEACHMENT) is measured under
// supplied provenance in TestImpeachsvcLadderPromotesWithProvenance.
func TestLadderDistributionHealthy(t *testing.T) {
	ix := loadIndex(t, loansvcGraph)
	// A synthetic corpus of unmodeled DB writes across distinct tables: every one
	// is a genuine ABSENT candidate, but with no provenance each must downgrade.
	mk := func(table string) *ir.CanonicalTrace {
		return &ir.CanonicalTrace{Flow: "probe", Service: "loansvc", Root: &ir.CanonicalSpan{
			Op: "HTTP POST /x", Kind: ir.KindServer,
			Children: []ir.ChildGroup{{Members: []*ir.CanonicalSpan{
				{Op: "DB postgres DELETE " + table, Kind: ir.KindClient},
			}}},
		}}
	}
	traces := []*ir.CanonicalTrace{mk("ledger_a"), mk("ledger_b"), mk("ledger_c")}
	r := Audit("loansvc", ix, traces, Provenance{})

	if len(r.Candidates) == 0 {
		t.Fatal("expected candidates to classify; got none")
	}
	dist := map[string]int{}
	for _, w := range r.Candidates {
		dist[w.Verdict]++
	}
	if dist[VerdictImpeachment] != 0 {
		t.Errorf("too credulous: %d IMPEACHMENT(s) with no provenance, want 0 (dist=%v)", dist[VerdictImpeachment], dist)
	}
	if dist[DowngradeVersionSkew] != len(r.Candidates) {
		t.Errorf("want every candidate downgraded to VERSION-SKEW, got dist=%v", dist)
	}
	t.Logf("rung distribution over %d candidate(s): %v (healthy = downgrade-dominated)", len(r.Candidates), dist)
}
