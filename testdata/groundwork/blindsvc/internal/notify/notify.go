// Package notify publishes events two ways on purpose: a static name the graph
// can resolve, and a dynamic (parameterized) name it cannot. The dynamic path is
// the routing-blindness case the design record keeps returning to — the service
// can publish to a topic the static graph is unable to name.
package notify

import (
	"context"

	"example.com/blindsvc/internal/bus"
	"example.com/blindsvc/internal/encode"
)

// Notifier publishes user-lifecycle events.
type Notifier struct {
	bus *bus.Bus
}

// New returns a Notifier over b.
func New(b *bus.Bus) *Notifier { return &Notifier{bus: b} }

// Created publishes a statically-named event — a resolvable boundary edge.
func (n *Notifier) Created(ctx context.Context, user any) error {
	return n.bus.Publish(ctx, "user.created", encode.Marshal(user))
}

// Emit publishes an event whose name is chosen at runtime — a `<dynamic>` edge
// the static graph cannot name, plus a NonConstantBoundaryArg blind spot.
func (n *Notifier) Emit(ctx context.Context, event string, user any) error {
	return n.bus.Publish(ctx, event, encode.Marshal(user))
}
