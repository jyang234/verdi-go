// Package nodecount owns the ONE phrasing for a node count that had to collapse
// several node RECORDS into one function.
//
// A display FQN can legally carry several node records: a generic instantiated at two
// function-local declared types renders to the same types.TypeString, and the local
// generic type identity design deliberately KEEPS every instance rather than deduping
// it in sortGraph or in serialization. Every consumer that reports "how many functions"
// must therefore count DISTINCT FQNs — and, having counted them, must say that it
// collapsed something. A clean number with no trace of the fold is a laundered unknown
// (tenet 3): the consumer cannot tell 5 functions from 5-of-7-records, and the
// discrepancy against the graph's own nodes[] array is unexplainable from the artifact.
//
// The phrasing lives here, once, so the disclosure a reader meets in a mermaid note, a
// rollup caveat and a CLI line is the SAME sentence rather than a per-site invention
// (CLAUDE.md: one source of truth).
package nodecount

import "strconv"

// RecordSuffix is the multiplicity disclosure that rides immediately after a
// DISTINCT-function node count: RecordSuffix(5, 7) renders " (7 instance records)", so
// a caller emitting "5 nodes" prints "5 nodes (7 instance records)".
//
// It is EMPTY when nothing was collapsed (records == functions — the duplicate-free
// case, which is every graph without a scope-lossy generic instantiation). That is what
// keeps the disclosure a signal instead of a suffix a reader learns to skip, and it is
// what keeps every duplicate-free artifact byte-identical to one produced before this
// disclosure existed. records < functions is impossible — a record list holds at least
// one record per distinct FQN in it — and is treated as the nothing-collapsed case
// rather than rendered as a nonsense suffix.
func RecordSuffix(functions, records int) string {
	if records <= functions {
		return ""
	}
	return " (" + strconv.Itoa(records) + " instance records)"
}
