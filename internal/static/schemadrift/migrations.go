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
// forward replay so its DROP does not apply as if it were forward. It matches the
// documented "*_rollback.sql" convention by SUFFIX, not a loose substring: a
// substring match would wrongly exclude a legitimate FORWARD migration whose
// description merely contains the word (e.g. V12__add_rollback_audit_table.sql),
// dropping its CREATE from the defined-schema set and manufacturing false drift.
func isRollback(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), "_rollback.sql")
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
	// createRe matches "CREATE [UNLOGGED] TABLE [IF NOT EXISTS] <name>", capturing a
	// bare, quoted, or schema-qualified name (cleanName normalizes it). UNLOGGED is a
	// real PERSISTENT table, so it must be in the defined set or a write to it reads as
	// false drift. TEMP/TEMPORARY (and the GLOBAL/LOCAL that only qualify TEMPORARY)
	// are deliberately NOT matched: a session-scoped temp table is not persistent
	// schema, and a migration-created temp table sharing a name with a real, genuinely
	// missing table would MASK that real drift (the unsound direction — a false
	// "no drift"). Excluding them costs nothing real (a migration's temp table is gone
	// after the migration, so no code path can persist-write it) and keeps the
	// absence claim sound.
	createRe = regexp.MustCompile("(?is)\\bCREATE\\s+(?:UNLOGGED\\s+)?TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?(\"[^\"]+\"|`[^`]+`|[a-zA-Z_][\\w$.]*)")
	// dropRe matches "DROP TABLE [IF EXISTS] <list>". scanDDL runs it per statement
	// (splitStatements has already bounded statements on ';' outside quotes), so the
	// capture can take the rest of the statement (.+) rather than stopping at the next
	// ';' — which lets a quoted target name containing a ';' (e.g. "gone;too") survive,
	// while still not bleeding across statements.
	dropRe = regexp.MustCompile(`(?is)\bDROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(.+)`)
	// alterRenameRe detects "ALTER TABLE ... RENAME TO ...", a table rename the
	// create/drop scan cannot follow — surfaced as a fail-closed caveat.
	alterRenameRe = regexp.MustCompile(`(?is)\bALTER\s+TABLE\b.*?\bRENAME\s+TO\b`)
)

// stripSQL removes SQL line comments (-- … EOL), block comments (/* … */),
// single-quoted string literals (honoring ” escapes), and PostgreSQL
// dollar-quoted bodies ($tag$ … $tag$), replacing each with a space. Without this
// the DDL regexes would match a CREATE/DROP that appears inside a comment or a
// literal — a phantom CREATE masks real drift (a false "no drift", the unsound
// direction), a phantom DROP manufactures it. A dollar-quoted body is a string
// literal too (typically a plpgsql function body): a `CREATE TABLE` inside it is
// the function's runtime behavior, not migration-time DDL, so scanning it as real
// schema would mask drift for a code write to that table (H-12). Double-quoted
// text is KEPT: in SQL "…" is a quoted IDENTIFIER (a table name), not a string
// literal, so stripping it would lose real table names.
func stripSQL(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "--"):
			if j := strings.IndexByte(s[i:], '\n'); j >= 0 {
				i += j // keep the newline (written on the next iteration)
			} else {
				i = len(s)
			}
			b.WriteByte(' ')
		case strings.HasPrefix(s[i:], "/*"):
			if j := strings.Index(s[i+2:], "*/"); j >= 0 {
				i += j + 4
			} else {
				i = len(s)
			}
			b.WriteByte(' ')
		case s[i] == '\'':
			i++
			for i < len(s) {
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			b.WriteByte(' ')
		case s[i] == '"':
			// Double-quoted identifier: KEEP it (a table name), but consume it as a
			// UNIT so an embedded '$', '\'', '--' or '/*' inside the quoted name is not
			// mis-lexed as a dollar quote / literal / comment — which would swallow the
			// closing quote and any following real DDL, masking drift. A doubled "" is
			// an escaped quote inside the identifier.
			b.WriteByte(s[i])
			i++
			for i < len(s) {
				if s[i] == '"' {
					b.WriteByte(s[i])
					i++
					if i < len(s) && s[i] == '"' {
						b.WriteByte(s[i])
						i++
						continue
					}
					break
				}
				b.WriteByte(s[i])
				i++
			}
		case s[i] == '$':
			// A '$' that continues an identifier (previous byte is an identifier byte —
			// PostgreSQL allows '$' inside identifiers, e.g. a$b$c) is NOT a dollar-quote
			// opener. Treating it as one would read a legal identifier as a dollar body
			// and swallow following DDL (a false "no drift"), so only attempt the
			// dollar-quote scan at a token boundary.
			if i == 0 || !isIdentByte(s[i-1]) {
				if d := dollarQuoteDelim(s, i); d > 0 {
					delim := s[i : i+d]
					if k := strings.Index(s[i+d:], delim); k >= 0 {
						i += d + k + d // consume body AND the closing delimiter
					} else {
						i = len(s) // unterminated body: consume the rest, never leak it (fail closed)
					}
					b.WriteByte(' ')
					continue
				}
			}
			b.WriteByte(s[i]) // a bare '$' (a $1 placeholder or an identifier's '$'), not a dollar quote
			i++
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// isIdentByte reports whether c can appear inside an SQL identifier. PostgreSQL
// permits '$' (never as the first character), which is why a '$' following one of
// these is an identifier continuation, not a dollar-quote delimiter.
func isIdentByte(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// dollarQuoteDelim returns the byte length of a PostgreSQL dollar-quote opening
// delimiter beginning at s[i] ("$$" → 2, "$body$" → 6), or 0 if s[i] does not
// open one. The tag between the two '$' follows the unquoted-identifier rule
// (letters, digits, underscores; never starting with a digit) and may be empty,
// so a "$1" bind placeholder — digit after the first '$' — is correctly NOT a
// delimiter.
func dollarQuoteDelim(s string, i int) int {
	if i >= len(s) || s[i] != '$' {
		return 0
	}
	for j := i + 1; j < len(s); j++ {
		c := s[j]
		switch {
		case c == '$':
			return j - i + 1
		case c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
			// tag byte
		case c >= '0' && c <= '9':
			if j == i+1 {
				return 0 // a tag cannot start with a digit ($1 is a placeholder)
			}
		default:
			return 0
		}
	}
	return 0
}

// scanDDL returns the CREATE/DROP table operations in one migration's SQL, in
// textual order, so the replay nets create-then-drop correctly within a file. It
// strips comments/literals first and scans per ';'-delimited statement so a DROP
// list cannot swallow a following statement.
func scanDDL(sqlText string) []ddlOp {
	var out []ddlOp
	for _, stmt := range splitStatements(stripSQL(sqlText)) {
		out = append(out, scanStatement(stmt)...)
	}
	return out
}

// splitStatements splits stripped SQL into ';'-delimited statements, treating a
// semicolon INSIDE a double-quoted identifier as part of the name. stripSQL has
// already removed comments, single-quoted literals, AND dollar-quoted bodies
// ($tag$…$tag$) — so a ';' inside a plpgsql function body can never reach here to
// split a CREATE FUNCTION statement mid-body — leaving double quotes as the only
// quoting left, and they are KEPT because "..." is a table name, not a literal.
// A plain strings.Split on ';' would break a quoted identifier like "weird;name"
// across two statements, dropping the CREATE/DROP (false drift) — defeating the very
// reason stripSQL preserves double quotes.
func splitStatements(s string) []string {
	var stmts []string
	var b strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote // adjacent "" (escaped quote) toggles off-then-on; no ';' can sit between them
			b.WriteByte(c)
		case c == ';' && !inQuote:
			stmts = append(stmts, b.String())
			b.Reset()
		default:
			b.WriteByte(c)
		}
	}
	if b.Len() > 0 {
		stmts = append(stmts, b.String())
	}
	return stmts
}

// scanStatement recovers the CREATE/DROP table ops within one statement, in
// textual order.
func scanStatement(stmt string) []ddlOp {
	type positioned struct {
		pos int
		op  ddlOp
	}
	var ms []positioned
	for _, idx := range createRe.FindAllStringSubmatchIndex(stmt, -1) {
		name := cleanName(stmt[idx[2]:idx[3]])
		if name != "" {
			ms = append(ms, positioned{pos: idx[0], op: ddlOp{create: true, name: name}})
		}
	}
	for _, idx := range dropRe.FindAllStringSubmatchIndex(stmt, -1) {
		for _, name := range splitDropList(stmt[idx[2]:idx[3]]) {
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
// it is preferable to silently producing (or hiding) drift. It scans stripSQL output
// (not raw SQL) so a RENAME inside a comment or string literal does not raise a
// spurious caveat — consistent with scanDDL.
func renameCaveat(f MigrationFile) string {
	if alterRenameRe.MatchString(stripSQL(f.SQL)) {
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
