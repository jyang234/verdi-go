// Package handler holds layeredsvc's HTTP entry points. Every method calls into
// the app layer and never touches store directly — the strict handler -> app ->
// store spine that makes this fixture a clean base for the skip-level review
// demo: adding a single handler -> store edge on a branch is a brand-new
// layering violation, not one the base already contains.
package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"example.com/layeredsvc/internal/app"
)

// Server serves the user endpoints.
type Server struct {
	svc *app.Service
}

// New returns a Server over svc.
func New(svc *app.Service) *Server { return &Server{svc: svc} }

// GetUser handles GET /users/{id}.
func (s *Server) GetUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.svc.GetProfile(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(r.Context(), w, u)
}

// UpdateUser handles PUT /users/{id}.
func (s *Server) UpdateUser(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.svc.UpdateProfile(r.Context(), r.PathValue("id"), body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(_ context.Context, w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
