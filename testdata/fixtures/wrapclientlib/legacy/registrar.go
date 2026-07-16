package legacy

// This is the HAND-WRITTEN half of the legacy client. Prime is the ergonomic wrapper the
// eventbus package's EnsureViaLegacy calls. In a build that widened this package it would
// reach the generated GetTemplate operation through the client; here legacy is declared
// WITHOUT followWrappers, so this body is never materialized and the wrapper descent sees
// only a bodiless declared-package hop. Because an operation may hide behind that bodiless
// function, the walk is INCOMPLETE — the labeler must disclose the gap (naming legacy as the
// package to opt into followWrappers), never name the edge from the truncated lower bound.

import (
	"context"
	"net/http"
)

// Prime is the bodiless hop. It reaches the generated operation through the concrete client,
// but since legacy is not widened its body is invisible to the descent.
func Prime(ctx context.Context, server string) error {
	c := &Client{Server: server, HTTP: http.DefaultClient}
	_, err := c.GetTemplateWithResponse(ctx, "warm")
	return err
}
