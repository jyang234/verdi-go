package features

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/model"
)

// TestStringArgsNilSite pins the nil-site guard: a synthetic self-edge (the one
// nodeTier feeds through Edge as ext.Edge(fn, fn, nil)) has no call instruction,
// so StringArgs must return nil rather than dereference a nil Common() and panic.
func TestStringArgsNilSite(t *testing.T) {
	if got := StringArgs(nil); got != nil {
		t.Fatalf("StringArgs(nil) = %v, want nil", got)
	}
	if got := constSQLOp(nil); got != "" {
		t.Fatalf("constSQLOp(nil) = %q, want empty", got)
	}
}

// TestDBEffectNilSiteFailsClosed reproduces H-4: nodeTier computes a node's
// compute-floor via ext.Edge(fn, fn, nil); when fn's package matches a classify.db
// hint, Edge routes into dbEffect(callee, nil), which read the statement through a
// nil site and panicked. With the guard, dbEffect must fall back to the method-name
// signal (Exec* → mutate, everything else → io) and never crash — the safe,
// never-a-read direction.
func TestDBEffectNilSiteFailsClosed(t *testing.T) {
	const src = `package fix
type DB struct{}
func (DB) QueryContext(q string) {}
func (DB) ExecContext(q string) {}
func UseQuery(db DB) { db.QueryContext("SELECT a FROM t") }
func UseExec(db DB) { db.ExecContext("DELETE FROM t") }
`
	fns := buildInline(t, src)

	queryCallee, _ := firstCall(findInline(t, fns, "UseQuery"))
	if got := dbEffect(queryCallee, nil); got != model.EffectIO {
		t.Errorf("dbEffect(Query*, nil site) = %q, want io (fail closed, never a read)", got)
	}

	execCallee, _ := firstCall(findInline(t, fns, "UseExec"))
	if got := dbEffect(execCallee, nil); got != model.EffectMutate {
		t.Errorf("dbEffect(Exec*, nil site) = %q, want mutate", got)
	}
}
