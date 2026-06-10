// Command layeredsvc is a minimal, dependency-free (stdlib-only) fixture service
// for groundwork. It is strictly layered handler -> app -> store and registers
// its HTTP handlers through dynamic dispatch, the registration root discovery
// must see through. main never calls a handler directly.
package main

import (
	"database/sql"
	"log"
	"net/http"

	"example.com/layeredsvc/internal/app"
	"example.com/layeredsvc/internal/handler"
	"example.com/layeredsvc/internal/store"
)

func main() {
	log.Fatal(run())
}

// run builds and serves the service. A nil *sql.DB stands in for the real
// driver; the static pipeline never executes this code.
func run() error {
	var db *sql.DB
	st := store.New(db)
	svc := app.New(st)
	srv := handler.New(svc)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", srv.GetUser)
	mux.HandleFunc("PUT /users/{id}", srv.UpdateUser)

	httpSrv := &http.Server{Addr: ":8080", Handler: mux}
	return httpSrv.ListenAndServe()
}
