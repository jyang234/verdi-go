// Package scoring resolves an applicant's credit score behind an interface with
// two implementations. It exists so the call graph exercises interface dispatch:
// because Select constructs both *Remote and *Stub, RTA marks both as runtime
// types and resolves the interface call in origination.Evaluate to two callees.
package scoring

import (
	"context"

	"example.com/loansvc/internal/client"
)

// Score is the resolved credit score.
type Score struct {
	Value int
}

// Scorer abstracts credit scoring so the evaluator does not depend on a concrete
// source. It has two implementations in this package.
type Scorer interface {
	Score(ctx context.Context, id string, out *Score) error
}

// Remote scores against the live credit-bureau peer.
type Remote struct {
	bureau *client.Bureau
}

// Score delegates to the credit-bureau client.
func (r *Remote) Score(ctx context.Context, id string, out *Score) error {
	v, err := r.bureau.Score(ctx, id)
	if err != nil {
		return err
	}
	out.Value = v
	return nil
}

// Stub returns a fixed passing score, used when the bureau is disabled.
type Stub struct{}

// Score returns a constant score without any I/O.
func (s *Stub) Score(_ context.Context, _ string, out *Score) error {
	out.Value = 700
	return nil
}

// Select chooses an implementation. It mentions both concrete types, so static
// analysis sees both as runtime types and the interface call resolves to both.
func Select(degraded bool, bureau *client.Bureau) Scorer {
	if degraded {
		return &Stub{}
	}
	return &Remote{bureau: bureau}
}
