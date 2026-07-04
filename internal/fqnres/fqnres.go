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
