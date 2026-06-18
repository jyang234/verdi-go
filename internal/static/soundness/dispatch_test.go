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

// TestInitRegisteredDispatchIsCaught is probe #2 of the static-checker audit, asserting
// the seam is now soundly handled end-to-end by BOTH fixes.
//
// FINDING (the original gap): the call-graph root set (roots.Discover) was {main,
// recognized HTTP/bus registrars} — it EXCLUDED init(). A func value whose address is
// taken only in init() (the idiomatic place to register) was never explored, so an
// unrecognized registry populated in init() lost its handlers and their effects from
// the graph. must_not_reach then read a clean provenAbsent — laundering an unknown into
// a concrete claim, the exact "say so explicitly, never launder" tenet (CLAUDE.md core
// tenet 3) the comparable seams (reflect/unsafe/cgo/linkname/high-fan-out) all honor.
//
// THE FIXES, layered:
//   - B (blindspots.UnresolvedCall) discloses a func-value call that resolves to no
//     callee, so an UNRECOVERABLE seam abstains (noPathFound) instead of proving
//     absence — the backstop, exercised in isolation by the reflect probe and the
//     blindspots unit test.
//   - A (rooting init() in roots.Discover) makes init's address-takes visible, so RTA
//     RESOLVES the registry hop to purge: the edge and its DELETE re-enter the graph.
//     Recovery dominates disclosure — there is no longer a blind spot here because the
//     call is no longer unresolved.
//
// So for this fixture the verdict is the strongest one: Handle→Dispatch→purge→DELETE is
// a Violation, exactly as the directly-reached control. (If A regresses, the call goes
// unresolved again and B catches it as a Caution — fail-closed either way; this test
// would then need to assert that weaker-but-safe outcome.)
func TestInitRegisteredDispatchIsCaught(t *testing.T) {
	ix := indexFixture(t, "dispatchsvc")

	handle := nodeWithSuffix(ix, "dispatchsvc.Handle")
	report := nodeWithSuffix(ix, "dispatchsvc.Report")
	if handle == "" || report == "" {
		t.Fatalf("fixture entrypoints missing: Handle=%q Report=%q", handle, report)
	}

	// RECOVERY (A): purge — reachable only via the init-registered registry hop — is now
	// in the graph, so the edge that was silently lost is modeled.
	if nodeWithSuffix(ix, "dispatchsvc.purge") == "" {
		t.Fatal("purge missing from graph; rooting init() did not recover the registry hop")
	}
	// The hop resolves now, so there is no UnresolvedCall to disclose on Handle's cone:
	// recovery (a real edge) dominates the abstain backstop.
	cone := append([]string{handle}, ix.Reachable(handle)...)
	if coneHasBlindSpot(ix, cone) {
		for _, fn := range cone {
			if bs := ix.BlindSpotsAt(fn); len(bs) > 0 {
				t.Errorf("unexpected blind spot after recovery at %s: %+v", fn, bs)
			}
		}
	}

	// THE CATCH: must_not_reach over the init-registry path is now a Violation — the
	// effect is reachable from Handle through the recovered hop.
	got := mustNotReachFindings(ix, handle)
	if len(got) != 1 || got[0].Severity != fitness.Violation {
		t.Fatalf("Handle must_not_reach = %+v, want one Violation (Handle reaches DELETE via the recovered registry hop)", got)
	}

	// CONTROL: the SAME effect reached directly is also a Violation — the recovered path
	// produces the identical verdict as the statically-obvious one.
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
