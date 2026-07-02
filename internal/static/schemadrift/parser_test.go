package schemadrift

import (
	"sort"
	"testing"
)

func ddlNames(ops []ddlOp, create bool) []string {
	var out []string
	for _, o := range ops {
		if o.create == create {
			out = append(out, o.name)
		}
	}
	sort.Strings(out)
	return out
}

func eqSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A CREATE/DROP inside a comment or a string literal must NOT be scanned as real DDL
// — a phantom CREATE would mask real drift (the unsound direction), a phantom DROP
// would manufacture it.
func TestScanDDLIgnoresCommentsAndLiterals(t *testing.T) {
	sqlText := `
-- CREATE TABLE commented_out (id int);
/* CREATE TABLE block_commented (id int); DROP TABLE real_one; */
CREATE TABLE real_one (id int);
INSERT INTO audit(note) VALUES ('CREATE TABLE shadow; DROP TABLE real_one');
`
	ops := scanDDL(sqlText)
	if got := ddlNames(ops, true); !eqSet(got, []string{"real_one"}) {
		t.Errorf("creates = %v, want [real_one] (comment/literal CREATEs must be ignored)", got)
	}
	if got := ddlNames(ops, false); len(got) != 0 {
		t.Errorf("drops = %v, want none (the DROP lives only in a comment and a literal)", got)
	}
}

// A DROP without a trailing semicolon must not swallow the following statement's
// column list into the drop set.
func TestScanDDLDropWithoutSemicolonDoesNotBleed(t *testing.T) {
	// Note: statements ARE semicolon-terminated here; the risk is the LAST drop with
	// no terminator. Put the unterminated DROP last.
	sqlText := "CREATE TABLE keep (a int);\nDROP TABLE gone"
	ops := scanDDL(sqlText)
	if got := ddlNames(ops, true); !eqSet(got, []string{"keep"}) {
		t.Errorf("creates = %v, want [keep]", got)
	}
	if got := ddlNames(ops, false); !eqSet(got, []string{"gone"}) {
		t.Errorf("drops = %v, want [gone] (must not capture column lists or other tokens)", got)
	}
}

// CREATE UNLOGGED TABLE (a real persistent table) and CREATE TABLE AS must be
// recognized, or they'd be missing from the defined set and manufacture false drift.
// CREATE TEMP/TEMPORARY tables are session-scoped and must NOT enter the persistent
// defined set — a migration temp table sharing a name with a real, genuinely missing
// table would otherwise MASK that real drift (the unsound direction).
func TestScanDDLRecognizesUnloggedExcludesTemp(t *testing.T) {
	sqlText := `
CREATE UNLOGGED TABLE u (id int);
CREATE TEMP TABLE t (id int);
CREATE TEMPORARY TABLE t2 (id int);
CREATE GLOBAL TEMPORARY TABLE t3 (id int);
CREATE TABLE plain AS SELECT 1;
`
	if got := ddlNames(scanDDL(sqlText), true); !eqSet(got, []string{"plain", "u"}) {
		t.Errorf("creates = %v, want [plain u] (UNLOGGED + AS recognized; TEMP/TEMPORARY excluded)", got)
	}
}

// A semicolon inside a double-quoted identifier must not split the statement and
// drop the CREATE/DROP (the quote-aware splitStatements guards this).
func TestScanDDLQuotedIdentifierWithSemicolon(t *testing.T) {
	create := scanDDL(`CREATE TABLE "weird;name" (id int);`)
	if got := ddlNames(create, true); !eqSet(got, []string{"weird;name"}) {
		t.Errorf("creates = %v, want [weird;name] (';' inside a quoted identifier must not split)", got)
	}
	drop := scanDDL(`DROP TABLE "gone;too";`)
	if got := ddlNames(drop, false); !eqSet(got, []string{"gone;too"}) {
		t.Errorf("drops = %v, want [gone;too]", got)
	}
}

// A CREATE/DROP inside a PostgreSQL dollar-quoted body ($$…$$, a plpgsql function
// body) is the function's runtime behavior, not migration-time schema DDL, so the
// scan must ignore it in BOTH directions: a phantom CREATE inside a body must not
// enter the defined set (which would mask real drift — the unsound direction),
// and a phantom DROP must not leave it (H-12).
func TestScanDDLIgnoresDollarQuotedBodies(t *testing.T) {
	sqlText := `
CREATE TABLE real_one (id int);
CREATE FUNCTION audit() RETURNS trigger AS $$
BEGIN
  CREATE TABLE ghost (id int);
  DROP TABLE real_one;
END;
$$ LANGUAGE plpgsql;
`
	ops := scanDDL(sqlText)
	if got := ddlNames(ops, true); !eqSet(got, []string{"real_one"}) {
		t.Errorf("creates = %v, want [real_one] (DDL inside a $$ body must be ignored)", got)
	}
	if got := ddlNames(ops, false); len(got) != 0 {
		t.Errorf("drops = %v, want none (the DROP lives only inside a $$ body)", got)
	}
}

// A TAGGED dollar quote ($body$…$body$) must be handled like the empty-tag form,
// and a bare "$1" placeholder must NOT be mistaken for a dollar-quote delimiter.
func TestScanDDLTaggedDollarQuoteAndPlaceholder(t *testing.T) {
	sqlText := `
CREATE TABLE keep (id int);
CREATE FUNCTION f(x int) RETURNS void AS $body$ SELECT $1; DROP TABLE keep; $body$ LANGUAGE plpgsql;
`
	ops := scanDDL(sqlText)
	if got := ddlNames(ops, true); !eqSet(got, []string{"keep"}) {
		t.Errorf("creates = %v, want [keep]", got)
	}
	if got := ddlNames(ops, false); len(got) != 0 {
		t.Errorf("drops = %v, want none (DROP inside a tagged $body$ must be ignored)", got)
	}
}

// End-to-end: a CREATE only inside a dollar-quoted body must not become a phantom
// table that masks a real drift on that name (the H-12 unsound direction).
func TestDollarQuotedCreateDoesNotMaskDrift(t *testing.T) {
	files := []MigrationFile{mig("V1__init.sql",
		"CREATE FUNCTION seed() RETURNS void AS $$ CREATE TABLE audit_x (id int); $$ LANGUAGE plpgsql;\nCREATE TABLE real (id int);")}
	edges := []Edge{dbEdge("svc.W", "INSERT audit_x")}
	r := Check(edges, files, nil)
	if len(r.Drift) != 1 || r.Drift[0].Table != "audit_x" {
		t.Fatalf("a write to a table only 'defined' inside a $$ body must drift; got %v", r.Drift)
	}
}

// A RENAME inside a comment must not raise a spurious caveat (renameCaveat scans
// stripped SQL).
func TestRenameCaveatIgnoresComments(t *testing.T) {
	if c := renameCaveat(mig("V3__x.sql", "-- ALTER TABLE a RENAME TO b;\nCREATE TABLE c (id int);")); c != "" {
		t.Errorf("commented RENAME must not raise a caveat, got %q", c)
	}
	if c := renameCaveat(mig("V3__x.sql", "ALTER TABLE a RENAME TO b;")); c == "" {
		t.Error("a real RENAME must raise a caveat")
	}
}

// isRollback matches the *_rollback.sql suffix, NOT a loose substring — a forward
// migration whose description merely contains "rollback" must replay.
func TestIsRollbackSuffixOnly(t *testing.T) {
	if !isRollback("V8__drop_queue_rollback.sql") {
		t.Error("a *_rollback.sql file must be excluded from forward replay")
	}
	if isRollback("V12__add_rollback_audit_table.sql") {
		t.Error("a forward migration whose name merely contains 'rollback' must NOT be excluded")
	}
}

// End-to-end: a commented-out CREATE must not become a phantom table that masks a
// real drift on that table name.
func TestCommentedCreateDoesNotMaskDrift(t *testing.T) {
	files := []MigrationFile{mig("V1__init.sql", "-- CREATE TABLE ghost (id int);\nCREATE TABLE real (id int);")}
	edges := []Edge{dbEdge("svc.W", "INSERT ghost")}
	r := Check(edges, files, nil)
	if len(r.Drift) != 1 || r.Drift[0].Table != "ghost" {
		t.Fatalf("a write to a table only 'defined' in a comment must drift; got %v", r.Drift)
	}
}
