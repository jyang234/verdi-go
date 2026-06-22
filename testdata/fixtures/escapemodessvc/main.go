// Command escapemodessvc is the adversarial guard for the rebind de-union pass. Every
// command hands its closure to the shared runner u.RunInTx (so the direct-invoker
// predicate fires and a runner→closure union edge exists) BUT also lets the closure
// ESCAPE its parent a different way: stored into a field, returned, sent on a channel,
// or captured into another closure. Because an escaped closure may be invoked on another
// path, REMOVING its union edge would be unsound (a false absence). The confinement
// guard must therefore ABSTAIN on every command here — the rebind plan over this service
// must be EMPTY (no add, no remove). The confined positive case lives in unionsvc.
//
// Nothing is executed under static analysis.
package main

import (
	"context"
	"database/sql"
)

// Exec is the in-transaction handle.
type Exec struct{ db *sql.DB }

// UnitOfWork is the shared runner: RunInTx invokes its fn parameter directly.
type UnitOfWork struct{ db *sql.DB }

func (u *UnitOfWork) RunInTx(ctx context.Context, fn func(*Exec) error) error {
	e := &Exec{db: u.db}
	return fn(e)
}

// Store holds the per-command write.
type Store struct{ db *sql.DB }

func (s *Store) write(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO t (id) VALUES ($1)", "1")
	return err
}

// storeCmd: the closure ESCAPES by being stored into a struct field.
type storeCmd struct {
	u     *UnitOfWork
	st    *Store
	saved func(*Exec) error
}

func (c *storeCmd) Handle(ctx context.Context) error {
	fn := func(e *Exec) error { return c.st.write(ctx) }
	c.saved = fn // escape: store
	return c.u.RunInTx(ctx, fn)
}

// returnCmd: the closure ESCAPES by being returned to the caller.
type returnCmd struct {
	u  *UnitOfWork
	st *Store
}

func (c *returnCmd) Handle(ctx context.Context) (func(*Exec) error, error) {
	fn := func(e *Exec) error { return c.st.write(ctx) }
	err := c.u.RunInTx(ctx, fn)
	return fn, err // escape: return
}

// chanCmd: the closure ESCAPES by being sent on a channel.
type chanCmd struct {
	u  *UnitOfWork
	st *Store
	ch chan func(*Exec) error
}

func (c *chanCmd) Handle(ctx context.Context) error {
	fn := func(e *Exec) error { return c.st.write(ctx) }
	c.ch <- fn // escape: channel send
	return c.u.RunInTx(ctx, fn)
}

// captureCmd: the closure ESCAPES by being captured into another closure.
type captureCmd struct {
	u  *UnitOfWork
	st *Store
}

func (c *captureCmd) Handle(ctx context.Context) error {
	fn := func(e *Exec) error { return c.st.write(ctx) }
	wrap := func() error { return fn(&Exec{}) } // escape: captured into another closure
	_ = wrap
	return c.u.RunInTx(ctx, fn)
}

func main() {
	db, _ := sql.Open("postgres", "")
	u := &UnitOfWork{db: db}
	st := &Store{db: db}
	ctx := context.Background()
	_ = (&storeCmd{u: u, st: st}).Handle(ctx)
	_, _ = (&returnCmd{u: u, st: st}).Handle(ctx)
	_ = (&chanCmd{u: u, st: st, ch: make(chan func(*Exec) error, 1)}).Handle(ctx)
	_ = (&captureCmd{u: u, st: st}).Handle(ctx)
}
