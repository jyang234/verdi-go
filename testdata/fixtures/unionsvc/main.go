// Command unionsvc is the de-union (--rebind) fixture. Two commands hand their OWN
// writing closure to one shared runner, u.RunInTx(ctx, fn). Because the call graph is
// context-insensitive, the single fn(exec) site inside RunInTx resolves to EVERY closure
// that flows to it — so CmdA→RunInTx→{A$1,B$1} and CmdA appears to issue CmdB's write
// (the over-report union). The escape-guarded de-union pass (--rebind) ADDs each
// command's own enclosing→closure edge and REMOVEs the shared RunInTx→closure union
// edges, so each command reaches only its OWN write.
//
// leakCmd is the ADVERSARIAL case: its closure is passed BOTH directly to RunInTx AND to
// a helper that also invokes it, so the closure ESCAPES its parent. De-unioning it would
// drop a real path (a false absence); the confinement guard must keep its union.
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

// Store holds the per-command writes — each a distinct INSERT so the over-report (CmdA
// reaching b, CmdB reaching a) is observable by table.
type Store struct{ db *sql.DB }

func (s *Store) writeA(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO table_a (id) VALUES ($1)", "1")
	return err
}

func (s *Store) writeB(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO table_b (id) VALUES ($1)", "1")
	return err
}

func (s *Store) writeLeak(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO table_leak (id) VALUES ($1)", "1")
	return err
}

// CmdA / CmdB are the CONFINED commands: each closure's only use in its parent is the
// RunInTx argument, so de-union is sound.
type CmdA struct {
	u  *UnitOfWork
	st *Store
}

func (c *CmdA) Handle(ctx context.Context) error {
	return c.u.RunInTx(ctx, func(e *Exec) error { return c.st.writeA(ctx) })
}

type CmdB struct {
	u  *UnitOfWork
	st *Store
}

func (c *CmdB) Handle(ctx context.Context) error {
	return c.u.RunInTx(ctx, func(e *Exec) error { return c.st.writeB(ctx) })
}

// invoke is a helper that ALSO invokes the closure handed to it — a second invocation
// path for leakCmd's closure.
func invoke(fn func(*Exec) error) error { return fn(&Exec{}) }

// LeakCmd hands its closure to BOTH RunInTx and the helper invoke — the closure escapes
// its parent (a non-RunInTx use), so the confinement guard must KEEP its union edge.
type LeakCmd struct {
	u  *UnitOfWork
	st *Store
}

func (c *LeakCmd) Handle(ctx context.Context) error {
	fn := func(e *Exec) error { return c.st.writeLeak(ctx) }
	_ = invoke(fn)
	return c.u.RunInTx(ctx, fn)
}

func main() {
	db, _ := sql.Open("postgres", "")
	u := &UnitOfWork{db: db}
	st := &Store{db: db}
	ctx := context.Background()
	_ = (&CmdA{u: u, st: st}).Handle(ctx)
	_ = (&CmdB{u: u, st: st}).Handle(ctx)
	_ = (&LeakCmd{u: u, st: st}).Handle(ctx)
}
