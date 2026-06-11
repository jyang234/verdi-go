// Package store is the fixture's resource lifecycle: BeginTx acquires, Commit
// and Rollback release.
package store

// Tx is an open transaction.
type Tx struct{ closed bool }

// Store hands out transactions.
type Store struct{}

// BeginTx acquires a transaction (the obligation's acquire anchor).
func (s *Store) BeginTx() (*Tx, error) { return &Tx{}, nil }

// Commit releases the transaction successfully.
func (t *Tx) Commit() error { t.closed = true; return nil }

// Rollback releases the transaction on failure.
func (t *Tx) Rollback() { t.closed = true }
