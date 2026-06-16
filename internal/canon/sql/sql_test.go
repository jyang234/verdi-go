package sql

import (
	"strings"
	"testing"
)

func TestNormalizeStripsLiterals(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"SELECT name, income FROM applicants WHERE id = $1", "SELECT name , income FROM applicants WHERE id = ?"},
		{"SELECT name FROM applicants WHERE id = 8412", "SELECT name FROM applicants WHERE id = ?"},
		{"select * from loans where status = 'paid'", "SELECT * FROM loans WHERE status = ?"},
		{"INSERT INTO ledger (loan_id, amount) VALUES ($1, $2)", "INSERT INTO ledger ( loan_id , amount ) VALUES (?)"},
	}
	for _, c := range cases {
		if got := Normalize(c.raw).Statement; got != c.want {
			t.Errorf("Normalize(%q).Statement = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestNormalizeCollapsesInList(t *testing.T) {
	got := Normalize("SELECT * FROM loans WHERE id IN (1, 2, 3, 4, 5)").Statement
	want := "SELECT * FROM loans WHERE id IN (?)"
	if got != want {
		t.Errorf("IN-list: got %q, want %q", got, want)
	}
}

func TestNormalizeCollapsesMultiRow(t *testing.T) {
	got := Normalize("INSERT INTO t (a, b) VALUES (1, 2), (3, 4), (5, 6)").Statement
	want := "INSERT INTO t ( a , b ) VALUES (?)"
	if got != want {
		t.Errorf("multi-row: got %q, want %q", got, want)
	}
}

func TestNormalizeStableUnderCardinality(t *testing.T) {
	// 3 rows vs 300 rows must normalize identically — the whole point.
	three := Normalize("INSERT INTO t (a) VALUES (1), (2), (3)").Statement
	many := Normalize("INSERT INTO t (a) VALUES (1), (2), (3), (4), (5), (6), (7)").Statement
	if three != many {
		t.Errorf("cardinality leaked: %q != %q", three, many)
	}
}

func TestOperationAndTable(t *testing.T) {
	cases := []struct {
		raw, op, table string
	}{
		{"SELECT name FROM applicants WHERE id = $1", "SELECT", "applicants"},
		{"INSERT INTO ledger (loan_id) VALUES ($1)", "INSERT", "ledger"},
		{"UPDATE loans SET status = 'paid' WHERE id = $1", "UPDATE", "loans"},
		{"DELETE FROM sessions WHERE id = $1", "DELETE", "sessions"},
	}
	for _, c := range cases {
		n := Normalize(c.raw)
		if n.Operation != c.op || n.Table != c.table {
			t.Errorf("Normalize(%q) = {op:%q table:%q}, want {op:%q table:%q}", c.raw, n.Operation, n.Table, c.op, c.table)
		}
	}
}

// TestOperationAndTableUpdateQualifier covers UPDATE statements with a leading
// dialect qualifier (Postgres ONLY, MySQL LOW_PRIORITY/IGNORE): the table must
// be the real target, not the qualifier word.
func TestOperationAndTableUpdateQualifier(t *testing.T) {
	cases := []struct {
		raw, table string
	}{
		{"UPDATE ONLY loans SET status = 'paid' WHERE id = $1", "loans"},
		{"UPDATE LOW_PRIORITY accounts SET balance = 0", "accounts"},
		{"UPDATE loans SET status = 'paid'", "loans"},
	}
	for _, c := range cases {
		n := Normalize(c.raw)
		if n.Operation != "UPDATE" || n.Table != c.table {
			t.Errorf("Normalize(%q) = {op:%q table:%q}, want UPDATE/%q", c.raw, n.Operation, n.Table, c.table)
		}
	}
}

// TestNormalizeKeepsFunctionCallArity pins that the placeholder-list collapse is
// scoped to variable-cardinality clauses (IN-lists, VALUES rows) and does NOT
// collapse a SQL function call's argument list. coalesce(?, ?) and
// coalesce(?, ?, ?) are structurally distinct statements; collapsing both to
// coalesce (?) would merge them under one canonical key and report "no change"
// where the SQL shape actually changed.
func TestNormalizeKeepsFunctionCallArity(t *testing.T) {
	two := Normalize("SELECT coalesce(?, ?) FROM t").Statement
	three := Normalize("SELECT coalesce(?, ?, ?) FROM t").Statement
	if two == three {
		t.Errorf("function-call arity collapsed: %q == %q (distinct statements merged)", two, three)
	}
	if !strings.Contains(two, "? , ?") {
		t.Errorf("function-call placeholders were collapsed: %q", two)
	}
	// The IN-list and VALUES collapses must still hold (their cardinality IS
	// variable and legitimately churns the golden otherwise).
	if got := Normalize("SELECT * FROM t WHERE id IN (1, 2, 3)").Statement; got != "SELECT * FROM t WHERE id IN (?)" {
		t.Errorf("IN-list no longer collapses: %q", got)
	}
	if got := Normalize("INSERT INTO t (a, b) VALUES (1, 2), (3, 4)").Statement; got != "INSERT INTO t ( a , b ) VALUES (?)" {
		t.Errorf("multi-row VALUES no longer collapses: %q", got)
	}
}

// TestOperationAndTableMergeReplaceUpsert pins that the verbs sqlverb.Mutating
// treats as writes (MERGE/UPSERT/REPLACE) are parsed with their operation
// upper-cased and their target table extracted, identically to INSERT — so the
// canonical op key for a MERGE matches the static labeler's and the table is not
// dropped.
func TestOperationAndTableMergeReplaceUpsert(t *testing.T) {
	cases := []struct {
		raw, op, table string
	}{
		{"MERGE INTO accounts USING staging ON accounts.id = staging.id", "MERGE", "accounts"},
		{"REPLACE INTO sessions (id, data) VALUES ($1, $2)", "REPLACE", "sessions"},
		{"UPSERT INTO counters (k, n) VALUES ($1, $2)", "UPSERT", "counters"},
	}
	for _, c := range cases {
		n := Normalize(c.raw)
		if n.Operation != c.op || n.Table != c.table {
			t.Errorf("Normalize(%q) = {op:%q table:%q}, want {op:%q table:%q}", c.raw, n.Operation, n.Table, c.op, c.table)
		}
	}
}

func TestNormalizeDeterministic(t *testing.T) {
	// Whitespace and placeholder-style variations of the same logical statement
	// converge.
	a := Normalize("SELECT  id ,\n status\tFROM loans WHERE id = $1").Statement
	b := Normalize("SELECT id, status FROM loans WHERE id = ?").Statement
	if a != b {
		t.Errorf("whitespace/placeholder variants diverged: %q != %q", a, b)
	}
}
