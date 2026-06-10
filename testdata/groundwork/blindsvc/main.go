// Command blindsvc is a minimal, dependency-free fixture whose only purpose is to
// make the static graph's blind spots concrete: a dynamic publish, a reflect
// call, and an unsafe import. It registers one HTTP route through dynamic
// dispatch so root discovery has an entrypoint to anchor reachability on.
package main

import (
	"log"
	"net/http"

	"example.com/blindsvc/internal/bus"
	"example.com/blindsvc/internal/handler"
	"example.com/blindsvc/internal/notify"
	"example.com/blindsvc/internal/rawmem"
)

func main() {
	log.Fatal(run())
}

func run() error {
	b := bus.New()
	n := notify.New(b)
	srv := handler.New(n)

	// Keep rawmem (the unsafe package) in the build so its disclosure is part of
	// the analyzed service.
	_ = rawmem.Size(0)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /publish/{id}", srv.Publish)
	mux.HandleFunc("POST /users/{id}", srv.Create)

	httpSrv := &http.Server{Addr: ":8080", Handler: mux}
	return httpSrv.ListenAndServe()
}
