// Package model holds the normalized feature vocabulary shared by both flowmap
// pipelines. Tier-map rules match on these features — never on raw source or raw
// spans — which is what lets one ruleset classify a static call edge and a
// runtime span identically (tier-map spec §1).
package model

// Boundary describes where an operation sits relative to the service boundary.
type Boundary string

const (
	BoundaryInbound       Boundary = "inbound"
	BoundaryInternal      Boundary = "internal"
	BoundaryCrossPackage  Boundary = "cross-package"
	BoundaryOutboundSync  Boundary = "outbound-sync"
	BoundaryOutboundAsync Boundary = "outbound-async"
)

// Effect describes what an operation does.
type Effect string

const (
	EffectMutate    Effect = "mutate"
	EffectRead      Effect = "read"
	EffectIO        Effect = "io"
	EffectTelemetry Effect = "telemetry"
	EffectCompute   Effect = "compute"
	EffectUnknown   Effect = "unknown"
)

// Origin describes where the callee lives. Static-strong: runtime data often
// leaves this unknown.
type Origin string

const (
	OriginSamePackage Origin = "same-package"
	OriginFirstParty  Origin = "first-party"
	OriginThirdParty  Origin = "third-party"
	OriginStdlib      Origin = "stdlib"
	OriginUnknown     Origin = "unknown"
)

// Features is the normalized reduction of one operation that the tier-map
// classifier consumes.
type Features struct {
	Boundary   Boundary
	Effect     Effect
	Origin     Origin
	Fallible   bool   // returns or propagates an error
	Concurrent bool   // spawned in a goroutine / async dispatch
	Identity   string // fully-qualified symbol (static) or canonical op (runtime)
}
