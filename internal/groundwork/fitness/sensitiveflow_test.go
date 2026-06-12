package fitness

import (
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// The sensitive-flow rule pack (correctness plan, CX-4) is vocabulary over
// the existing families — these tests lock that the documented pack shapes
// bind and verdict as usage.md claims, with writeJSON standing in for a log
// sink on the layeredsvc graph. No new engine: a regression here is a
// regression in must_not_reach itself.
func TestSensitiveFlowPack(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	sink := "example.com/layeredsvc/internal/handler.writeJSON"

	// The violating direction: a PII-handling method reaches the sink — a
	// lead with a named pair, not a proven flow. The from is the explicit
	// receiver-qualified FQN, the form the pack documents (a bare package
	// prefix would not bind methods).
	p := layeredPolicy()
	p.MustNotReach = []policy.ReachRule{{
		Name: "pii-never-logged",
		From: []string{hGetUser},
		To:   []string{sink},
	}}
	res := Check(p, graph.NewIndex(g))
	if v := res.Violations(); len(v) == 0 || v[0].Rule != "must_not_reach" {
		t.Fatalf("PII-handling code reaching the sink must fire, got %v", res.Findings)
	}

	// The strong direction: store code provably never reaches the sink —
	// proven absence is the silent pass.
	p = layeredPolicy()
	p.MustNotReach = []policy.ReachRule{{
		Name: "pii-never-logged",
		From: []string{sSelectUser},
		To:   []string{sink},
	}}
	if res := Check(p, graph.NewIndex(g)); len(res.Findings) != 0 {
		t.Fatalf("proven absence must be silent, got %v", res.Findings)
	}

	// A from that binds nothing (the renamed-away PII package) is a
	// disclosed inert rule, never a silent proven-absent.
	p = layeredPolicy()
	p.MustNotReach = []policy.ReachRule{{
		Name: "pii-never-logged",
		From: []string{"example.com/layeredsvc/internal/pii"},
		To:   []string{sink},
	}}
	res = Check(p, graph.NewIndex(g))
	c := res.Cautions()
	if len(c) != 1 || !strings.Contains(c[0].Summary, "inert") {
		t.Fatalf("an unbound from must be a disclosed inert-rule caution, got %v", res.Findings)
	}
}
