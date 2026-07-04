package fqnres

import (
	"reflect"
	"strings"
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
	// EXACTLY at the cap (len == n) is NOT truncation — no "(+N more)" suffix. A '<=' → '<'
	// mutation would send this to the truncation branch and print a false "a; b; c (+0
	// more)"; this boundary case pins it out (every other case was strictly under/over).
	if got := CapList([]string{"a", "b", "c"}, 3); got != "a; b; c" {
		t.Errorf("CapList at cap = %q, want a; b; c (no truncation suffix)", got)
	}
	// One over the cap (len == n+1): first n joined, then "(+1 more)".
	if got := CapList([]string{"a", "b", "c", "d"}, 3); got != "a; b; c (+1 more)" {
		t.Errorf("CapList at cap+1 = %q, want a; b; c (+1 more)", got)
	}
	// Over the cap: first n joined, then a disclosed " (+N more)".
	if got := CapList([]string{"a", "b", "c", "d", "e"}, 3); got != "a; b; c (+2 more)" {
		t.Errorf("CapList over cap = %q, want a; b; c (+2 more)", got)
	}
	// Empty list: the empty string — no items, no suffix (never a bare "(+0 more)").
	if got := CapList(nil, 3); got != "" {
		t.Errorf("CapList empty = %q, want empty string", got)
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

// TestDamaged pins the split-path XOR predicate: a leading '/' XOR a trailing '/' is a
// comma-severed half-regex; both slashes (a well-formed regex) or neither (a plain name)
// is coherent. "/" is a single byte that is both prefix AND suffix, so it is NOT damaged.
func TestDamaged(t *testing.T) {
	for _, tc := range []struct {
		frag string
		want bool
	}{
		{"/a{1", true},            // leading only
		{"2}/", true},             // trailing only
		{"/a|b/", false},          // both (well-formed regex)
		{"handler.App).X", false}, // neither (plain name)
		{"", false},               // neither
		{"/", false},              // one byte is both prefix and suffix
	} {
		if got := Damaged(tc.frag); got != tc.want {
			t.Errorf("Damaged(%q) = %v, want %v", tc.frag, got, tc.want)
		}
	}
}

// TestSplitQueries pins the whole --focus query-list grammar (the FIX-1 corner outcomes):
// the non-regex comma-split, the whole-value /regex/ exemption, and the fail-closed
// ambiguity/split-damage refusals — so the one grammar behind cmd/flowmap's splitter is
// enforced here, beside the resolver it belongs to (CLAUDE.md: one source of truth).
func TestSplitQueries(t *testing.T) {
	ambiguous := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("want ambiguous error, got nil")
		}
		if !strings.Contains(err.Error(), "ambiguous") {
			t.Fatalf("want ambiguous error, got %v", err)
		}
	}
	// --- single-regex readings (must resolve as ONE name) ---
	for _, v := range []string{
		"/a{1,2}/",                    // comma in {n,m} leaves a damaged half on split
		"/a,b/",                       // comma inside the pattern
		"/db (SELECT|UPDATE), loans/", // comma inside a boundary-ish regex
		"/x[,]y/",                     // the [,] literal-comma escape hatch
	} {
		got, err := SplitQueries(v)
		if err != nil {
			t.Errorf("SplitQueries(%q) unexpected error: %v", v, err)
			continue
		}
		if !reflect.DeepEqual(got, []string{v}) {
			t.Errorf("SplitQueries(%q) = %v, want one regex [%q]", v, got, v)
		}
	}
	// --- ambiguous refusals ---
	for _, v := range []string{
		"/a|b/,/c/",    // adjacent boundaries "/,/" → list of regexes
		"/a{1,2}/,/b/", // has "/,/" (}/,/b) → ambiguous
		`/scoring\.|client\./,loansvc/internal/handler,/store\./`, // all fragments undamaged
	} {
		_, err := SplitQueries(v)
		ambiguous(t, err)
	}
	// --- non-regex comma-split ---
	got, err := SplitQueries("a, b ,c")
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("SplitQueries plain list = %v, %v; want [a b c]", got, err)
	}
	// --- split damage on a non-whole-value regex ---
	if _, err := SplitQueries("x,/a{1,2}/"); err == nil || !strings.Contains(err.Error(), "split on ','") {
		t.Errorf("SplitQueries split-damage = %v; want a split-on-',' error", err)
	}
	// --- empty value: empty slice, no error (the CLI makes empty a per-flag usage error) ---
	if got, err := SplitQueries(""); err != nil || len(got) != 0 {
		t.Errorf("SplitQueries(\"\") = %v, %v; want empty, nil", got, err)
	}
	if got, err := SplitQueries("  ,  "); err != nil || len(got) != 0 {
		t.Errorf("SplitQueries all-empty = %v, %v; want empty, nil", got, err)
	}
}
