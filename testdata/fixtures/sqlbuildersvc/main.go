// Command sqlbuildersvc is a hermetic fixture whose persistence layer assembles
// every query through a constant-fragment SQL builder (see ./store). main wires
// the store methods behind HTTP routes so they are rooted in the call graph, the
// way a real service reaches its storage layer.
package main

import (
	"context"
	"net/http"

	"example.com/sqlbuildersvc/store"
)

func main() {
	s := store.New(nil)
	ctx := context.Background()

	http.HandleFunc("GET /messages", func(w http.ResponseWriter, r *http.Request) {
		_ = s.GetMessage(ctx, r.URL.Query().Get("id"))
	})
	http.HandleFunc("POST /messages", func(w http.ResponseWriter, r *http.Request) {
		_ = s.CreateMessage(ctx, r.URL.Query().Get("body"))
	})
	http.HandleFunc("DELETE /rows", func(w http.ResponseWriter, r *http.Request) {
		_ = s.DeleteByTable(ctx, r.URL.Query().Get("table"), r.URL.Query().Get("id"))
	})
	http.HandleFunc("PATCH /accounts", func(w http.ResponseWriter, r *http.Request) {
		_ = s.UpdatePartial(ctx, r.URL.Query().Get("id"), r.URL.Query().Get("name"), r.URL.Query().Get("email"))
	})
	http.HandleFunc("GET /filter", func(w http.ResponseWriter, r *http.Request) {
		_ = s.ReadDynamicFilter(ctx, r.URL.Query().Get("col"), r.URL.Query().Get("val"))
	})
	http.HandleFunc("POST /raw", func(w http.ResponseWriter, r *http.Request) {
		_ = s.ExecOpaque(ctx, r.URL.Query().Get("verb"), r.URL.Query().Get("table"))
	})

	_ = http.ListenAndServe(":8080", nil)
}
