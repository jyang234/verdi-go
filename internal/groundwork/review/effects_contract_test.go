package review

import (
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/fitness"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// TestBoundaryLabelContract pins the coupling between flowmap's rendered boundary
// labels ("boundary:<system> <op> <target>") and groundwork's effect classifiers
// (fitness.IsWrite and classifyContract). Both parse that string format; if
// flowmap changes it, this test — run over real flowmap goldens — fails loudly
// instead of letting writes go uncounted or contract changes silently vanish.
func TestBoundaryLabelContract(t *testing.T) {
	type want struct {
		write   bool
		surface string // "" means excluded from the inter-service contract (a DB effect)
	}
	// Exact labels emitted by the loansvc fixture (real flowmap output), covering
	// the whole vocabulary: db read/write, bus publish/consume, outbound GET/POST.
	cases := map[string]want{
		"boundary:db SELECT loans":               {false, ""},
		"boundary:db INSERT ledger":              {true, ""},
		"boundary:db UPDATE loans":               {true, ""},
		"boundary:bus PUBLISH loan.approved":     {true, "publish"},
		"boundary:bus PUBLISH <dynamic>":         {true, "publish"},
		"boundary:bus CONSUME payment.settled":   {false, "consume"},
		"boundary:credit-bureau GET /score/{id}": {false, "outbound"},
		"boundary:payment-gw POST /charge/{id}":  {true, "outbound"},
	}
	for to, w := range cases {
		e := graph.Edge{To: to, Boundary: "outbound-sync"}
		if got := fitness.IsWrite(e); got != w.write {
			t.Errorf("IsWrite(%q) = %v, want %v", to, got, w.write)
		}
		surface, _, ok := classifyContract(e)
		switch {
		case w.surface == "" && ok:
			t.Errorf("classifyContract(%q) ok=true, want excluded (a DB effect)", to)
		case w.surface != "" && (!ok || surface != w.surface):
			t.Errorf("classifyContract(%q) = %q (ok=%v), want %q", to, surface, ok, w.surface)
		}
	}

	// Guard: every boundary label across the real goldens must match the
	// "<system> <op> ..." shape the classifiers assume — no silent fall-through.
	for _, name := range []string{"loansvc.graph.json", "layeredsvc.graph.json", "blindsvc.graph.json"} {
		g := loadGraph(t, name)
		for _, e := range g.Edges {
			if !e.IsBoundary() {
				continue
			}
			if fields := strings.Fields(strings.TrimPrefix(e.To, "boundary:")); len(fields) < 2 {
				t.Errorf("%s: boundary label %q has <2 fields; the classifiers assume '<system> <op> ...'", name, e.To)
			}
		}
	}
}
