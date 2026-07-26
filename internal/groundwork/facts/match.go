package facts

import (
	"sort"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
	"github.com/jyang234/golang-code-graph/internal/groundwork/setutil"
)

// MatchesAny reports whether value is bound by any selector under the one
// boundary-aware matcher shared by rich fact evaluators.
func MatchesAny(value string, selectors []string) bool {
	return matchesAny(value, selectors)
}

// matchesAny is the only facts helper that invokes policy.MatchPrefix.
func matchesAny(value string, selectors []string) bool {
	for _, selector := range selectors {
		if policy.MatchPrefix(value, selector) {
			return true
		}
	}
	return false
}

// BindSources expands selectors against graph nodes. entrypoint:* contributes
// every structural graph source. The returned union is sorted and de-duplicated.
func BindSources(ix *graph.Index, selectors []string) []string {
	set := make(map[string]bool)
	for _, selector := range selectors {
		if selector == policy.EntrypointSelector {
			for _, source := range ix.Sources() {
				set[source] = true
			}
			continue
		}
		ix.RangeNodes(func(fqn string) {
			if matchesAny(fqn, []string{selector}) {
				set[fqn] = true
			}
		})
	}
	return nonEmptySortedKeys(set)
}

// BindFunctions expands selectors against graph nodes without the
// entrypoint:* source expansion. It is used for waypoint bindings, where only
// actual function identities can be removed from a traversal.
func BindFunctions(ix *graph.Index, selectors []string) []string {
	set := make(map[string]bool)
	ix.RangeNodes(func(fqn string) {
		if matchesAny(fqn, selectors) {
			set[fqn] = true
		}
	})
	return nonEmptySortedKeys(set)
}

// BindTargets expands selectors against every graph node and boundary label.
// Non-boundary external edges are not targets because the reach index cannot
// traverse or classify them as effects.
func BindTargets(ix *graph.Index, selectors []string) []string {
	set := make(map[string]bool)
	for _, fqn := range ix.Nodes() {
		if matchesAny(fqn, selectors) {
			set[fqn] = true
		}
	}
	for _, edge := range canonicalBoundaryEdges(ix) {
		if matchesAny(edge.To, selectors) {
			set[edge.To] = true
		}
	}
	return nonEmptySortedKeys(set)
}

func nonEmptySortedKeys(set map[string]bool) []string {
	result := setutil.SortedKeys(set)
	if len(result) == 0 {
		return nil
	}
	return result
}

// PackageOf returns the declaring import path encoded in an SSA-style FQN.
// An FQN that does not parse yields "".
func PackageOf(fqn string) string {
	value := fqn
	if strings.HasPrefix(value, "(") {
		end := strings.IndexByte(value, ')')
		if end < 0 {
			return ""
		}
		value = strings.TrimPrefix(value[1:end], "*")
	}
	return packageFromQualified(stripTypeArgs(value))
}

func packageFromQualified(value string) string {
	prefix, segment := "", value
	if slash := strings.LastIndexByte(value, '/'); slash >= 0 {
		prefix, segment = value[:slash+1], value[slash+1:]
	}
	dot := strings.IndexByte(segment, '.')
	if dot < 0 {
		return value
	}
	return prefix + segment[:dot]
}

func stripTypeArgs(value string) string {
	if i := strings.IndexByte(value, '['); i >= 0 {
		return value[:i]
	}
	return value
}

func canonicalStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	if len(result) == 0 {
		return result
	}
	n := 1
	for _, value := range result[1:] {
		if value == result[n-1] {
			continue
		}
		result[n] = value
		n++
	}
	return result[:n]
}
