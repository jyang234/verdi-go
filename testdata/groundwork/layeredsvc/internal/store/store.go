// Package store is layeredsvc's persistence layer over database/sql (a built-in
// DB classification hint). Its methods are the service's only DB boundary edges.
// Nothing above the app layer is permitted to call into store directly — that
// invariant is exactly what the groundwork layering fitness function guards.
package store

import (
	"context"
	"database/sql"
)

// User is one row of the users table.
type User struct {
	ID   string
	Name string
}

// Store persists users. A nil *sql.DB is fine for static analysis; the methods
// are never executed by the graph pipeline.
type Store struct {
	db *sql.DB
}

// New returns a Store backed by db.
func New(db *sql.DB) *Store { return &Store{db: db} }

// SelectUser reads one user by id (a DB read effect).
func (s *Store) SelectUser(ctx context.Context, id string, out *User) error {
	const q = "SELECT id, name FROM users WHERE id = $1"
	row := s.db.QueryRowContext(ctx, q, id)
	return row.Scan(&out.ID, &out.Name)
}

// UpdateUser renames a user (a DB mutate effect).
func (s *Store) UpdateUser(ctx context.Context, id, name string) error {
	const q = "UPDATE users SET name = $2 WHERE id = $1"
	_, err := s.db.ExecContext(ctx, q, id, name)
	return err
}

// InsertAudit appends an audit record (a DB mutate effect).
func (s *Store) InsertAudit(ctx context.Context, id string) error {
	const q = "INSERT INTO audit_log (user_id) VALUES ($1)"
	_, err := s.db.ExecContext(ctx, q, id)
	return err
}
