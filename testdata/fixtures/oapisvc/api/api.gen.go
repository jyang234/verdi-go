// Package api is a hand-written stand-in for oapi-codegen's chi-server output:
// a ServerInterface the service implements, a wrapper adapting each operation to
// http.HandlerFunc, and RegisterHandlers registering them on a chi.Router via the
// per-method functions (r.Post / r.Get) with baseURL-prefixed paths. It mirrors
// the generated shape flowmap's root discovery must see through: chi.Router is an
// interface (so registration is an interface-method invoke) and the route is a
// `baseURL + "/path"` concatenation.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ServerInterface is the operation set the service implements.
type ServerInterface interface {
	CreateLoanApplication(w http.ResponseWriter, r *http.Request)
	GetLoanApplicationStatus(w http.ResponseWriter, r *http.Request, id string)
}

// ServerInterfaceWrapper adapts ServerInterface to http.HandlerFunc, decoding
// path params before delegating to the implementation.
type ServerInterfaceWrapper struct {
	Handler ServerInterface
}

func (siw *ServerInterfaceWrapper) CreateLoanApplication(w http.ResponseWriter, r *http.Request) {
	siw.Handler.CreateLoanApplication(w, r)
}

func (siw *ServerInterfaceWrapper) GetLoanApplicationStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	siw.Handler.GetLoanApplicationStatus(w, r, id)
}

// RegisterHandlers registers the operations on r.
func RegisterHandlers(r chi.Router, si ServerInterface) {
	RegisterHandlersWithBaseURL(r, si, "")
}

// RegisterHandlersWithBaseURL registers the operations on r under baseURL.
func RegisterHandlersWithBaseURL(r chi.Router, si ServerInterface, baseURL string) {
	wrapper := ServerInterfaceWrapper{Handler: si}
	r.Group(func(r chi.Router) {
		r.Post(baseURL+"/loan-application", wrapper.CreateLoanApplication)
		r.Get(baseURL+"/loan-application/{id}/status", wrapper.GetLoanApplicationStatus)
	})
}
