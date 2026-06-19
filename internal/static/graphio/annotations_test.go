package graphio_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/config"
	"github.com/jyang234/golang-code-graph/internal/static/graphio"
)

// TestAnnotationsBindAndAreContextOnly is the end-to-end Phase-1 guarantee over a
// real fixture: a config annotation binds to a detected blind spot and rides the
// graph, AND adding it changes NOTHING the analysis concluded — the blind-spot
// manifest and the frontier are byte-identical with and without it. An annotation
// explains a gap; it never closes one (CLAUDE.md tenet 3).
func TestAnnotationsBindAndAreContextOnly(t *testing.T) {
	res := analyzeFixture(t)
	base, err := graphio.Build(res, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(base.BlindSpots) == 0 {
		t.Fatal("fixture has no blind spots to annotate; the test would be vacuous")
	}
	target := base.BlindSpots[0]

	res.Config.Static.Annotations = []config.Annotation{{
		Site: target.Site, Kind: string(target.Kind),
		Note: "behind this seam: an outbound HTTPS POST", By: "tester@example.com",
	}}
	withAnn, err := graphio.Build(res, "")
	if err != nil {
		t.Fatalf("build with annotation: %v", err)
	}

	// Bound to the target.
	if len(withAnn.Annotations) != 1 {
		t.Fatalf("want exactly one bound annotation, got %d: %+v", len(withAnn.Annotations), withAnn.Annotations)
	}
	if a := withAnn.Annotations[0]; a.Site != target.Site || a.Kind != string(target.Kind) || a.By != "tester@example.com" {
		t.Errorf("annotation bound wrong: %+v (target %s/%s)", a, target.Kind, target.Site)
	}

	// Context-only: the manifest and frontier are unchanged.
	if !reflect.DeepEqual(base.BlindSpots, withAnn.BlindSpots) {
		t.Error("annotation changed the blind-spot manifest; it must be disclosure-only")
	}
	if !reflect.DeepEqual(base.Frontier, withAnn.Frontier) {
		t.Error("annotation changed the frontier; it must be disclosure-only")
	}

	// The note and an annotated marker surface in the render.
	m := withAnn.Mermaid(graphio.MermaidOptions{})
	if !strings.Contains(m, "behind this seam: an outbound HTTPS POST") {
		t.Error("annotation note missing from mermaid header")
	}
	if !strings.Contains(m, "🗒") {
		t.Error("annotated blind-spot marker (🗒) missing from mermaid")
	}
}

// TestAnnotationOrphanFailsBuild pins that an annotation at a site with no blind
// spot fails the build — drift is refused, not silently dropped, even though the
// channel is disclosure-only.
func TestAnnotationOrphanFailsBuild(t *testing.T) {
	res := analyzeFixture(t)
	res.Config.Static.Annotations = []config.Annotation{{
		Site: "example.com/loansvc/internal/nope.Vanished", Note: "stale",
	}}
	if _, err := graphio.Build(res, ""); err == nil {
		t.Fatal("an annotation at a non-existent blind-spot site must fail the build")
	}
}
