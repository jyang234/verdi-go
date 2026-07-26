// Package fitness evaluates a policy against a graph index and returns
// deterministic findings — the architectural invariants that fail closed in CI.
// It is the first verdict-bearing surface: layering, must-not-reach (three-valued
// so an over-approximated "no path" is never disguised as a proof), and the
// per-route I/O budget. Every finding names the exact edge or symbol it fires on.
package fitness

import (
	"strings"

	"github.com/jyang234/golang-code-graph/internal/groundwork/facts"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// PkgOf returns the import path of the package that declares the function named
// by an ssa-style FQN. It handles the two shapes flowmap emits:
//
//	example.com/svc/internal/app.Do                  (free function)
//	(*example.com/svc/internal/app.Service).Do       (method; receiver may be a value)
//
// Type arguments on a generic receiver or function (a "[...]" suffix) are
// stripped before the package is read off. An FQN that does not parse yields "".
func PkgOf(fqn string) string {
	return facts.PackageOf(fqn)
}

// ShortName renders an FQN compactly for summaries — it drops the module path
// prefix, the pointer star and the receiver parens, leaving e.g.
// "handler.Server.UpdateUser" or "layeredsvc.run". The exact FQN is still carried
// in a finding's From/To; this is display only.
func ShortName(fqn string) string {
	s := strings.ReplaceAll(strings.TrimPrefix(fqn, "("), "*", "")
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	return strings.ReplaceAll(s, ")", "")
}

// MatchesAny is the compatibility form of facts.MatchesAny for surfaces that
// have not yet migrated their imports.
func MatchesAny(s string, patterns []string) bool { return matchAny(s, patterns) }

// matchAny delegates rich-selector matching to facts, the single owner of
// policy.MatchPrefix semantics.
func matchAny(s string, patterns []string) bool {
	return facts.MatchesAny(s, patterns)
}

// expandFroms expands a rule's From selectors against the graph: the
// "entrypoint:*" selector matches every graph source, anything else is an FQN
// exact-or-prefix pattern. It delegates binding to facts so fitness and claims
// cannot diverge. The proposal lens retains this compatibility name while
// verdict-bearing checks consume complete typed fact results.
func expandFroms(ix *graph.Index, patterns []string) []string {
	return facts.BindSources(ix, patterns)
}
