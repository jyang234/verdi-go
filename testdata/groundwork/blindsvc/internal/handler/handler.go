// Package handler is blindsvc's HTTP entry point. The Publish route reaches the
// dynamic publish, so a reachability question rooted at an entrypoint runs
// straight into the `<dynamic>` frontier — exactly the case a must_not_reach
// "no path found" verdict must NOT report as a proof.
package handler

import (
	"net/http"

	"example.com/blindsvc/internal/notify"
)

// Server exposes the notify endpoints.
type Server struct {
	notifier *notify.Notifier
}

// New returns a Server over n.
func New(n *notify.Notifier) *Server { return &Server{notifier: n} }

// Publish handles POST /publish: it emits an event whose name comes from the
// request, reaching the dynamic publish boundary.
func (s *Server) Publish(w http.ResponseWriter, r *http.Request) {
	event := r.URL.Query().Get("event")
	if err := s.notifier.Emit(r.Context(), event, map[string]string{"id": r.PathValue("id")}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// Create handles POST /users/{id}: it publishes a statically-named event, so
// this entrypoint reaches only a resolvable publish — the clean contrast to the
// dynamic one Publish reaches.
func (s *Server) Create(w http.ResponseWriter, r *http.Request) {
	if err := s.notifier.Created(r.Context(), map[string]string{"id": r.PathValue("id")}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
