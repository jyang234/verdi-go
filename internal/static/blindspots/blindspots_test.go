package blindspots_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
	"github.com/jyang234/golang-code-graph/internal/static/features"
	"github.com/jyang234/golang-code-graph/internal/static/statictest"
)

func TestDetectNonConstantPublish(t *testing.T) {
	res, err := statictest.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	bs := blindspots.Detect(res, features.NewHintSet(res.Config))

	var nonConst int
	for _, b := range bs {
		if b.Kind == blindspots.NonConstantBoundaryArg {
			nonConst++
			if !strings.Contains(b.Site, "notify") {
				t.Errorf("unexpected NonConstantBoundaryArg site %q", b.Site)
			}
		}
	}
	// Exactly one: the notify publish. The three constant publishes and the two
	// constant outbound calls must NOT be false positives.
	if nonConst != 1 {
		t.Errorf("NonConstantBoundaryArg count = %d, want 1: %+v", nonConst, bs)
	}
}

func TestDetectDeterministic(t *testing.T) {
	res, err := statictest.Analyze()
	if err != nil {
		t.Fatal(err)
	}
	hints := features.NewHintSet(res.Config)
	a := blindspots.Detect(res, hints)
	b := blindspots.Detect(res, hints)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("blind-spot detection not deterministic:\n%+v\n%+v", a, b)
	}
}
