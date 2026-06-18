// Package soundness holds adversarial probes of the static reachability checker:
// real fixtures, through the real analyze→graphio→graph→fitness pipeline, that test
// whether a must_not_reach verdict launders an unknown into a confident provenAbsent
// (the prime directive's worst failure) instead of reaching it (over-approximation) or
// abstaining on a disclosed blind spot.
package soundness

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/fitness"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/callgraph"
	"github.com/jyang234/golang-code-graph/internal/static/graphio"
)

// indexFixture runs the REAL toolchain on a fixture: analyze → graphio.Build → marshal
// → groundwork graph.Load → Index, exactly as flowmap+groundwork do (default algo: rta).
func indexFixture(t *testing.T, name string) *graph.Index {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", name)
	res, err := analyze.Analyze(dir, callgraph.Options{Algo: callgraph.AlgoRTA})
	if err != nil {
		t.Fatalf("analyze %s: %v", name, err)
	}
	gg, err := graphio.Build(res, "")
	if err != nil {
		t.Fatalf("build %s: %v", name, err)
	}
	b, err := gg.Marshal()
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	p := filepath.Join(t.TempDir(), name+".graph.json")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatalf("write graph: %v", err)
	}
	g, err := graph.LoadFile(p)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return graph.NewIndex(g)
}

func nodeWithSuffix(ix *graph.Index, suffix string) string {
	for _, fqn := range ix.Nodes() {
		if strings.HasSuffix(fqn, suffix) {
			return fqn
		}
	}
	return ""
}

func mustNotReachFindings(ix *graph.Index, from string) []fitness.Finding {
	p := &policy.Policy{Service: "dispatchsvc", MustNotReach: []policy.ReachRule{
		{Name: "no-delete-ledger", From: []string{from}, To: []string{"boundary:db DELETE ledger"}},
	}}
	var out []fitness.Finding
	for _, f := range fitness.Check(p, ix).Findings {
		if f.Rule == "must_not_reach" {
			out = append(out, f)
		}
	}
	return out
}

// TestInitRegisteredDispatchIsSilentlyUnsound is probe #2 of the static-checker audit.
//
// FINDING (soundness gap, demonstrated): the call-graph root set (roots.Discover) is
// {main, recognized HTTP/bus registrars} — it EXCLUDES init(). A func value whose
// address is taken only in init() (the idiomatic place to register) is never explored,
// so an unrecognized registry populated in init() loses its handlers and their effects
// from the graph entirely, with NO blind spot disclosed. A must_not_reach over that
// path then reads a clean provenAbsent — laundering an unknown into a concrete claim,
// the exact "say so explicitly, never launder" tenet (CLAUDE.md core tenet 3) that the
// comparable seams (reflect/unsafe/cgo/linkname/high-fan-out) all honor.
//
// This test CHARACTERIZES the current (unsound) behavior so the gap is mechanically
// visible; the control proves the effect is genuinely in the graph and catchable from
// the route that statically reaches it. When the gap is closed — by adding init() to
// the roots (over-approximation then connects the hop, errs safe) or by flagging a
// zero-resolution dynamic call as a blind spot (abstain) — the Handle assertion flips
// from "no finding" to a violation/caution, and this test should be updated to expect it.
func TestInitRegisteredDispatchIsSilentlyUnsound(t *testing.T) {
	ix := indexFixture(t, "dispatchsvc")

	handle := nodeWithSuffix(ix, "dispatchsvc.Handle")
	report := nodeWithSuffix(ix, "dispatchsvc.Report")
	if handle == "" || report == "" {
		t.Fatalf("fixture entrypoints missing: Handle=%q Report=%q", handle, report)
	}

	// The forbidden effect IS in the graph (via the directly-reachable decoy), so the
	// unbindable-target safeguard does not fire — any pass here is a real provenAbsent.
	if nodeWithSuffix(ix, "dispatchsvc.audit") == "" {
		t.Fatal("decoy audit() missing from graph; the DELETE label would not bind")
	}
	// purge (the init-registered handler) and its DELETE are absent: the seam is lost.
	if p := nodeWithSuffix(ix, "dispatchsvc.purge"); p != "" {
		t.Errorf("purge unexpectedly in graph (%s) — the init-registry seam may now be modeled; "+
			"update this probe to assert the sound (caught) behavior", p)
	}
	// ...and the loss is UNDISCLOSED: no blind spot anywhere on Handle's reach.
	cone := append([]string{handle}, ix.Reachable(handle)...)
	for _, fn := range cone {
		if bs := ix.BlindSpotsAt(fn); len(bs) > 0 {
			t.Errorf("unexpected blind spot on Handle's cone at %s: %+v — the seam may now be disclosed", fn, bs)
		}
	}

	// THE GAP: must_not_reach over the init-registry path produces NO finding — a
	// confident provenAbsent for an effect runtime-reachable via Handle→Dispatch→purge.
	if got := mustNotReachFindings(ix, handle); len(got) != 0 {
		t.Logf("Handle now produces findings (gap may be closed): %+v", got)
		t.Errorf("expected the CURRENT (unsound) silent pass from Handle; got %d findings — "+
			"if the gap was fixed, flip this assertion to require a finding", len(got))
	}

	// CONTROL: the SAME effect, reached directly, IS caught — proving it is in the graph
	// and the silence above is the dispatch seam, not an absent target.
	ctrl := mustNotReachFindings(ix, report)
	if len(ctrl) != 1 || ctrl[0].Severity != fitness.Violation {
		t.Fatalf("control failed: must_not_reach(Report, DELETE) = %+v, want one Violation", ctrl)
	}
}
