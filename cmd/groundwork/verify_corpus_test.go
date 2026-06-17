package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// corpusDir is the committed impeachsvc behavioral corpus (the missed-root DB
// DELETE + the sound POST /loan baseline).
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
