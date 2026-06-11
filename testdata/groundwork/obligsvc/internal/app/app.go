// Package app holds one function per obligation verdict — the fixture shapes
// the path-obligations plan's table commits to.
package app

import (
	"example.com/obligsvc/internal/audit"
	"example.com/obligsvc/internal/bus"
	"example.com/obligsvc/internal/store"
)

func debit(t *store.Tx) error  { return nil }
func credit(t *store.Tx) error { return nil }

// Transfer leaks: the debit-failure return has no release (VIOLATED).
func Transfer(s *store.Store) error {
	tx, err := s.BeginTx()
	if err != nil {
		return err
	}
	if err := debit(tx); err != nil {
		return err
	}
	if err := credit(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// TransferDefer is covered on every exit by the deferred rollback (SATISFIED).
func TransferDefer(s *store.Store) error {
	tx, err := s.BeginTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := debit(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// TransferOwn returns the open transaction: ownership leaves the function
// (CANT-PROVE).
func TransferOwn(s *store.Store) (*store.Tx, error) {
	tx, err := s.BeginTx()
	if err != nil {
		return nil, err
	}
	return tx, nil
}

// Disburse audits before publishing on every path (SATISFIED).
func Disburse(approved bool) {
	audit.Write("loan.approved")
	if approved {
		bus.Publish("loan.approved")
	}
}

// DisburseRacy publishes on a path that skipped the audit (VIOLATED).
func DisburseRacy(approved bool) {
	if approved {
		audit.Write("loan.approved")
	}
	bus.Publish("loan.approved")
}
