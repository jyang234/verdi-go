// Package legacy is a SECOND hand-written stand-in for oapi-codegen client output in the
// wrapclientlib module, declared as an OpenAPI client by wrapclientsvc but WITHOUT
// followWrappers. Unlike its sibling eventbus, its bodies are therefore never widened into
// SSA (the widening materializes only the opted-in packages), so it stays a bodiless
// external declaration.
//
// It exists to exercise the BODILESS-HOP incompleteness path: eventbus's EnsureViaLegacy
// calls legacy.Prime, but legacy.Prime has no materialized body, so the descent cannot
// enter it. An operation could hide behind that bodiless hop, so the walk is INCOMPLETE and
// the labeler must refuse to name the edge and disclose the gap — naming this package as the
// one that needs followWrappers on its own hint — never guess a label from the lower bound.
//
// This is the GENERATED half. Dispatch is CONCRETE throughout (no interfaces), matching the
// eventbus fixture, and the route is fmt.Sprintf-assembled so only the spec labeler could
// name it. It shares wrapclientsvc's event-bus spec (its GetTemplate operationId), so the
// generated-name shapes here join back to that same table.
package legacy

import (
	"context"
	"fmt"
	"net/http"
)

// Client is the generated client stand-in: a peer base URL plus an HTTP client.
type Client struct {
	Server string
	HTTP   *http.Client
}

// NewGetTemplateRequest builds the GetTemplate request (fmt.Sprintf path assembly), the
// generated-name shape the labeler joins to the shared spec's GetTemplate operation.
func NewGetTemplateRequest(server, templateID string) (*http.Request, error) {
	p := fmt.Sprintf("/v1/eventTypeTemplates/%s", templateID)
	return http.NewRequest(http.MethodGet, server+p, nil)
}

// GetTemplateWithResponse is the typed-response generated method for GetTemplate. It is
// never actually descended in this fixture — the point is that Prime, the wrapper reaching
// it, is bodiless — but it keeps legacy a realistic generated client.
func (c *Client) GetTemplateWithResponse(ctx context.Context, templateID string) (*http.Response, error) {
	req, err := NewGetTemplateRequest(c.Server, templateID)
	if err != nil {
		return nil, err
	}
	return c.HTTP.Do(req.WithContext(ctx))
}
