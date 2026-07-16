package eventbus

// This file is the HAND-WRITTEN half of the client package — the real-fleet idiom the
// wrapper-descent feature exists for. A service does not call the generated
// <Op>WithResponse methods directly; it calls these ergonomic Registrar wrappers, and
// each wrapper reaches the generated operation(s) through concrete same-package calls.
// With followWrappers OFF every one of these calls is an UnresolvedSpecOperation blind
// spot; with it ON the labeler descends the wrapper's declared-package call tree and
// names the edge from the single operation it reaches (or discloses the descent outcome
// when it reaches zero or several — never guessing between candidates).

import (
	"context"
	"net/http"
)

// Registrar is a hand-written wrapper over the generated ClientWithResponses. It holds
// the client CONCRETELY (a *ClientWithResponses field, no interface), so the descent's
// call-graph walk resolves each hop exactly.
type Registrar struct {
	client *ClientWithResponses
}

// NewRegistrar constructs the wrapper. Like NewClientWithResponses it is NOT a spec
// operation and reaches none, so a service call to it is a zero-operation
// UnresolvedSpecOperation disclosure (descent visits one function, finds no op).
func NewRegistrar(client *ClientWithResponses) *Registrar {
	return &Registrar{client: client}
}

// EnsureTemplate reaches EXACTLY ONE operation (CreateTemplate), through one unexported
// same-package helper hop — the depth-2 descent case. With followWrappers on, the
// service→EnsureTemplate edge is NAMED boundary:event-bus POST /v1/eventTypeTemplates,
// tagged via=openapi-client-wrapper.
func (r *Registrar) EnsureTemplate(ctx context.Context) error {
	return r.createTemplate(ctx)
}

// createTemplate is the unexported helper hop between the exported wrapper and the
// generated operation, so the descent has to walk two declared-package functions
// (EnsureTemplate, createTemplate) before it reaches the operation leaf.
func (r *Registrar) createTemplate(ctx context.Context) error {
	_, err := r.client.CreateTemplateWithResponse(ctx)
	return err
}

// EnsureParticipant reaches TWO operations (CreatePublisher and CreateSubscriber) via an
// if/else branch — the real-fleet AMBIGUOUS case. Both call sites are statically present,
// so RTA records both edges regardless of kind; the descent finds two distinct operations
// and, rather than guess, leaves the edge unnamed and discloses both in sorted order.
func (r *Registrar) EnsureParticipant(ctx context.Context, kind string) error {
	if kind == "publisher" {
		_, err := r.client.CreatePublisherWithResponse(ctx)
		return err
	}
	_, err := r.client.CreateSubscriberWithResponse(ctx)
	return err
}

// Warm reaches ZERO operations: it calls only a same-package transport helper that does
// plain net/http, never a generated method. The descent walks two declared-package
// functions (Warm, probe) and finds no operation, so the call stays disclosed with the
// zero-found descent outcome appended.
func (r *Registrar) Warm(ctx context.Context) error {
	return r.probe(ctx)
}

// probe is a hand-written transport helper: raw net/http against the client's base URL,
// reaching no generated operation. It is a declared-package function (so the descent
// counts it as visited) that contributes no operation label.
func (r *Registrar) probe(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.client.Server+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := r.client.HTTP.Do(req)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}
