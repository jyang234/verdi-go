// Package app is layeredsvc's domain layer. It is the only layer permitted to
// call store; the handler layer must go through it. UpdateProfile deliberately
// performs two DB writes (the rename plus an audit insert) so the per-route I/O
// budget fitness function has something to measure.
package app

import (
	"context"

	"example.com/layeredsvc/internal/store"
)

// Service holds the domain logic.
type Service struct {
	store *store.Store
}

// New returns a Service over st.
func New(st *store.Store) *Service { return &Service{store: st} }

// GetProfile reads a user's profile (one DB read).
func (s *Service) GetProfile(ctx context.Context, id string) (store.User, error) {
	var u store.User
	if err := s.store.SelectUser(ctx, id, &u); err != nil {
		return store.User{}, err
	}
	return u, nil
}

// UpdateProfile renames a user and records an audit entry (two DB writes).
func (s *Service) UpdateProfile(ctx context.Context, id, name string) error {
	if err := s.store.UpdateUser(ctx, id, name); err != nil {
		return err
	}
	return s.store.InsertAudit(ctx, id)
}
