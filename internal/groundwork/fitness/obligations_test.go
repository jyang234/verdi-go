package fitness

import (
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// The obligsvc golden carries one obligation verdict per shape (path-obligations
// plan §7); groundwork judges them: VIOLATED → violation, CANT-PROVE and
// UNMATCHED → caution, SATISFIED → nothing.
func TestObligationsJudged(t *testing.T) {
	g := loadGraph(t, "obligsvc.graph.json")
	res := Check(&policy.Policy{Service: "obligsvc", Version: 1}, graph.NewIndex(g))

	v := res.Violations()
	if len(v) != 2 {
		t.Fatalf("want 2 obligation violations (Transfer leak, DisburseRacy), got %v", v)
	}
	froms := map[string]bool{}
	for _, f := range v {
		if f.Rule != "obligation" {
			t.Errorf("violation rule = %q, want obligation", f.Rule)
		}
		froms[f.From] = true
	}
	if !froms["example.com/obligsvc/internal/app.Transfer"] || !froms["example.com/obligsvc/internal/app.DisburseRacy"] {
		t.Errorf("violations name %v, want Transfer and DisburseRacy", froms)
	}

	c := res.Cautions()
	if len(c) != 2 {
		t.Fatalf("want 2 obligation cautions (CANT-PROVE, UNMATCHED), got %v", c)
	}
	var inert, cantProve bool
	for _, f := range c {
		if strings.Contains(f.Summary, "inert guardrail") {
			inert = true
		}
		if strings.Contains(f.Summary, "cannot prove") {
			cantProve = true
		}
	}
	if !inert || !cantProve {
		t.Errorf("cautions = %v, want one inert-rule and one cannot-prove", c)
	}
}

// SATISFIED is the proof: it must produce no finding at all.
func TestObligationsSatisfiedIsSilent(t *testing.T) {
	g := loadGraph(t, "obligsvc.graph.json")
	res := Check(&policy.Policy{Service: "obligsvc", Version: 1}, graph.NewIndex(g))
	for _, f := range res.Findings {
		switch f.From {
		case "example.com/obligsvc/internal/app.TransferDefer",
			"example.com/obligsvc/internal/app.Disburse":
			t.Errorf("SATISFIED site produced a finding: %v", f)
		}
	}
}
