package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/canonjson"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// stampedGraphFile writes a copy of a real golden graph carrying the given
// identity stamp to a temp file, returning its path — a schema-valid stamped
// fixture for the gate-command wiring tests.
func stampedGraphFile(t *testing.T, golden, stamp string) string {
	t.Helper()
	g, err := graph.LoadFile(golden)
	if err != nil {
		t.Fatalf("load %s: %v", golden, err)
	}
	g.Stamp = stamp
	b, err := canonjson.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "stamped.graph.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestVerifyGateStampRequireEnv pins the un-forgettable enforcement: with
// GROUNDWORK_REQUIRE_STAMP set, a gate command run WITHOUT --expect is itself an
// error, so CI cannot silently gate a graph whose identity was never bound to the
// code under review. Without the env it stays opt-in, like triage/mcp.
func TestVerifyGateStampRequireEnv(t *testing.T) {
	g := &graph.Graph{Stamp: "sha", Nodes: []graph.Node{}}

	if err := verifyGateStamp(g, "", false); err != nil {
		t.Errorf("default (no env, no --expect) must not check: %v", err)
	}
	t.Setenv("GROUNDWORK_REQUIRE_STAMP", "1")
	if err := verifyGateStamp(g, "", false); err == nil {
		t.Error("GROUNDWORK_REQUIRE_STAMP set + no --expect must be an error")
	}
	if err := verifyGateStamp(g, "sha", true); err != nil {
		t.Errorf("env set + matching --expect must pass: %v", err)
	}
}

// TestGateCommandsBindStamp proves --expect is actually wired into the
// verdict-bearing gate commands: a mismatched stamp fails operationally (not as a
// verdict, and before any verdict is computed) on each of fitness/review/verify/
// verify-artifact, and a matching stamp does not trip the stamp check.
func TestGateCommandsBindStamp(t *testing.T) {
	const (
		policy   = "../../testdata/groundwork/policies/layeredsvc.json"
		base     = "../../testdata/groundwork/goldens/layeredsvc.graph.json"
		artifact = "../../testdata/groundwork/goldens/layeredsvc.branch-skip.artifact.json"
		golden   = "../../testdata/groundwork/goldens/layeredsvc.branch-skip.graph.json"
	)
	branch := stampedGraphFile(t, golden, "sha-good")

	mismatch := map[string][]string{
		"fitness":         {"fitness", policy, branch, "--expect", "sha-bad"},
		"review":          {"review", policy, base, branch, "--expect", "sha-bad"},
		"verify":          {"verify", policy, base, branch, "--expect", "sha-bad"},
		"verify-artifact": {"verify-artifact", artifact, policy, base, branch, "--expect", "sha-bad"},
	}
	for name, args := range mismatch {
		err := run(args)
		if err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Errorf("%s: run = %v, want a stamp-mismatch error", name, err)
		}
		var v verdictError
		if errors.As(err, &v) {
			t.Errorf("%s: stamp mismatch should be operational, not a verdict", name)
		}
	}

	// A matching stamp must not be rejected on identity (the verdict itself may
	// still BLOCK — that is a different, expected outcome).
	if err := run([]string{"fitness", policy, branch, "--expect", "sha-good"}); err != nil && strings.Contains(err.Error(), "does not match") {
		t.Errorf("matching stamp wrongly rejected: %v", err)
	}
}
