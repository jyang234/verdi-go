package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

func TestRunSmoke(t *testing.T) {
	cases := [][]string{
		{"version"},
		{"help"},
		{"policy-check", "../../testdata/groundwork/policies/layeredsvc.json"},
		{"reach", "../../testdata/groundwork/goldens/layeredsvc.graph.json",
			"(*example.com/layeredsvc/internal/handler.Server).UpdateUser"},
		// fitness passes on both fixtures (layeredsvc cleanly, blindsvc with a
		// caution that does not fail the gate).
		{"fitness", "../../testdata/groundwork/policies/layeredsvc.json",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json"},
		// the documented invocation forms, flags trailing the positionals.
		{"exceptions", "../../testdata/groundwork/policies/layeredsvc.json",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json", "--json"},
		{"triage", "--table", "users",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json"},
		{"triage", "../../testdata/groundwork/goldens/loansvc.graph.json",
			"--fail", "--peer", "credit-bureau"},
		{"fitness", "../../testdata/groundwork/policies/blindsvc.json",
			"../../testdata/groundwork/goldens/blindsvc.graph.json"},
		// review of the good branch is STRUCTURALLY-CLEAR (exit 0).
		{"review", "../../testdata/groundwork/policies/layeredsvc.json",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json",
			"../../testdata/groundwork/goldens/layeredsvc.branch-good.graph.json"},
		// the committed skip artifact verifies authentic against its source graphs.
		{"verify-artifact", "../../testdata/groundwork/goldens/layeredsvc.branch-skip.artifact.json",
			"../../testdata/groundwork/policies/layeredsvc.json",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json",
			"../../testdata/groundwork/goldens/layeredsvc.branch-skip.graph.json"},
		// verify passes on the good branch confined to its scope.
		{"verify", "../../testdata/groundwork/policies/layeredsvc.json",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json",
			"../../testdata/groundwork/goldens/layeredsvc.branch-good.graph.json",
			"--scope", "example.com/layeredsvc/internal/handler,example.com/layeredsvc/internal/app"},
		// chains composes the cross-service surface; it is observational (exit 0).
		{"chains",
			"--service", "loansvc=../../testdata/groundwork/goldens/loansvc.graph.json",
			"--service", "obligsvc=../../testdata/groundwork/goldens/obligsvc.graph.json",
			"--policy", "../../testdata/groundwork/policies/bus-brokers.json"},
	}
	for _, args := range cases {
		if err := run(args); err != nil {
			t.Errorf("run(%v) = %v, want nil", args, err)
		}
	}
}

func TestRunErrors(t *testing.T) {
	cases := [][]string{
		{"bogus"},
		{"reach", "../../testdata/groundwork/goldens/layeredsvc.graph.json", "no.Such.Func"},
		{"reach", "/nonexistent/graph.json", "x"},
		{"policy-check", "/nonexistent/policy.json"},
		{"fitness", "/nonexistent/policy.json", "../../testdata/groundwork/goldens/layeredsvc.graph.json"},
		{"fitness", "../../testdata/groundwork/policies/layeredsvc.json", "/nonexistent/graph.json"},
		// review of the skip branch is BLOCK → non-zero exit.
		{"review", "../../testdata/groundwork/policies/layeredsvc.json",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json",
			"../../testdata/groundwork/goldens/layeredsvc.branch-skip.graph.json"},
		{"review", "p", "b"}, // wrong arg count
		{"verify-artifact", "/nonexistent/artifact.json", "p", "b", "br"},
		// triage demands exactly one symptom flag: a silently-ignored second
		// symptom would mis-scope an incident hunt.
		{"triage", "--frame", "GetUser", "--table", "users",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json"},
		// a symptom that resolves to nothing is an error, not an empty card.
		{"triage", "--table", "no_such_table",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json"},
		// verify blocks on the skip branch (new violation).
		{"verify", "../../testdata/groundwork/policies/layeredsvc.json",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json",
			"../../testdata/groundwork/goldens/layeredsvc.branch-skip.graph.json"},
		// a trailing --scope with no value is a usage error: silently dropping
		// it would run the gate wider than the caller asked for.
		{"verify", "../../testdata/groundwork/policies/layeredsvc.json",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json",
			"../../testdata/groundwork/goldens/layeredsvc.branch-good.graph.json",
			"--scope"},
		// diff reports a breaking contract change → non-zero.
		{"diff", "../../testdata/groundwork/goldens/layeredsvc.contract.json",
			"../../testdata/groundwork/goldens/layeredsvc.branch.contract.json"},
		{"diff", "/nonexistent/a.json", "/nonexistent/b.json"},
		// chains with no graph is a usage error, not an empty surface.
		{"chains"},
		{"chains", "--service", "missingequals"},
		{"chains", "/nonexistent/graph.json"},
	}
	for _, args := range cases {
		if err := run(args); err == nil {
			t.Errorf("run(%v) = nil, want error", args)
		}
	}
}

// Verdict failures and operational failures exit differently (1 vs 2) so CI
// can tell "the change failed the gate" from "the gate failed to run". The
// boundary is the error's type; main maps it to the exit code.
func TestVerdictVsOperationalErrors(t *testing.T) {
	verdicts := [][]string{
		{"fitness", "../../testdata/groundwork/policies/layeredsvc.json",
			"../../testdata/groundwork/goldens/layeredsvc.branch-skip.graph.json"},
		{"review", "../../testdata/groundwork/policies/layeredsvc.json",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json",
			"../../testdata/groundwork/goldens/layeredsvc.branch-skip.graph.json"},
		{"verify", "../../testdata/groundwork/policies/layeredsvc.json",
			"../../testdata/groundwork/goldens/layeredsvc.graph.json",
			"../../testdata/groundwork/goldens/layeredsvc.branch-skip.graph.json"},
		{"diff", "../../testdata/groundwork/goldens/layeredsvc.contract.json",
			"../../testdata/groundwork/goldens/layeredsvc.branch.contract.json"},
	}
	for _, args := range verdicts {
		err := run(args)
		var v verdictError
		if !errors.As(err, &v) {
			t.Errorf("run(%v) = %v (%T), want a verdictError", args, err, err)
		}
	}
	operational := [][]string{
		{"bogus"},
		{"fitness", "/nonexistent/policy.json", "../../testdata/groundwork/goldens/layeredsvc.graph.json"},
		{"review", "p", "b"},
	}
	for _, args := range operational {
		err := run(args)
		var v verdictError
		if err == nil || errors.As(err, &v) {
			t.Errorf("run(%v) = %v (%T), want a non-verdict error", args, err, err)
		}
	}
}

// The stamp check is opt-in at both ends: silent when not asked, loud on
// mismatch or when verification was requested of an unstamped graph.
func TestVerifyStamp(t *testing.T) {
	stamped := &graph.Graph{Stamp: "abc123", Nodes: []graph.Node{}}
	bare := &graph.Graph{Nodes: []graph.Node{}}

	if err := verifyStamp(bare, "", false); err != nil {
		t.Errorf("no --expect must check nothing: %v", err)
	}
	if err := verifyStamp(stamped, "abc123", true); err != nil {
		t.Errorf("matching stamp rejected: %v", err)
	}
	if err := verifyStamp(stamped, "def456", true); err == nil {
		t.Error("mismatched stamp accepted")
	}
	if err := verifyStamp(bare, "abc123", true); err == nil {
		t.Error("unstamped graph accepted under --expect")
	}
}

// TestRunChainsOutput: the cross-service surface labels each link proven/assumed,
// prints the declared broker block flagged UNSIGNED (no warrant given), and is
// honest about the half-open chains the current fixture fleet actually has.
func TestRunChainsOutput(t *testing.T) {
	out := captureStdout(t, func() {
		if err := run([]string{"chains",
			"--service", "loansvc=../../testdata/groundwork/goldens/loansvc.graph.json",
			"--service", "obligsvc=../../testdata/groundwork/goldens/obligsvc.graph.json",
			"--policy", "../../testdata/groundwork/policies/bus-brokers.json"}); err != nil {
			t.Fatalf("chains: %v", err)
		}
	})
	for _, want := range []string{
		"chain: loan.approved",
		"[proven] producer — loansvc",
		"[proven] producer — obligsvc",
		"audit-before-publish: VIOLATED", // a real producer-side risk surfaced on the chain
		"[assumed] broker — bus",
		"UNSIGNED",                // values declared, no human warrant yet
		"open downstream",         // loan.approved has no consumer in this fleet
		"chain: payment.settled",  // consumed by loansvc
		"commits db UPDATE loans", // the consumer's downstream effect
		"open upstream",           // payment.settled has no producer in this fleet
		"dynamically-named bus effect(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("chains output missing %q:\n%s", want, out)
		}
	}
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what it
// wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	fn()
	_ = w.Close()
	os.Stdout = orig
	return <-done
}
