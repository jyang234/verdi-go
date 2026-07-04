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

// CapList joins up to n items with "; " — the resolution report's ONE list separator —
// disclosing any truncation with " (+N more)" so a capped list never reads as the whole
// set (tenet 3). Items are expected pre-sorted (fqnres.Resolve returns sorted matches);
// CapList does not reorder. Shared by assert's offender/candidate lists and --focus.
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
