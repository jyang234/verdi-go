// Command reflectsvc is an adversarial soundness fixture for the static reachability
// checker, the reflect analogue of dispatchsvc. Its admin route reaches a DB DELETE
// ONLY through a reflect.Value.Call — a reflective invocation the static call graph
// cannot follow. A directly-reachable decoy (Report→audit) issues the SAME DELETE so
// the target label binds in the graph and the unbindable-target safeguard cannot mask
// the gap.
//
// Unlike dispatchsvc's func-value seam (which init()-rooting can recover), the reflect
// seam is irreducible: no roots fix reconnects it. The probe (internal/static/soundness)
// confirms the EXISTING reflect blind spot already routes must_not_reach to noPathFound
// (abstain) rather than provenAbsent — coverage of the dangerous case that must hold.
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"reflect"
)

// purge issues a DB DELETE — invoked at runtime ONLY through reflect (never called
// directly, and no static edge reaches it). Its address is taken by reflect.ValueOf,
// so the function is reachable and its DELETE effect is present in the graph; what is
// absent is any call EDGE from Handle to it.
func purge(ctx context.Context, db *sql.DB) {
	const q = "DELETE FROM ledger WHERE id = $1"
	_, _ = db.ExecContext(ctx, q, "L1")
}

// audit issues the SAME DB DELETE directly from a normal route — the in-graph control
// that proves the effect is catchable when a static path reaches it.
func audit(ctx context.Context, db *sql.DB) {
	const q = "DELETE FROM ledger WHERE id = $1"
	_, _ = db.ExecContext(ctx, q, "audit")
}

// Handle reaches purge's DELETE only across a reflect.Value.Call — invisible to the
// static call graph, so the reflect blind spot must make must_not_reach abstain.
func Handle(w http.ResponseWriter, req *http.Request) {
	db, _ := sql.Open("postgres", "")
	fn := reflect.ValueOf(purge)
	fn.Call([]reflect.Value{reflect.ValueOf(req.Context()), reflect.ValueOf(db)})
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
