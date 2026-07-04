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
//   - Node universe (node / no_node): declared node FQNs only.
//   - Endpoint universe (edge / no_edge / edge_count / in_degree / out_degree):
//     node FQNs ∪ every edge from/to string, so a boundary pseudo-node
//     ("boundary:db QueryContext") — which appears only as an edge endpoint —
//     is claimable.
//
// # Claim kinds and outcomes
//
//	kind        required           passes when                         resolution failure
//	----------  -----------------  ----------------------------------  ------------------
//	edge        from, to           ≥1 unique pair between resolved      ERROR
//	                               endpoints exists
//	no_edge     from, to           NO pair between resolved endpoints   ERROR
//	edge_count  from, to, eq       count of present pairs == eq         ERROR
//	node        fqn [, tier]       fqn resolves (tier matches if set)   ERROR
//	no_node     fqn                fqn resolves to ZERO nodes           never (0=pass, ≥1=FAIL)
//	in_degree   of, eq [, cp]      #distinct callers (filtered) == eq   ERROR
//	out_degree  of, eq [, cp]      #distinct callees (filtered) == eq   ERROR
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
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/setutil"
)

// Caps on the offender/candidate lists a report line prints. Truncation is
// disclosed ("(+N more)"), never silent — a capped list must not read as the
// whole set (tenet 3).
const (
	maxCandidates = 4 // ambiguous-resolution candidates
	maxOffenders  = 3 // no_edge present-pair offenders
	maxMatches    = 4 // no_node / node offending matches
)

// Claim is one asserted fact. It is a union over every kind's fields; strict
// decoding (DisallowUnknownFields) rejects a typo'd field name (form: for
// from:) so it cannot silently become a zero-value claim. Per-kind required
// fields are validated at evaluation, which ERRORs the individual claim rather
// than aborting the file. Eq/Tier are pointers so an omitted count is
// distinguishable from an explicit zero.
type Claim struct {
	Kind                string `json:"kind"`
	From                string `json:"from,omitempty"`
	To                  string `json:"to,omitempty"`
	FQN                 string `json:"fqn,omitempty"`
	Of                  string `json:"of,omitempty"`
	Tier                *int   `json:"tier,omitempty"`
	Eq                  *int   `json:"eq,omitempty"`
	CounterpartMatching string `json:"counterpart_matching,omitempty"`
	ToMatching          string `json:"to_matching,omitempty"`
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

// Result is one evaluated claim, carrying the pre-rendered label and detail so
// the report is a pure format of the results (deterministic).
type Result struct {
	Kind    string
	Label   string
	Outcome Outcome
	Detail  string
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
// order), then ERROR lines (claims-file order), then the summary. Passing
// claims are silent — represented only in the summary count — so the output is
// the actionable set. A fully-passing run prints just the summary line.
func (r Report) String() string {
	var b strings.Builder
	for _, res := range r.Results {
		if res.Outcome == Fail {
			fmt.Fprintf(&b, "FAIL %s %s: %s\n", res.Kind, res.Label, res.Detail)
		}
	}
	for _, res := range r.Results {
		if res.Outcome == Errored {
			fmt.Fprintf(&b, "ERROR %s %s: %s\n", res.Kind, res.Label, res.Detail)
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
	nodeUniverse     []string // sorted declared node FQNs
	endpointUniverse []string // sorted node FQNs ∪ edge endpoints
	pairs            map[[2]string]bool
	callers          map[string][]string // to → sorted distinct froms
	callees          map[string][]string // from → sorted distinct tos
	// nodeTiers maps an FQN to the sorted DISTINCT tiers its node records carry.
	// graph.Load does not guarantee node-FQN uniqueness (a generic instance's
	// display FQN is documented non-unique — see graphio.sortGraph), so a single
	// FQN can carry more than one tier. A tier claim over such an FQN is
	// ambiguous and abstains (ERROR) rather than grading against an arbitrary
	// last-write record (fail closed, tenet 2).
	nodeTiers      map[string][]int
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
	endpointSet := make(map[string]bool, len(nodeSet))
	for k := range nodeSet {
		endpointSet[k] = true
	}
	pairs := make(map[[2]string]bool, len(g.Edges))
	callersSet := map[string]map[string]bool{}
	calleesSet := map[string]map[string]bool{}
	for _, e := range g.Edges {
		endpointSet[e.From] = true
		endpointSet[e.To] = true
		pairs[[2]string{e.From, e.To}] = true
		addSet(callersSet, e.To, e.From)
		addSet(calleesSet, e.From, e.To)
	}
	return &model{
		nodeUniverse:     setutil.SortedKeys(nodeSet),
		endpointUniverse: setutil.SortedKeys(endpointSet),
		pairs:            pairs,
		callers:          sortedSets(callersSet),
		callees:          sortedSets(calleesSet),
		nodeTiers:        sortedIntSets(tierSet),
		numNodes:         len(g.Nodes),
		numUniquePairs:   len(pairs),
	}
}

// allowedFields lists the fields each kind reads. Strict JSON decoding rejects
// UNKNOWN field names (a typo'd `form:`), but a KNOWN field on the wrong kind
// (e.g. `eq` on an `edge`, where the author meant `edge_count` — an ABSENCE
// assertion) decodes cleanly and would otherwise be silently ignored, inverting
// the verdict. Rejecting a field the kind does not read closes that (tenet 2).
var allowedFields = map[string][]string{
	"edge":       {"from", "to"},
	"no_edge":    {"from", "to"},
	"edge_count": {"from", "to", "eq"},
	"node":       {"fqn", "tier"},
	"no_node":    {"fqn"},
	"in_degree":  {"of", "eq", "counterpart_matching", "to_matching"},
	"out_degree": {"of", "eq", "counterpart_matching", "to_matching"},
}

func (m *model) eval(c Claim) Result {
	allowed, ok := allowedFields[c.Kind]
	if !ok {
		return errored(c, "unknown claim kind "+strconv.Quote(c.Kind))
	}
	if f := unexpectedField(c, allowed); f != "" {
		return errored(c, c.Kind+" does not accept field "+strconv.Quote(f))
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
	default:
		return errored(c, "unknown claim kind "+strconv.Quote(c.Kind))
	}
}

// unexpectedField returns the json name of the first populated field the kind
// does not read, or "" when every populated field is allowed. The field order
// is fixed so the message is deterministic.
func unexpectedField(c Claim, allowed []string) string {
	ok := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		ok[a] = true
	}
	for _, f := range []struct {
		name    string
		present bool
	}{
		{"from", c.From != ""},
		{"to", c.To != ""},
		{"fqn", c.FQN != ""},
		{"of", c.Of != ""},
		{"tier", c.Tier != nil},
		{"eq", c.Eq != nil},
		{"counterpart_matching", c.CounterpartMatching != ""},
		{"to_matching", c.ToMatching != ""},
	} {
		if f.present && !ok[f.name] {
			return f.name
		}
	}
	return ""
}

// resolveEndpoints resolves an edge claim's from/to endpoints under the shared
// fail-closed rule (each side plain-unique-or-die / regex-any, ERROR on either
// failing). It is the ONE place the three edge kinds' resolution contract
// lives, so a future change to it cannot silently reach only some of them
// (CLAUDE.md, one source of truth). On failure it returns a non-nil *Result
// (the ERROR) and the caller returns it verbatim.
func (m *model) resolveEndpoints(c Claim) (froms, tos []string, bad *Result) {
	if c.From == "" || c.To == "" {
		r := errored(c, c.Kind+" requires 'from' and 'to'")
		return nil, nil, &r
	}
	froms, det := m.resolveMany(c.From)
	if det != "" {
		r := errored(c, det)
		return nil, nil, &r
	}
	tos, det = m.resolveMany(c.To)
	if det != "" {
		r := errored(c, det)
		return nil, nil, &r
	}
	return froms, tos, nil
}

func (m *model) evalEdge(c Claim) Result {
	froms, tos, bad := m.resolveEndpoints(c)
	if bad != nil {
		return *bad
	}
	if m.anyPair(froms, tos) {
		return pass(c)
	}
	return fail(c, "no edge between the resolved endpoints")
}

func (m *model) evalNoEdge(c Claim) Result {
	froms, tos, bad := m.resolveEndpoints(c)
	if bad != nil {
		return *bad
	}
	present := m.presentPairs(froms, tos)
	if len(present) == 0 {
		return pass(c)
	}
	return fail(c, fmt.Sprintf("%d edge(s) present: %s", len(present), capList(present, maxOffenders)))
}

func (m *model) evalEdgeCount(c Claim) Result {
	if c.Eq == nil {
		return errored(c, "edge_count requires 'eq'")
	}
	froms, tos, bad := m.resolveEndpoints(c)
	if bad != nil {
		return *bad
	}
	n := m.countPresentPairs(froms, tos)
	if n == *c.Eq {
		return pass(c)
	}
	return fail(c, fmt.Sprintf("count %d, want %d", n, *c.Eq))
}

func (m *model) evalNode(c Claim) Result {
	if c.FQN == "" {
		return errored(c, "node requires 'fqn'")
	}
	matches, det := m.resolve(c.FQN, m.nodeUniverse)
	if det != "" {
		return errored(c, det)
	}
	if c.Tier != nil {
		var bad []string
		for _, fqn := range matches {
			tiers := m.nodeTiers[fqn]
			if len(tiers) > 1 {
				// The graph carries this FQN at more than one tier (a non-unique
				// display FQN): the claim is unanswerable, so abstain rather than
				// grade against an arbitrary record.
				return errored(c, fmt.Sprintf("ambiguous tier for %s: graph carries tiers %v", fqn, tiers))
			}
			if tiers[0] != *c.Tier {
				bad = append(bad, fmt.Sprintf("%s tier %d", fqn, tiers[0]))
			}
		}
		if len(bad) > 0 {
			return fail(c, fmt.Sprintf("want tier %d; %s", *c.Tier, capList(bad, maxMatches)))
		}
	}
	return pass(c)
}

func (m *model) evalNoNode(c Claim) Result {
	if c.FQN == "" {
		return errored(c, "no_node requires 'fqn'")
	}
	// no_node NEVER errors on a resolution OUTCOME: zero matches is the pass,
	// ≥1 is the fail. A malformed regex is still a claim-authoring ERROR.
	res, err := fqnres.Resolve(c.FQN, m.nodeUniverse)
	if err != nil {
		return errored(c, err.Error())
	}
	if len(res.Matches) == 0 {
		return pass(c)
	}
	return fail(c, fmt.Sprintf("%d matching node(s): %s", len(res.Matches), capList(res.Matches, maxMatches)))
}

func (m *model) evalDegree(c Claim, in bool) Result {
	kind := "out_degree"
	if in {
		kind = "in_degree"
	}
	if c.Of == "" {
		return errored(c, kind+" requires 'of'")
	}
	if c.Eq == nil {
		return errored(c, kind+" requires 'eq'")
	}
	if c.CounterpartMatching != "" && c.ToMatching != "" {
		return errored(c, "counterpart_matching and to_matching are mutually exclusive")
	}
	cp := counterpartQuery(c)
	of, det := m.resolveOne(c.Of, m.endpointUniverse)
	if det != "" {
		return errored(c, det)
	}
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
			return errored(c, err.Error())
		}
		if len(filter.Matches) == 0 {
			return errored(c, "unresolved counterpart filter "+strconv.Quote(cp))
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
		return pass(c)
	}
	return fail(c, fmt.Sprintf("degree %d, want %d", n, *c.Eq))
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

// resolveMany resolves an edge endpoint: a plain form must be unique (0 →
// unresolved, ≥2 → ambiguous, both ERROR), a regex may match many (≥1). It
// returns the matches, or an ERROR detail string (matches nil).
func (m *model) resolveMany(query string) (matches []string, detail string) {
	return m.resolve(query, m.endpointUniverse)
}

// resolve applies the shared endpoint rule over an arbitrary universe.
func (m *model) resolve(query string, universe []string) (matches []string, detail string) {
	res, err := fqnres.Resolve(query, universe)
	if err != nil {
		return nil, err.Error()
	}
	if len(res.Matches) == 0 {
		return nil, "unresolved " + strconv.Quote(query)
	}
	if !res.IsRegex && res.Ambiguous {
		return nil, ambiguousDetail(query, res.Matches)
	}
	return res.Matches, ""
}

// resolveOne resolves to EXACTLY one endpoint (the anchor of a degree claim):
// a regex that matches more than one is ambiguous here, an ERROR.
func (m *model) resolveOne(query string, universe []string) (fqn string, detail string) {
	matches, det := m.resolve(query, universe)
	if det != "" {
		return "", det
	}
	if len(matches) > 1 {
		return "", ambiguousDetail(query, matches)
	}
	return matches[0], ""
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

func ambiguousDetail(query string, candidates []string) string {
	return fmt.Sprintf("ambiguous %s (%d candidates): %s",
		strconv.Quote(query), len(candidates), capList(candidates, maxCandidates))
}

// capList joins up to n sorted items with ", ", disclosing any truncation so a
// capped list never reads as the whole set.
func capList(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + fmt.Sprintf(" (+%d more)", len(items)-n)
}

func label(c Claim) string {
	switch c.Kind {
	case "edge", "no_edge", "edge_count":
		return c.From + " -> " + c.To
	case "node", "no_node":
		return c.FQN
	case "in_degree", "out_degree":
		if cp := counterpartQuery(c); cp != "" {
			return c.Of + " (counterpart " + cp + ")"
		}
		return c.Of
	default:
		return strings.TrimSpace(c.From + c.To + c.FQN + c.Of)
	}
}

func pass(c Claim) Result { return Result{Kind: c.Kind, Label: label(c), Outcome: Pass} }
func fail(c Claim, d string) Result {
	return Result{Kind: c.Kind, Label: label(c), Outcome: Fail, Detail: d}
}
func errored(c Claim, d string) Result {
	return Result{Kind: c.Kind, Label: label(c), Outcome: Errored, Detail: d}
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
