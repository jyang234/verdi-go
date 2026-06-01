// Package handler holds the fixture's HTTP entry points. Create and Status are
// registered on the router as method values, so the static pipeline must resolve
// those func-value arguments to synthetic roots before it can reach the decision
// logic behind them.
package handler

import (
	"encoding/json"
	"net/http"

	"example.com/loansvc/internal/codec"
	"example.com/loansvc/internal/origination"
	"example.com/loansvc/internal/store"
)

// App serves the loan-application endpoints.
type App struct {
	eval  *origination.Evaluator
	loans *store.Loans
}

// New returns an App.
func New(eval *origination.Evaluator, loans *store.Loans) *App {
	return &App{eval: eval, loans: loans}
}

// Create handles POST /loan-application. It decodes the body through the generic
// codec.Decode[origination.Application] — the instantiation the call graph must
// reach — and evaluates the application.
func (a *App) Create(w http.ResponseWriter, r *http.Request) {
	app, err := codec.Decode[origination.Application](r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dec, err := a.eval.Evaluate(r.Context(), app)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(dec)
}

// Status handles GET /loan-application/{id}/status.
func (a *App) Status(w http.ResponseWriter, r *http.Request) {
	var ln store.Loan
	if err := a.loans.SelectLoan(r.Context(), r.PathValue("id"), &ln); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ln)
}
