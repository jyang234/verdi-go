// Package store mirrors the field fleet's constant-fragment SQL builder (field
// report §18): every query is assembled by a tiny fluent sqlWriter that launders
// a compile-time-constant statement through a strings.Builder, so the verb is a
// string constant at the call site yet the value reaching database/sql is a
// non-constant string. It exists to exercise the const-accumulation fold
// (internal/static/sqlfold): the labeler cannot fold these at the call site, but
// the fold can recover the verb one accumulator-hop back.
//
// The methods deliberately span every fold outcome:
//
//   - GetMessage  : SELECT, all-placeholder tail            → READ (whole stmt constant)
//   - CreateMessage: INSERT … RETURNING via QueryRowContext → WRITE (the F-B case)
//   - DeleteByTable: "DELETE FROM " + table (dynamic table) → WRITE, <dynamic> table
//   - UpdatePartial: branched SET-list                      → WRITE (verb unconditional)
//   - ExecOpaque  : verb itself is a runtime value          → ABSTAIN (fail closed)
package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// sqlWriter is the fleet's builder, verbatim in shape: Write appends a constant
// fragment, Arg appends a `$N` placeholder (the value is bound, never inlined),
// and Build returns the accumulated — runtime — string.
type sqlWriter struct {
	sb   strings.Builder
	args []any
}

func newSQLWriter() *sqlWriter { return &sqlWriter{} }

func (w *sqlWriter) Write(s string) *sqlWriter { w.sb.WriteString(s); return w }

func (w *sqlWriter) Arg(v any) *sqlWriter {
	w.args = append(w.args, v)
	w.sb.WriteByte('$')
	w.sb.WriteString(strconv.Itoa(len(w.args)))
	return w
}

func (w *sqlWriter) Build() (string, []any) { return w.sb.String(), w.args }

// messageColumns is a const column-list, so "SELECT " + messageColumns + " FROM
// …" const-folds to a single string at the call site (§18): the fold reads the
// first Write argument as one *ssa.Const.
const messageColumns = "id, body, created_at"

// Store persists messages. A nil *sql.DB is fine for static analysis.
type Store struct {
	db *sql.DB
}

// New returns a Store backed by db.
func New(db *sql.DB) *Store { return &Store{db: db} }

// GetMessage is a READ: the whole statement is constant text plus a bound
// placeholder, so the fold can soundly classify it SELECT.
func (s *Store) GetMessage(ctx context.Context, id string) error {
	w := newSQLWriter()
	w.Write("SELECT " + messageColumns + " FROM messages WHERE id = ").Arg(id)
	query, args := w.Build()
	_ = s.db.QueryRowContext(ctx, query, args...)
	return nil
}

// CreateMessage is a WRITE that rides QueryRowContext (INSERT … RETURNING). The
// method name says "read"; only the verb off the statement says "write" — the
// F-B case the fold vindicates.
func (s *Store) CreateMessage(ctx context.Context, body string) error {
	w := newSQLWriter()
	w.Write("INSERT INTO messages (body) VALUES (").Arg(body).Write(") RETURNING id")
	query, args := w.Build()
	_ = s.db.QueryRowContext(ctx, query, args...)
	return nil
}

// DeleteByTable interpolates a runtime table name. The verb DELETE is in the
// constant leading fragment, so the fold promotes it to a WRITE; only the table
// stays <dynamic>.
func (s *Store) DeleteByTable(ctx context.Context, table, id string) error {
	w := newSQLWriter()
	w.Write("DELETE FROM " + table + " WHERE id = ").Arg(id)
	query, args := w.Build()
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// UpdatePartial builds the SET-list conditionally. The verb+table fragment
// ("UPDATE accounts SET ") is written unconditionally before any branch, so the
// fold promotes it to a WRITE; the variable tail is irrelevant to that.
func (s *Store) UpdatePartial(ctx context.Context, id, name, email string) error {
	w := newSQLWriter()
	w.Write("UPDATE accounts SET ")
	first := true
	if name != "" {
		w.Write("name = ").Arg(name)
		first = false
	}
	if email != "" {
		if !first {
			w.Write(", ")
		}
		w.Write("email = ").Arg(email)
	}
	w.Write(" WHERE id = ").Arg(id)
	query, args := w.Build()
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

// ReadDynamicFilter splices a runtime column name into a SELECT — a dynamic TEXT
// tail (not a bound placeholder). The verb is SELECT, but the fold must NOT
// classify it as a read: an unconstrained text splice could carry a second,
// mutating statement ("… ; DELETE …"), so the read direction stays closed and the
// effect stays opaque. This is the prime-directive guard against multi-statement
// smuggling.
func (s *Store) ReadDynamicFilter(ctx context.Context, col, val string) error {
	w := newSQLWriter()
	w.Write("SELECT id FROM messages WHERE ").Write(col).Write(" = ").Arg(val)
	query, args := w.Build()
	_, err := s.db.QueryContext(ctx, query, args...)
	return err
}

// ExecOpaque takes the verb itself as a runtime value: the leading fragment is a
// hole, so there is no constant verb to read. The fold must ABSTAIN and leave the
// effect opaque (fail closed).
func (s *Store) ExecOpaque(ctx context.Context, verb, table string) error {
	w := newSQLWriter()
	w.Write(verb).Write(" FROM ").Write(table)
	query, args := w.Build()
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}
