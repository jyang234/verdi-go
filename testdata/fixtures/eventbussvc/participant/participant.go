// Package participant is a types/consts-only domain package: it declares the
// participant-role vocabulary the server layer references, but defines NO functions
// (no free functions, no methods) — so it contributes no call-graph node and is
// absent from the C3 component rollup.
//
// It is the fixture analog of the field-reported internal package a reader orienting
// on the architecture would otherwise have no signal exists: imported by a real
// component (server), yet invisible in the call-graph view. The rollup discloses it
// as an imported-but-omitted no-function package (see Graph.OmittedPackages).
package participant

// Role is a participant's role in an event-bus exchange. A bare named string with no
// methods, so the package stays genuinely function-less.
type Role string

const (
	// Publisher emits events onto the bus.
	Publisher Role = "publisher"
	// Subscriber consumes events from the bus.
	Subscriber Role = "subscriber"
)
