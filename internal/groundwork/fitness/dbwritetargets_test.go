package fitness

import (
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/sqlverb"
)

// TestDBWriteTargetsMatchSQLVerb is the parity guard CLAUDE.md's one-source-of-
// truth rule requires: the verbs in the proposed write-target rule must be
// exactly the verbs sqlverb.Mutating (and thus IsWrite) recognizes. Without it,
// a verb added to sqlverb.Mutating would make IsWrite count it as a write while
// the proposed rule's target set omitted it — a rule strictly weaker than the
// check that built it (the R6 regression).
func TestDBWriteTargetsMatchSQLVerb(t *testing.T) {
	got := dbWriteTargets()
	verbs := sqlverb.MutatingVerbs()
	if len(got) != len(verbs) {
		t.Fatalf("dbWriteTargets has %d entries, sqlverb.MutatingVerbs has %d", len(got), len(verbs))
	}
	for i, target := range got {
		verb, ok := strings.CutPrefix(target, "boundary:db ")
		if !ok {
			t.Errorf("target %q lacks the boundary:db prefix", target)
			continue
		}
		if !sqlverb.Mutating(verb) {
			t.Errorf("proposed write target %q is not a sqlverb.Mutating verb", verb)
		}
		if verb != verbs[i] {
			t.Errorf("target[%d] verb = %q, want %q (order must track MutatingVerbs)", i, verb, verbs[i])
		}
	}
}
