// Package routematch decides whether a hand-authored route/topic query names
// the same entrypoint as a graph entrypoints[].name registration literal. It is
// the single segment-wise tolerance matcher shared by two features that must
// answer a route name identically: the impact/triage route lens
// (internal/groundwork/impact.ResolveRoute) and the `groundwork assert`
// entrypoint claim kind (internal/groundwork/claims). It is a stdlib-only leaf:
// impact and claims may both import it, and it imports neither — the same
// layering precedent as internal/fqnres, internal/boundarylabel and
// internal/tiermap.
//
// # The grammar, and why the match is tolerant
//
// The names on the graph side are REGISTRATION-SITE literals (see the
// Entrypoint type doc in internal/groundwork/graph): a stdlib root may lack a
// method; a mounted route carries only its leaf pattern, not the full mounted
// path. The graph schema therefore tells consumers to match segment-wise, never
// exactly-or-nothing. Match encodes exactly that tolerance:
//
//   - Method: an optional leading token ("POST /loans"). Methods compare
//     case-insensitively, but ONLY when BOTH sides carry one. A method-less side
//     (stdlib HandleFunc, or a query that omits the verb) matches any method.
//   - Path segments: compared pairwise. A param token — "{x}", ":x", "<x>",
//     "$x", "*", "..." — is a single-segment wildcard on EITHER side, so a
//     registration "{id}" matches an observed "42" and a queried "{id}" matches
//     a registration "42".
//   - Mount tolerance: the shorter segment list aligns against the TAIL of the
//     longer (symmetric), so the alert's "/api/v1/loans" matches the
//     registration's "/loans" and vice versa. Alignment is by whole segments —
//     a non-segment-aligned suffix ("/loans" vs "/v2/loans-archive") never
//     collides.
//   - An empty segment list matches only an empty one (the bare "/" root).
//
// A name with no '/' and no space — a consumer topic like "payment.settled" —
// carries no method and splits into a single path segment, so it degrades to
// whole-string equality via the ordinary single-segment path: "payment.settled"
// matches "payment.settled" and nothing else (a topic is not a wildcard).
//
// # Two look-alikes that must NOT be unified (deliberate non-unification)
//
// Following internal/fqnres's precedent, the distinguishing case is pinned in
// prose so a future reader does not collapse these into one matcher:
//
//   - internal/static/graphio/mermaid_rooted.go's routeMatches is a per-handler
//     VIEW with deliberately DIFFERENT semantics: it aligns on EXACT whole
//     segments with NO param wildcards (its "{id}" matches only a literal
//     "{id}"), because it is rooting a rendered diagram, not resolving a
//     symptom. Distinguishing case: routematch.Match("/loans/{id}", "/loans/42")
//     is true; graphio's routeMatches("/loans/{id}", "/loans/42") is false.
//   - internal/static/boundary/boundary.go's splitRoute is a method/route field
//     SPLITTER for the contract wire shape, not a matcher — it splits on the
//     first space and never touches path segments or param tokens. It answers
//     "what are the method and route fields", not "do these two name the same
//     endpoint".
package routematch

import "strings"

// Match reports whether a hand-authored query names the same entrypoint as a
// graph entrypoints[].name registration literal. It splits both sides (method +
// path segments) and applies the full tolerance rule documented on the package:
// method compares case-insensitively only when BOTH sides carry one; param
// tokens are single-segment wildcards on either side; the shorter segment list
// aligns against the tail of the longer; an empty segment list matches only an
// empty one. A name with no '/' and no space degrades to whole-string equality
// via the single-segment path.
func Match(entryName, query string) bool {
	eMethod, eSegs := splitRoute(entryName)
	qMethod, qSegs := splitRoute(query)
	if qMethod != "" && eMethod != "" && !strings.EqualFold(qMethod, eMethod) {
		return false
	}
	return routeSegsMatch(eSegs, qSegs)
}

// splitRoute separates an optional leading method token from the path and
// splits the path into segments. "POST /loans/{id}" → ("POST", [loans {id}]);
// "/transfer" → ("", [transfer]).
func splitRoute(s string) (method string, segs []string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, ' '); i > 0 && !strings.Contains(s[:i], "/") {
		method, s = s[:i], strings.TrimSpace(s[i+1:])
	}
	for _, seg := range strings.Split(strings.Trim(s, "/"), "/") {
		if seg != "" {
			segs = append(segs, seg)
		}
	}
	return method, segs
}

// routeSegsMatch aligns the shorter segment list against the TAIL of the
// longer (mount tolerance) and compares segment-wise, with param tokens on
// either side acting as single-segment wildcards. An empty list matches only
// an empty list (the bare "/" route).
func routeSegsMatch(a, b []string) bool {
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	if len(short) == 0 {
		return len(long) == 0
	}
	long = long[len(long)-len(short):]
	for i := range short {
		if !routeSegEq(short[i], long[i]) {
			return false
		}
	}
	return true
}

func routeSegEq(x, y string) bool {
	return isParamSeg(x) || isParamSeg(y) || x == y
}

// isParamSeg reports whether a path segment is a param wildcard. It is never
// called with an empty string: splitRoute drops empty segments, so s[0] is
// always safe here.
func isParamSeg(s string) bool {
	if s == "*" || s == "..." {
		return true
	}
	switch s[0] {
	case '{', ':', '<', '$':
		return true
	}
	return false
}
