package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestAssertLoansvcAcceptance is the phase acceptance: the committed
// seven-claim file over the pinned loansvc graph exercises all four outcome
// classes at once — PASS, FAIL, AMBIGUOUS, and UNRESOLVED — and the report is
// asserted byte-for-byte, with the exit CLASS pinned to a verdictError (a FAIL
// takes precedence over the errored claims, exit 1).
//
// NOTE: the companion prototype spec (verdi-go-fr-prototype-spec-2026-07-03) is
// a consumer-side artifact not carried in this repo, so its byte-exact §1.6
// output cannot be reproduced here. This fixture is the in-repo equivalent —
// authored against the same field-validated semantics and the same loansvc pin
// (40 nodes / 49 unique edges / 3 Score candidates), exercising every outcome
// class the acceptance calls for.
func TestAssertLoansvcAcceptance(t *testing.T) {
	const want = `FAIL edge store.Loans).SelectLoan -> handler.App).Create: no edge between the resolved endpoints
ERROR node .Score: ambiguous ".Score" (3 candidates): (*example.com/loansvc/internal/client.Bureau).Score, (*example.com/loansvc/internal/scoring.Remote).Score, (*example.com/loansvc/internal/scoring.Stub).Score
ERROR node handler.App).Delete: unresolved "handler.App).Delete"
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
