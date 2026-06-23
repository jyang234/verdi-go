package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/review"
	"github.com/jyang234/golang-code-graph/internal/impeach"
)

// corpusDir is the committed impeachsvc behavioral corpus, loaded WHOLE by
// loadCommittedCorpus: the missed admin route's two DB DELETEs (ledger + audit_log)
// and its bus PUBLISH (ledger.purged), the sound POST /loan baseline, and the
// effectless reindex control. The gating tests below assert via the rule-bound
// ledger breach, not a corpus-wide candidate count.
const corpusDir = "../../testdata/fixtures/impeachsvc/flows/testdata/flows"

// stampedImpeachGraph writes a STAMPED copy of the committed impeachsvc graph to a
// temp file — mirroring CI passing the gated commit via --stamp — so the committed
// (stampless) corpus takes that identity and the code-identity rung clears.
func stampedImpeachGraph(t *testing.T) string {
	t.Helper()
	g, err := graph.LoadFile("../../internal/impeach/testdata/impeachsvc.graph.json")
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	g.Stamp = "deadbeefcafe"
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	p := filepath.Join(t.TempDir(), "impeachsvc.graph.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	return p
}

func writePolicy(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return p
}

// A require_proof rule from the DISCOVERED handler to the DELETE is SATISFIED
// statically (no discovered route reaches it — the missed root), so the static
// gate passes; the behavioral impeachment downgrades that proof to CANT-PROVE.
// Isolating the impeachment as the SOLE block cause (a from=admin rule would also
// fire statically). impeachment_gate.gate arms it.
const gatingPolicy = `{
  "service": "impeachsvc", "version": 1,
  "impeachment_gate": {"gate": true},
  "must_not_reach": [
    {"name": "routes-no-ledger-delete",
     "from": ["(*example.com/impeachsvc/internal/handler.App).Create"],
     "to": ["boundary:db DELETE ledger"],
     "require_proof": true}
  ]
}`

// TestVerifyCorpusImpeachmentBlocks is the CLI gate path end to end: groundwork
// verify --corpus over the real committed corpus (which SELF-DESCRIBES its
// "integration" grade, §12.6 — no --capture asserted) and a stamped graph, with the
// impeachment gate armed — the behaviorally-confirmed downgrade of a require_proof
// proof BLOCKS the merge (a verdictError).
func TestVerifyCorpusImpeachmentBlocks(t *testing.T) {
	g := stampedImpeachGraph(t)
	pol := writePolicy(t, gatingPolicy)
	err := run([]string{"verify", pol, g, g, "--corpus", corpusDir})
	var v verdictError
	if !errors.As(err, &v) {
		t.Fatalf("run(verify --corpus) = %v (%T), want a verdictError (BLOCK)", err, err)
	}

	// Causal isolation #1: the SAME policy+graph WITHOUT the corpus PASSES. The
	// require_proof rule is statically SATISFIED (no discovered route reaches the
	// DELETE — the missed root), so the static gate is clean; a block therefore
	// appears only when the behavioral corpus is added.
	if err := run([]string{"verify", pol, g, g}); err != nil {
		t.Fatalf("static-only verify must pass (the proof is SATISFIED without behavior), got %v", err)
	}

	// Causal isolation #2: inspect the --json gate result — the impeachment must be
	// the SOLE block cause. No static dimension (violations, scope, breaking
	// contract, blind spots, write targets) may be set, so the BLOCK is attributable
	// to ImpeachmentBreaches alone, not laundered in alongside a static failure.
	out := captureStdout(t, func() {
		_ = run([]string{"verify", pol, g, g, "--corpus", corpusDir, "--json"})
	})
	var gr review.GateResult
	if err := json.Unmarshal([]byte(out), &gr); err != nil {
		t.Fatalf("parse gate JSON: %v\n%s", err, out)
	}
	if gr.Pass {
		t.Fatal("gate JSON reports Pass=true despite the verdictError")
	}
	if len(gr.ImpeachmentBreaches) == 0 {
		t.Error("gate blocked but disclosed no impeachment breach — the cause is not the impeachment")
	}
	if n := len(gr.NewViolations) + len(gr.ScopeEscapes) + len(gr.BreakingContract) + len(gr.NewBlindSpots) + len(gr.NewWriteTargets); n != 0 {
		t.Errorf("a static block dimension is also set (%d) — impeachment is not the sole cause: %+v", n, gr)
	}
}

// TestVerifyCorpusSurfacesSeveranceLocalization pins Fix #2 end to end: the gate's
// disclosed breach must carry the severance localization (Kind/Site), and the human
// render must NAME the seam — "missed-root: static never discovered the entry …" —
// so the certificate's payload is "where static was blind", not just "a proof was
// unproved". The impeachsvc trap is a missed admin route, so Kind is missed-root.
func TestVerifyCorpusSurfacesSeveranceLocalization(t *testing.T) {
	g := stampedImpeachGraph(t)
	pol := writePolicy(t, gatingPolicy)

	out := captureStdout(t, func() {
		_ = run([]string{"verify", pol, g, g, "--corpus", corpusDir, "--json"})
	})
	var gr review.GateResult
	if err := json.Unmarshal([]byte(out), &gr); err != nil {
		t.Fatalf("parse gate JSON: %v\n%s", err, out)
	}
	if len(gr.ImpeachmentBreaches) != 1 {
		t.Fatalf("want 1 breach, got %d: %+v", len(gr.ImpeachmentBreaches), gr.ImpeachmentBreaches)
	}
	if k := gr.ImpeachmentBreaches[0].Kind; k != impeach.SeveranceMissedRoot {
		t.Errorf("breach Kind = %q, want %q (the undiscovered admin route)", k, impeach.SeveranceMissedRoot)
	}
	if gr.ImpeachmentBreaches[0].Site == "" {
		t.Error("breach Site is empty — the localized seam was not surfaced")
	}

	text := captureStdout(t, func() {
		_ = run([]string{"verify", pol, g, g, "--corpus", corpusDir})
	})
	if !strings.Contains(text, "missed-root: static never discovered the entry") {
		t.Errorf("the certificate does not name the seam (Fix #2 regressed):\n%s", text)
	}
}

// TestVerifyCorpusNonBindingIsDisclosedNotSilent pins Fix #3 end to end: an
// UNSTAMPED graph makes the committed corpus fail the code-identity rung
// (VERSION-SKEW), so it binds to nothing and yields no blockers. The gate still
// PASSES (a non-binding corpus is not a code violation) but must NOT pass silently —
// the result must disclose that the corpus provided no protection, or an operator
// will mistake an inert corpus for a behavioral all-clear (the original silent-PASS).
func TestVerifyCorpusNonBindingIsDisclosedNotSilent(t *testing.T) {
	// The committed graph on disk is stampless, so its injected trace identity is
	// empty ⇒ VERSION-SKEW for every candidate.
	g := "../../internal/impeach/testdata/impeachsvc.graph.json"
	pol := writePolicy(t, gatingPolicy)

	out := captureStdout(t, func() {
		_ = run([]string{"verify", pol, g, g, "--corpus", corpusDir, "--json"})
	})
	var gr review.GateResult
	if err := json.Unmarshal([]byte(out), &gr); err != nil {
		t.Fatalf("parse gate JSON: %v\n%s", err, out)
	}
	if !gr.Pass {
		t.Fatal("a non-binding corpus must not BLOCK (it is not a code violation)")
	}
	if len(gr.ImpeachmentBreaches) != 0 {
		t.Fatalf("a non-binding corpus must yield no blockers, got %+v", gr.ImpeachmentBreaches)
	}
	disclosed := false
	for _, c := range gr.Caveats {
		if strings.Contains(c, "did not bind to this commit") && strings.Contains(c, "VERSION-SKEW") {
			disclosed = true
		}
	}
	if !disclosed {
		t.Errorf("non-binding corpus was not disclosed (silent PASS is the bug) — caveats: %v", gr.Caveats)
	}
}

// TestVerifyCorpusContradictoryCaptureDoesNotBlock is the §12.6 fail-closed fence:
// the committed corpus self-describes "integration", so a caller asserting a
// CONTRADICTING --capture production yields an unestablished grade
// (CAPTURE-UNTRUSTED), no impeachment promotes, and the gate passes — the audit
// cannot launder a test corpus into a production-grade gating impeachment.
func TestVerifyCorpusContradictoryCaptureDoesNotBlock(t *testing.T) {
	g := stampedImpeachGraph(t)
	pol := writePolicy(t, gatingPolicy)
	if err := run([]string{"verify", pol, g, g, "--corpus", corpusDir, "--capture", "production"}); err != nil {
		t.Fatalf("a contradicting capture grade must fail closed (no block), got %v", err)
	}
}

// TestVerifyCorpusRejectsUnknownGrade is the boundary fail-closed, parity with the
// MCP server (both validate via capture.AssertableGrade): an unrecognized --capture
// grade is refused up front, never laundered into a silent CAPTURE-UNTRUSTED downgrade.
func TestVerifyCorpusRejectsUnknownGrade(t *testing.T) {
	g := stampedImpeachGraph(t)
	pol := writePolicy(t, gatingPolicy)
	err := run([]string{"verify", pol, g, g, "--corpus", corpusDir, "--capture", "staging"})
	if err == nil || !strings.Contains(err.Error(), "grade must be") {
		t.Fatalf("an unknown --capture grade must be refused, got %v", err)
	}
}

// TestVerifyCorpusObserveFirstWithoutOptIn is observe-first (§10): the SAME corpus
// and attested capture, but with the impeachment gate NOT armed, passes — the
// breach is disclosed in the report, never blocking until ratified.
func TestVerifyCorpusObserveFirstWithoutOptIn(t *testing.T) {
	g := stampedImpeachGraph(t)
	pol := writePolicy(t, `{
  "service": "impeachsvc", "version": 1,
  "must_not_reach": [
    {"name": "routes-no-ledger-delete",
     "from": ["(*example.com/impeachsvc/internal/handler.App).Create"],
     "to": ["boundary:db DELETE ledger"],
     "require_proof": true}
  ]
}`)
	if err := run([]string{"verify", pol, g, g, "--corpus", corpusDir}); err != nil {
		t.Fatalf("without the opt-in the gate must pass (observe-first), got %v", err)
	}
}
