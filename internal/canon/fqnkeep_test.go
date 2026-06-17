package canon

import (
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/capture"
	"github.com/jyang234/golang-code-graph/ir"
)

// taggedWaypointFlow is a flow with TWO internal-compute wrappers, each wrapping a
// distinct DB effect: one carries an L1 flowmap.fqn tag (a producer-tagged
// first-party waypoint), the other does not. Tier-3 internal compute is normally
// contracted away; the keep-rule must preserve ONLY the tagged one (plan §7).
func taggedWaypointFlow() capture.CapturedFlow {
	spans := []capture.Span{
		{ID: "root", Kind: ir.KindServer, Status: capture.StatusOK, Start: ms(0, 0), End: ms(0, 100),
			Attrs: map[string]string{"http.request.method": "POST", "http.route": "/x", capture.CorrelationKey: "run"}},

		{ID: "tagged", ParentID: "root", Kind: ir.KindInternal, Name: "taggedWaypoint", Start: ms(0, 1), End: ms(0, 20),
			Attrs: map[string]string{capture.FQNTagKey: "example.com/svc/internal/admin.(*Admin).Purge", capture.CorrelationKey: "run"}},
		{ID: "del", ParentID: "tagged", Kind: ir.KindClient, Start: ms(0, 2), End: ms(0, 10),
			Attrs: map[string]string{"db.system": "postgres", "db.statement": "DELETE FROM ledger WHERE id = 1", capture.CorrelationKey: "run"}},

		{ID: "untagged", ParentID: "root", Kind: ir.KindInternal, Name: "untaggedCompute", Start: ms(0, 30), End: ms(0, 50),
			Attrs: map[string]string{capture.CorrelationKey: "run"}},
		{ID: "ins", ParentID: "untagged", Kind: ir.KindClient, Start: ms(0, 31), End: ms(0, 40),
			Attrs: map[string]string{"db.system": "postgres", "db.statement": "INSERT INTO audit (id) VALUES (1)", capture.CorrelationKey: "run"}},
	}
	return capture.CapturedFlow{Flow: "POST /x", Service: "svc", Spans: spans, Root: &spans[0], Complete: true}
}

func allOps(s *ir.CanonicalSpan, out *[]string) {
	*out = append(*out, s.Op)
	for _, g := range s.Children {
		for _, m := range g.Members {
			allOps(m, out)
		}
	}
}

// fqnOf finds the canonical span with the given op and returns its flowmap.fqn.
func fqnOf(s *ir.CanonicalSpan, op string) (string, bool) {
	if s.Op == op {
		return s.Attrs[capture.FQNTagKey], true
	}
	for _, g := range s.Children {
		for _, m := range g.Members {
			if v, ok := fqnOf(m, op); ok {
				return v, true
			}
		}
	}
	return "", false
}

// TestFQNTaggedInternalKeptUntaggedDropped is the scoped keep-rule (plan §7): a
// tier-3 internal span is PRESERVED when it carries a flowmap.fqn waypoint tag and
// DROPPED when it does not — so the localization signal survives while ordinary
// compute is still contracted away (the keep never broadly stops pruning).
func TestFQNTaggedInternalKeptUntaggedDropped(t *testing.T) {
	tr := mustCanon(t, taggedWaypointFlow())
	var ops []string
	allOps(tr.Root, &ops)
	joined := strings.Join(ops, "\n")

	if !strings.Contains(joined, "taggedWaypoint") {
		t.Errorf("tagged internal waypoint was dropped; ops:\n%s", joined)
	}
	if strings.Contains(joined, "untaggedCompute") {
		t.Errorf("untagged internal compute was NOT dropped (keep-rule too broad); ops:\n%s", joined)
	}
	// The tag survives canon's attribute projection, verbatim — the severance walk
	// reconciles exactly this runtime spelling to an ssa node.
	if got, _ := fqnOf(tr.Root, "taggedWaypoint"); got != "example.com/svc/internal/admin.(*Admin).Purge" {
		t.Errorf("kept waypoint's flowmap.fqn = %q, want the producer's runtime FQN", got)
	}
	// Both DB effects survive (the tagged one nested under its waypoint, the
	// untagged one promoted to the root) — keeping a waypoint never loses an effect.
	if !strings.Contains(joined, "DB postgres DELETE ledger") || !strings.Contains(joined, "DB postgres INSERT audit") {
		t.Errorf("an effect was lost by the keep/contract; ops:\n%s", joined)
	}
}

// TestFQNTaggedInternalDeterministic confirms the kept waypoint does not perturb
// determinism: shuffling the input span order yields byte-identical IR (the
// snapshot-gate guarantee, extended to the newly-kept spans).
func TestFQNTaggedInternalDeterministic(t *testing.T) {
	want := marshal(t, mustCanon(t, taggedWaypointFlow()))
	for i := 0; i < 8; i++ {
		cf := taggedWaypointFlow()
		shuffleSpans(cf.Spans)
		fixupRoot(&cf)
		if got := marshal(t, mustCanon(t, cf)); string(got) != string(want) {
			t.Fatalf("kept waypoint perturbed determinism under shuffle:\n--- want ---\n%s\n--- got ---\n%s", want, got)
		}
	}
}
