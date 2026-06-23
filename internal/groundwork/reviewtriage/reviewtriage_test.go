package reviewtriage

import (
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// TestBuildPartitionsVouchedAndFocus pins the core contract: a changed function with
// a fully-resolved effect surface is VOUCHED (and its complete evidence is rendered),
// while a changed function that touches a disclosed blind spot is FOCUS (with the
// reason rendered). Both functions changed (signature moved), so the partition — not
// the change detection — is what is under test.
func TestBuildPartitionsVouchedAndFocus(t *testing.T) {
	base := &graph.Graph{
		Nodes: []graph.Node{{FQN: "svc.Clean", Sig: "old"}, {FQN: "svc.Blind", Sig: "old"}},
	}
	branch := &graph.Graph{
		Nodes: []graph.Node{{FQN: "svc.Clean", Sig: "new"}, {FQN: "svc.Blind", Sig: "new"}},
		Edges: []graph.Edge{
			{From: "svc.Clean", To: "boundary:db SELECT users", Boundary: "outbound-sync"},
		},
		Entrypoints: []graph.Entrypoint{
			{Kind: "http", Name: "GET /clean", Fn: "svc.Clean"},
			{Kind: "http", Name: "GET /blind", Fn: "svc.Blind"},
		},
		BlindSpots: []graph.BlindSpot{{Kind: "reflect", Site: "svc.Blind", Detail: "reflective call"}},
	}

	rep := Build(base, branch)

	if got := len(rep.Vouched) + len(rep.Focus); got != 2 {
		t.Fatalf("want 2 changed functions partitioned, got %d (%+v / %+v)", got, rep.Vouched, rep.Focus)
	}
	if len(rep.Vouched) != 1 || rep.Vouched[0].FQN != "svc.Clean" {
		t.Errorf("svc.Clean should be VOUCHED (resolved effect, no blind spot): %+v", rep.Vouched)
	}
	if len(rep.Focus) != 1 || rep.Focus[0].FQN != "svc.Blind" {
		t.Errorf("svc.Blind should be FOCUS (reflection blind spot): %+v", rep.Focus)
	}
	if len(rep.Focus) == 1 && len(rep.Focus[0].Reasons) == 0 {
		t.Error("a FOCUS change must carry at least one reason the tool cannot vouch")
	}

	md := rep.RenderMarkdown()
	// The vouched change must expose its complete, checkable effect surface as evidence.
	if !strings.Contains(md, "db SELECT users") || !strings.Contains(md, "COMPLETE boundary-effect surface") {
		t.Errorf("vouched evidence (the resolved effect surface) not rendered:\n%s", md)
	}
	// The focus change must name the blind spot, and its evidence must read as a FLOOR.
	if !strings.Contains(md, "reflection") || !strings.Contains(md, "FLOOR") {
		t.Errorf("focus reason/floor not rendered:\n%s", md)
	}
}

// TestSeverityTrivialStaysVouched pins #2: a change whose only forward blind spot is
// producer-tagged "trivial" (a benign seam) is NOT pulled into focus — it stays
// vouched, but the benign seam is still disclosed so completeness is never over-claimed.
func TestSeverityTrivialStaysVouched(t *testing.T) {
	base := &graph.Graph{Nodes: []graph.Node{{FQN: "svc.Benign", Sig: "old"}}}
	branch := &graph.Graph{
		Nodes:       []graph.Node{{FQN: "svc.Benign", Sig: "new"}},
		BlindSpots:  []graph.BlindSpot{{Kind: "ConcurrentDispatch", Site: "svc.Benign", Detail: "cancel func", Severity: "trivial"}},
		Entrypoints: []graph.Entrypoint{{Kind: "http", Name: "GET /b", Fn: "svc.Benign"}},
	}
	rep := Build(base, branch)
	if len(rep.Focus) != 0 {
		t.Fatalf("a trivial-severity seam must not trigger focus, got %+v", rep.Focus)
	}
	if len(rep.Vouched) != 1 || len(rep.Vouched[0].BenignSeams) != 1 {
		t.Fatalf("the benign seam must be vouched AND disclosed, got %+v", rep.Vouched)
	}
	if !strings.Contains(rep.RenderMarkdown(), "producer-tagged trivial") {
		t.Errorf("the set-aside benign seam was not disclosed:\n%s", rep.RenderMarkdown())
	}
}

// TestFocusRankedByConsequence pins #4: focus items are ordered most-consequential
// first — a critical-tier change ahead of a low-tier one. Both have a forward blind
// spot, so the partition is fixed and only the ORDER is under test.
func TestFocusRankedByConsequence(t *testing.T) {
	base := &graph.Graph{Nodes: []graph.Node{{FQN: "svc.Low", Sig: "o"}, {FQN: "svc.Crit", Sig: "o"}}}
	branch := &graph.Graph{
		Nodes: []graph.Node{{FQN: "svc.Low", Sig: "n", Tier: 3}, {FQN: "svc.Crit", Sig: "n", Tier: 1}},
		Edges: []graph.Edge{
			{From: "svc.Crit", To: "boundary:db INSERT ledger", Boundary: "outbound-sync"},
			{From: "svc.Low", To: "boundary:db SELECT users", Boundary: "outbound-sync"},
		},
		BlindSpots: []graph.BlindSpot{
			{Kind: "reflect", Site: "svc.Crit", Detail: "c"},
			{Kind: "reflect", Site: "svc.Low", Detail: "l"},
		},
	}
	rep := Build(base, branch)
	if len(rep.Focus) != 2 {
		t.Fatalf("want 2 focus changes, got %+v", rep.Focus)
	}
	if rep.Focus[0].FQN != "svc.Crit" {
		t.Errorf("focus order = [%s, %s], want the critical-tier change first", rep.Focus[0].FQN, rep.Focus[1].FQN)
	}
}

// TestCallerBlindSpotDoesNotForceFocus pins #1 at the package boundary: a clean change
// that is merely CALLED by reflective code stays vouched — the blind spot is in the
// caller (reverse reach), not in what the change can do.
func TestCallerBlindSpotDoesNotForceFocus(t *testing.T) {
	base := &graph.Graph{Nodes: []graph.Node{{FQN: "svc.Clean", Sig: "o"}}}
	branch := &graph.Graph{
		Nodes: []graph.Node{{FQN: "svc.RefCaller", Sig: "o"}, {FQN: "svc.Clean", Sig: "n"}},
		Edges: []graph.Edge{
			{From: "svc.RefCaller", To: "svc.Clean"},
			{From: "svc.Clean", To: "boundary:db SELECT users", Boundary: "outbound-sync"},
		},
		BlindSpots:  []graph.BlindSpot{{Kind: "reflect", Site: "svc.RefCaller", Detail: "upstream"}},
		Entrypoints: []graph.Entrypoint{{Kind: "http", Name: "GET /x", Fn: "svc.RefCaller"}},
	}
	rep := Build(base, branch)
	// svc.Clean changed; svc.RefCaller is unchanged (same sig, no new edge in base→branch? it is new here, so it is "changed" too).
	var clean *ChangedFn
	for i := range rep.Vouched {
		if rep.Vouched[i].FQN == "svc.Clean" {
			clean = &rep.Vouched[i]
		}
	}
	if clean == nil {
		t.Fatalf("svc.Clean must be VOUCHED despite a reflective caller; focus=%+v vouched=%+v", rep.Focus, rep.Vouched)
	}
}

// TestRenderMermaidZonesAndSafety pins the diagram: it declares both zone classes,
// colors a focus change with its blind-seam node, a vouched change green, shares an
// effect node, and entity-escapes the angle brackets a <dynamic> effect carries (an
// unescaped "<" would break the Mermaid parser).
func TestRenderMermaidZonesAndSafety(t *testing.T) {
	base := &graph.Graph{Nodes: []graph.Node{{FQN: "svc.Clean", Sig: "o"}, {FQN: "svc.Dyn", Sig: "o"}}}
	branch := &graph.Graph{
		Nodes: []graph.Node{{FQN: "svc.Clean", Sig: "n", Tier: 2}, {FQN: "svc.Dyn", Sig: "n", Tier: 1}},
		Edges: []graph.Edge{
			{From: "svc.Clean", To: "boundary:db SELECT users", Boundary: "outbound-sync"},
			{From: "svc.Dyn", To: "boundary:bus PUBLISH <dynamic>", Boundary: "outbound-async"},
		},
		BlindSpots: []graph.BlindSpot{{Kind: "NonConstantBoundaryArg", Site: "svc.Dyn", Detail: "non-const topic"}},
	}
	md := Build(base, branch).RenderMermaid()

	for _, want := range []string{"flowchart LR", "classDef focus", "classDef vouched", ":::focus", ":::vouched", ":::blind"} {
		if !strings.Contains(md, want) {
			t.Errorf("mermaid missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "PUBLISH <dynamic>") || !strings.Contains(md, "&lt;dynamic&gt;") {
		t.Errorf("the <dynamic> effect label must be entity-escaped for Mermaid:\n%s", md)
	}
}

// TestBuildNoStructuralChange: identical graphs ⇒ nothing to triage, and the render
// says so explicitly rather than emitting a blank page (silence is never a silent pass).
func TestBuildNoStructuralChange(t *testing.T) {
	g := &graph.Graph{Nodes: []graph.Node{{FQN: "svc.A", Sig: "s"}}}
	rep := Build(g, g)
	if len(rep.Vouched)+len(rep.Focus) != 0 {
		t.Fatalf("identical graphs must yield no changed functions, got %+v / %+v", rep.Vouched, rep.Focus)
	}
	if !strings.Contains(rep.RenderMarkdown(), "No structural change detected") {
		t.Errorf("a no-change render must say so explicitly:\n%s", rep.RenderMarkdown())
	}
}
