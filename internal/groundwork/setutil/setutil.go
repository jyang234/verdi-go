// Package setutil holds the small string-set helpers shared across the
// groundwork packages, so the same sorted-keys / membership logic is defined once
// rather than copied per package.
package setutil

import "sort"

// SortedKeys returns the keys of m in ascending order. The value type is free, so
// it serves both membership sets (map[string]bool) and keyed maps.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// StringSet returns a membership set over ss.
func StringSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
