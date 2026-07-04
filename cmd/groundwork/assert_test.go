package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestAssertLoansvcAcceptance runs the committed seven-claim file over the
// pinned loansvc graph, exercising all four outcome classes at once — PASS,
// FAIL, AMBIGUOUS, and UNRESOLVED — with the report asserted byte-for-byte and
// the exit CLASS pinned to a verdictError (a FAIL takes precedence over the
// errored claims, exit 1). This fixture is kept alongside the byte-pinned
// spec-acceptance case (TestAssertSpecAcceptance) because it exercises cases the
// spec file does not: a `no_node` claim and a boundary-endpoint edge claim. Its
// claims carry no `id`, so it also pins the id-less fallback label (the
// endpoint-derived label) in the report shape.
func TestAssertLoansvcAcceptance(t *testing.T) {
	const want = `FAIL  store.Loans).SelectLoan -> handler.App).Create [edge] 0 edge(s)
ERROR .Score [node] AMBIGUOUS: '.Score' matches 3: (*example.com/loansvc/internal/client.Bureau).Score; (*example.com/loansvc/internal/scoring.Remote).Score; (*example.com/loansvc/internal/scoring.Stub).Score
ERROR handler.App).Delete [node] UNRESOLVED: 'handler.App).Delete' matches no node
assert: 4 passed, 1 failed, 2 errored (graph: 40 nodes, 49 unique edges)
`
	args := []string{"assert",
		"../../testdata/groundwork/goldens/loansvc.graph.json",
		"../../testdata/groundwork/claims/loansvc-acceptance.claims.json"}

	var err error
	got := captureStdout(t, func() { err = run(args) })
	if got != want {
		t.Errorf("assert report:\n got:\n%s\nwant:\n%s", got, want)
	}
	// A FAIL is a verdictError (exit 1), taking precedence over the errored claims.
	var v verdictError
	if !errors.As(err, &v) {
		t.Errorf("run(assert) = %v (%T), want a verdictError (exit 1)", err, err)
	}
}

// TestAssertSpecAcceptance is the phase's byte-for-byte pin of the companion
// spec's §1.6 acceptance case: the verbatim seven-claim file (ids L1–L7, `fn`
// aliases, `to_matching` on L3) over the pinned loansvc graph must reproduce the
// spec's expected output exactly, and the exit CLASS is a verdictError (a FAIL
// takes precedence over the errored claims, exit 1). The byte-pin is the point —
// if this diverges the schema/report-shape closure regressed, not the golden.
func TestAssertSpecAcceptance(t *testing.T) {
	const want = `FAIL  L5-deliberate-fail [edge] 0 edge(s)
ERROR L6-ambiguous-name [node] AMBIGUOUS: 'Score' matches 3: (*example.com/loansvc/internal/client.Bureau).Score; (*example.com/loansvc/internal/scoring.Remote).Score; (*example.com/loansvc/internal/scoring.Stub).Score
ERROR L7-unresolved-name [edge] UNRESOLVED: 'handler.App).Delete' matches no node/endpoint
assert: 4 passed, 1 failed, 2 errored (graph: 40 nodes, 49 unique edges)
`
	args := []string{"assert",
		"../../testdata/groundwork/goldens/loansvc.graph.json",
		"../../testdata/groundwork/claims/loansvc-spec-acceptance.claims.json"}

	var err error
	got := captureStdout(t, func() { err = run(args) })
	if got != want {
		t.Errorf("assert report:\n got:\n%s\nwant:\n%s", got, want)
	}
	// A FAIL is a verdictError (exit 1), taking precedence over the errored claims.
	var v verdictError
	if !errors.As(err, &v) {
		t.Errorf("run(assert) = %v (%T), want a verdictError (exit 1)", err, err)
	}
}

// TestAssertExitClasses pins the three-way exit split on minimal graphs: a
// clean pass (nil), a FAIL (verdictError, exit 1), and an errored-only run
// (plain operational error, exit 2 — the claim's gate could not run).
func TestAssertExitClasses(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "g.json")
	if err := os.WriteFile(graphPath, []byte(`{
	  "algo":"rta",
	  "nodes":[{"fqn":"pkg.A","sig":"func()","tier":1},{"fqn":"pkg.B","sig":"func()","tier":2}],
	  "edges":[{"from":"pkg.A","to":"pkg.B","tier":2}],
	  "blind_spots":[]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	write := func(claims string) string {
		p := filepath.Join(dir, "c.json")
		if err := os.WriteFile(p, []byte(claims), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// All pass → nil.
	if err := run([]string{"assert", graphPath, write(`{"claims":[{"kind":"edge","from":"pkg.A","to":"pkg.B"}]}`)}); err != nil {
		t.Errorf("clean pass = %v, want nil", err)
	}
	// A FAIL → verdictError (exit 1).
	err := run([]string{"assert", graphPath, write(`{"claims":[{"kind":"edge","from":"pkg.B","to":"pkg.A"}]}`)})
	var v verdictError
	if !errors.As(err, &v) {
		t.Errorf("FAIL run = %v (%T), want verdictError", err, err)
	}
	// Errored-only (no FAIL) → plain operational error (exit 2).
	err = run([]string{"assert", graphPath, write(`{"claims":[{"kind":"node","fqn":"pkg.Missing"}]}`)})
	if err == nil || errors.As(err, &v) {
		t.Errorf("errored-only run = %v (%T), want a non-verdict error", err, err)
	}
}
