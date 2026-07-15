// Command oapiclientsvc is the fixture for the OpenAPI-client labeler
// (--reclaim-openapi): a service whose only outbound edges are calls THROUGH a
// spec-generated client (clients/eventbus). The routes are fmt.Sprintf-assembled from
// path params, so with the labeler OFF these calls are unnamed internal edges; with it
// ON they are named boundary:event-bus <METHOD> <template> (via=openapi-client) from
// the declared spec. The single call to the generated CONSTRUCTOR (a non-operation)
// is disclosed as an UnresolvedSpecOperation blind spot.
package main

import (
	"net/http"

	"example.com/oapiclientsvc/clients/eventbus"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /publish", publish)
	_ = http.ListenAndServe(":8080", mux)
}

// publish is the HTTP entry point. It reaches the event-bus peer only through the
// generated client, exercising all four generated-name shapes.
func publish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	const server = "http://event-bus"

	// The generated constructor is NOT a spec operation: this call surfaces as an
	// UnresolvedSpecOperation blind spot (the callee FQN is disclosed), never labeled.
	cr := eventbus.NewClientWithResponses(server)

	// The bare <Op> method and its WithResponse / WithBodyWithResponse variants — each
	// NAMED boundary:event-bus POST /v1/publishers/{publisherId}/... from the spec.
	_, _ = cr.CreateEvent(ctx, "pub1", "created", "v1", "e1")
	_, _ = cr.CreateEventWithResponse(ctx, "pub1", "created", "v1", "e1")
	_, _ = cr.CreateEventWithBodyWithResponse(ctx, "pub1", "created", "v1", "e1", "application/json", nil)

	// The GET operation, through its WithResponse method.
	_, _ = cr.GetEventWithResponse(ctx, "e1")

	// The package-level request builders (New<Op>Request[WithBody]) — also NAMED, since
	// fn.Name() is the bare symbol for a package function just as for a method.
	if req, err := eventbus.NewCreateEventRequest(server, "pub1", "created", "v1", "e1"); err == nil {
		_ = req
	}
	if req, err := eventbus.NewCreateEventRequestWithBody(server, "pub1", "created", "v1", "e1", "application/json", nil); err == nil {
		_ = req
	}
}
