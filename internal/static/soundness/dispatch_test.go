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

// TestInitRegisteredDispatchAbstainsAtTheSeam is probe #2 of the static-checker audit.
//
// FINDING (soundness gap, demonstrated then closed by the UnresolvedCall blind spot):
// the call-graph root set (roots.Discover) is {main, recognized HTTP/bus registrars} —
// it EXCLUDES init(). A func value whose address is taken only in init() (the
// idiomatic place to register) is never explored, so an unrecognized registry
// populated in init() loses its handlers and their effects from the graph. Before the
// fix this surfaced as a clean provenAbsent — laundering an unknown into a concrete
// claim, the exact "say so explicitly, never launder" tenet (CLAUDE.md core tenet 3)
// that the comparable seams (reflect/unsafe/cgo/linkname/high-fan-out) all honor.
//
// THE FIX (B): blindspots.Detect now flags a func-value call that resolves to NO
// callee (UnresolvedCall) — the zero-resolution mirror of HighFanOut, invisible to the
// edge-walking detector because a site with no callee has no out-edge. The Dispatch
// hop is now disclosed blind, so must_not_reach abstains (noPathFound → Caution)
// instead of proving absence. The control proves the effect is genuinely in the graph
// and caught when reached directly.
//
// NOTE the residual: B abstains but does not RECOVER the edge — purge and its DELETE
// are still absent (the registry handler is unreachable until init() is rooted). Probe
// #2's companion fix (A, rooting init()) reconnects the hop and flips the verdict from
// Caution to Violation; when A lands, the Handle assertion below is updated to require
// a Violation and the purge-absent / blind-spot assertions are removed.
func TestInitRegisteredDispatchAbstainsAtTheSeam(t *testing.T) {
	ix := indexFixture(t, "dispatchsvc")

	handle := nodeWithSuffix(ix, "dispatchsvc.Handle")
	report := nodeWithSuffix(ix, "dispatchsvc.Report")
	if handle == "" || report == "" {
		t.Fatalf("fixture entrypoints missing: Handle=%q Report=%q", handle, report)
	}

	// The forbidden effect IS in the graph (via the directly-reachable decoy), so the
	// unbindable-target safeguard does not fire — the abstain below is a real
	// noPathFound at the seam, not a vacuous "target binds nothing".
	if nodeWithSuffix(ix, "dispatchsvc.audit") == "" {
		t.Fatal("decoy audit() missing from graph; the DELETE label would not bind")
	}
	// purge (the init-registered handler) and its DELETE are still absent: B discloses
	// the seam, it does not reconnect it. Recovery is A's job (rooting init()).
	if p := nodeWithSuffix(ix, "dispatchsvc.purge"); p != "" {
		t.Errorf("purge unexpectedly in graph (%s) — the init-registry edge may now be "+
			"recovered (A landed); update this probe to assert the Violation behavior", p)
	}

	// THE DISCLOSURE: the Dispatch hop is flagged UnresolvedCall, so the loss is no
	// longer silent — there is a blind spot on Handle's reachable cone.
	cone := append([]string{handle}, ix.Reachable(handle)...)
	if !coneHasBlindSpot(ix, cone) {
		t.Error("expected an UnresolvedCall blind spot on Handle's cone; the seam is silent again")
	}

	// THE ABSTAIN: must_not_reach over the init-registry path now yields a Caution
	// (noPathFound), not a silent provenAbsent — the checker says it cannot prove
	// absence past the unresolved func-value hop.
	got := mustNotReachFindings(ix, handle)
	if len(got) != 1 || got[0].Severity != fitness.Caution {
		t.Fatalf("Handle must_not_reach = %+v, want one Caution (noPathFound at the seam)", got)
	}

	// CONTROL: the SAME effect, reached directly, is a Violation — proving it is in the
	// graph and the abstain above is the dispatch seam, not an absent target.
	ctrl := mustNotReachFindings(ix, report)
	if len(ctrl) != 1 || ctrl[0].Severity != fitness.Violation {
		t.Fatalf("control failed: must_not_reach(Report, DELETE) = %+v, want one Violation", ctrl)
	}
}

// coneHasBlindSpot reports whether any node in the cone carries a disclosed blind spot.
func coneHasBlindSpot(ix *graph.Index, cone []string) bool {
	for _, fn := range cone {
		if len(ix.BlindSpotsAt(fn)) > 0 {
			return true
		}
	}
	return false
}
