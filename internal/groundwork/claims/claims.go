// Package claims evaluates a point-in-time claims file against a flowmap graph
// — the verification complement to fitness. Where fitness gates ongoing
// invariants over a policy, `groundwork assert` answers "does THIS graph, right
// now, match what a design doc says about it?" Every claim resolves to a
// three-valued outcome and nothing is guessed: a claim that cannot be evaluated
// ERRORs rather than passing vacuously (tenet 2, fail closed).
//
// # Counting basis
//
// Every count and edge test is over UNIQUE (from, to) pairs, not raw edge
// records. edges[] legitimately carries the same pair more than once (a plain
// call and a `go`-launched call are two records with the same endpoints — see
// docs/groundwork/usage.md §"Consuming graph.json directly"). Collapsing to
// unique pairs is the FR-mandated, documented basis so a count is reproducible
// and mode-independent.
//
// # Universes
//
//   - Node universe (node / no_node, and an entrypoint claim's `fn`): declared
//     node FQNs only.
//   - Endpoint universe (edge / no_edge / edge_count / in_degree / out_degree):
//     node FQNs ∪ every edge from/to string, so a boundary pseudo-node
//     ("boundary:db QueryContext") — which appears only as an edge endpoint —
//     is claimable.
//   - Entrypoint-records universe (entrypoint): the graph's entrypoints[]
//     route/topic → handler join records. An entrypoint record is NEITHER a node
//     nor an edge endpoint — the route/topic name it carries lives in no other
//     universe — which is the whole reason the kind exists: it is the one
//     graph-known fact the node/edge kinds cannot reach.
//
// # Claim kinds and outcomes
//
//	kind        required               passes when                         resolution failure
//	----------  ---------------------  ----------------------------------  ------------------
//	edge        from, to               ≥1 unique pair between resolved      ERROR
//	                                   endpoints exists
//	no_edge     from, to               NO pair between resolved endpoints   ERROR
//	edge_count  from, to, eq           count of present pairs == eq         ERROR
//	node        fqn [, tier]           fqn resolves (tier matches if set)   ERROR
//	no_node     fqn                    fqn resolves to ZERO nodes           never (0=pass, ≥1=FAIL)
//	in_degree   of, eq [, cp]          #distinct callers (filtered) == eq   ERROR
//	out_degree  of, eq [, cp]          #distinct callees (filtered) == eq   ERROR
//	entrypoint  name, fn               a name-matched record's fn equals    ERROR
//	            [, entry_kind]         the resolved fn
//	reach       from, to, expect        reachability matches expectation     ERROR
//	pass_through from, to, through      every visible path enters waypoint  ERROR
//	no_concurrent_reach to              target is absent from the visible    ERROR
//	                                   concurrent surface
//	obligation  name, expect            the graph's own obligation verdicts  ERROR
//	                                   prove the named rule (expect:
//	                                   "satisfied" only)
//
// entrypoint has its own two-poled polarity, distinct from the absence kinds:
// ZERO records matching the route/topic name is a FAIL (not an ERROR) — existence
// IS the assertion, so a renamed/removed route fails loudly, and unlike no_node's
// zero-is-pass there is no OTHER absence claim a vacuous pass could smuggle into.
// When ≥1 record matches but they DISAGREE on the handler fn (overlapping route
// templates), the claim ERRORs listing the joins, forcing a tighter name — the
// deliberate native tightening over the FR's any-record-passes reference. A
// resolution failure of `fn` (over the node universe) still ERRORs before any
// FAIL polarity is computed, exactly like every other kind.
//
// Name matching is KIND-AWARE: routematch's segment-wise route tolerance applies
// only between a route-shaped ('/'-bearing) claim name and an http record; a
// non-http record (a consumer topic, a declared callback/worker "import/path#Symbol"
// name) and a slash-less claim name are matched by EXACT equality — the same
// http/consumer split the triage lens applies (impact.ResolveRoute uses routematch
// on http records, impact.ResolveEvent matches consumer names exactly). Two further
// native tightenings, both fail-closed: an EMPTY entrypoints[] universe ABSTAINS
// (ERROR) rather than FAILing every claim from total blindness (the join is absent —
// routers outside root discovery's coverage, or a pre-join producer); and among
// overlapping matched templates a record whose name equals the claim name VERBATIM
// wins the grade (the exact-spelling tiebreak — the tightest possible name), leaving
// the disagree ERROR only for non-exact spellings.
//
// The no_node asymmetry is load-bearing: zero matches IS the pass, so a rename
// that deletes the named node cannot vacuously pass some OTHER absence check —
// which is exactly why every other kind must ERROR (not silently pass) when a
// name fails to resolve. Resolution follows internal/fqnres: a PLAIN endpoint
// is a normalized-suffix, unique-or-die (0 → unresolved ERROR, ≥2 → ambiguous
// ERROR); a "/regex/" endpoint sees the raw FQN and may match many (per-side —
// a regex on one endpoint does not relax the uniqueness the other's plain form
// implies). The `of` of a degree claim must resolve to exactly one endpoint
// (an ambiguous regex there is an ERROR); the counterpart filter (cp) is a
// set predicate, so it never needs to be unique.
//
// A degree's counterpart filter is spelled `counterpart_matching` (canonical);
// `to_matching` is accepted as a documented alias (the field-validated
// prototype's name, which lies — it filters the FROM side on in_degree). Both
// present is an ERROR.
//
// # Claim metadata and the `fn` alias
//
// A claim may carry a free-form `id`, echoed as the report-line label when
// present (else the endpoint-derived label is used). `fn` is a documented
// alias for the anchor field — `fqn` on node/no_node, `of` on the degree kinds
// — accepted for companion-spec conformance; the alias and its canonical
// spelling both present on one claim is an ERROR, same as the counterpart
// alias. `fn` on an edge kind is a wrong-kind ERROR. The one exception: on the
// entrypoint kind `fn` is the CANONICAL anchor field (the handler), not an alias
// — it names the function the route/topic must join to and is required there.
//
// # Report shape
//
// Each non-passing claim renders one line — `FAIL  <label> [<kind>] <detail>`
// or `ERROR <label> [<kind>] <detail>` (FAIL is padded to column-align with
// ERROR). FAIL lines precede ERROR lines, claims-file order within each, then
// the summary. Passing claims are silent.
package claims

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/fqnres"
	"github.com/jyang234/golang-code-graph/internal/groundwork/facts"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/setutil"
	"github.com/jyang234/golang-code-graph/internal/routematch"
)

// Caps on the offender/match lists a report line prints. Truncation is disclosed
// ("(+N more)"), never silent — a capped list must not read as the whole set (tenet
// 3). The ambiguous-CANDIDATE cap lives in fqnres (AmbiguousDetail owns it), shared
// with `flowmap graph --focus`; these two are claims-local list caps.
const (
	maxOffenders = 3 // no_edge present-pair offenders
	maxMatches   = 4 // no_node / node offending matches
)

// Claim is one asserted fact. It is a union over every kind's fields; strict
// decoding (DisallowUnknownFields) rejects a typo'd field name (form: for
// from:) so it cannot silently become a zero-value claim. Per-kind required
// fields are validated at evaluation, which ERRORs the individual claim rather
// than aborting the file. Eq/Tier are pointers so an omitted count is
// distinguishable from an explicit zero.
type Claim struct {
	Kind string `json:"kind"`
	// ID is free-form claim metadata, echoed as the report-line label when
	// present (uniqueness is recommended in docs, not enforced). It is claim
	// metadata, not a kind field, so it is allowed on EVERY kind and is excluded
	// from the wrong-kind field check entirely.
	ID string `json:"id,omitempty"`
	// From, To, and Through retain whether JSON supplied a scalar or list.
	// Structural claims remain scalar-only; rich claim families consume the list
	// shape without changing this shared decoder.
	From                Selectors `json:"from,omitempty"`
	To                  Selectors `json:"to,omitempty"`
	Through             Selectors `json:"through,omitempty"`
	Expect              string    `json:"expect,omitempty"`
	FQN                 string    `json:"fqn,omitempty"`
	Of                  string    `json:"of,omitempty"`
	Tier                *int      `json:"tier,omitempty"`
	Eq                  *int      `json:"eq,omitempty"`
	CounterpartMatching string    `json:"counterpart_matching,omitempty"`
	ToMatching          string    `json:"to_matching,omitempty"`
	// Fn is a documented alias: for node/no_node it aliases `fqn`; for
	// in_degree/out_degree it aliases `of`. Both alias and canonical present on
	// one claim → that claim ERRORs (same treatment as counterpart_matching/
	// to_matching). Accepted on those four kinds only (`fn` on an edge claim is a
	// wrong-kind ERROR). Precedence routes through nodeAnchor/degreeAnchor so the
	// evaluator and label() cannot drift.
	//
	// On the entrypoint kind `fn` is NOT an alias — it is the CANONICAL anchor,
	// naming the handler function the route/topic (Name) must join to. It is read
	// directly there (no nodeAnchor/degreeAnchor detour) and is required.
	Fn string `json:"fn,omitempty"`
	// Name is the entrypoint claim's route/topic/symbol query (an HTTP route like
	// "POST /loan-application", a consumer topic like "payment.settled", or a
	// declared callback/worker "import/path#Symbol" reference). Matching is
	// KIND-AWARE (evalEntrypoint): internal/routematch's segment-wise tolerance
	// applies only between a route-shaped ('/'-bearing) Name and an http record;
	// every other case is exact string equality. entrypoint-only.
	Name string `json:"name,omitempty"`
	// EntryKind optionally filters an entrypoint claim to records of exactly this
	// record Kind — one of graph.EntrypointKinds ("http"/"consumer"/"callback"/
	// "worker"); any other value ERRORs, fail-closed on an authoring typo.
	// entrypoint-only.
	EntryKind string `json:"entry_kind,omitempty"`
}

// File is a decoded claims file: a single "claims" array.
type File struct {
	Claims []Claim `json:"claims"`
}

// LoadFile reads and strictly decodes a claims file. A malformed file (bad
// JSON, unknown field, missing "claims") is an operational error — distinct
// from a claim that decodes cleanly but cannot be evaluated (which becomes a
// per-claim ERROR at Evaluate time).
func LoadFile(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	var cf File
	if err := dec.Decode(&cf); err != nil {
		return nil, fmt.Errorf("%s: decode claims: %w", path, err)
	}
	// Exactly one JSON value: any trailing token (a second document, stray
	// bytes) means the input is not the single claims file it claims to be. This
	// is the same fail-closed single-value guard graph.Load applies to graph
	// JSON (graph.go) — dec.Token() must reach a clean io.EOF; dec.More() alone
	// is insufficient because it returns false when the trailing bytes begin
	// with '}' or ']'. The parity is pinned by TestLoadFileStrict's trailing-data
	// case (a shared strict-single-value decoder would be the cleaner home if a
	// third caller appears).
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%s: trailing data after claims JSON (expected a single object)", path)
	}
	if cf.Claims == nil {
		return nil, fmt.Errorf("%s: missing \"claims\" array (not a claims file?)", path)
	}
	return &cf, nil
}

// Outcome is a claim's three-valued verdict.
type Outcome int

const (
	Pass Outcome = iota
	Fail
	Errored // resolution/schema failure: the claim's gate could not run
)

// Reason is the closed vocabulary for a machine-report ERROR result.
type Reason string

const (
	ReasonUnresolved       Reason = "UNRESOLVED"
	ReasonAmbiguous        Reason = "AMBIGUOUS"
	ReasonUnboundSelector  Reason = "UNBOUND_SELECTOR"
	ReasonBlindFrontier    Reason = "BLIND_FRONTIER"
	ReasonMalformedClaim   Reason = "MALFORMED_CLAIM"
	ReasonUnknownStatus    Reason = "UNKNOWN_STATUS"
	ReasonMissingGraphData Reason = "MISSING_GRAPH_DATA"
	ReasonCantProve        Reason = "CANT_PROVE"
	ReasonUnmatched        Reason = "UNMATCHED"
)

type evaluationError struct {
	Reason Reason
	Detail string
}

// Bindings identifies the canonical graph values a claim resolved against.
type Bindings struct {
	From       []string
	To         []string
	Through    []string
	FQN        []string
	Of         []string
	Fn         []string
	Entrypoint []string
	Obligation []string
}

// Witness is deterministic graph evidence for a claim outcome. Path preserves
// its ordered BFS sequence.
type Witness struct {
	From      string
	To        string
	Path      []string
	BlindSite string
	Rule      string
	Fn        string
	Site      string
	Status    string
	Detail    string
}

// Result is one evaluated claim, carrying the pre-rendered label and detail so
// the report is a pure format of the results (deterministic).
type Result struct {
	ID        string
	Kind      string
	Label     string
	Outcome   Outcome
	Reason    Reason
	Detail    string
	Bindings  Bindings
	Witnesses []Witness
}

// Report is the full evaluation, in claims-file order.
type Report struct {
	Results        []Result
	NumNodes       int
	NumUniquePairs int
}

// Passed/Failed/Errored count the results by outcome.
func (r Report) Passed() int  { return r.count(Pass) }
func (r Report) Failed() int  { return r.count(Fail) }
func (r Report) Errored() int { return r.count(Errored) }

func (r Report) count(o Outcome) int {
	n := 0
	for _, res := range r.Results {
		if res.Outcome == o {
			n++
		}
	}
	return n
}

// String renders the report deterministically: FAIL lines first (claims-file
// order), then ERROR lines (claims-file order), then the summary. Each line is
// "FAIL  <label> [<kind>] <detail>" / "ERROR <label> [<kind>] <detail>" — FAIL
// carries two trailing spaces so it column-aligns with "ERROR ". The label is
// the claim's id when present, else the endpoint-derived label. Passing claims
// are silent — represented only in the summary count — so the output is the
// actionable set. A fully-passing run prints just the summary line.
func (r Report) String() string {
	var b strings.Builder
	for _, res := range r.Results {
		if res.Outcome == Fail {
			fmt.Fprintf(&b, "FAIL  %s [%s] %s\n", res.Label, res.Kind, res.Detail)
		}
	}
	for _, res := range r.Results {
		if res.Outcome == Errored {
			fmt.Fprintf(&b, "ERROR %s [%s] %s\n", res.Label, res.Kind, res.Detail)
		}
	}
	fmt.Fprintf(&b, "assert: %d passed, %d failed, %d errored (graph: %d nodes, %d unique edges)\n",
		r.Passed(), r.Failed(), r.Errored(), r.NumNodes, r.NumUniquePairs)
	return b.String()
}

// Evaluate runs every claim against the graph and returns the report. It never
// errors: an unevaluable claim becomes an Errored Result; the CLI maps the
// aggregate to an exit code.
func Evaluate(g *graph.Graph, cf *File) Report {
	m := newModel(g)
	rep := Report{NumNodes: m.numNodes, NumUniquePairs: m.numUniquePairs}
	for _, c := range cf.Claims {
		rep.Results = append(rep.Results, m.eval(c))
	}
	return rep
}

// model is the once-per-run graph view every claim shares.
type model struct {
	reachIndex        *graph.Index // shared typed-fact substrate
	concurrentSurface *facts.ConcurrentSurface
	nodeUniverse      []string // sorted declared node FQNs
	endpointUniverse  []string // sorted node FQNs ∪ edge endpoints
	pairs             map[[2]string]bool
	callers           map[string][]string // to → sorted distinct froms
	callees           map[string][]string // from → sorted distinct tos
	// nodeTiers maps an FQN to the sorted DISTINCT tiers its node records carry.
	// graph.Load does not guarantee node-FQN uniqueness (a generic instance's
	// display FQN is documented non-unique — see graphio.sortGraph), so a single
	// FQN can carry more than one tier. A tier claim over such an FQN is
	// ambiguous and abstains (ERROR) rather than grading against an arbitrary
	// last-write record (fail closed, tenet 2).
	nodeTiers map[string][]int
	// entrypoints is the graph's entrypoints[] records in graph order. An
	// entrypoint claim iterates them in this order but every list it emits (the
	// disagreeing-joins ERROR) is sorted, so the verdict is arrival-order
	// independent (pinned by TestDeterministicOverShuffledInput).
	entrypoints    []graph.Entrypoint
	numNodes       int
	numUniquePairs int
}

func newModel(g *graph.Graph) *model {
	nodeSet := make(map[string]bool, len(g.Nodes))
	tierSet := make(map[string]map[int]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeSet[n.FQN] = true
		if tierSet[n.FQN] == nil {
			tierSet[n.FQN] = map[int]bool{}
		}
		tierSet[n.FQN][n.Tier] = true
	}
	pairs := make(map[[2]string]bool, len(g.Edges))
	callersSet := map[string]map[string]bool{}
	calleesSet := map[string]map[string]bool{}
	for _, e := range g.Edges {
		pairs[[2]string{e.From, e.To}] = true
		addSet(callersSet, e.To, e.From)
		addSet(calleesSet, e.From, e.To)
	}
	return &model{
		reachIndex:       graph.NewIndex(g),
		nodeUniverse:     setutil.SortedKeys(nodeSet),
		endpointUniverse: EndpointUniverse(g),
		pairs:            pairs,
		callers:          sortedSets(callersSet),
		callees:          sortedSets(calleesSet),
		nodeTiers:        sortedIntSets(tierSet),
		entrypoints:      g.Entrypoints,
		numNodes:         len(g.Nodes),
		numUniquePairs:   len(pairs),
	}
}

// EndpointUniverse returns the sorted, deduped resolvable-name universe for a graph:
// node FQNs ∪ every edge from/to string, so a boundary pseudo-node ("boundary:db
// SELECT loans") — which appears only as an edge endpoint — is resolvable. It is the
// PRODUCTION constructor `newModel` builds the endpoint universe from, exported so
// `flowmap graph --focus` (graphio.endpointUniverse) can be pinned against the SAME
// rule by the resolver-parity test — production vs production, not against a test-local
// re-derivation that could silently drift (CLAUDE.md: one source of truth). Routes
// through setutil.SortedKeys, the codebase's single ordering primitive.
func EndpointUniverse(g *graph.Graph) []string {
	set := make(map[string]bool, len(g.Nodes)+2*len(g.Edges))
	for _, n := range g.Nodes {
		set[n.FQN] = true
	}
	for _, e := range g.Edges {
		set[e.From] = true
		set[e.To] = true
	}
	return setutil.SortedKeys(set)
}

// allowedFields lists the fields each kind reads. Strict JSON decoding rejects
// UNKNOWN field names (a typo'd `form:`), but a KNOWN field on the wrong kind
// (e.g. `eq` on an `edge`, where the author meant `edge_count` — an ABSENCE
// assertion) decodes cleanly and would otherwise be silently ignored, inverting
// the verdict. Rejecting a field the kind does not read closes that (tenet 2).
//
// `id` is claim metadata (allowed on every kind), so it is never listed here
// and unexpectedField skips it. `fn` is the documented anchor alias — accepted
// on the four kinds that have an anchor field (node/no_node → `fqn`,
// in_degree/out_degree → `of`) and a wrong-kind ERROR on the edge kinds. On
// entrypoint, `fn` is the CANONICAL anchor (the handler), not an alias, and
// `name`/`entry_kind` are entrypoint-only.
var allowedFields = map[string][]string{
	"edge":       {"from", "to"},
	"no_edge":    {"from", "to"},
	"edge_count": {"from", "to", "eq"},
	"node":       {"fqn", "tier", "fn"},
	"no_node":    {"fqn", "fn"},
	"in_degree":  {"of", "eq", "counterpart_matching", "to_matching", "fn"},
	"out_degree": {"of", "eq", "counterpart_matching", "to_matching", "fn"},
	"entrypoint": {"name", "fn", "entry_kind"},
	"reach":      {"from", "to", "expect"},
	"pass_through": {
		"from", "to", "through",
	},
	"no_concurrent_reach": {"to"},
	"obligation":          {"name", "expect"},
}

func (m *model) eval(c Claim) Result {
	allowed, ok := allowedFields[c.Kind]
	if !ok {
		return errored(c, ReasonMalformedClaim, "unknown claim kind "+strconv.Quote(c.Kind))
	}
	if f := unexpectedField(c, allowed); f != "" {
		return errored(c, ReasonMalformedClaim, c.Kind+" does not accept field "+strconv.Quote(f))
	}
	switch c.Kind {
	case "edge":
		return m.evalEdge(c)
	case "no_edge":
		return m.evalNoEdge(c)
	case "edge_count":
		return m.evalEdgeCount(c)
	case "node":
		return m.evalNode(c)
	case "no_node":
		return m.evalNoNode(c)
	case "in_degree":
		return m.evalDegree(c, true)
	case "out_degree":
		return m.evalDegree(c, false)
	case "entrypoint":
		return m.evalEntrypoint(c)
	case "reach":
		return m.evalReach(c)
	case "pass_through":
		return m.evalPassThrough(c)
	case "no_concurrent_reach":
		return m.evalNoConcurrentReach(c)
	case "obligation":
		return m.evalObligation(c)
	default:
		return errored(c, ReasonMalformedClaim, "unknown claim kind "+strconv.Quote(c.Kind))
	}
}

func (m *model) evalNoConcurrentReach(c Claim) Result {
	if !c.To.Present() {
		return errored(c, ReasonMalformedClaim, "no_concurrent_reach requires 'to'")
	}

	fact := m.concurrentFacts().Evaluate(c.To.Values())
	bindings := Bindings{To: append([]string(nil), fact.To...)}
	switch fact.State {
	case facts.ConcurrentUnbound:
		return withBindings(errored(c, ReasonUnboundSelector,
			"to selector(s) bind nothing: "+strings.Join(fact.UnboundTo, ", ")), bindings)
	case facts.ConcurrentHit:
		witnesses := make([]Witness, len(fact.Hits))
		for i, hit := range fact.Hits {
			witnesses[i] = Witness{From: hit.From, To: hit.To}
		}
		result := withBindings(fail(c, "target reachable on a concurrent path"), bindings)
		result.Witnesses = witnesses
		return result
	case facts.ConcurrentClean:
		return withBindings(passWithDetail(c, "no concurrent path found"), bindings)
	case facts.ConcurrentBlind:
		if fact.Blind == nil {
			return withBindings(errored(c, ReasonMalformedClaim,
				"concurrent evaluator returned blind state without evidence"), bindings)
		}
		// Unlike reach and pass_through, the witness carries no From. This kind has
		// no from selector, and the fact's Blind.From is never one: it is the cone's
		// first member for a cone-frontier witness and empty for the dynamic-boundary
		// and ConcurrentDispatch layers. Either way it is an internal traversal
		// detail the claim never named, and emitting it would read as a
		// caller-supplied source the author can act on.
		result := withBindings(errored(c, ReasonBlindFrontier,
			"no concurrent path found, but the frontier is blind at "+fact.Blind.Site), bindings)
		result.Witnesses = []Witness{{BlindSite: fact.Blind.Site}}
		return result
	default:
		return errored(c, ReasonMalformedClaim, "concurrent evaluator returned an unknown state")
	}
}

// concurrentFacts lazily builds the rule-independent surface once per model.
// A model belongs to one Evaluate call, so the cache is file-local and carries
// no package-global state across graphs or claims files.
func (m *model) concurrentFacts() facts.ConcurrentSurface {
	if m.concurrentSurface == nil {
		surface := facts.BuildConcurrentSurface(m.reachIndex)
		m.concurrentSurface = &surface
	}
	return *m.concurrentSurface
}

// evalObligation reads the obligation verdicts the graph already carries; it
// cannot author a rule. Only `satisfied` is accepted in v1 — the inverse
// ("prove this obligation is broken") is not something a caller may assert from
// the outside, and the four abstaining states below have no negative pole to map
// it onto.
//
// Every abstention is an ERROR with its own reason rather than a FAIL: the claim
// asks whether the obligation is proven, and "the producer could not say" is not
// a disproof (tenet 2). Only a concrete VIOLATED fails.
func (m *model) evalObligation(c Claim) Result {
	if c.Name == "" {
		return errored(c, ReasonMalformedClaim, "obligation requires 'name'")
	}
	if c.Expect != "satisfied" {
		return errored(c, ReasonMalformedClaim, `obligation requires expect "satisfied"`)
	}

	fact := facts.EvaluateObligation(m.reachIndex, c.Name)
	switch fact.State {
	case facts.ObligationMissingData:
		return errored(c, ReasonMissingGraphData, "graph carries no obligations section")
	case facts.ObligationUnresolved:
		// The section exists and does not name this rule. Do NOT bind
		// Bindings.Obligation: no rule matched, and a fabricated binding would read
		// as a resolved identity the graph never carried.
		return errored(c, ReasonUnresolved, "no obligation rule named "+strconv.Quote(c.Name))
	}

	// Past this point a rule matched, so the bindings and the complete witness set
	// are the same whatever the dominant status is: a FAIL exposes exactly the
	// evidence a PASS would.
	bindings := Bindings{Obligation: []string{c.Name}}
	witnesses := make([]Witness, len(fact.Records))
	for i, record := range fact.Records {
		witnesses[i] = Witness{
			Rule:   record.Rule,
			Fn:     record.Fn,
			Site:   record.Site,
			Status: record.Status,
			Detail: record.Detail,
		}
	}
	// Human detail names the dominant status and how many records were weighed.
	// The per-record producer prose stays in the witnesses, so no machine consumer
	// has to read this string to learn what happened.
	summary := func(status string) string {
		return fmt.Sprintf("%s across %d matched record(s)", status, len(fact.Records))
	}

	var result Result
	switch fact.State {
	case facts.ObligationViolated:
		result = fail(c, summary(facts.ObligationStatusViolated))
	case facts.ObligationSatisfied:
		result = passWithDetail(c, summary(facts.ObligationStatusSatisfied))
	case facts.ObligationUnknown:
		result = errored(c, ReasonUnknownStatus, summary("unrecognized producer status"))
	case facts.ObligationCantProve:
		result = errored(c, ReasonCantProve, summary(facts.ObligationStatusCantProve))
	case facts.ObligationUnmatched:
		result = errored(c, ReasonUnmatched, summary(facts.ObligationStatusUnmatched))
	default:
		return errored(c, ReasonMalformedClaim, "obligation evaluator returned an unknown state")
	}
	result = withBindings(result, bindings)
	result.Witnesses = witnesses
	return result
}

func (m *model) evalReach(c Claim) Result {
	if !c.From.Present() || !c.To.Present() {
		return errored(c, ReasonMalformedClaim, "reach requires 'from' and 'to'")
	}
	if c.Expect != "present" && c.Expect != "absent" {
		return errored(c, ReasonMalformedClaim, `reach requires expect "present" or "absent"`)
	}

	fact := facts.EvaluateReach(m.reachIndex, c.From.Values(), c.To.Values())
	bindings := Bindings{
		From: append([]string(nil), fact.From...),
		To:   append([]string(nil), fact.To...),
	}
	switch fact.State {
	case facts.ReachUnbound:
		field, selectors := "from", fact.UnboundFrom
		if len(selectors) == 0 {
			field, selectors = "to", fact.UnboundTo
		}
		return withBindings(errored(c, ReasonUnboundSelector,
			fmt.Sprintf("%s selector(s) bind nothing: %s", field, strings.Join(selectors, ", "))), bindings)
	case facts.ReachFound:
		witnesses := make([]Witness, len(fact.Paths))
		for i, path := range fact.Paths {
			witnesses[i] = Witness{
				From: path.From,
				To:   path.To,
				Path: append([]string(nil), path.Path...),
			}
		}
		result := resultForExpectation(c, true, "path found")
		result.Bindings = bindings
		result.Witnesses = witnesses
		return result
	case facts.ReachAbsent:
		result := resultForExpectation(c, false, "no path found")
		result.Bindings = bindings
		return result
	case facts.ReachBlind:
		result := withBindings(errored(c, ReasonBlindFrontier,
			"no path found, but the frontier is blind at "+fact.Blind.Site), bindings)
		result.Witnesses = []Witness{{
			From:      fact.Blind.From,
			BlindSite: fact.Blind.Site,
		}}
		return result
	default:
		return errored(c, ReasonMalformedClaim, "reach evaluator returned an unknown state")
	}
}

func (m *model) evalPassThrough(c Claim) Result {
	if !c.From.Present() || !c.To.Present() || !c.Through.Present() {
		return errored(c, ReasonMalformedClaim, "pass_through requires 'from', 'to', and 'through'")
	}

	fact := facts.EvaluatePassThrough(m.reachIndex, facts.PassThroughInput{
		From: c.From.Values(), To: c.To.Values(), Through: c.Through.Values(),
	})
	bindings := Bindings{
		From:    append([]string(nil), fact.From...),
		To:      append([]string(nil), fact.To...),
		Through: append([]string(nil), fact.Through...),
	}
	if len(fact.UnboundFrom) > 0 {
		return withBindings(errored(c, ReasonUnboundSelector,
			"from selector(s) bind nothing: "+strings.Join(fact.UnboundFrom, ", ")), bindings)
	}
	if len(fact.UnboundTo) > 0 {
		return withBindings(errored(c, ReasonUnboundSelector,
			"to selector(s) bind nothing: "+strings.Join(fact.UnboundTo, ", ")), bindings)
	}
	if len(fact.UnboundThrough) > 0 {
		return withBindings(errored(c, ReasonUnboundSelector,
			"through selector(s) bind nothing: "+strings.Join(fact.UnboundThrough, ", ")), bindings)
	}

	switch fact.State {
	case facts.PassThroughBypassed:
		witnesses := make([]Witness, len(fact.Bypasses))
		for i, bypass := range fact.Bypasses {
			witnesses[i] = Witness{
				From: bypass.From,
				To:   bypass.To,
				Path: append([]string(nil), bypass.Path...),
			}
		}
		result := withBindings(fail(c, "bypass path found"), bindings)
		result.Witnesses = witnesses
		return result
	case facts.PassThroughGuarded:
		return withBindings(passWithDetail(c, "all visible paths pass through the waypoint"), bindings)
	case facts.PassThroughBlind:
		result := withBindings(errored(c, ReasonBlindFrontier,
			"no bypass found, but the frontier is blind at "+fact.Blind.Site), bindings)
		result.Witnesses = []Witness{{
			From:      fact.Blind.From,
			BlindSite: fact.Blind.Site,
		}}
		return result
	case facts.PassThroughUnbound:
		return withBindings(errored(c, ReasonUnboundSelector,
			"pass_through selector family binds nothing"), bindings)
	default:
		return errored(c, ReasonMalformedClaim, "pass_through evaluator returned an unknown state")
	}
}

func passWithDetail(c Claim, detail string) Result {
	result := pass(c)
	result.Detail = detail
	return result
}

func resultForExpectation(c Claim, present bool, detail string) Result {
	matches := (c.Expect == "present" && present) || (c.Expect == "absent" && !present)
	if matches {
		result := pass(c)
		result.Detail = detail
		return result
	}
	return fail(c, detail)
}

// claimFieldChecks is the fixed, ordered list of every kind-field unexpectedField
// screens: the anchor alias `fn` and every per-kind field, each paired with the
// predicate reading whether a claim populates it. It is a package-level var — not an
// inline slice — precisely so a reflection test (TestUnexpectedFieldCoversClaim) can
// assert it stays a COMPLETE parallel of the Claim struct: every json-tagged field
// except `kind` and `id` (claim metadata, allowed on every kind) must appear here.
// A new Claim field missed here would be silently ignored on the wrong kind — the
// verdict-inverting hazard unexpectedField exists to close. The order is fixed so the
// wrong-kind message is deterministic.
var claimFieldChecks = []struct {
	name    string
	present func(Claim) bool
}{
	{"from", func(c Claim) bool { return c.From.Present() }},
	{"to", func(c Claim) bool { return c.To.Present() }},
	{"through", func(c Claim) bool { return c.Through.Present() }},
	{"expect", func(c Claim) bool { return c.Expect != "" }},
	{"fqn", func(c Claim) bool { return c.FQN != "" }},
	{"of", func(c Claim) bool { return c.Of != "" }},
	{"tier", func(c Claim) bool { return c.Tier != nil }},
	{"eq", func(c Claim) bool { return c.Eq != nil }},
	{"counterpart_matching", func(c Claim) bool { return c.CounterpartMatching != "" }},
	{"to_matching", func(c Claim) bool { return c.ToMatching != "" }},
	{"fn", func(c Claim) bool { return c.Fn != "" }},
	{"name", func(c Claim) bool { return c.Name != "" }},
	{"entry_kind", func(c Claim) bool { return c.EntryKind != "" }},
}

// unexpectedField returns the json name of the first populated field the kind
// does not read, or "" when every populated field is allowed. It walks the fixed
// claimFieldChecks order so the message is deterministic. `id` is intentionally
// absent from the checked set — it is claim metadata allowed on every kind, never a
// wrong-kind field.
func unexpectedField(c Claim, allowed []string) string {
	ok := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		ok[a] = true
	}
	for _, f := range claimFieldChecks {
		if f.present(c) && !ok[f.name] {
			return f.name
		}
	}
	return ""
}

// resolveEndpoints resolves an edge claim's from/to endpoints under the shared
// fail-closed rule (each side plain-unique-or-die / regex-any, ERROR on either
// failing). List-shaped inputs abstain before resolution so the widened JSON
// input shape cannot silently widen structural semantics. It is the ONE place
// the three edge kinds' resolution contract lives, so a future change to it
// cannot silently reach only some of them (CLAUDE.md, one source of truth). On
// failure it returns a typed evaluation error plus whichever earlier side
// resolved successfully.
func (m *model) resolveEndpoints(c Claim) (froms, tos []string, bad *evaluationError) {
	if !c.From.Present() || !c.To.Present() {
		return nil, nil, &evaluationError{
			Reason: ReasonMalformedClaim,
			Detail: c.Kind + " requires 'from' and 'to'",
		}
	}
	from, fromScalar := c.From.Scalar()
	to, toScalar := c.To.Scalar()
	if c.From.WasList() || c.To.WasList() || !fromScalar || !toScalar {
		return nil, nil, &evaluationError{
			Reason: ReasonMalformedClaim,
			Detail: c.Kind + " requires scalar 'from' and 'to'",
		}
	}
	froms, bad = m.resolveMany(from)
	if bad != nil {
		return nil, nil, bad
	}
	tos, bad = m.resolveMany(to)
	if bad != nil {
		return froms, nil, bad
	}
	return froms, tos, nil
}

func (m *model) evalEdge(c Claim) Result {
	froms, tos, bad := m.resolveEndpoints(c)
	bindings := Bindings{From: froms, To: tos}
	if bad != nil {
		return withBindings(errored(c, bad.Reason, bad.Detail), bindings)
	}
	if m.anyPair(froms, tos) {
		return withBindings(pass(c), bindings)
	}
	return withBindings(fail(c, "0 edge(s)"), bindings)
}

func (m *model) evalNoEdge(c Claim) Result {
	froms, tos, bad := m.resolveEndpoints(c)
	bindings := Bindings{From: froms, To: tos}
	if bad != nil {
		return withBindings(errored(c, bad.Reason, bad.Detail), bindings)
	}
	present := m.presentPairs(froms, tos)
	if len(present) == 0 {
		return withBindings(pass(c), bindings)
	}
	return withBindings(fail(c, fmt.Sprintf("%d edge(s) present: %s", len(present), fqnres.CapList(present, maxOffenders))), bindings)
}

func (m *model) evalEdgeCount(c Claim) Result {
	if c.Eq == nil {
		return errored(c, ReasonMalformedClaim, "edge_count requires 'eq'")
	}
	froms, tos, bad := m.resolveEndpoints(c)
	bindings := Bindings{From: froms, To: tos}
	if bad != nil {
		return withBindings(errored(c, bad.Reason, bad.Detail), bindings)
	}
	n := m.countPresentPairs(froms, tos)
	if n == *c.Eq {
		return withBindings(pass(c), bindings)
	}
	return withBindings(fail(c, fmt.Sprintf("count %d, want %d", n, *c.Eq)), bindings)
}

func (m *model) evalNode(c Claim) Result {
	q, adet := nodeAnchor(c)
	if adet != "" {
		return errored(c, ReasonMalformedClaim, adet)
	}
	if q == "" {
		return errored(c, ReasonMalformedClaim, "node requires 'fqn'")
	}
	matches, bad := m.resolve(q, m.nodeUniverse, "node")
	if bad != nil {
		return errored(c, bad.Reason, bad.Detail)
	}
	bindings := Bindings{FQN: matches}
	if c.Tier != nil {
		var bad []string
		for _, fqn := range matches {
			tiers := m.nodeTiers[fqn]
			if len(tiers) > 1 {
				// The graph carries this FQN at more than one tier (a non-unique
				// display FQN): the claim is unanswerable, so abstain rather than
				// grade against an arbitrary record.
				return withBindings(errored(c, ReasonAmbiguous,
					fmt.Sprintf("ambiguous tier for %s: graph carries tiers %v", fqn, tiers)), bindings)
			}
			if tiers[0] != *c.Tier {
				bad = append(bad, fmt.Sprintf("%s tier %d", fqn, tiers[0]))
			}
		}
		if len(bad) > 0 {
			return withBindings(fail(c,
				fmt.Sprintf("want tier %d; %s", *c.Tier, fqnres.CapList(bad, maxMatches))), bindings)
		}
	}
	return withBindings(pass(c), bindings)
}

func (m *model) evalNoNode(c Claim) Result {
	q, adet := nodeAnchor(c)
	if adet != "" {
		return errored(c, ReasonMalformedClaim, adet)
	}
	if q == "" {
		return errored(c, ReasonMalformedClaim, "no_node requires 'fqn'")
	}
	// no_node NEVER errors on a resolution OUTCOME: zero matches is the pass,
	// ≥1 is the fail. A malformed regex is still a claim-authoring ERROR.
	res, err := fqnres.Resolve(q, m.nodeUniverse)
	if err != nil {
		return errored(c, ReasonMalformedClaim, err.Error())
	}
	bindings := Bindings{FQN: res.Matches}
	if len(res.Matches) == 0 {
		return withBindings(pass(c), bindings)
	}
	return withBindings(fail(c,
		fmt.Sprintf("%d matching node(s): %s", len(res.Matches), fqnres.CapList(res.Matches, maxMatches))), bindings)
}

func (m *model) evalDegree(c Claim, in bool) Result {
	kind := "out_degree"
	if in {
		kind = "in_degree"
	}
	anchor, adet := degreeAnchor(c)
	if adet != "" {
		return errored(c, ReasonMalformedClaim, adet)
	}
	if anchor == "" {
		return errored(c, ReasonMalformedClaim, kind+" requires 'of'")
	}
	if c.Eq == nil {
		return errored(c, ReasonMalformedClaim, kind+" requires 'eq'")
	}
	if c.CounterpartMatching != "" && c.ToMatching != "" {
		return errored(c, ReasonMalformedClaim, "counterpart_matching and to_matching are mutually exclusive")
	}
	cp := counterpartQuery(c)
	of, bad := m.resolveOne(anchor, m.endpointUniverse, "node/endpoint")
	if bad != nil {
		return errored(c, bad.Reason, bad.Detail)
	}
	bindings := Bindings{Of: []string{of}}
	var counterparts []string
	if in {
		counterparts = m.callers[of]
	} else {
		counterparts = m.callees[of]
	}
	n := len(counterparts)
	if cp != "" {
		// The counterpart filter is a set predicate (it may legitimately match
		// many counterparts, so it is NOT unique-or-die like an endpoint). But it
		// must still be fail-closed against a typo/rename: resolve it against the
		// WHOLE endpoint universe first — a filter that matches NOTHING anywhere
		// is an unresolvable name, not a legitimate "zero counterparts", and
		// ERRORs (otherwise an `eq: 0` claim would pass vacuously the moment the
		// filter name is misspelled — the no_node hazard, one level down). Then
		// count how many of this node's counterparts fall in that matched set.
		filter, err := fqnres.Resolve(cp, m.endpointUniverse)
		if err != nil {
			return withBindings(errored(c, ReasonMalformedClaim, err.Error()), bindings)
		}
		if len(filter.Matches) == 0 {
			// Same UNRESOLVED shape every other resolution failure uses (fqnres.
			// UnresolvedDetail), so a counterpart-filter miss reads like any other
			// unresolved name; the noun names what it failed to match.
			return withBindings(errored(c, ReasonUnresolved,
				fqnres.UnresolvedDetail(cp, "node/endpoint (counterpart filter)")), bindings)
		}
		allowed := setutil.StringSet(filter.Matches)
		n = 0
		for _, cpn := range counterparts {
			if allowed[cpn] {
				n++
			}
		}
	}
	if n == *c.Eq {
		return withBindings(pass(c), bindings)
	}
	return withBindings(fail(c, fmt.Sprintf("degree %d, want %d", n, *c.Eq)), bindings)
}

// evalEntrypoint grades an entrypoint claim: the route/topic/symbol Name must join
// to the handler Fn in the graph's entrypoints[] records. The order is fixed so a
// schema/resolution failure ERRORs BEFORE any FAIL polarity is computed, matching
// every other kind (see the package doc for the polarity WHY):
//
//	(a) require Name (non-whitespace) and Fn; validate EntryKind against the shared
//	    graph.EntrypointKinds vocabulary (an unknown filter fails closed).
//	(b) if the graph carries ZERO entrypoints[] records, ABSTAIN (ERROR) — the join
//	    is absent, so a per-name FAIL would be a confident negative from blindness.
//	(c) resolve Fn over the NODE universe (plain unique-or-die, regex any).
//	(d) collect records whose Kind matches EntryKind (when set) and whose Name matches
//	    KIND-AWARELY: routematch's segment-wise tolerance for an http record against a
//	    route-shaped ('/'-bearing) Name, exact string equality for every other case.
//	(e) if some matched record's Name equals the claim Name verbatim, narrow to those
//	    exact records (the exact-spelling tiebreak for overlapping templates).
//	(f) ZERO matched records is a FAIL — existence IS the assertion.
//	(g) matched records that DISAGREE on the handler (overlapping templates) ERROR
//	    listing the joins, forcing a tighter Name — the native tightening over the FR's
//	    any-record-passes reference.
//	(h) otherwise the matched records agree on exactly one handler H — PASS iff H is in
//	    the resolved-fn set (a plain fn resolved to exactly one; a /regex/ fn may have
//	    resolved to several — membership is the test).
func (m *model) evalEntrypoint(c Claim) Result {
	if strings.TrimSpace(c.Name) == "" {
		// A whitespace-only Name passes a bare != "" check but grades against the
		// bare-"/" root route — a fabricated match. Require real content (tenet 2).
		return errored(c, ReasonMalformedClaim, "entrypoint requires 'name'")
	}
	if c.Fn == "" {
		return errored(c, ReasonMalformedClaim, "entrypoint requires 'fn'")
	}
	if c.EntryKind != "" && !graph.KnownEntrypointKind(c.EntryKind) {
		// Fail closed on an authoring typo: an unknown filter value must ERROR, not
		// silently exclude every record and read like a real zero-match FAIL verdict.
		// The known set is graph.EntrypointKinds (sorted → deterministic detail).
		return errored(c, ReasonMalformedClaim, fmt.Sprintf("unknown entry_kind %s (known kinds: %s)",
			strconv.Quote(c.EntryKind), quotedKinds(graph.EntrypointKinds)))
	}
	if len(m.entrypoints) == 0 {
		// Fail-closed abstention over a BLIND universe: a graph can legitimately carry
		// zero entrypoints[] records (routers outside root discovery's coverage — gin
		// variadic, gorilla chains, gRPC — or a pre-join producer). Grading against an
		// absent join would turn total blindness into a confident "no entrypoint
		// matches" negative, misread as "the route was removed". Abstain instead
		// (tenet 2: abstain over a fabricated pole). Distinct from the per-name
		// zero-match FAIL below, which is only meaningful over a NON-empty join — an
		// entry_kind filter matching zero records over a non-empty universe stays a FAIL.
		return errored(c, ReasonMissingGraphData, "graph carries no entrypoints[] records: the route/topic -> handler join is absent (routers outside root discovery's coverage, or a pre-join producer)")
	}
	resolved, bad := m.resolve(c.Fn, m.nodeUniverse, "node")
	if bad != nil {
		return errored(c, bad.Reason, bad.Detail)
	}
	bindings := Bindings{Fn: resolved}
	var matched []graph.Entrypoint
	for _, ep := range m.entrypoints {
		if c.EntryKind != "" && ep.Kind != c.EntryKind {
			continue
		}
		// Kind-aware name matching. Route grammar (routematch's segment-wise tolerance)
		// applies ONLY between a route-shaped ('/'-bearing) query and an http record;
		// every other case is exact string equality. This is the SAME http/consumer
		// split the triage lens applies — impact.ResolveRoute filters to http records
		// and uses routematch.Match; impact.ResolveEvent matches consumer names by
		// exact equality (parity named on both sides; drift would resolve a topic one
		// way in triage and another in a claim). Two reproduced hazards this closes:
		// (1) a NON-http name is not route grammar — a "$"-prefixed topic like
		// "$internal.events" would act as a universal single-segment wildcard under
		// routematch, false-matching any query (and a consumer-topic claim could then
		// false-PASS via an http route's param-wildcard tail after the real consumer
		// record is deleted — a silent false SATISFIED, tenet 1/4); (2) a claim Name
		// with no '/' cannot be a route path, so it must not tail-match a param-wildcard
		// registration tail (Match("GET /widget/{id}", "GET") was true).
		if ep.Kind == "http" && strings.Contains(c.Name, "/") {
			if !routematch.Match(ep.Name, c.Name) {
				continue
			}
		} else if ep.Name != c.Name {
			continue
		}
		matched = append(matched, ep)
	}
	if len(matched) == 0 {
		return withBindings(fail(c, "no entrypoint matches "+fqnres.QuoteSingle(c.Name)), bindings)
	}
	// Exact-name tiebreak: for the idiomatic literal-vs-template overlap (registrations
	// "GET /users/me" and "GET /users/{id}"), EVERY claim spelling matches both records
	// (a query-side param token wildcards a registration literal too), so the disagree
	// ERROR's "tighten the name" advice is impossible to follow. When some matched
	// record's Name equals the claim Name verbatim, grade against those exact records
	// only: the verbatim registration literal is the tightest possible name, so
	// selecting it reads the author's exact spelling, not a guess. Non-exact spellings
	// still reach the disagree ERROR below.
	if exact := exactNameMatches(matched, c.Name); len(exact) > 0 {
		matched = exact
	}
	for _, ep := range matched {
		bindings.Entrypoint = append(bindings.Entrypoint, entrypointBinding(ep))
	}
	// Track the single agreed handler without materializing the join list; only when
	// the set DISAGREES do we build the (sorted, deduped, QuoteSingle'd, capped) join
	// list for the ERROR — a record set that disagrees on the handler cannot yield one
	// answer, so ERROR and force a tighter Name.
	h := matched[0].Fn
	disagree := false
	for _, ep := range matched[1:] {
		if ep.Fn != h {
			disagree = true
			break
		}
	}
	if disagree {
		joinSet := make(map[string]bool, len(matched))
		for _, ep := range matched {
			joinSet[fqnres.QuoteSingle(ep.Name)+" -> "+ep.Fn] = true
		}
		joins := setutil.SortedKeys(joinSet)
		return withBindings(errored(c, ReasonAmbiguous,
			fmt.Sprintf("ambiguous entrypoint: %s matches %d joins with differing handlers: %s",
				fqnres.QuoteSingle(c.Name), len(joins), fqnres.CapList(joins, maxMatches))), bindings)
	}
	// The matched records agree on exactly one handler H — PASS iff H is in the
	// resolved-fn set (a plain fn resolved to exactly one; a /regex/ fn may have
	// resolved to several — membership is the test).
	for _, r := range resolved {
		if r == h {
			return withBindings(pass(c), bindings)
		}
	}
	return withBindings(fail(c, "handled by "+h), bindings)
}

// entrypointBinding encodes the complete record identity with byte lengths,
// rather than a human delimiter that graph-provided strings could contain.
func entrypointBinding(ep graph.Entrypoint) string {
	return fmt.Sprintf("entrypoint/v1\x00%d:%s\x00%d:%s\x00%d:%s",
		len(ep.Kind), ep.Kind, len(ep.Name), ep.Name, len(ep.Fn), ep.Fn)
}

// quotedKinds renders a sorted kind vocabulary as a comma-separated quoted list for a
// deterministic error detail: `"callback", "consumer", "http", "worker"`.
func quotedKinds(kinds []string) string {
	qs := make([]string, len(kinds))
	for i, k := range kinds {
		qs[i] = strconv.Quote(k)
	}
	return strings.Join(qs, ", ")
}

// exactNameMatches returns the subset of matched records whose Name equals name
// verbatim — the exact-spelling tiebreak's selection (the tightest possible name).
func exactNameMatches(matched []graph.Entrypoint, name string) []graph.Entrypoint {
	var out []graph.Entrypoint
	for _, ep := range matched {
		if ep.Name == name {
			out = append(out, ep)
		}
	}
	return out
}

// counterpartQuery returns the effective counterpart filter — canonical
// counterpart_matching preferred, to_matching as the accepted alias. Both the
// evaluator and label() read it through here so the alias precedence has one
// source and cannot drift between the verdict and its printed label.
func counterpartQuery(c Claim) string {
	if c.CounterpartMatching != "" {
		return c.CounterpartMatching
	}
	return c.ToMatching
}

// anchor resolves an aliased anchor field to its effective value: the canonical
// spelling preferred, the `fn` alias accepted, BOTH present an ERROR (detail
// non-empty, query "") — no silent precedence, mirroring the counterpart alias.
// canonicalName names the canonical field in that error ("fqn" / "of"). It is the
// ONE alias-precedence rule for both the node anchor (fqn/fn) and the degree anchor
// (of/fn), so the evaluator and label() cannot drift (CLAUDE.md: one source of truth).
func anchor(canonical, alias, canonicalName string) (query, detail string) {
	if canonical != "" && alias != "" {
		return "", canonicalName + " and fn are mutually exclusive"
	}
	if canonical != "" {
		return canonical, ""
	}
	return alias, ""
}

// nodeAnchor returns the effective node FQN for a node/no_node claim: canonical
// `fqn` preferred, `fn` accepted as the documented alias (both present → ERROR).
func nodeAnchor(c Claim) (query, detail string) { return anchor(c.FQN, c.Fn, "fqn") }

// degreeAnchor returns the effective anchor for an in_degree/out_degree claim:
// canonical `of` preferred, `fn` accepted as the documented alias (both present → ERROR).
func degreeAnchor(c Claim) (query, detail string) { return anchor(c.Of, c.Fn, "of") }

// resolveMany resolves an edge endpoint: a plain form must be unique (0 →
// unresolved, ≥2 → ambiguous, both ERROR), a regex may match many (≥1). It
// returns the matches, or a typed evaluation error (matches nil).
func (m *model) resolveMany(query string) (matches []string, bad *evaluationError) {
	return m.resolve(query, m.endpointUniverse, "node/endpoint")
}

// resolve applies the shared endpoint rule over an arbitrary universe. noun
// names the universe in an UNRESOLVED detail ("node/endpoint" for the endpoint
// universe, "node" for the node universe) so the message matches the universe
// the claim was resolved against.
func (m *model) resolve(query string, universe []string, noun string) (matches []string, bad *evaluationError) {
	res, err := fqnres.Resolve(query, universe)
	if err != nil {
		return nil, &evaluationError{Reason: ReasonMalformedClaim, Detail: err.Error()}
	}
	if len(res.Matches) == 0 {
		return nil, &evaluationError{
			Reason: ReasonUnresolved,
			Detail: fqnres.UnresolvedDetail(query, noun),
		}
	}
	if !res.IsRegex && res.Ambiguous {
		return nil, &evaluationError{
			Reason: ReasonAmbiguous,
			Detail: fqnres.AmbiguousDetail(query, res.Matches),
		}
	}
	return res.Matches, nil
}

// resolveOne resolves to EXACTLY one endpoint (the anchor of a degree claim):
// a regex that matches more than one is ambiguous here, an ERROR.
func (m *model) resolveOne(query string, universe []string, noun string) (fqn string, bad *evaluationError) {
	matches, bad := m.resolve(query, universe, noun)
	if bad != nil {
		return "", bad
	}
	if len(matches) > 1 {
		return "", &evaluationError{
			Reason: ReasonAmbiguous,
			Detail: fqnres.AmbiguousDetail(query, matches),
		}
	}
	return matches[0], nil
}

func (m *model) anyPair(froms, tos []string) bool {
	for _, f := range froms {
		for _, t := range tos {
			if m.pairs[[2]string{f, t}] {
				return true
			}
		}
	}
	return false
}

// presentPairs returns the sorted "from -> to" strings for every resolved pair
// that exists in the graph. froms and tos are each distinct (resolve returns
// distinct matches), so each (f,t) is visited once — no dedup needed.
func (m *model) presentPairs(froms, tos []string) []string {
	var out []string
	for _, f := range froms {
		for _, t := range tos {
			if m.pairs[[2]string{f, t}] {
				out = append(out, f+" -> "+t)
			}
		}
	}
	sort.Strings(out)
	return out
}

// countPresentPairs counts the resolved pairs present in the graph without
// materializing the offender strings — the count is all edge_count needs.
func (m *model) countPresentPairs(froms, tos []string) int {
	n := 0
	for _, f := range froms {
		for _, t := range tos {
			if m.pairs[[2]string{f, t}] {
				n++
			}
		}
	}
	return n
}

// label is the report-line label for a claim: the free-form `id` when present
// (a suite identifies its claims by id), else an endpoint-derived label. The
// derived label reads the anchor through nodeAnchor/degreeAnchor so an `fn`
// alias is reflected exactly as the evaluator saw it. Selector families retain
// input order and join with ", "; existing scalar labels remain byte-identical.
func label(c Claim) string {
	if c.ID != "" {
		return c.ID
	}
	switch c.Kind {
	case "edge", "no_edge", "edge_count":
		return selectorLabel(c.From) + " -> " + selectorLabel(c.To)
	case "node", "no_node":
		q, det := nodeAnchor(c)
		if det != "" {
			// fqn AND fn both set: the anchor is an ERROR, so nodeAnchor returns an
			// EMPTY query — but the line still needs a label naming WHICH claim erred,
			// or the ERROR renders with a blank label. Name both fields deterministically.
			return c.FQN + "/" + c.Fn
		}
		return q
	case "in_degree", "out_degree":
		q, det := degreeAnchor(c)
		if det != "" {
			// of AND fn both set: same empty-anchor hazard as the node case — a blank
			// label (or a bare " (counterpart …)") would not identify the claim.
			return c.Of + "/" + c.Fn
		}
		if cp := counterpartQuery(c); cp != "" {
			return q + " (counterpart " + cp + ")"
		}
		return q
	case "entrypoint":
		// Mirror the edge label shape: the route/topic → handler join the claim
		// asserts. A required field left empty (an ERRORed claim) renders as an
		// empty side, same as the edge kinds' From/To fallback.
		return c.Name + " -> " + c.Fn
	case "reach":
		return selectorLabel(c.From) + " -> " + selectorLabel(c.To)
	case "pass_through":
		return selectorLabel(c.From) + " -> " + selectorLabel(c.Through) + " -> " + selectorLabel(c.To)
	case "no_concurrent_reach":
		return "concurrent -> " + selectorLabel(c.To)
	case "obligation":
		return c.Name
	default:
		return strings.TrimSpace(selectorLabel(c.From) + selectorLabel(c.To) +
			selectorLabel(c.Through) + c.Expect + c.FQN + c.Of + c.Fn + c.Name)
	}
}

func selectorLabel(selectors Selectors) string {
	return strings.Join(selectors.Values(), ", ")
}

func pass(c Claim) Result {
	return Result{ID: c.ID, Kind: c.Kind, Label: label(c), Outcome: Pass}
}
func fail(c Claim, d string) Result {
	return Result{ID: c.ID, Kind: c.Kind, Label: label(c), Outcome: Fail, Detail: d}
}
func errored(c Claim, reason Reason, detail string) Result {
	return Result{
		ID: c.ID, Kind: c.Kind, Label: label(c),
		Outcome: Errored, Reason: reason, Detail: detail,
	}
}

func withBindings(result Result, bindings Bindings) Result {
	result.Bindings = bindings
	return result
}

func addSet(m map[string]map[string]bool, k, v string) {
	if m[k] == nil {
		m[k] = map[string]bool{}
	}
	m[k][v] = true
}

// sortedSets freezes an adjacency set to sorted distinct slices. It routes
// through setutil.SortedKeys — the codebase's single ordering primitive — so
// its determinism contract cannot drift from every other groundwork pass
// (CLAUDE.md, one source of truth).
func sortedSets(m map[string]map[string]bool) map[string][]string {
	out := make(map[string][]string, len(m))
	for k, set := range m {
		out[k] = setutil.SortedKeys(set)
	}
	return out
}

func sortedIntSets(m map[string]map[int]bool) map[string][]int {
	out := make(map[string][]int, len(m))
	for k, set := range m {
		vs := make([]int, 0, len(set))
		for v := range set {
			vs = append(vs, v)
		}
		sort.Ints(vs)
		out[k] = vs
	}
	return out
}
