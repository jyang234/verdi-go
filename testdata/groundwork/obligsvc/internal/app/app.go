// Package app holds one function per obligation verdict — the fixture shapes
// the path-obligations plan's table commits to.
package app

import (
	"example.com/obligsvc/internal/audit"
	"example.com/obligsvc/internal/billing"
	"example.com/obligsvc/internal/bus"
	"example.com/obligsvc/internal/gate"
	"example.com/obligsvc/internal/outbound"
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

// handle recovers in a deferred NAMED function: control can rejoin invisibly,
// so the analysis must abstain (CANT-PROVE).
func handle() { _ = recover() }

// TransferRecoverNamed abstains: recover via a deferred named function.
func TransferRecoverNamed(s *store.Store) error {
	defer handle()
	tx, err := s.BeginTx()
	if err != nil {
		return err
	}
	return tx.Commit()
}

// TransferClosure is the errcheck-clean cleanup idiom: the deferred closure
// releasing the captured tx is in-frame and credited (SATISFIED).
func TransferClosure(s *store.Store) error {
	tx, err := s.BeginTx()
	if err != nil {
		return err
	}
	defer func() { tx.Rollback() }()
	return tx.Commit()
}

// TransferAnnotate has a named result captured by an annotating defer: the
// failure branch must still be recognized through the alloc/load web
// (SATISFIED).
func TransferAnnotate(s *store.Store) (err error) {
	defer func() { err = annotate(err) }()
	tx, err := s.BeginTx()
	if err != nil {
		return err
	}
	defer func() { tx.Rollback() }()
	return tx.Commit()
}

func annotate(err error) error { return err }

// TransferConcrete acquires with a concrete error type (*store.TxError): it is
// the err, not the resource, and its failure branch prunes (SATISFIED).
func TransferConcrete(s *store.Store) error {
	tx, terr := s.BeginTxC()
	if terr != nil {
		return terr
	}
	defer func() { tx.Rollback() }()
	return tx.Commit()
}

// HoldSem is the single-result error acquire: the failure branch must prune
// even with no tuple Extract (SATISFIED).
func HoldSem(s *store.Store) error {
	if err := s.Acquire(); err != nil {
		return err
	}
	defer s.Release()
	return nil
}

// DeferredPublish publishes via defer with no audit anywhere: the deferred B
// still happens and still needs its A (VIOLATED, and the rule must not read
// as inert).
func DeferredPublish() { defer bus.Publish("loan.approved") }

// DeferredPublishAudited: the audit dominates the defer registration
// (SATISFIED).
func DeferredPublishAudited() {
	audit.Write("loan.approved")
	defer bus.Publish("loan.approved")
}

// DisburseAndCharge is the IT-3 disburse scenario: the publish DOMINATES the
// fallible charge, so a fault at the charge means loan.approved is CERTAINLY
// already committed — approved-but-uncharged loans.
func DisburseAndCharge(id string) error {
	audit.Write("loan.approved")
	bus.Publish("loan.approved")
	return billing.Charge(id)
}

// DisburseAndChargeRisky publishes on one arm only: the publish CAN precede
// the charge but does not dominate it (possibly-committed, not certainly).
func DisburseAndChargeRisky(id string, approved bool) error {
	audit.Write("loan.approved")
	if approved {
		bus.Publish("loan.approved")
	}
	return billing.Charge(id)
}

// sendFanout holds its Before sites one frame below the validation — the
// doPublish→publishWithFanout split the fromCallers lift exists for. Every
// entry is require-dominated (SATISFIED via entry domination, CX-2).
func sendFanout() {
	outbound.Send("loan.approved.v1")
	outbound.Send("loan.approved.v2")
}

// DispatchSend is the guard intent: validate, then fan out.
func DispatchSend() error {
	if err := gate.Validate(); err != nil {
		return err
	}
	sendFanout()
	return nil
}

// sendFanoutOpen is entered only via OpenSend, which never validates. The
// lift has no NEVER pole (out-of-unit or init callers could exist), so the
// rule author's opt-in turns the undominated B into a disclosed abstention
// (CANT-PROVE naming the unproven entry), never a borrowed witness.
func sendFanoutOpen() { outbound.Send("loan.approved.v1") }

// OpenSend skips the validation.
func OpenSend() { sendFanoutOpen() }

// sendFanoutTaken's address is taken (sendHook): an unseen dynamic caller may
// exist, so its entries are beyond proof (CANT-PROVE).
func sendFanoutTaken() { outbound.Send("loan.approved.v1") }

var sendHook = sendFanoutTaken

// CallTaken keeps sendFanoutTaken reachable alongside the taken address.
func CallTaken() { sendFanoutTaken() }

// publishApproved audits and publishes on its every path: an ALWAYS-effect
// (and ALWAYS-require) helper. Callers inherit a derived effect site (CX-3).
func publishApproved() {
	audit.Write("loan.approved")
	bus.Publish("loan.approved")
}

// DisburseViaHelper is the field's same-function miss, fixed: the publish is
// one frame down, the charge can still fault — the fault card needs "the
// publish already happened", which only the derived site can give it.
func DisburseViaHelper(id string) error {
	publishApproved()
	return billing.Charge(id)
}
