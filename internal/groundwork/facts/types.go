// Package facts evaluates presentation-free, deterministic graph facts shared
// by standing fitness policy and caller-supplied claims.
package facts

// ReachState is the closed outcome of a transitive reachability evaluation.
type ReachState uint8

const (
	// ReachUnbound means at least one required selector family bound no graph
	// identity, so reachability cannot be evaluated.
	ReachUnbound ReachState = iota
	// ReachFound means a deterministic shortest source-to-target path exists.
	ReachFound
	// ReachAbsent means no path exists over the fully visible frontier.
	ReachAbsent
	// ReachBlind means no path was found, but the reachable frontier is incomplete.
	ReachBlind
)

// PathWitness is one ordered source-to-target path. A boundary target is the
// terminal path element.
type PathWitness struct {
	From string
	To   string
	Path []string
}

// BlindLocation identifies how a blind witness is attached to the graph. It is
// typed so presentation adapters never need to parse Kind, Site, or Detail.
type BlindLocation uint8

const (
	// BlindAtFunction is a graph blind spot attached directly to a function.
	BlindAtFunction BlindLocation = iota
	// BlindInPackage is a graph blind spot attached to a function's package.
	BlindInPackage
	// BlindAtDynamicEffect is a dynamically named boundary effect made by Site.
	BlindAtDynamicEffect
	// BlindAtConcurrentBoundary is a dynamically named boundary effect entered
	// directly through a concurrent edge.
	BlindAtConcurrentBoundary
	// BlindAtConcurrentDispatch is an unresolved graph-wide concurrent dispatch
	// at Site. It blinds the complete concurrent surface because no edge or seed
	// exists for the spawned body.
	BlindAtConcurrentDispatch
)

// BlindWitness identifies the canonical blind frontier selected for a source.
// Kind, Site, and Detail retain graph-native values; Location tells adapters
// whether Site is a function, package, or dynamic-effect owner.
type BlindWitness struct {
	From     string
	Site     string
	Kind     string
	Detail   string
	Location BlindLocation
}

// ReachResult carries the complete bound identities and the decisive reach fact.
//
// UnboundFrom/UnboundTo are PER-SELECTOR dead sets, populated in every state:
// each names the input selectors that bind nothing ON THEIR OWN, copied exactly,
// sorted, and de-duplicated. A non-empty dead set therefore does NOT imply
// State == ReachUnbound — that state means a whole family bound nothing (which
// in turn always makes that family's dead set the complete selector list).
// Consumers proving something about the family (fitness) read State; consumers
// requiring every named selector to bind (claims) read these fields.
type ReachResult struct {
	State       ReachState
	From        []string
	To          []string
	Paths       []PathWitness
	Blind       *BlindWitness
	UnboundFrom []string
	UnboundTo   []string
}

// AllowPair exempts one source-target pair from pass-through evaluation. An
// empty side is a wildcard; a non-empty side uses the shared rich-selector
// matcher.
type AllowPair struct {
	From string
	To   string
}

// PassThroughInput is the presentation-free input to a waypoint proof.
type PassThroughInput struct {
	From    []string
	To      []string
	Through []string
	Allow   []AllowPair
}

// PassThroughState is the closed outcome of a waypoint proof.
type PassThroughState uint8

const (
	// PassThroughUnbound means a required source or target family bound no
	// graph identity. An unbound waypoint is recorded separately and does not
	// stop traversal, preserving the standing fitness contract.
	PassThroughUnbound PassThroughState = iota
	// PassThroughBypassed means at least one unallowed path avoids the waypoint.
	PassThroughBypassed
	// PassThroughGuarded means no bypass exists over a fully visible frontier.
	PassThroughGuarded
	// PassThroughBlind means no bypass was found, but the frontier is incomplete.
	PassThroughBlind
)

// PassThroughResult carries complete bindings and deterministic evidence.
//
// UnboundFrom/UnboundTo/UnboundThrough are PER-SELECTOR dead sets with the same
// meaning as ReachResult's: each names the input selectors that bind nothing on
// their own, in every state, sorted and de-duplicated. State is decided by the
// bound families alone, so a partially bound source or target family is still
// traversed and still yields bypass evidence.
type PassThroughResult struct {
	State    PassThroughState
	From     []string
	To       []string
	Through  []string
	Bypasses []PathWitness
	// BypassOccurrences retains one canonical witness per legacy traversal
	// occurrence. Bypasses is the pair-deduplicated claim evidence; standing
	// fitness consumes this field to preserve its existing per-boundary-edge
	// finding multiplicity without owning a second traversal.
	BypassOccurrences []PathWitness
	Blind             *BlindWitness
	UnboundFrom       []string
	UnboundTo         []string
	UnboundThrough    []string
}
