// Command oapisvc is a toy service whose HTTP routes are registered the way
// oapi-codegen's chi server does — through the chi.Router interface with
// base-URL-prefixed paths — so the static pipeline's root discovery is exercised
// against that pattern.
package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"example.com/oapisvc/api"
)

func main() {
	r := chi.NewRouter()
	api.RegisterHandlers(r, api.NewServer())
	_ = http.ListenAndServe(":8080", r)
}
