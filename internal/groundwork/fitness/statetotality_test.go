package fitness

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// TestFactStateSwitchesAreTotal is the mechanical guard on the defect class the
// three live gates in this package share: every verdict-bearing evaluator folds
// a CLOSED typed fact state (facts.ReachState, facts.PassThroughState,
// facts.ConcurrentState, facts.ObligationState) into findings, and in every one
// of them at least one arm is deliberately SILENT — the proof pole emits nothing
// because absence is the desired state. A state the switch has not been taught
// therefore does not merely go unhandled: it produces no finding at all and the
// rule reports as a clean hold. That is a silent pass in a live gate, the worst
// outcome this codebase can have (tenet 4), and it is latent by construction —
// it appears the day someone adds a state to facts, in a file they never opened.
//
// A reviewer cannot see the hole by reading either side alone, and no input can
// reach it today, so there is nothing to unit-test. Make the invariant
// self-checking instead (tenet 5): every switch in this package whose case
// expressions name a `facts.` constant must carry a `default` clause. The rule
// is syntactic on purpose — it needs no type checker and no fixture, and it
// fires on the NEXT such switch someone writes, not just today's four.
//
// The claims judge closes the same hole on its side (claims.go's four switches
// on the same states each end in an "evaluator returned an unknown state" ERROR);
// this test does not reach across packages to assert that.
func TestFactStateSwitchesAreTotal(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, entry := range entries {
		path := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok || !switchesOnFactsConstant(sw) {
				return true
			}
			checked++
			for _, stmt := range sw.Body.List {
				if clause, ok := stmt.(*ast.CaseClause); ok && clause.List == nil {
					return true // a default clause: this switch is total
				}
			}
			t.Errorf("%s:%d: switch over facts.* constants has no default clause — "+
				"an unmodelled state would emit no finding and the rule would read as a clean hold",
				path, fset.Position(sw.Pos()).Line)
			return true
		})
	}

	// Negative guard on the guard: if the detector stops recognizing these
	// switches it would pass vacuously, which is the same silent-green failure it
	// exists to prevent. Four are known today (must_not_reach, must_pass_through,
	// no_concurrent_reach, obligation) plus the evalReach compatibility wrapper
	// and blindDescription's presentation fold.
	if checked < 4 {
		t.Fatalf("detector matched only %d facts.* switches; it is no longer finding the live gates", checked)
	}
}

// switchesOnFactsConstant reports whether any case expression of sw is a
// `facts.X` qualified identifier — the syntactic signature of a fold over a
// closed typed fact state.
func switchesOnFactsConstant(sw *ast.SwitchStmt) bool {
	for _, stmt := range sw.Body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok {
			continue
		}
		for _, expr := range clause.List {
			sel, ok := expr.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "facts" {
				return true
			}
		}
	}
	return false
}

// TestUnrecognizedStateFinding pins the disclosure the three total switches emit
// when they meet a state they have not been taught: an abstention, never a
// silent hold, and never a proof. require_proof escalates it exactly as it
// escalates the inert-rule and unbindable-target disclosures beside it.
func TestUnrecognizedStateFinding(t *testing.T) {
	tests := []struct {
		name         string
		requireProof bool
		want         Finding
	}{
		{
			name: "advisory by default",
			want: Finding{
				Rule:     "must_not_reach",
				Severity: Caution,
				Summary:  "no-write: reach state 7 is not understood by this groundwork — upgrade or investigate",
			},
		},
		{
			name:         "require_proof escalates",
			requireProof: true,
			want: Finding{
				Rule:     "must_not_reach",
				Severity: Violation,
				Summary:  "no-write: reach state 7 is not understood by this groundwork — require_proof is set and an unrecognized state cannot be proven",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unrecognizedStateFinding("must_not_reach", "no-write", "reach state 7", tt.requireProof)
			if got != tt.want {
				t.Fatalf("unrecognizedStateFinding()\ngot:  %#v\nwant: %#v", got, tt.want)
			}
			// The disclosure names no edge: there is no witness to point at, and a
			// fabricated From/To would read as evidence the graph never produced.
			if got.From != "" || got.To != "" || got.Detail != "" {
				t.Errorf("disclosure carries fabricated evidence: %#v", got)
			}
		})
	}
}
