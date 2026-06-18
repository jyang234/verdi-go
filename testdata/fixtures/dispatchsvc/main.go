// Command dispatchsvc is an adversarial soundness fixture for the static reachability
// checker. Its admin operations live in a string-keyed handler REGISTRY populated in
// init() — the idiomatic place Go code registers things. Each handler registers a func
// value (address-taken) and Dispatch invokes one by a runtime key: a plain func-value
// indirection with NO reflect/unsafe/cgo/linkname marker.
//
// The probe (internal/static/soundness) measures whether the must_not_reach checker
// silently proves absence past this seam. A directly-reachable decoy (Report→audit)
// issues the SAME DB DELETE so the target label binds in the graph — otherwise the
// unbindable-target safeguard would mask the gap.
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
)

// registry is the dynamic dispatch seam: handlers register a func value, Dispatch
// selects one by a runtime key.
var registry = map[string]func(context.Context, *sql.DB){}

func register(name string, h func(context.Context, *sql.DB)) { registry[name] = h }

// Dispatch invokes the registered handler for name — the func-value hop under test.
func Dispatch(ctx context.Context, db *sql.DB, name string) {
	if h := registry[name]; h != nil {
		h(ctx, db)
	}
}

// purge issues a DB DELETE — reached at runtime ONLY through the registry (never called
// directly). Its address is taken in init(), which is NOT a call-graph root, so the
// builder never sees it: purge and its DELETE are absent from the static graph.
func purge(ctx context.Context, db *sql.DB) {
	const q = "DELETE FROM ledger WHERE id = $1"
	_, _ = db.ExecContext(ctx, q, "L1")
}

func init() {
	register("purge", purge)
}

// audit issues the SAME DB DELETE directly from a normal route — so the
// "boundary:db DELETE ledger" label IS present in the graph (the must_not_reach target
// binds) and the unbindable-target safeguard cannot mask the registry gap.
func audit(ctx context.Context, db *sql.DB) {
	const q = "DELETE FROM ledger WHERE id = $1"
	_, _ = db.ExecContext(ctx, q, "audit")
}

// Handle reaches purge's DELETE only across the init-registered registry hop.
func Handle(w http.ResponseWriter, req *http.Request) {
	db, _ := sql.Open("postgres", "")
	Dispatch(req.Context(), db, req.URL.Query().Get("op"))
}

// Report reaches the DELETE directly — the in-graph control.
func Report(w http.ResponseWriter, req *http.Request) {
	db, _ := sql.Open("postgres", "")
	audit(req.Context(), db)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin", Handle)
	mux.HandleFunc("GET /report", Report)
	log.Fatal(http.ListenAndServe(":8080", mux))
}
