package api

import (
	"net/http"

	"example.com/oapisvc/bus"
)

// Server implements ServerInterface. Its create handler publishes an event, so
// the boundary contract should surface that publish — reached only through the
// generated wrapper and the ServerInterface interface, proving the chi root is
// wired into the call graph.
type Server struct{}

// NewServer returns the implementation.
func NewServer() *Server { return &Server{} }

func (*Server) CreateLoanApplication(w http.ResponseWriter, r *http.Request) {
	bus.Publish("loan.created")
	w.WriteHeader(http.StatusCreated)
}

func (*Server) GetLoanApplicationStatus(w http.ResponseWriter, r *http.Request, id string) {
	_ = id
	w.WriteHeader(http.StatusOK)
}
