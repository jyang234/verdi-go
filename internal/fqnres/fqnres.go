// Package fqnres resolves a hand-authored claim label to fully-qualified
// function names (FQNs) in a flowmap graph. It is the single name resolver
// shared by `groundwork assert` (claim endpoints) and `flowmap graph --focus`
// (focus nodes), so both features answer a name identically. It is a
// stdlib-only leaf: neither cmd/groundwork nor internal/static imports the
// other, and both may import this — the same layering precedent as
// internal/boundarylabel and internal/tiermap.
//
// # Two query forms, one deliberate asymmetry
//
//   - A PLAIN string is a normalized-FQN SUFFIX. The query and each candidate
//     are normalized by stripping the receiver punctuation '(', ')' and '*'
//     from both, then matched with strings.HasSuffix. So "handler.App).Create"
//     and "handler.App.Create" both resolve to
//     "(*example.com/loansvc/internal/handler.App).Create". Plain is the
//     ERGONOMIC form — forgiving about the receiver syntax a human is unlikely
//     to type exactly — and unique-or-nothing: zero matches is UNRESOLVED,
//     two or more is AMBIGUOUS. A caller that needs a single function treats
//     both as unusable (an assert claim ERRORs on either), so over-matching
//     fails CLOSED — never a silent wrong pass (tenet 2/4).
//   - A "/regex/" string (a leading AND trailing '/', at least one byte
//     between) is an explicit Go RE2 regex, searched UNANCHORED against the RAW
//     FQN — receiver punctuation visible, so a claim can anchor on it
//     ("/PostgresStore\\).GetMessage$/"). Regex is the PRECISION form: it sees
//     every byte and multi-match is legal.
//
// The raw-vs-normalized split is the point: plain names forgive receiver
// syntax; regexes are reached for precisely when that syntax is what you want
// to pin. RE2 has no backreferences/lookahead — a claim using unsupported
// syntax is a compile error, surfaced as an error (fail closed), never a
// silent no-match.
//
// # Relationship to impact.ResolveFrame (deliberate non-unification)
//
// internal/groundwork/impact.ResolveFrame does RAW token-bounded suffix
// matching for RUNTIME STACK FRAMES: a different input grammar with a
// different forgiveness contract — it must NOT strip "(*T)" punctuation,
// because frames arrive in runtime form ("pkg.(*T).M") and go through
// frameToFQN. Claims here are hand-authored doc labels; frames are
// machine-emitted. Two genuinely distinct rules, not one rule copied — each
// package pins the distinguishing case (fqnres resolves "handler.App).Create";
// impact does not). A token-boundary tightening of this resolver is
// deliberately deferred so the shipped semantics match the field-validated
// prototype; looseness here is fail-closed anyway (over-match → AMBIGUOUS).
//
// # Shared report formatting and the query-list grammar
//
// Beyond resolving one name, this package OWNS two cross-feature conventions so
// `groundwork assert` and `flowmap graph --focus` cannot drift:
//
//   - The resolution-report formatting — QuoteSingle, CapList, UnresolvedDetail,
//     AmbiguousDetail, and the candidate cap (maxCandidates) — so an UNRESOLVED /
//     AMBIGUOUS / offender line reads byte-identically in both features (and is what
//     TestAssertSpecAcceptance byte-pins). CapList also formats the render-note and
//     offender lists, so it is the ONE list separator for every such disclosure.
//   - The query-list grammar (SplitQueries): how ONE hand-authored flag value splits
//     into the individual resolver queries it authorizes — comma-split, the whole-value
//     /regex/ exemption (a single /re/ may contain commas), and the fail-closed
//     ambiguity/split-damage refusals. Query-FORM knowledge (IsRegex, the '/'-boundary
//     rules) lives beside the resolver, so cmd/flowmap's --focus splitter calls
//     SplitQueries rather than keeping a drifting copy of the '/' arithmetic (CLAUDE.md:
//     one source of truth).
package fqnres

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Result is a resolved query. Matches is sorted (deterministic). For a PLAIN
// query a unique success has len(Matches)==1; len 0 is UNRESOLVED and len>1 is
// AMBIGUOUS (Ambiguous set true). For a REGEX query multi-match is legal and
// Ambiguous is never set. IsRegex records which form produced the result so a
// caller can apply the per-form rule (plain: unique-or-die; regex: any).
type Result struct {
	Matches   []string
	Ambiguous bool
	IsRegex   bool
}

// receiverPunct strips the receiver punctuation a hand-authored plain label is
// unlikely to reproduce exactly. Kept in sync with the package doc and the
// normalization test (TestNormalizeSuffix).
var receiverPunct = strings.NewReplacer("(", "", ")", "", "*", "")

// isRegex reports whether query is the explicit /regex/ form: a leading and
// trailing '/' with at least one byte between them. "/", "//", and "" are
// plain — len must be ≥3 so the inner pattern is non-empty (an empty regex
// matches EVERY candidate, so treating "//" as a regex would silently resolve
// an endpoint to the whole universe and pass a claim trivially; fail closed by
// falling through to the plain suffix rule instead). An FQN never starts with
// '/', so this cannot shadow a real name.
func isRegex(query string) bool {
	return len(query) >= 3 && query[0] == '/' && query[len(query)-1] == '/'
}

// IsRegex reports whether query is the explicit /regex/ form (the exported view of
// isRegex). cmd/flowmap's --focus splitter uses it to apply the SAME whole-value-regex
// exemption Resolve does — a single well-formed /regex/ is ONE focus name even when it
// contains commas — without a drifting copy of the 3-byte rule (CLAUDE.md: one source
// of truth). Kept a thin wrapper so the rule has exactly one definition.
func IsRegex(query string) bool { return isRegex(query) }

// Damaged reports whether frag is HALF of a /regex/ that a comma split in two: it
// carries a leading '/' XOR a trailing '/' — never both, never neither. It is the ONE
// split-damage predicate (the split-path XOR rule), applied by SplitQueries and, through
// it, by cmd/flowmap's --focus splitter, so there is no second copy of the '/' arithmetic
// to drift (CLAUDE.md: one source of truth). A well-formed /re/ has BOTH slashes (not
// damaged); a plain name has NEITHER (not damaged); only a comma-severed regex has exactly
// one — e.g. splitting "/a{1,2}/" on ',' yields "/a{1" and "2}/", both Damaged.
func Damaged(frag string) bool {
	return strings.HasPrefix(frag, "/") != strings.HasSuffix(frag, "/")
}

// SplitQueries splits ONE hand-authored --focus value into the individual resolver
// queries it authorizes, applying the query-list grammar in this exact order:
//
//   - Whole value is NOT a /regex/: comma-split (whitespace-trimmed, empty fragments
//     dropped); any Damaged fragment is an error (a comma severed a bare regex — never
//     let a comma inside a regex silently become two wrong plain names); else return the
//     fragments. An all-empty value returns an empty slice with no error — the CALLER
//     decides whether an empty focus set is a usage error (it is, per --focus occurrence).
//   - Whole value IS a /regex/ with NO comma: exactly one regex.
//   - Whole value IS a /regex/ WITH a comma — it could read as one regex OR a comma list,
//     so keep the single-regex reading ONLY when the list reading is incoherent:
//     (a) the value contains "/,/" (adjacent regex boundaries — reads unambiguously as
//     a list of regexes) → ambiguous error;
//     (b) else EVERY comma fragment is undamaged (each reads as its own plain/regex item
//     — a coherent list) → ambiguous error;
//     (c) else (some fragment is a Damaged half-regex, so the list reading is incoherent)
//     → the single-regex reading is the only coherent one: one regex.
//
// Ambiguity fails CLOSED (tenet 2): a value that reads as BOTH forms is refused, never
// silently merged into one unauthored pattern. The escape hatch for a literal comma inside
// a single regex is the RE2 character class [,] (e.g. "/x[,]y/"): splitting it leaves the
// Damaged fragments "/x[" and "]y/", so rule (c) keeps it as one regex.
func SplitQueries(value string) ([]string, error) {
	if !isRegex(value) {
		frags := splitCommaDropEmpty(value)
		for _, f := range frags {
			if Damaged(f) {
				return nil, fmt.Errorf("%s looks like part of a regex split on ','; pass a comma-bearing regex as its own --focus flag", f)
			}
		}
		return frags, nil
	}
	if !strings.Contains(value, ",") {
		return []string{value}, nil
	}
	// A comma-bearing whole-value regex. Refuse it as ambiguous unless the comma-list
	// reading is incoherent (a Damaged half-regex among the fragments), which is the only
	// case where the single-regex reading is the one coherent one.
	if strings.Contains(value, "/,/") {
		return nil, ambiguousQuery(value)
	}
	for _, f := range splitCommaDropEmpty(value) {
		if Damaged(f) {
			return []string{value}, nil // list reading incoherent → one regex
		}
	}
	return nil, ambiguousQuery(value)
}

// ambiguousQuery is the shared AMBIGUOUS-value error for a --focus value that reads as
// both one regex and a comma-separated list. The message names no fragile count or kind
// (a prior wording claimed "list of N regexes", which was wrong for a plain-bearing list)
// and points at both escape hatches: split the flag, or write the literal comma as [,].
func ambiguousQuery(value string) error {
	return fmt.Errorf("value %q is ambiguous: it reads as both one regex and a comma-separated list; pass each list item as its own --focus flag, or spell a literal comma inside a single regex as [,]", value)
}

// splitCommaDropEmpty comma-splits value, trimming whitespace and dropping the empty
// fragments — the same shape the CLI's own list split uses, kept here because fqnres is a
// stdlib-only leaf that cannot import the CLI.
func splitCommaDropEmpty(value string) []string {
	var out []string
	for _, s := range strings.Split(value, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Resolve matches query against universe (a slice the caller builds once;
// Resolve sorts its own output, so an unsorted universe is fine). Plain
// queries use normalized-suffix matching with unique-or-die semantics surfaced
// via Ambiguous; /regex/ queries return every raw-string match. The returned
// error covers ONLY a regex compile failure (a malformed claim) — a plain
// query never errors here, and a regex's 0/≥2 match outcomes are carried in
// Result for the caller to judge.
func Resolve(query string, universe []string) (Result, error) {
	if isRegex(query) {
		re, err := regexp.Compile(query[1 : len(query)-1])
		if err != nil {
			return Result{IsRegex: true}, fmt.Errorf("invalid regex %s: %w", query, err)
		}
		var m []string
		for _, cand := range universe {
			if re.MatchString(cand) {
				m = append(m, cand)
			}
		}
		sort.Strings(m)
		return Result{Matches: m, IsRegex: true}, nil
	}
	nq := receiverPunct.Replace(query)
	var m []string
	for _, cand := range universe {
		if strings.HasSuffix(receiverPunct.Replace(cand), nq) {
			m = append(m, cand)
		}
	}
	sort.Strings(m)
	return Result{Matches: m, Ambiguous: len(m) > 1}, nil
}

// maxCandidates caps the candidate list AmbiguousDetail prints — the ONE home of the
// resolution-report candidate cap (CLAUDE.md: one source of truth). Both `groundwork
// assert` and `flowmap graph --focus` report an ambiguous name through AmbiguousDetail,
// so the cap lives here rather than in each caller where two copies could drift.
const maxCandidates = 4

// QuoteSingle wraps a query in single quotes for a resolution-report detail — the
// report's ONE quoting convention (distinct from strconv.Quote's double quotes used
// in schema-level messages). Shared by assert and --focus so the two features quote a
// name identically (CLAUDE.md: one source of truth).
func QuoteSingle(s string) string { return "'" + s + "'" }

// CapList joins up to n items with "; " — the ONE list separator for every resolution
// report AND render-note/offender/dangling disclosure — so no list joined on a raw comma
// can be misread when an item (a generic FQN like "util.Map[K,V]", a boundary op)
// contains a comma. It discloses any truncation with " (+N more)" so a capped list never
// reads as the whole set (tenet 3). Items are expected pre-sorted (fqnres.Resolve returns
// sorted matches); CapList does not reorder. Shared by assert's offender/candidate lists,
// --focus's dangling/pinned-plumbing notes, and every other capped disclosure.
func CapList(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, "; ")
	}
	return strings.Join(items[:n], "; ") + fmt.Sprintf(" (+%d more)", len(items)-n)
}

// UnresolvedDetail formats the shared UNRESOLVED report line: `UNRESOLVED: '<query>'
// matches no <noun>`, where noun names the universe the query was resolved against
// ("node" for the node universe, "node/endpoint" for the endpoint universe). One
// format for assert and --focus so a resolution failure reads identically in both.
func UnresolvedDetail(query, noun string) string {
	return fmt.Sprintf("UNRESOLVED: %s matches no %s", QuoteSingle(query), noun)
}

// AmbiguousDetail formats the shared AMBIGUOUS report line: `AMBIGUOUS: '<query>'
// matches <N>: <c1>; <c2>; …` — the query single-quoted, the candidates "; "-joined,
// sorted (Resolve returns them sorted), and capped at maxCandidates with " (+N more)".
// The candidate cap is owned HERE so assert and --focus disclose an ambiguous name
// identically (CLAUDE.md: one source of truth).
func AmbiguousDetail(query string, candidates []string) string {
	return fmt.Sprintf("AMBIGUOUS: %s matches %d: %s",
		QuoteSingle(query), len(candidates), CapList(candidates, maxCandidates))
}
