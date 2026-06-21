package schemadrift

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// LoadMigrations reads every *.sql file in dir into MigrationFile{Name, SQL}. It
// does NOT filter or order — Check excludes rollbacks and orders by version itself,
// so the result is independent of directory-read order. Subdirectories are ignored.
func LoadMigrations(dir string) ([]MigrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []MigrationFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		out = append(out, MigrationFile{Name: name, SQL: string(b)})
	}
	return out, nil
}

// schemaTables replays the forward migrations in version order and applies their
// CREATE/DROP statements, returning the defined-table set, the count of forward
// files replayed, and any fail-closed parse caveats. The set is creates − drops;
// the caller folds in the library-owned tables. Replay order is a total order on
// the file names (numeric version, then repeatable, then other), so the result is
// deterministic regardless of input order.
func schemaTables(files []MigrationFile) (map[string]bool, int, []string) {
	ordered := forwardOrdered(files)
	defined := map[string]bool{}
	var caveats []string
	for _, f := range ordered {
		// Apply DDL in textual order within the file so a create-then-drop (or
		// drop-then-create) of the same table nets out correctly.
		for _, op := range scanDDL(f.SQL) {
			if op.create {
				defined[op.name] = true
			} else {
				delete(defined, op.name)
			}
		}
		if c := renameCaveat(f); c != "" {
			caveats = append(caveats, c)
		}
	}
	return defined, len(ordered), caveats
}

var (
	// versionedRe matches a Flyway versioned migration "V<version>__desc.sql".
	versionedRe = regexp.MustCompile(`^[Vv]([0-9]+(?:[._][0-9]+)*)__`)
	// repeatableRe matches a Flyway repeatable migration "R__desc.sql".
	repeatableRe = regexp.MustCompile(`^[Rr]__`)
)

// forwardOrdered filters out rollback files and returns the forward migrations in
// replay order: versioned (by numeric version), then repeatable, then any other
// .sql, each group broken by name. The ordering is total and name-derived, so it is
// run-independent.
func forwardOrdered(files []MigrationFile) []MigrationFile {
	type keyed struct {
		f   MigrationFile
		cat int // 0 versioned, 1 repeatable, 2 other
		ver []int
	}
	var ks []keyed
	for _, f := range files {
		if isRollback(f.Name) {
			continue
		}
		switch {
		case versionedRe.MatchString(f.Name):
			ks = append(ks, keyed{f: f, cat: 0, ver: parseVersion(f.Name)})
		case repeatableRe.MatchString(f.Name):
			ks = append(ks, keyed{f: f, cat: 1})
		default:
			ks = append(ks, keyed{f: f, cat: 2})
		}
	}
	sort.SliceStable(ks, func(i, j int) bool {
		if ks[i].cat != ks[j].cat {
			return ks[i].cat < ks[j].cat
		}
		if ks[i].cat == 0 {
			if c := compareVersion(ks[i].ver, ks[j].ver); c != 0 {
				return c < 0
			}
		}
		return ks[i].f.Name < ks[j].f.Name
	})
	out := make([]MigrationFile, len(ks))
	for i, k := range ks {
		out[i] = k.f
	}
	return out
}

// isRollback reports whether a file is a rollback (undo) script — excluded from the
// forward replay. A rollback's DROP must not apply as if it were forward, so we
// drop any .sql whose base name contains "rollback" (the documented "*_rollback.sql"
// convention, matched loosely so a variant spelling still fails closed to exclusion).
func isRollback(name string) bool {
	return strings.Contains(strings.ToLower(name), "rollback")
}

// parseVersion extracts the numeric version components from a "V<version>__" name,
// e.g. "V3_1__x.sql" → [3, 1]. Non-numeric or absent → nil (sorts first among
// versioned files, which only affects an already-malformed corpus).
func parseVersion(name string) []int {
	m := versionedRe.FindStringSubmatch(name)
	if m == nil {
		return nil
	}
	parts := strings.FieldsFunc(m[1], func(r rune) bool { return r == '.' || r == '_' })
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// compareVersion orders two version component slices, shorter-is-lower on a common
// prefix (V3 before V3.1).
func compareVersion(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	default:
		return 0
	}
}

// ddlOp is a single table-affecting DDL statement recovered from a migration.
type ddlOp struct {
	create bool // true = CREATE TABLE, false = DROP TABLE
	name   string
}

var (
	// createRe matches "CREATE TABLE [IF NOT EXISTS] <name>", capturing a bare,
	// quoted, or schema-qualified name (cleanName normalizes it).
	createRe = regexp.MustCompile("(?is)\\bCREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?(\"[^\"]+\"|`[^`]+`|[a-zA-Z_][\\w$.]*)")
	// dropRe matches "DROP TABLE [IF EXISTS] <list>" up to the statement terminator;
	// the captured list is comma-split into individual names.
	dropRe = regexp.MustCompile(`(?is)\bDROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([^;]+)`)
	// alterRenameRe detects "ALTER TABLE ... RENAME TO ...", a table rename the
	// create/drop scan cannot follow — surfaced as a fail-closed caveat.
	alterRenameRe = regexp.MustCompile(`(?is)\bALTER\s+TABLE\b.*?\bRENAME\s+TO\b`)
)

// scanDDL returns the CREATE/DROP table operations in one migration's SQL, in
// textual order, so the replay nets create-then-drop correctly within a file.
func scanDDL(sqlText string) []ddlOp {
	type positioned struct {
		pos int
		op  ddlOp
	}
	var ms []positioned
	for _, idx := range createRe.FindAllStringSubmatchIndex(sqlText, -1) {
		name := cleanName(sqlText[idx[2]:idx[3]])
		if name != "" {
			ms = append(ms, positioned{pos: idx[0], op: ddlOp{create: true, name: name}})
		}
	}
	for _, idx := range dropRe.FindAllStringSubmatchIndex(sqlText, -1) {
		for _, name := range splitDropList(sqlText[idx[2]:idx[3]]) {
			if name != "" {
				ms = append(ms, positioned{pos: idx[0], op: ddlOp{create: false, name: name}})
			}
		}
	}
	sort.SliceStable(ms, func(i, j int) bool { return ms[i].pos < ms[j].pos })
	out := make([]ddlOp, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.op)
	}
	return out
}

// splitDropList turns a "DROP TABLE" target list ("a, b CASCADE") into clean names.
// Each comma-separated part's first token is the name; trailing words (CASCADE,
// RESTRICT) are ignored.
func splitDropList(list string) []string {
	var out []string
	for _, part := range strings.Split(list, ",") {
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		out = append(out, cleanName(fields[0]))
	}
	return out
}

// renameCaveat returns a fail-closed disclosure when a migration renames a table:
// the renamed name may not appear as a CREATE, so the scan could miss it. Surfacing
// it is preferable to silently producing (or hiding) drift.
func renameCaveat(f MigrationFile) string {
	if alterRenameRe.MatchString(f.SQL) {
		return fmt.Sprintf("%s: ALTER TABLE ... RENAME — a renamed table may be unseen by the CREATE/DROP scan", f.Name)
	}
	return ""
}

// cleanName normalizes a DDL identifier to match the code-side table name: strip
// surrounding quotes/brackets, take the segment after the last "." (schema
// qualifier), and lower-case. (A dot inside a quoted identifier is a rare literal
// and is treated as a qualifier — a documented limitation.)
func cleanName(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "\"`[]")
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	s = strings.Trim(s, "\"`[]")
	return strings.ToLower(strings.TrimSpace(s))
}
