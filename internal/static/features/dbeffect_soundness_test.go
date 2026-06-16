package features

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/model"
	"github.com/jyang234/golang-code-graph/internal/sqlverb"
)

// TestDBEffectNeverDemotesMutatingVerb makes the DB-effect soundness invariant
// self-checking: for EVERY verb sqlverb counts as a write, a constant statement
// using that verb — even via a Query* method (the Postgres INSERT...RETURNING
// shape) — must classify as EffectMutate, never the lower-salience EffectRead.
// A SELECT is the one read. Iterating sqlverb.MutatingVerbs() ties this to the
// single source of truth: add a verb there and this test fails until dbEffect is
// shown to handle it, so a write can never be silently demoted on method name.
func TestDBEffectNeverDemotesMutatingVerb(t *testing.T) {
	stmts := map[string]string{
		"INSERT":  "INSERT INTO t (a) VALUES ($1)",
		"UPDATE":  "UPDATE t SET a = $1",
		"DELETE":  "DELETE FROM t WHERE id = $1",
		"UPSERT":  "UPSERT INTO t (a) VALUES ($1)",
		"MERGE":   "MERGE INTO t USING s ON t.id = s.id",
		"REPLACE": "REPLACE INTO t (a) VALUES ($1)",
	}
	var b strings.Builder
	b.WriteString("package fix\ntype DB struct{}\nfunc (DB) QueryContext(q string) {}\n")
	verbs := sqlverb.MutatingVerbs()
	for _, v := range verbs {
		stmt, ok := stmts[v]
		if !ok {
			t.Fatalf("no test statement for mutating verb %q — add one so dbEffect soundness stays covered for every write verb", v)
		}
		fmt.Fprintf(&b, "func Use%s(db DB) { db.QueryContext(%q) }\n", v, stmt)
	}
	b.WriteString(`func UseSelect(db DB) { db.QueryContext("SELECT a FROM t WHERE id = $1") }` + "\n")

	fns := buildInline(t, b.String())
	for _, v := range verbs {
		callee, site := firstCall(findInline(t, fns, "Use"+v))
		if got := dbEffect(callee, site); got != model.EffectMutate {
			t.Errorf("dbEffect(%s via QueryContext) = %q, want mutate — a write must never be demoted to a read", v, got)
		}
	}
	callee, site := firstCall(findInline(t, fns, "UseSelect"))
	if got := dbEffect(callee, site); got != model.EffectRead {
		t.Errorf("dbEffect(SELECT) = %q, want read", got)
	}
}
