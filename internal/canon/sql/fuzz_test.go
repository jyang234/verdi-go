package sql

import "testing"

// FuzzNormalizeIdempotent makes the SQL normalizer self-checking on two
// properties that both review rounds' findings violated in spirit:
//   - it never panics on arbitrary statement text (the gated db.statement is
//     attacker-influenced), and
//   - the normal form is a FIXED POINT: re-normalizing an already-normalized
//     statement yields the identical statement. Idempotence is what guarantees
//     a statement's canonical key is stable no matter how many normalization
//     passes it survives, and it catches a collapse rule (IN/VALUES/function
//     args) that is not self-consistent.
func FuzzNormalizeIdempotent(f *testing.F) {
	for _, s := range []string{
		"SELECT name, income FROM applicants WHERE id = $1",
		"select * from loans where id IN (1,2,3)",
		"INSERT INTO t (a,b) VALUES ($1,$2),($3,$4)",
		"MERGE INTO accounts USING staging ON accounts.id = staging.id",
		"SELECT coalesce(?, ?, ?) FROM t",
		"UPDATE ONLY loans SET status = 'paid'",
		"",
		")((((",
		"'unterminated",
		"0\x850",      // regression: a non-ASCII byte outside quotes (FuzzNormalizeIdempotent found this)
		"SELECT café", // UTF-8 identifier outside quotes must round-trip, not grow
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		n := Normalize(raw)
		again := Normalize(n.Statement)
		if again.Statement != n.Statement {
			t.Errorf("Normalize not idempotent:\n in:   %q\n once: %q\n twice:%q", raw, n.Statement, again.Statement)
		}
	})
}
