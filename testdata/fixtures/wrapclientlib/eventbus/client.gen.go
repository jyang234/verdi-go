// Package eventbus is a hand-written stand-in for oapi-codegen v2.x client output for
// the event-bus service, living in its OWN module (example.com/wrapclientlib) so a
// service that depends on it exercises the SEPARATE-MODULE wrapper-descent path: the
// package's function bodies are a bodiless external declaration until the service opts
// into descent (classify.openapiClients[i].followWrappers), which widens the SSA
// horizon so the labeler can follow the hand-written wrappers to the generated ops.
//
// This file is the GENERATED half. Each operation is exposed under the oapi-codegen
// generated-name conventions the labeler joins back to the spec — here the package-level
// New<Op>Request builder and the *ClientWithResponses <Op>WithResponse method — and its
// route is ASSEMBLED with fmt.Sprintf from path params (no compile-time-constant
// argument), so the constant-fold HTTP labeler cannot name it and the spec labeler must.
// The constructor (NewClientWithResponses) is NOT an operation, so a service call to it
// surfaces as an UnresolvedSpecOperation blind spot — the labeler's honesty channel.
//
// The hand-written wrappers the service actually calls live in registrar.go (same
// package). Dispatch is kept CONCRETE (no interfaces) throughout, so RTA resolves the
// wrapper→helper→operation chain without any dynamic-dispatch over-approximation
// contaminating the descent.
package eventbus

import (
	"context"
	"fmt"
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

// --- CreateTemplate: POST /v1/eventTypeTemplates ---

// NewCreateTemplateRequest builds the CreateTemplate request (fmt.Sprintf path assembly,
// so no argument is a compile-time constant — exactly the call the constant-fold labeler
// cannot name and the spec labeler can).
func NewCreateTemplateRequest(server string) (*http.Request, error) {
	p := fmt.Sprintf("/%s/eventTypeTemplates", "v1")
	return http.NewRequest(http.MethodPost, server+p, nil)
}

// CreateTemplateWithResponse is the typed-response generated method for CreateTemplate.
func (c *ClientWithResponses) CreateTemplateWithResponse(ctx context.Context) (*http.Response, error) {
	req, err := NewCreateTemplateRequest(c.Server)
	if err != nil {
		return nil, err
	}
	return c.HTTP.Do(req.WithContext(ctx))
}

// --- CreatePublisher: POST /v1/publishers ---

// NewCreatePublisherRequest builds the CreatePublisher request (fmt.Sprintf path assembly).
func NewCreatePublisherRequest(server string) (*http.Request, error) {
	p := fmt.Sprintf("/%s/publishers", "v1")
	return http.NewRequest(http.MethodPost, server+p, nil)
}

// CreatePublisherWithResponse is the typed-response generated method for CreatePublisher.
func (c *ClientWithResponses) CreatePublisherWithResponse(ctx context.Context) (*http.Response, error) {
	req, err := NewCreatePublisherRequest(c.Server)
	if err != nil {
		return nil, err
	}
	return c.HTTP.Do(req.WithContext(ctx))
}

// --- CreateSubscriber: POST /v1/subscribers ---

// NewCreateSubscriberRequest builds the CreateSubscriber request (fmt.Sprintf path assembly).
func NewCreateSubscriberRequest(server string) (*http.Request, error) {
	p := fmt.Sprintf("/%s/subscribers", "v1")
	return http.NewRequest(http.MethodPost, server+p, nil)
}

// CreateSubscriberWithResponse is the typed-response generated method for CreateSubscriber.
func (c *ClientWithResponses) CreateSubscriberWithResponse(ctx context.Context) (*http.Response, error) {
	req, err := NewCreateSubscriberRequest(c.Server)
	if err != nil {
		return nil, err
	}
	return c.HTTP.Do(req.WithContext(ctx))
}

// --- GetTemplate: GET /v1/eventTypeTemplates/{templateId} ---

// NewGetTemplateRequest builds the GetTemplate request (fmt.Sprintf path assembly from
// the templateId path param).
func NewGetTemplateRequest(server, templateID string) (*http.Request, error) {
	p := fmt.Sprintf("/v1/eventTypeTemplates/%s", templateID)
	return http.NewRequest(http.MethodGet, server+p, nil)
}

// GetTemplateWithResponse is the typed-response generated method for GetTemplate. The
// service calls this one DIRECTLY (not through a wrapper), so it stays a plain
// via=openapi-client boundary edge — the direct-call path the descent must leave inert.
func (c *ClientWithResponses) GetTemplateWithResponse(ctx context.Context, templateID string) (*http.Response, error) {
	req, err := NewGetTemplateRequest(c.Server, templateID)
	if err != nil {
		return nil, err
	}
	return c.HTTP.Do(req.WithContext(ctx))
}
