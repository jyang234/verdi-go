// Package handler holds the service's PUBLIC HTTP handlers — the ones registered
// through net/http's recognized HandleFunc, so root discovery anchors them as
// entrypoints. Create reaches a DB INSERT; that effect is reachable from a
// discovered route and so is CONFIRMED-LIVE when a flow exercises it — the
// fixture's sound control against which the missed admin route stands out.
package handler

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"example.com/impeachsvc/internal/store"
)

// App serves the public loan API.
type App struct {
	loans *store.Loans
}

// New builds the App over the shared store.
func New(loans *store.Loans) *App { return &App{loans: loans} }

// Create handles POST /loan: it inserts a loan (a DB mutate effect on a
// discovered route).
func (a *App) Create(w http.ResponseWriter, req *http.Request) {
	ctx, span := otel.Tracer("impeachsvc").Start(req.Context(), "create", trace.WithSpanKind(trace.SpanKindInternal))
	defer span.End()
	_ = a.create(ctx)
	w.WriteHeader(http.StatusCreated)
}

func (a *App) create(ctx context.Context) error {
	return a.loans.InsertLoan(ctx, "L1", 5000)
}
