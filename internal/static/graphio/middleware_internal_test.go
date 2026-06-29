package graphio

import (
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
	"github.com/jyang234/golang-code-graph/internal/static/reclaim"
)

// detailFor builds an UnresolvedCall Detail the way blindspots.unresolvedFuncValueCalls does,
// so the anchored match in matchesResolvedSeam is exercised against the real disclosure prose.
func detailFor(typeName string) string {
	return "a func-value call of type " + typeName + " resolved to no callee; the invoked function and its downstream edges are invisible to the static call graph"
}

// dropResolvedSeams must clear ONLY the UnresolvedCall whose disclosed type EXACTLY matches a
// resolved-empty seam — not one whose type merely has the seam's type as a substring. The
// regression: `mwchainsvc.MW` is a prefix of `mwchainsvc.MWAudit`, so an unanchored
// strings.Contains would drop the still-blind MWAudit disclosure when the empty MW loop
// cleared, laundering a real seam into a false absence proof.
func TestDropResolvedSeamsAnchoredByType(t *testing.T) {
	g := &Graph{BlindSpots: []blindspots.BlindSpot{
		{Kind: blindspots.UnresolvedCall, Site: "F", Detail: detailFor("mwchainsvc.MW")},
		{Kind: blindspots.UnresolvedCall, Site: "F", Detail: detailFor("mwchainsvc.MWAudit")},
	}}
	seams := []reclaim.MiddlewareSeam{{Site: "F", TypeName: "mwchainsvc.MW"}}

	cleared := dropResolvedSeams(g, seams)
	if cleared != 1 {
		t.Fatalf("want exactly 1 seam cleared (mwchainsvc.MW), got %d", cleared)
	}
	survived := false
	for _, b := range g.BlindSpots {
		if strings.Contains(b.Detail, "mwchainsvc.MWAudit") {
			survived = true
		}
		if strings.Contains(b.Detail, "of type mwchainsvc.MW resolved") {
			t.Errorf("the exact-type seam mwchainsvc.MW should have been cleared, but it survived")
		}
	}
	if !survived {
		t.Error("substring false-clear: the unrelated mwchainsvc.MWAudit disclosure was wrongly dropped")
	}
}

// A seam only clears a blind spot at the SAME site; a same-type UnresolvedCall at a different
// function is untouched.
func TestDropResolvedSeamsSiteScoped(t *testing.T) {
	g := &Graph{BlindSpots: []blindspots.BlindSpot{
		{Kind: blindspots.UnresolvedCall, Site: "OtherFn", Detail: detailFor("mwchainsvc.MW")},
	}}
	seams := []reclaim.MiddlewareSeam{{Site: "F", TypeName: "mwchainsvc.MW"}}

	if cleared := dropResolvedSeams(g, seams); cleared != 0 {
		t.Fatalf("a seam at site F must not clear an UnresolvedCall at OtherFn; cleared %d", cleared)
	}
}
