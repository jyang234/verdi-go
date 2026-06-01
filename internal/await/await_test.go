package await

import (
	"strings"
	"testing"
	"time"

	"github.com/jyang234/golang-code-graph/internal/capture"
)

// fakeClock advances only when Sleep is called, so the loop runs without real
// time. emit lets a test schedule spans to appear at a given clock offset.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time        { return c.now }
func (c *fakeClock) Sleep(d time.Duration) { c.now = c.now.Add(d) }

func substrMatch(s capture.Span, marker string) bool {
	return strings.Contains(s.Name, marker)
}

func TestAwaitCompletesAfterQuietDrain(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	// The root and a publish are present immediately; a fire-and-forget audit
	// span lands 20ms in. Completion must wait for it plus the quiet period.
	snapshot := func() []capture.Span {
		base := []capture.Span{
			{ID: "root", Name: "POST /x"},
			{ID: "pub", ParentID: "root", Name: "PUBLISH loan.approved"},
		}
		if clk.now.Sub(time.Unix(0, 0)) >= 20*time.Millisecond {
			base = append(base, capture.Span{ID: "audit", ParentID: "root", Name: "DB INSERT audit"})
		}
		return base
	}
	spans, complete := Await(snapshot, Options{
		// The audit write is a declared expected exit, so completion waits for the
		// fire-and-forget goroutine span rather than finishing on the quiet period
		// before it arrives.
		Markers: []string{"PUBLISH loan.approved", "DB INSERT audit"},
		Match:   substrMatch,
		Quiet:   5 * time.Millisecond,
		Timeout: time.Second,
		Poll:    5 * time.Millisecond,
		Now:     clk.Now,
		Sleep:   clk.Sleep,
	})
	if !complete {
		t.Fatal("expected complete=true after the goroutine drained")
	}
	if len(spans) != 3 {
		t.Fatalf("expected the late audit span captured, got %d spans", len(spans))
	}
}

func TestAwaitTimesOutOnMissingMarker(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	snapshot := func() []capture.Span {
		return []capture.Span{{ID: "root", Name: "POST /x"}}
	}
	_, complete := Await(snapshot, Options{
		Markers: []string{"PUBLISH loan.approved"}, // never appears
		Match:   substrMatch,
		Quiet:   5 * time.Millisecond,
		Timeout: 50 * time.Millisecond,
		Poll:    5 * time.Millisecond,
		Now:     clk.Now,
		Sleep:   clk.Sleep,
	})
	if complete {
		t.Fatal("expected complete=false (truncated) when a marker never arrives")
	}
}

func TestAwaitWaitsForRoot(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	// Only a child span is visible until 15ms; the parentless root arrives later.
	snapshot := func() []capture.Span {
		if clk.now.Sub(time.Unix(0, 0)) < 15*time.Millisecond {
			return []capture.Span{{ID: "child", ParentID: "root", Name: "work"}}
		}
		return []capture.Span{
			{ID: "child", ParentID: "root", Name: "work"},
			{ID: "root", Name: "POST /x"},
		}
	}
	_, complete := Await(snapshot, Options{
		Quiet:   5 * time.Millisecond,
		Timeout: time.Second,
		Poll:    5 * time.Millisecond,
		Now:     clk.Now,
		Sleep:   clk.Sleep,
	})
	if !complete {
		t.Fatal("should complete once the root span ends")
	}
}

func TestAwaitMinSpansFloor(t *testing.T) {
	clk := &fakeClock{now: time.Unix(0, 0)}
	snapshot := func() []capture.Span {
		return []capture.Span{{ID: "root", Name: "POST /x"}}
	}
	_, complete := Await(snapshot, Options{
		Quiet:    5 * time.Millisecond,
		Timeout:  50 * time.Millisecond,
		Poll:     5 * time.Millisecond,
		MinSpans: 3, // floor not met → never completes
		Now:      clk.Now,
		Sleep:    clk.Sleep,
	})
	if complete {
		t.Fatal("min-span floor should keep an under-count trace from completing")
	}
}
