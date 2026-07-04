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
	// bytes) means the input is not the single claims file it claims to be.
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
	nodeTier         map[string]int
	numNodes         int
	numUniquePairs   int
}

func newModel(g *graph.Graph) *model {
	nodeSet := make(map[string]bool, len(g.Nodes))
	tier := make(map[string]int, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeSet[n.FQN] = true
		tier[n.FQN] = n.Tier
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
		nodeUniverse:     sortedKeys(nodeSet),
		endpointUniverse: sortedKeys(endpointSet),
		pairs:            pairs,
		callers:          sortedSets(callersSet),
		callees:          sortedSets(calleesSet),
		nodeTier:         tier,
		numNodes:         len(g.Nodes),
		numUniquePairs:   len(pairs),
	}
}

func (m *model) eval(c Claim) Result {
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

func (m *model) evalEdge(c Claim) Result {
	if c.From == "" || c.To == "" {
		return errored(c, "edge requires 'from' and 'to'")
	}
	froms, det := m.resolveMany(c.From)
	if det != "" {
		return errored(c, det)
	}
	tos, det := m.resolveMany(c.To)
	if det != "" {
		return errored(c, det)
	}
	if m.anyPair(froms, tos) {
		return pass(c)
	}
	return fail(c, "no edge between the resolved endpoints")
}

func (m *model) evalNoEdge(c Claim) Result {
	if c.From == "" || c.To == "" {
		return errored(c, "no_edge requires 'from' and 'to'")
	}
	froms, det := m.resolveMany(c.From)
	if det != "" {
		return errored(c, det)
	}
	tos, det := m.resolveMany(c.To)
	if det != "" {
		return errored(c, det)
	}
	present := m.presentPairs(froms, tos)
	if len(present) == 0 {
		return pass(c)
	}
	return fail(c, fmt.Sprintf("%d edge(s) present: %s", len(present), capList(present, maxOffenders)))
}

func (m *model) evalEdgeCount(c Claim) Result {
	if c.From == "" || c.To == "" {
		return errored(c, "edge_count requires 'from' and 'to'")
	}
	if c.Eq == nil {
		return errored(c, "edge_count requires 'eq'")
	}
	froms, det := m.resolveMany(c.From)
	if det != "" {
		return errored(c, det)
	}
	tos, det := m.resolveMany(c.To)
	if det != "" {
		return errored(c, det)
	}
	n := len(m.presentPairs(froms, tos))
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
			if m.nodeTier[fqn] != *c.Tier {
				bad = append(bad, fmt.Sprintf("%s tier %d", fqn, m.nodeTier[fqn]))
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
	cp := c.CounterpartMatching
	if cp == "" {
		cp = c.ToMatching
	}
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
		res, err := fqnres.Resolve(cp, counterparts)
		if err != nil {
			return errored(c, err.Error())
		}
		n = len(res.Matches)
	}
	if n == *c.Eq {
		return pass(c)
	}
	return fail(c, fmt.Sprintf("degree %d, want %d", n, *c.Eq))
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

// presentPairs returns the sorted, distinct "from -> to" strings for every
// resolved pair that exists in the graph.
func (m *model) presentPairs(froms, tos []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range froms {
		for _, t := range tos {
			if m.pairs[[2]string{f, t}] {
				s := f + " -> " + t
				if !seen[s] {
					seen[s] = true
					out = append(out, s)
				}
			}
		}
	}
	sort.Strings(out)
	return out
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
		if c.CounterpartMatching != "" {
			return c.Of + " (counterpart " + c.CounterpartMatching + ")"
		}
		if c.ToMatching != "" {
			return c.Of + " (counterpart " + c.ToMatching + ")"
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

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedSets(m map[string]map[string]bool) map[string][]string {
	out := make(map[string][]string, len(m))
	for k, set := range m {
		out[k] = sortedKeys(set)
	}
	return out
}
