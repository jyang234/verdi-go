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
// Unbound selector values are copied exactly, sorted, and de-duplicated.
type ReachResult struct {
	State       ReachState
	From        []string
	To          []string
	Paths       []PathWitness
	Blind       *BlindWitness
	UnboundFrom []string
	UnboundTo   []string
}
