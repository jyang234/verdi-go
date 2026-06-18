// Package store mirrors the §19 Tier-B/C residue: the persistence layer routes
// every database/sql round-trip through a single PASS-THROUGH helper that takes the
// statement as a `query string` PARAMETER and forwards it unmodified to the sink.
// The verb is therefore invisible at the sink (it is the same opaque parameter for
// every caller); it lives one call-hop up, at each caller that built the statement
// with the constant-fragment sqlWriter. The Tier-B/C reclaimer re-attributes the
// effect to those callers, where the SQL is recoverable.
//
// The callers deliberately span the outcomes:
//
//   - DeleteEventType        : constant DELETE             → caller WRITE, event_types
//   - InsertEventTypeVersion : constant INSERT             → caller WRITE, event_type_versions
//   - UpdateEventTypeVersion : constant UPDATE             → caller WRITE, event_type_versions
//     (Insert/Update prove PER-CALLER re-attribution: one helper, different verbs.)
//   - DeleteParticipant      : "DELETE FROM "+s.table      → Tier C: WRITE, {publishers, subscribers}
//   - ExecRaw                : dynamic verb                → ABSTAIN: opaque ExecContext re-homed at the caller
package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// sqlWriter is the constant-fragment builder (verbatim shape from the fleet): Write
// appends a constant fragment, Arg appends a `$N` placeholder, Build returns the
// accumulated — runtime — string.
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

// execByID is the PASS-THROUGH helper: it forwards the `query` parameter unmodified
// to the database/sql sink. The sink's query argument IS the bare parameter, so the
// helper carries no recoverable verb of its own — the re-attribution moves the
// effect to each caller, where the statement was built.
func execByID(ctx context.Context, db *sql.DB, query string, args []any, opName string) error {
	_, err := db.ExecContext(ctx, query, args...)
	_ = opName // a non-SQL parameter the helper also takes; never reaches the sink
	return err
}

// Store persists event types. A nil *sql.DB is fine for static analysis.
type Store struct {
	db *sql.DB
}

// New returns a Store backed by db.
func New(db *sql.DB) *Store { return &Store{db: db} }

// DeleteEventType builds a fully-constant DELETE and forwards it through the
// pass-through helper. Tier B: the effect re-homes at this caller as a WRITE to
// event_types.
func (s *Store) DeleteEventType(ctx context.Context, id string) error {
	w := newSQLWriter()
	w.Write("DELETE FROM event_types WHERE id = ").Arg(id)
	query, args := w.Build()
	return execByID(ctx, s.db, query, args, "delete event type")
}

// InsertEventTypeVersion forwards a constant INSERT through the SAME helper as
// UpdateEventTypeVersion — proof that re-attribution is per-caller: one helper edge
// must not be relabeled with a single verb, since its callers carry different ones.
func (s *Store) InsertEventTypeVersion(ctx context.Context, id, schema string) error {
	w := newSQLWriter()
	w.Write("INSERT INTO event_type_versions (event_type_id, schema) VALUES (").Arg(id).Write(", ").Arg(schema).Write(")")
	query, args := w.Build()
	return execByID(ctx, s.db, query, args, "insert version")
}

// UpdateEventTypeVersion forwards a constant UPDATE through the same helper.
func (s *Store) UpdateEventTypeVersion(ctx context.Context, id string) error {
	w := newSQLWriter()
	w.Write("UPDATE event_type_versions SET current_version = current_version + 1 WHERE event_type_id = ").Arg(id)
	query, args := w.Build()
	return execByID(ctx, s.db, query, args, "bump version")
}

// ExecRaw builds a statement whose VERB is a runtime value, then forwards it through
// the helper. The fold abstains at the caller, so the effect re-homes here as an
// opaque ExecContext — never dropped. This is the soundness guard: re-attribution
// preserves an unrecoverable effect rather than hiding it.
func (s *Store) ExecRaw(ctx context.Context, verb, table string) error {
	w := newSQLWriter()
	w.Write(verb).Write(" FROM ").Write(table)
	query, args := w.Build()
	return execByID(ctx, s.db, query, args, "raw")
}

// participantStore is the per-table store: the table name is a struct field set to
// one of a finite set of string CONSTANTS at construction. Its DeleteParticipant
// routes through the SAME pass-through helper — so naming the table needs the
// finite-value-set resolution to compose with the helper hop (Tier C).
type participantStore struct {
	db    *sql.DB
	table string
}

// NewPublisherStore and NewSubscriberStore are the only two constructors: the table
// field only ever holds these two constants.
func NewPublisherStore(db *sql.DB) *participantStore {
	return &participantStore{db: db, table: "publishers"}
}

func NewSubscriberStore(db *sql.DB) *participantStore {
	return &participantStore{db: db, table: "subscribers"}
}

// DeleteParticipant interpolates the per-store table into a DELETE, then forwards it
// through the pass-through helper. Tier C: the verb const-folds (a write) and the
// table resolves to the finite set {publishers, subscribers} — recovered at this
// caller, where the table field is visible (it is invisible through the helper's
// `query` parameter).
func (p *participantStore) DeleteParticipant(ctx context.Context, id string) error {
	w := newSQLWriter()
	w.Write("DELETE FROM " + p.table + " WHERE id = ").Arg(id)
	query, args := w.Build()
	return execByID(ctx, p.db, query, args, "delete participant")
}
