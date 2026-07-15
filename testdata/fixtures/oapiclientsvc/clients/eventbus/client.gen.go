// Package eventbus is a hand-written stand-in for oapi-codegen v2.x client output for
// the event-bus service. It mirrors the two shapes the OpenAPI-client labeler must see
// through:
//
//   - the route is ASSEMBLED with fmt.Sprintf from path params (no compile-time-constant
//     argument), so the constant-fold HTTP labeler cannot name it; and
//   - each operation is exposed under the generated-name conventions the labeler joins
//     back to the spec — the bare method (<Op>), its WithResponse / WithBody /
//     WithBodyWithResponse variants, and the package-level New<Op>Request[WithBody]
//     request builders.
//
// The constructors (NewClientWithResponses) are NOT operations, so a service call to
// one surfaces as an UnresolvedSpecOperation blind spot — the labeler's honesty channel.
//
// Simplification vs. real oapi-codegen: the bare <Op> methods live directly on
// *ClientWithResponses here rather than on an embedded low-level ClientInterface, so
// the fixture has no interface-promotion wrappers to splice — the generated-name
// grammar is what the labeler keys on, not the receiver layout.
package eventbus

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// ClientWithResponses is the generated client. Server is the peer base URL; every
// method builds a request against it and issues it through HTTP.
type ClientWithResponses struct {
	Server string
	HTTP   *http.Client
}

// NewClientWithResponses is the generated constructor. It is NOT a spec operation, so
// a call to it is disclosed as an UnresolvedSpecOperation, never labeled.
func NewClientWithResponses(server string) *ClientWithResponses {
	return &ClientWithResponses{Server: server, HTTP: http.DefaultClient}
}

// --- CreateEvent: POST /v1/publishers/{publisherId}/eventTypes/{eventType}/versions/{version}/events/{eventId} ---

// NewCreateEventRequest builds the CreateEvent request. The path is fmt.Sprintf-assembled
// from the path params, so no argument is a compile-time constant — this is exactly the
// call the constant-fold labeler cannot name and the spec labeler can.
func NewCreateEventRequest(server, publisherID, eventType, version, eventID string) (*http.Request, error) {
	p := fmt.Sprintf("/v1/publishers/%s/eventTypes/%s/versions/%s/events/%s", publisherID, eventType, version, eventID)
	return http.NewRequest(http.MethodPost, server+p, nil)
}

// NewCreateEventRequestWithBody builds the CreateEvent request with a body reader.
func NewCreateEventRequestWithBody(server, publisherID, eventType, version, eventID, contentType string, body io.Reader) (*http.Request, error) {
	p := fmt.Sprintf("/v1/publishers/%s/eventTypes/%s/versions/%s/events/%s", publisherID, eventType, version, eventID)
	req, err := http.NewRequest(http.MethodPost, server+p, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return req, nil
}

// CreateEvent is the bare generated method (<Op> shape).
func (c *ClientWithResponses) CreateEvent(ctx context.Context, publisherID, eventType, version, eventID string) (*http.Response, error) {
	req, err := NewCreateEventRequest(c.Server, publisherID, eventType, version, eventID)
	if err != nil {
		return nil, err
	}
	return c.HTTP.Do(req.WithContext(ctx))
}

// CreateEventWithResponse is the typed-response generated method (<Op>WithResponse).
func (c *ClientWithResponses) CreateEventWithResponse(ctx context.Context, publisherID, eventType, version, eventID string) (*http.Response, error) {
	req, err := NewCreateEventRequest(c.Server, publisherID, eventType, version, eventID)
	if err != nil {
		return nil, err
	}
	return c.HTTP.Do(req.WithContext(ctx))
}

// CreateEventWithBodyWithResponse is the body+typed-response generated method
// (<Op>WithBodyWithResponse).
func (c *ClientWithResponses) CreateEventWithBodyWithResponse(ctx context.Context, publisherID, eventType, version, eventID, contentType string, body io.Reader) (*http.Response, error) {
	req, err := NewCreateEventRequestWithBody(c.Server, publisherID, eventType, version, eventID, contentType, body)
	if err != nil {
		return nil, err
	}
	return c.HTTP.Do(req.WithContext(ctx))
}

// --- GetEvent: GET /v1/events/{eventId} ---

// NewGetEventRequest builds the GetEvent request (fmt.Sprintf path assembly).
func NewGetEventRequest(server, eventID string) (*http.Request, error) {
	p := fmt.Sprintf("/v1/events/%s", eventID)
	return http.NewRequest(http.MethodGet, server+p, nil)
}

// GetEventWithResponse is the typed-response generated method for GetEvent.
func (c *ClientWithResponses) GetEventWithResponse(ctx context.Context, eventID string) (*http.Response, error) {
	req, err := NewGetEventRequest(c.Server, eventID)
	if err != nil {
		return nil, err
	}
	return c.HTTP.Do(req.WithContext(ctx))
}
