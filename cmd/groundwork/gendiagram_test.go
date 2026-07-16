package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateDiagramGolden rebases the committed .diagram.mmd golden instead of
// asserting against it — the standard Go golden-file convention, matching
// internal/static/graphio's mermaid golden harness. Regenerate with:
//
//	go test ./cmd/groundwork -run TestGenDiagramLoansvcGolden -update
var updateDiagramGolden = flag.Bool("update", false, "rewrite the .diagram.mmd gen-diagram golden")

const (
	loansvcGraphPair   = "loansvc=../../testdata/groundwork/goldens/loansvc.graph.json"
	loansvcCoreDiagram = "../../testdata/groundwork/diagrams/loansvc-core.diagram.json"
	loansvcCoreGolden  = "../../testdata/groundwork/goldens/loansvc-core.diagram.mmd"
)

// TestGenDiagramLoansvcGolden byte-pins the full loansvc-core diagram against a
// committed golden. The manifest exercises a subgraph (origination), an overlay db
// node + labeled dashed overlay edge (the planned audit ledger), and TWO
// boundary:* endpoints drawn as core nodes (db SELECT/UPDATE loans) — so the golden
// is a real, user-facing exemplar. The graph carries no `tool` (goldens strip it),
// so the provenance stamp is `unstamped`.
func TestGenDiagramLoansvcGolden(t *testing.T) {
	var got string
	err := captureStdoutErr(t, func() error {
		return run([]string{"gen-diagram", loansvcCoreDiagram, loansvcGraphPair})
	}, &got)
	if err != nil {
		t.Fatalf("gen-diagram: %v", err)
	}
	if *updateDiagramGolden {
		if err := os.WriteFile(loansvcCoreGolden, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", loansvcCoreGolden)
		return
	}
	want, err := os.ReadFile(loansvcCoreGolden)
	if err != nil {
		t.Fatalf("missing golden %s (run `go test ./cmd/groundwork -run TestGenDiagramLoansvcGolden -update`): %v", loansvcCoreGolden, err)
	}
	if string(want) != got {
		t.Errorf("loansvc-core diagram is stale (run -update to rebase):\n got:\n%s\nwant:\n%s", got, string(want))
	}
}

// TestGenDiagramCheckModes pins --check both ways it selects a committed copy: a
// mermaid FENCE inside a Markdown doc (block-selected by the manifest id) and a
// RAW .mmd file (whole-file compare). Both must pass silently; a drifted copy is a
// verdictError (exit 1) naming the first differing line.
func TestGenDiagramCheckModes(t *testing.T) {
	golden, err := os.ReadFile(loansvcCoreGolden)
	if err != nil {
		t.Fatalf("read golden (run the golden test with -update first): %v", err)
	}

	// (a) Fenced inside a Markdown doc.
	md := "# loansvc core\n\nSome prose.\n\n```mermaid\n" + string(golden) + "```\n\nMore prose.\n"
	mdPath := filepath.Join(t.TempDir(), "doc.md")
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	var out string
	if err := captureStdoutErr(t, func() error {
		return run([]string{"gen-diagram", "--check", mdPath, loansvcCoreDiagram, loansvcGraphPair})
	}, &out); err != nil {
		t.Errorf("--check against a fenced doc should pass: %v", err)
	}
	if out != "" {
		t.Errorf("a clean --check must print nothing, got:\n%s", out)
	}

	// (b) Raw .mmd file (no fences → whole-file compare) — the committed golden itself.
	if err := run([]string{"gen-diagram", "--check", loansvcCoreGolden, loansvcCoreDiagram, loansvcGraphPair}); err != nil {
		t.Errorf("--check against the raw golden should pass: %v", err)
	}

	// (c) Drift: mutate one line inside the fenced copy → verdictError naming the line.
	drift := strings.Replace(md, `App.Status`, `App.StatusRENAMED`, 1)
	if drift == md {
		t.Fatal("drift setup failed: label not found in golden")
	}
	driftPath := filepath.Join(t.TempDir(), "drift.md")
	if err := os.WriteFile(driftPath, []byte(drift), 0o644); err != nil {
		t.Fatal(err)
	}
	err = run([]string{"gen-diagram", "--check", driftPath, loansvcCoreDiagram, loansvcGraphPair})
	var v verdictError
	if !errors.As(err, &v) {
		t.Fatalf("--check drift = %v (%T), want a verdictError (exit 1)", err, err)
	}
	if !strings.Contains(err.Error(), "line ") {
		t.Errorf("--check drift message should name the differing line: %q", err.Error())
	}
}

// TestGenDiagramExitClasses pins the verdict/operational split: an overlay guard
// violation (the graph contradicts the manifest's judgment) is a verdictError
// (exit 1), while an unresolved fn is a plain operational error (exit 2).
func TestGenDiagramExitClasses(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// Overlay guard: Create→Evaluate is a real loansvc edge, so dash-drawing it is a
	// verdict (judgment may not restate a computed fact).
	guard := write("guard.json", `{"id":"GUARD","direction":"LR",
	  "nodes":[
	    {"id":"CR","graph":"loansvc","fn":"handler.App).Create","label":"Create"},
	    {"id":"EV","graph":"loansvc","fn":"origination.Evaluator).Evaluate","label":"Evaluate"}],
	  "overlays":{"edges":[{"from":"CR","to":"EV","label":"FIX"}]}}`)
	err := run([]string{"gen-diagram", guard, loansvcGraphPair})
	var v verdictError
	if !errors.As(err, &v) {
		t.Errorf("overlay-guard violation = %v (%T), want a verdictError (exit 1)", err, err)
	}

	// Unresolved fn: a plain operational error (exit 2), NOT a verdict.
	unres := write("unres.json", `{"id":"UNRES","direction":"LR",
	  "nodes":[{"id":"X","graph":"loansvc","fn":"nonexistent.Thing","label":"X"}]}`)
	err = run([]string{"gen-diagram", unres, loansvcGraphPair})
	if err == nil || errors.As(err, &v) {
		t.Errorf("unresolved fn = %v (%T), want a non-verdict operational error (exit 2)", err, err)
	}
}

// TestGenDiagramTiers pins the tiers:true variant: every resolved node's label
// carries the graph's tier as ` (tN)`, and a boundary pseudo-node genuinely gets
// none (honest omission).
func TestGenDiagramTiers(t *testing.T) {
	var out string
	err := captureStdoutErr(t, func() error {
		return run([]string{"gen-diagram", "../../testdata/groundwork/diagrams/loansvc-tiers.diagram.json", loansvcGraphPair})
	}, &out)
	if err != nil {
		t.Fatalf("gen-diagram tiers: %v", err)
	}
	// SelectLoan is tier 2, Create is tier 1 in the golden graph.
	for _, want := range []string{`SL["Loans.SelectLoan (t2)"]`, `CR["App.Create (t1)"]`, `DBSEL["db SELECT loans"]`} {
		if !strings.Contains(out, want) {
			t.Errorf("tiers output missing %q:\n%s", want, out)
		}
	}
	// The boundary endpoint must carry NO tier suffix.
	if strings.Contains(out, "db SELECT loans (t") {
		t.Errorf("boundary pseudo-node must not get a tier suffix:\n%s", out)
	}
}

// TestGenDiagramPairParsing pins the CLI-level pair grammar (the one place the map
// hides duplicates from the diagram package): a duplicate logical name and a
// missing `=` are both operational errors (exit 2), never a verdict.
func TestGenDiagramPairParsing(t *testing.T) {
	var v verdictError
	// Duplicate logical name.
	err := run([]string{"gen-diagram", loansvcCoreDiagram, loansvcGraphPair, loansvcGraphPair})
	if err == nil || errors.As(err, &v) {
		t.Errorf("duplicate logical name = %v (%T), want a non-verdict error", err, err)
	}
	// A bare token with no `=`.
	err = run([]string{"gen-diagram", loansvcCoreDiagram, "loansvc"})
	if err == nil || errors.As(err, &v) {
		t.Errorf("bad pair = %v (%T), want a non-verdict error", err, err)
	}
}

// captureStdoutErr runs fn (which returns run()'s error) with stdout captured into
// *dst, returning fn's error. It threads the error out of the captureStdout
// closure so a test can assert BOTH the printed bytes and the exit-class error.
func captureStdoutErr(t *testing.T, fn func() error, dst *string) error {
	t.Helper()
	var err error
	*dst = captureStdout(t, func() { err = fn() })
	return err
}
