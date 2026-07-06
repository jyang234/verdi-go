package graph_test

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/static/roots"
)

// TestEntrypointKindsParity pins that groundwork's consumer-side entrypoint-kind
// vocabulary (graph.EntrypointKinds) set-equals the producer's emitted kinds. The
// GROUND TRUTH is graphio.go's entrypoints[] emission switch, which emits from
// roots.KindHTTP/KindConsumer/KindCallback/KindWorker; if the producer starts emitting
// a new kind, this test fails until the consumer vocabulary learns it — the parity
// CLAUDE.md requires be named in a comment AND guarded by a test (one source of truth).
// It imports internal/static/roots ONLY in test scope: the graph package stays on its
// own side of the trust boundary in production, but the test may reach across the
// module to pin the two ends of the contract against each other.
func TestEntrypointKindsParity(t *testing.T) {
	producer := []roots.Kind{roots.KindHTTP, roots.KindConsumer, roots.KindCallback, roots.KindWorker}
	want := map[string]bool{}
	for _, k := range producer {
		want[string(k)] = true
	}
	got := map[string]bool{}
	for _, k := range graph.EntrypointKinds {
		got[k] = true
	}
	if len(got) != len(graph.EntrypointKinds) {
		t.Fatalf("EntrypointKinds has duplicate entries: %v", graph.EntrypointKinds)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("EntrypointKinds is missing producer kind %q (graphio.go emits it)", k)
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("EntrypointKinds has %q, which graphio.go's emission switch does not emit", k)
		}
	}
}
