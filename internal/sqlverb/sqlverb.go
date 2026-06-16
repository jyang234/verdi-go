// Package sqlverb is the single source of truth for which SQL verbs commit a row
// mutation. The static labeler (graphio.mutatingSQLOp) and the groundwork write
// surface (fitness.IsWrite / fitness.UnclassifiedDBLabel) are otherwise
// decoupled — they communicate only through the emitted graph JSON — so the
// mutating-verb set lives here, imported by both, instead of being copied into
// each and left to drift. The partial-effect disclosure and the I/O-budget write
// surface must agree on what counts as a committed write; this package is what
// makes that parity mechanical rather than a hand-maintained comment.
package sqlverb

// mutatingVerbs is the canonical set of SQL verbs that commit a row mutation,
// kept SORTED so any list derived from it (e.g. fitness's proposed write-target
// rule) is deterministic. It is the one backing list both Mutating and
// MutatingVerbs read, so a verb added here reaches every consumer at once and no
// consumer re-types the set.
var mutatingVerbs = []string{"DELETE", "INSERT", "MERGE", "REPLACE", "UPDATE", "UPSERT"}

// Mutating reports whether an uppercase SQL verb commits a row mutation. Callers
// pass an already upper-cased verb — the form graphio reads off a constant
// statement and the form fitness derives from the boundary label. A verb the
// labeler could NOT read (a dynamic statement that fell back to the driver method
// name) is not in this set and must be routed through the unclassified-DB caution
// channel rather than asserted as a definite write.
func Mutating(op string) bool {
	for _, v := range mutatingVerbs {
		if v == op {
			return true
		}
	}
	return false
}

// MutatingVerbs returns the canonical mutating-verb set, sorted, as a fresh copy
// so a caller may retain or extend it without aliasing the source of truth. A
// caller that builds a rule or target list spanning the write vocabulary derives
// it from here instead of hand-enumerating the verbs (which silently drifts when
// a verb is added to the set above).
func MutatingVerbs() []string {
	return append([]string(nil), mutatingVerbs...)
}
