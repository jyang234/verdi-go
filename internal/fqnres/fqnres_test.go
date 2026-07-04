package fqnres

import (
	"reflect"
	"testing"
)

// universe is a small, deliberately varied FQN set: pointer- and value-receiver
// methods, a plain function, a generic instance's bracketed display FQN, a
// closure ($N suffix), and a boundary pseudo-node endpoint.
var universe = []string{
	"(*example.com/loansvc/internal/handler.App).Create",
	"(*example.com/loansvc/internal/store.PostgresStore).GetMessage",
	"example.com/loansvc/internal/store.PostgresStore.GetMessage", // value-receiver twin
	"example.com/loansvc/internal/scoring.Score",
	"(*example.com/loansvc/internal/scoring.Remote).Score",
	"example.com/loansvc/internal/handler.newReadinessCheck$1",
	"example.com/loansvc/internal/util.Map[int]",
	"boundary:db QueryContext",
}

func TestPlainNormalizedSuffix(t *testing.T) {
	// Receiver punctuation is stripped from BOTH sides, so the ')' form and the
	// bare-dot form resolve to the same pointer-receiver method.
	for _, q := range []string{"handler.App).Create", "handler.App.Create"} {
		got, err := Resolve(q, universe)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", q, err)
		}
		want := []string{"(*example.com/loansvc/internal/handler.App).Create"}
		if got.Ambiguous || !reflect.DeepEqual(got.Matches, want) {
			t.Errorf("Resolve(%q) = %+v, want unique %v", q, got, want)
		}
	}
}

func TestPlainUnresolved(t *testing.T) {
	got, err := Resolve("nope.Missing", universe)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Matches) != 0 || got.Ambiguous {
		t.Errorf("Resolve unresolved = %+v, want 0 matches", got)
	}
}

func TestPlainAmbiguousSorted(t *testing.T) {
	// ".Score" suffix-matches three functions; the plain form flags AMBIGUOUS
	// and returns them sorted (deterministic candidate list).
	got, err := Resolve(".Score", universe)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"(*example.com/loansvc/internal/scoring.Remote).Score",
		"example.com/loansvc/internal/scoring.Score",
	}
	if !got.Ambiguous || !reflect.DeepEqual(got.Matches, want) {
		t.Errorf("Resolve(.Score) = %+v, want ambiguous %v", got, want)
	}
}

func TestPlainClosureAndGenericPassThroughAsBytes(t *testing.T) {
	// A closure's $1 suffix and a generic instance's [int] brackets are matched
	// as raw bytes (no bracket normalization) under the plain suffix rule.
	if got, _ := Resolve("newReadinessCheck$1", universe); len(got.Matches) != 1 {
		t.Errorf("closure suffix = %+v, want 1 match", got)
	}
	if got, _ := Resolve("util.Map[int]", universe); len(got.Matches) != 1 {
		t.Errorf("generic-instance suffix = %+v, want 1 match", got)
	}
}

func TestPlainMatchesBoundaryEndpoint(t *testing.T) {
	got, _ := Resolve("boundary:db QueryContext", universe)
	if len(got.Matches) != 1 {
		t.Errorf("boundary endpoint = %+v, want 1 match", got)
	}
}

func TestRegexSeesRawBytes(t *testing.T) {
	// The regex sees the RAW FQN including receiver punctuation, so it can anchor
	// on ')' — which the plain normalized form has stripped away.
	got, err := Resolve(`/PostgresStore\).GetMessage$/`, universe)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"(*example.com/loansvc/internal/store.PostgresStore).GetMessage"}
	if !got.IsRegex || got.Ambiguous || !reflect.DeepEqual(got.Matches, want) {
		t.Errorf("regex = %+v, want raw-byte anchored %v", got, want)
	}
}

func TestRegexMultiMatchLegal(t *testing.T) {
	// Unanchored regex over the raw bytes; multi-match is legal (never ambiguous).
	got, err := Resolve(`/\.Score$/`, universe)
	if err != nil {
		t.Fatal(err)
	}
	if got.Ambiguous || len(got.Matches) != 2 {
		t.Errorf("regex multi-match = %+v, want 2 non-ambiguous matches", got)
	}
}

func TestRegexCompileErrorIsError(t *testing.T) {
	if _, err := Resolve(`/(unclosed/`, universe); err == nil {
		t.Error("a regex compile error must be surfaced, not a silent no-match")
	}
}

// TestNotRegexEdgeForms pins that a single "/", the empty-inner "//", and the
// empty string are treated as plain, not as a (degenerate) regex. "//" is the
// load-bearing case: an empty regex matches every candidate, so if "//" were
// treated as a regex it would resolve an endpoint to the entire universe and
// pass an edge/node claim trivially — a silent false pass. As plain, "//"
// suffix-matches (almost) nothing → UNRESOLVED, fail closed.
func TestNotRegexEdgeForms(t *testing.T) {
	for _, q := range []string{"/", "//", ""} {
		if isRegex(q) {
			t.Errorf("isRegex(%q) = true, want false (plain)", q)
		}
	}
	// "//" as a claim endpoint must NOT resolve to the whole universe.
	got, err := Resolve("//", universe)
	if err != nil {
		t.Fatalf("Resolve(//): %v", err)
	}
	if len(got.Matches) == len(universe) {
		t.Errorf("Resolve(//) matched the entire universe (%d) — empty regex leaked", len(got.Matches))
	}
}

// TestReportHelpers pins the shared resolution-report formatters (QuoteSingle, CapList,
// UnresolvedDetail, AmbiguousDetail) — the byte-exact templates `groundwork assert` and
// `flowmap graph --focus` both render through, so a drift here would desync the two
// features' report shape (and TestAssertSpecAcceptance's byte-pinned output).
func TestReportHelpers(t *testing.T) {
	if got := QuoteSingle("x"); got != "'x'" {
		t.Errorf("QuoteSingle = %q, want 'x'", got)
	}
	// Under the cap: plain "; " join, no truncation marker.
	if got := CapList([]string{"a", "b", "c"}, 4); got != "a; b; c" {
		t.Errorf("CapList under cap = %q", got)
	}
	// Over the cap: first n joined, then a disclosed " (+N more)".
	if got := CapList([]string{"a", "b", "c", "d", "e"}, 3); got != "a; b; c (+2 more)" {
		t.Errorf("CapList over cap = %q, want a; b; c (+2 more)", got)
	}
	if got := UnresolvedDetail("handler.App).Delete", "node/endpoint"); got != "UNRESOLVED: 'handler.App).Delete' matches no node/endpoint" {
		t.Errorf("UnresolvedDetail = %q", got)
	}
	// AmbiguousDetail owns the candidate cap (4): a 5th candidate is truncated with "(+1 more)".
	got := AmbiguousDetail("Score", []string{"a", "b", "c", "d", "e"})
	if want := "AMBIGUOUS: 'Score' matches 5: a; b; c; d (+1 more)"; got != want {
		t.Errorf("AmbiguousDetail = %q, want %q", got, want)
	}
}
