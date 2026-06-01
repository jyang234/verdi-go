package capture

import (
	"testing"
	"time"

	"github.com/jyang234/golang-code-graph/ir"
)

func t0(ms int) time.Time { return time.Unix(0, 0).Add(time.Duration(ms) * time.Millisecond) }

func TestScopeFiltersByCorrelation(t *testing.T) {
	spans := []Span{
		{ID: "root", Kind: ir.KindServer, Attrs: map[string]string{CorrelationKey: "mine"}, Start: t0(0), End: t0(10)},
		{ID: "child", ParentID: "root", Kind: ir.KindClient, Attrs: map[string]string{CorrelationKey: "mine"}, Start: t0(1), End: t0(2)},
		{ID: "other", Kind: ir.KindServer, Attrs: map[string]string{CorrelationKey: "theirs"}, Start: t0(0), End: t0(5)},
	}
	scoped, root := Scope(spans, "mine")
	if len(scoped) != 2 {
		t.Fatalf("scoped %d spans, want 2 (the foreign run dropped)", len(scoped))
	}
	if root == nil || root.ID != "root" {
		t.Fatalf("root = %+v, want the server span 'root'", root)
	}
}

func TestScopeEmptyRunIDKeepsAll(t *testing.T) {
	spans := []Span{
		{ID: "root", Start: t0(0), End: t0(2)},
		{ID: "child", ParentID: "root", Start: t0(1), End: t0(2)},
	}
	scoped, root := Scope(spans, "")
	if len(scoped) != 2 || root == nil || root.ID != "root" {
		t.Fatalf("in-process fast path: scoped=%d root=%v", len(scoped), root)
	}
}

func TestScopeRejectsMultipleRoots(t *testing.T) {
	spans := []Span{
		{ID: "a", Start: t0(0), End: t0(1)},
		{ID: "b", Start: t0(0), End: t0(1)},
	}
	if _, root := Scope(spans, ""); root != nil {
		t.Fatal("two parentless spans should yield a nil (ambiguous) root")
	}
}

func TestOrderSequential(t *testing.T) {
	a := Span{ID: "a", Start: t0(0), End: t0(5)}
	b := Span{ID: "b", Start: t0(6), End: t0(9)}
	if got := Order(a, b, time.Millisecond); got != Before {
		t.Errorf("Order(a,b) = %v, want Before", got)
	}
	if got := Order(b, a, time.Millisecond); got != After {
		t.Errorf("Order(b,a) = %v, want After", got)
	}
}

func TestOrderConcurrentOnOverlap(t *testing.T) {
	a := Span{ID: "a", Start: t0(0), End: t0(8)}
	b := Span{ID: "b", Start: t0(2), End: t0(4)}
	if got := Order(a, b, time.Millisecond); got != Concurrent {
		t.Errorf("overlapping intervals = %v, want Concurrent", got)
	}
}

// TestOrderTimingStable is the spec's fast-vs-slow check (Phase 4): two errgroup
// legs that start together stay a concurrent pair whether one leg is fast or
// slow, because both intervals still overlap.
func TestOrderTimingStable(t *testing.T) {
	// Both legs dispatched at ~t0; leg b finishes fast in one run, slow in another.
	fastA := Span{ID: "a", Start: t0(0), End: t0(10)}
	fastB := Span{ID: "b", Start: t0(0), End: t0(2)}
	slowB := Span{ID: "b", Start: t0(0), End: t0(50)}
	if Order(fastA, fastB, time.Millisecond) != Concurrent {
		t.Error("fast leg should be concurrent")
	}
	if Order(fastA, slowB, time.Millisecond) != Concurrent {
		t.Error("slow leg should still be concurrent — classification must be timing-stable")
	}
}

func TestOrderGuardBand(t *testing.T) {
	// A tiny back-to-back gap below the guard band is treated as concurrent, not
	// spuriously sequential.
	a := Span{ID: "a", Start: t0(0), End: t0(5)}
	b := Span{ID: "b", Start: t0(5).Add(100 * time.Microsecond), End: t0(9)}
	if got := Order(a, b, time.Millisecond); got != Concurrent {
		t.Errorf("sub-guard-band gap = %v, want Concurrent", got)
	}
}
