package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
	"github.com/jyang234/golang-code-graph/internal/impeach"
)

// resultText extracts a tool result's text and whether it is an error result —
// the shape every MCP tool returns (toolText / toolError).
func resultText(t *testing.T, r map[string]any) (string, bool) {
	t.Helper()
	content, ok := r["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatalf("tool result has no content: %+v", r)
	}
	text, _ := content[0]["text"].(string)
	_, isErr := r["isError"]
	return text, isErr
}

// impeachServer builds an MCP server over the impeachsvc graph + its committed
// corpus, reusing the verify gate's own fixtures (stampedImpeachGraph, corpusDir,
// gatingPolicy) so the MCP lens is exercised against the EXACT inputs the gate sees.
func impeachServer(t *testing.T) *mcpServer {
	t.Helper()
	p, err := policy.Load(writePolicy(t, gatingPolicy))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	traces, err := loadCommittedCorpus(corpusDir)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	srv := &mcpServer{path: stampedImpeachGraph(t), p: p, corpus: traces, corpusDir: corpusDir}
	if err := srv.load(); err != nil { // sets ix + mtime exactly as cmdMCP does
		t.Fatalf("load server: %v", err)
	}
	srv.computeImpeach() // cmdMCP renders the audit once when inputs are final
	return srv
}

// TestMCPImpeachDisclosesWitnessNeverGates is the core soundness property of the
// lens: it reaches the SAME audit verdict the gate does (the integration-graded,
// stamp-cleared corpus impeaches the missed-root DELETE ledger), yet it can NEVER
// be read as a merge verdict — it runs at OriginLive, so it discloses the candidate
// and explicitly disclaims gating, leaving the actual gate to verify --corpus.
func TestMCPImpeachDisclosesWitnessNeverGates(t *testing.T) {
	srv := impeachServer(t)
	text, isErr := resultText(t, srv.call("impeach", toolArgs{}))
	if isErr {
		t.Fatalf("impeach returned an error result: %s", text)
	}
	// The observed effect static placed nowhere is disclosed as a candidate.
	if !strings.Contains(text, "DELETE ledger") {
		t.Errorf("impeach did not disclose the observed missed-root effect:\n%s", text)
	}
	// The integration-graded corpus over a stamp-cleared graph promotes the candidate
	// to a full IMPEACHMENT (the same classification the gate integrates) — the lens
	// does not silently downgrade what the gate would act on.
	if !strings.Contains(text, "IMPEACHMENT") {
		t.Errorf("expected an IMPEACHMENT verdict for the integration-graded corpus:\n%s", text)
	}
	// It must NEVER read as a gate. The audit-only disclaimer and the pointer to the
	// real gate are load-bearing: an agent must not treat this lens as a merge BLOCK
	// against a possibly-local graph.
	if !strings.Contains(text, "does not gate") {
		t.Errorf("impeach omitted its audit-only disclaimer (it must never read as a gate):\n%s", text)
	}
	if !strings.Contains(text, "verify --corpus") {
		t.Errorf("impeach did not point to the real merge gate (verify --corpus):\n%s", text)
	}
	// The load-once contract is disclosed so corpus freshness is legible, not silent:
	// the source dir, the golden count, and the restart-to-refresh boundary.
	if !strings.Contains(text, corpusDir) || !strings.Contains(text, "loaded at startup") || !strings.Contains(text, "restart to refresh") {
		t.Errorf("impeach did not disclose the load-once corpus contract (dir + restart-to-refresh):\n%s", text)
	}
}

// TestMCPImpeachWitnessesAnnotation pins Phase 3 end-to-end through the lens: an
// annotation at a site where the corpus witnesses a severed effect is graded
// WITNESSED and discloses the observed effect; one at a site with no corpus
// activity is UNWITNESSED. The corpus corroborates the SEAM, never the note —
// and never gates (the lens runs at OriginLive).
func TestMCPImpeachWitnessesAnnotation(t *testing.T) {
	srv := impeachServer(t)
	// Find a localized candidate from the SAME audit the lens runs (mirroring
	// computeImpeach's provenance), so the witnessed site is the real one.
	prov := impeach.Provenance{TraceIdentity: srv.ix.Stamp(), Capture: srv.capture}
	r := impeach.Audit(srv.p.Service, srv.ix, srv.corpus, prov)
	var site, effect string
	for _, w := range r.Candidates {
		if w.Severance != nil && w.Severance.Site != "" {
			site, effect = w.Severance.Site, w.Effect
			break
		}
	}
	if site == "" {
		t.Skip("fixture produced no localized candidate to witness against")
	}

	// Attach an annotation at that site (consumer-side; the producer orphan-check is
	// covered by the graphio tests). Adding it cannot change the candidates —
	// annotations are disclosure-only — so the severance site stays the same.
	g, err := graph.LoadFile(srv.path)
	if err != nil {
		t.Fatal(err)
	}
	g.Annotations = []graph.Annotation{{Site: site, Kind: "ExternalBoundaryCall", Note: "the SDK behind this seam issues the call", By: "agent:claude"}}
	srv.ix = graph.NewIndex(g)
	srv.computeImpeach()

	text, _ := resultText(t, srv.call("impeach", toolArgs{}))
	if !strings.Contains(text, "WITNESSED") || !strings.Contains(text, "the SDK behind this seam issues the call") {
		t.Errorf("annotation at a witnessed severance site should be graded WITNESSED with its note:\n%s", text)
	}
	if !strings.Contains(text, effect) {
		t.Errorf("witnessed annotation should disclose the observed effect %q:\n%s", effect, text)
	}

	// An annotation at an unrelated site has no corpus corroboration → UNWITNESSED.
	g.Annotations = []graph.Annotation{{Site: "example.com/impeachsvc/internal/nope.Ghost", Kind: "reflect", Note: "stray"}}
	srv.ix = graph.NewIndex(g)
	srv.computeImpeach()
	if text2, _ := resultText(t, srv.call("impeach", toolArgs{})); !strings.Contains(text2, "UNWITNESSED") {
		t.Errorf("annotation with no corpus effect at its site should be UNWITNESSED:\n%s", text2)
	}
}

// TestMCPImpeachDeterministic pins the lens to the determinism the whole toolchain
// rests on: the same (graph, corpus, capture, policy) yields byte-identical text.
func TestMCPImpeachDeterministic(t *testing.T) {
	srv := impeachServer(t)
	first, _ := resultText(t, srv.call("impeach", toolArgs{}))
	second, _ := resultText(t, srv.call("impeach", toolArgs{}))
	if first != second {
		t.Errorf("impeach output is not byte-identical across calls:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestMCPImpeachFailsClosedWithoutInputs verifies the lens refuses rather than
// guesses when its inputs are absent: no corpus and no policy each yield an honest
// error result, never an empty or fabricated audit.
func TestMCPImpeachFailsClosedWithoutInputs(t *testing.T) {
	full := impeachServer(t)

	noCorpus := &mcpServer{path: full.path, ix: full.ix, p: full.p} // corpus nil
	if text, isErr := resultText(t, noCorpus.call("impeach", toolArgs{})); !isErr || !strings.Contains(text, "--corpus") {
		t.Errorf("impeach without a corpus must fail closed naming --corpus, got isErr=%v: %s", isErr, text)
	}

	noPolicy := &mcpServer{path: full.path, ix: full.ix, corpus: full.corpus} // policy nil
	if text, isErr := resultText(t, noPolicy.call("impeach", toolArgs{})); !isErr || !strings.Contains(text, "--policy") {
		t.Errorf("impeach without a policy must fail closed naming --policy, got isErr=%v: %s", isErr, text)
	}
}

// TestMCPCaptureRequiresCorpus is the §12.6 fail-closed fence at the MCP boundary,
// mirroring the verify CLI: asserting a capture-fidelity grade with no corpus to
// grade is a dangling trust claim and is refused at startup, not accepted as a
// silent no-op.
func TestMCPCaptureRequiresCorpus(t *testing.T) {
	g := stampedImpeachGraph(t)
	err := run([]string{"mcp", g, "--capture", "production"})
	if err == nil || !strings.Contains(err.Error(), "requires --corpus") {
		t.Fatalf("--capture without --corpus must fail closed, got %v", err)
	}
}

// TestMCPImpeachContradictoryCaptureDowngrades is the audit-side companion to the
// gate's TestVerifyCorpusContradictoryCaptureDoesNotBlock: a caller-asserted grade
// the corpus does not carry (the corpus self-describes "integration") yields an
// unestablished grade, so the candidate is capped below IMPEACHMENT (CAPTURE-
// UNTRUSTED) — the lens cannot launder a contradicted grade into a full impeachment.
func TestMCPImpeachContradictoryCaptureDowngrades(t *testing.T) {
	srv := impeachServer(t)
	srv.capture = "production" // contradicts the corpus's self-declared "integration"
	srv.computeImpeach()       // re-render with the changed grade
	text, isErr := resultText(t, srv.call("impeach", toolArgs{}))
	if isErr {
		t.Fatalf("impeach returned an error result: %s", text)
	}
	if strings.Contains(text, "\n• IMPEACHMENT") {
		t.Errorf("a contradicted capture grade must not promote to a full IMPEACHMENT:\n%s", text)
	}
	if !strings.Contains(text, "CAPTURE-UNTRUSTED") {
		t.Errorf("expected the candidate capped at CAPTURE-UNTRUSTED on a contradicted grade:\n%s", text)
	}
}

// TestMCPCaptureRejectsUnknownGrade is the boundary fail-closed for an unrecognized
// grade: an unknown --capture value is REFUSED at startup, not silently demoted to
// CAPTURE-UNTRUSTED deep in the ladder. The single-graph error must NOT name a
// phantom "service" the user never supplied.
func TestMCPCaptureRejectsUnknownGrade(t *testing.T) {
	g := stampedImpeachGraph(t)
	err := run([]string{"mcp", g, "--corpus", corpusDir, "--capture", "staging"})
	if err == nil || !strings.Contains(err.Error(), "grade must be") {
		t.Fatalf("an unknown --capture grade must be refused at startup, got %v", err)
	}
	if strings.Contains(err.Error(), "service") {
		t.Errorf("single-graph error must not name a phantom service: %v", err)
	}
}

// TestMCPCorpusRequiresPolicy mirrors the capture guard symmetrically: a corpus with
// no policy to integrate its witnesses against is a dangling impeach configuration,
// refused at startup rather than deferred to a call-time error.
func TestMCPCorpusRequiresPolicy(t *testing.T) {
	g := stampedImpeachGraph(t)
	err := run([]string{"mcp", g, "--corpus", corpusDir})
	if err == nil || !strings.Contains(err.Error(), "requires --policy") {
		t.Fatalf("--corpus without --policy must fail closed at startup, got %v", err)
	}
}

// TestMCPImpeachReloadReaudits pins the caching invalidation: the audit body is
// computed once and refreshed on reload. A reload that swaps in a graph whose stamp
// no longer matches the (stampless, committed) corpus must re-derive the verdict —
// the candidate downgrades from IMPEACHMENT to a code-identity downgrade (VERSION-
// SKEW), proving the cached body is not stale after the graph changes underneath it.
func TestMCPImpeachReloadReaudits(t *testing.T) {
	srv := impeachServer(t)
	before, _ := resultText(t, srv.call("impeach", toolArgs{}))
	if !strings.Contains(before, "IMPEACHMENT") {
		t.Fatalf("precondition: stamped graph should impeach, got:\n%s", before)
	}
	// Rewrite the graph file at srv.path with an empty stamp, then reload. The
	// committed corpus can no longer establish code identity → VERSION-SKEW.
	g, err := graph.LoadFile(srv.path)
	if err != nil {
		t.Fatal(err)
	}
	g.Stamp = ""
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srv.path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, isErr := resultText(t, srv.call("reload", toolArgs{})); isErr {
		t.Fatal("reload failed")
	}
	after, _ := resultText(t, srv.call("impeach", toolArgs{}))
	if strings.Contains(after, "\n• IMPEACHMENT") {
		t.Errorf("after reload to a stampless graph the cached body was not re-audited (still IMPEACHMENT):\n%s", after)
	}
	if !strings.Contains(after, "VERSION-SKEW") {
		t.Errorf("expected VERSION-SKEW after the stamp no longer matches the corpus:\n%s", after)
	}
}
