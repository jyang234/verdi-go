// Package store is the fixture's persistence layer over database/sql (a built-in
// DB classification hint). Its operations are DB boundary edges: present in the
// non-gated call graph but deliberately excluded from the gated inter-service
// boundary contract, because the database is the service's own store.
package store

import (
	"context"
	"database/sql"
)

// Applicant is a row read from the applicants table.
type Applicant struct {
	Name   string
	Income int
}

// Loan is a row read from the loans table.
type Loan struct {
	ID     string
	Status string
}

// Loans persists loan-origination state. A nil *sql.DB is acceptable for static
// analysis (the methods are never executed); a real driver is wired in for the
// behavioral harness.
type Loans struct {
	db *sql.DB
}

// New returns a Loans backed by db.
func New(db *sql.DB) *Loans { return &Loans{db: db} }

// SelectApplicant reads one applicant by id (a DB read effect).
func (l *Loans) SelectApplicant(ctx context.Context, id string, out *Applicant) error {
	row := l.db.QueryRowContext(ctx, "SELECT name, income FROM applicants WHERE id = $1", id)
	return row.Scan(&out.Name, &out.Income)
}

// SelectLoan reads one loan by id (a DB read effect).
func (l *Loans) SelectLoan(ctx context.Context, id string, out *Loan) error {
	row := l.db.QueryRowContext(ctx, "SELECT id, status FROM loans WHERE id = $1", id)
	return row.Scan(&out.ID, &out.Status)
}

// InsertLedger records a disbursement (a DB mutate effect).
func (l *Loans) InsertLedger(ctx context.Context, loanID string, amount int) error {
	_, err := l.db.ExecContext(ctx, "INSERT INTO ledger (loan_id, amount) VALUES ($1, $2)", loanID, amount)
	return err
}

// InsertAudit appends an audit record (a DB mutate effect, written off the
// fire-and-forget goroutine).
func (l *Loans) InsertAudit(ctx context.Context, loanID string) error {
	_, err := l.db.ExecContext(ctx, "INSERT INTO audit_log (loan_id) VALUES ($1)", loanID)
	return err
}

// MarkPaid flips a loan to settled (a DB mutate effect, driven by the consumer).
func (l *Loans) MarkPaid(ctx context.Context, loanID string) error {
	_, err := l.db.ExecContext(ctx, "UPDATE loans SET status = 'paid' WHERE id = $1", loanID)
	return err
}
