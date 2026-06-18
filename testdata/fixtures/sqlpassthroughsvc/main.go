// Command sqlpassthroughsvc is a hermetic fixture whose persistence layer routes
// every database/sql round-trip through a single pass-through helper (see ./store).
// main wires the store methods behind HTTP routes so they are rooted in the call
// graph, the way a real service reaches its storage layer.
package main

import (
	"context"
	"net/http"

	"example.com/sqlpassthroughsvc/store"
)

func main() {
	s := store.New(nil)
	ctx := context.Background()

	http.HandleFunc("DELETE /eventTypes", func(w http.ResponseWriter, r *http.Request) {
		_ = s.DeleteEventType(ctx, r.URL.Query().Get("id"))
	})
	http.HandleFunc("POST /eventTypeVersions", func(w http.ResponseWriter, r *http.Request) {
		_ = s.InsertEventTypeVersion(ctx, r.URL.Query().Get("id"), r.URL.Query().Get("schema"))
	})
	http.HandleFunc("PATCH /eventTypeVersions", func(w http.ResponseWriter, r *http.Request) {
		_ = s.UpdateEventTypeVersion(ctx, r.URL.Query().Get("id"))
	})
	http.HandleFunc("POST /raw", func(w http.ResponseWriter, r *http.Request) {
		_ = s.ExecRaw(ctx, r.URL.Query().Get("verb"), r.URL.Query().Get("table"))
	})
	http.HandleFunc("DELETE /eventTypesIndirect", func(w http.ResponseWriter, r *http.Request) {
		_ = s.IndirectCaller(ctx, r.URL.Query().Get("id"), r.URL.Query().Get("alt") == "1")
	})

	pubs := store.NewPublisherStore(nil)
	subs := store.NewSubscriberStore(nil)
	http.HandleFunc("DELETE /participants", func(w http.ResponseWriter, r *http.Request) {
		_ = pubs.DeleteParticipant(ctx, r.URL.Query().Get("id"))
		_ = subs.DeleteParticipant(ctx, r.URL.Query().Get("id"))
	})

	_ = http.ListenAndServe(":8080", nil)
}
