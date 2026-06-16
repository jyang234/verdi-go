package graphio

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/canon/sql"
)

// TestSQLOpTableMatchesCanon pins that the static view labeler shares the one
// canonical SQL normalizer (canon/sql) instead of a divergent second parser, so
// the op/table it emits cannot drift from the behavioral pipeline's op key. The
// MERGE/REPLACE/UPSERT cases are the regression the old light heuristic caused:
// it lower-cased the verb and dropped the table, yielding a different canonical
// key than canon/sql for the very verbs sqlverb.Mutating counts as writes.
func TestSQLOpTableMatchesCanon(t *testing.T) {
	cases := []string{
		"SELECT name FROM applicants WHERE id = $1",
		"INSERT INTO ledger (loan_id, amount) VALUES ($1, $2)",
		"UPDATE loans SET status = 'paid' WHERE id = $1",
		"DELETE FROM sessions WHERE id = $1",
		"MERGE INTO accounts USING staging ON accounts.id = staging.id",
		"REPLACE INTO cache (k, v) VALUES ($1, $2)",
		"UPSERT INTO counters (k, n) VALUES ($1, $2)",
	}
	for _, stmt := range cases {
		op, table := sqlOpTable(stmt)
		n := sql.Normalize(stmt)
		if op != n.Operation || table != n.Table {
			t.Errorf("sqlOpTable(%q) = {%q,%q}, canon/sql = {%q,%q}; the two SQL parsers diverge", stmt, op, table, n.Operation, n.Table)
		}
	}
}
