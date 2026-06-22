// Package server implements StrictServerInterface — the (ctx,request)->(response,
// error) operations. These are the DOMAIN handlers: the create path commits a DB
// write and a named publish, so api->server is a real domain `call` (in contrast
// to the api->main wiring back-edge the injected error handlers produce).
package server

import (
	"context"

	"example.com/eventbussvc/api"
	"example.com/eventbussvc/bus"
	"example.com/eventbussvc/participant"
	"example.com/eventbussvc/store"
)

// Server is the StrictServerInterface implementation.
type Server struct {
	st *store.Store
	// role references the types-only participant package so the import is real; that
	// package declares no functions, so it contributes no call-graph node and the C3
	// rollup discloses it as an imported-but-omitted no-function package.
	role participant.Role
}

// New returns a Server backed by st.
func New(st *store.Store) *Server { return &Server{st: st, role: participant.Publisher} }

func (s *Server) CreateEventTypeTemplate(ctx context.Context, _ api.CreateEventTypeTemplateRequestObject) (api.CreateEventTypeTemplateResponseObject, error) {
	bus.Publish("eventtype.created")
	return api.CreateEventTypeTemplateResponseObject{}, s.st.DeleteOutbox(ctx)
}

func (s *Server) SyncEventTypes(ctx context.Context, _ api.SyncEventTypesRequestObject) (api.SyncEventTypesResponseObject, error) {
	bus.Publish("eventtype.synced")
	return api.SyncEventTypesResponseObject{}, nil
}

func (s *Server) GetHealth(ctx context.Context, _ api.GetHealthRequestObject) (api.GetHealthResponseObject, error) {
	return api.GetHealthResponseObject{}, s.st.Ping(ctx)
}
