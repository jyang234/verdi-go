// Package admin is an internal maintenance sub-app mounted on the custom router.
// It models two patterns common in real services that make static attribution
// hard: a FACTORY that builds handlers over injected dependencies, and a DYNAMIC
// ROUTE TABLE assembled at runtime rather than spelled out as literal
// registration calls. The handlers are named method values (not $N closures), so
// they draw no severed-closure frontier marker; mounted through the custom
// (unhinted) router, they are missed roots. PurgeLedger reaches a real DB DELETE.
package admin

import (
	"context"
	"net/http"

	"example.com/impeachsvc/internal/router"
	"example.com/impeachsvc/internal/store"
)

// Admin holds the dependencies the factory closes over.
type Admin struct {
	loans *store.Loans
}

// New builds the admin sub-app over the shared store.
func New(loans *store.Loans) *Admin { return &Admin{loans: loans} }

// route pairs a runtime-built pattern with its handler — the dynamic route table.
type route struct {
	method, pattern string
	handler         http.HandlerFunc
}

// table is the FACTORY: it assembles the admin routes at runtime from the
// injected store. The patterns are built here (not literals at a registration
// call site), and the handlers are method values bound to this Admin.
func (a *Admin) table() []route {
	base := "/admin"
	return []route{
		{"DELETE", base + "/ledger", a.PurgeLedger},
		{"POST", base + "/reindex", a.Reindex},
	}
}

// Mount registers every dynamically-built route on the custom router. Because
// router.Router.Add is not a recognized registrar, none of these become a
// discovered entrypoint.
func (a *Admin) Mount(r *router.Router) {
	for _, rt := range a.table() {
		r.Add(rt.method, rt.pattern, rt.handler)
	}
}

// PurgeLedger wipes the ledger — a DB DELETE reachable only through this missed
// route. This is the effect the impeachment cell must catch: statically owned by
// no discovered entrypoint, behaviorally reached on DELETE /admin/ledger.
func (a *Admin) PurgeLedger(w http.ResponseWriter, req *http.Request) {
	id := req.URL.Query().Get("loan")
	if id == "" {
		id = "ALL"
	}
	_ = a.loans.Purge(req.Context(), id)
	w.WriteHeader(http.StatusNoContent)
}

// Reindex is a benign second admin route, so the missed-root pattern is not a
// single special case but a genuine sub-app surface.
func (a *Admin) Reindex(w http.ResponseWriter, req *http.Request) {
	_ = context.Canceled // no boundary effect; exercises the table's second entry
	w.WriteHeader(http.StatusAccepted)
}
