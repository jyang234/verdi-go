// Command loansvc is a toy, fully-instrumented loan-origination service used as
// the hermetic fixture for flowmap's own static and behavioral tests. It is a
// separate module (its own go.mod) so flowmap loads it the way it would load any
// external service, and so its instrumentation dependencies stay out of the
// engine module.
//
// main wires the object graph and registers entry points through dynamic
// dispatch — HTTP handlers via (*http.ServeMux).HandleFunc and a bus consumer via
// (*eventbus.Bus).Subscribe — which is precisely the registration the static
// pipeline's root discovery must see through. main never calls a handler
// directly; the decision logic is reachable only from the discovered roots.
package main

import (
	"database/sql"
	"log"
	"net/http"

	"example.com/loansvc/internal/client"
	"example.com/loansvc/internal/consumer"
	"example.com/loansvc/internal/eventbus"
	"example.com/loansvc/internal/handler"
	"example.com/loansvc/internal/origination"
	"example.com/loansvc/internal/scoring"
	"example.com/loansvc/internal/store"
)

func main() {
	log.Fatal(run())
}

// run builds the service and serves it. A nil *sql.DB stands in for the real
// driver, which the behavioral harness supplies; the static pipeline never
// executes this code.
func run() error {
	bus := eventbus.New()

	var db *sql.DB
	loans := store.New(db)

	hc := client.New()
	bureau := client.NewBureau(hc)
	gateway := client.NewGateway(hc)
	scorer := scoring.Select(false, bureau)

	eval := origination.NewEvaluator(loans, scorer, gateway, bus)
	app := handler.New(eval, loans)
	payments := consumer.New(loans)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /loan-application", app.Create)
	mux.HandleFunc("GET /loan-application/{id}/status", app.Status)

	bus.Subscribe("payment.settled", payments.OnSettled)

	srv := &http.Server{Addr: ":8080", Handler: mux}
	return srv.ListenAndServe()
}
