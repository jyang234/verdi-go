// Package consumer holds the fixture's bus consumers. OnSettled is registered via
// (*eventbus.Bus).Subscribe, so the static pipeline resolves it as a synthetic
// consumer root and records payment.settled as a consumed event in the boundary
// contract.
package consumer

import (
	"context"

	"example.com/loansvc/internal/store"
)

// Payments consumes payment lifecycle events.
type Payments struct {
	loans *store.Loans
}

// New returns a Payments consumer.
func New(loans *store.Loans) *Payments { return &Payments{loans: loans} }

// OnSettled consumes payment.settled and marks the loan paid. Its signature is
// the eventbus.Handler shape the registrar expects.
func (p *Payments) OnSettled(ctx context.Context, payload []byte) error {
	return p.loans.MarkPaid(ctx, string(payload))
}
