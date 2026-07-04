package graphio

// Delta is the attribute-aware, structured base→branch difference between two call
// graphs — the ONE comparison both the JSON delta (`flowmap graph --diff` with plain
// JSON output) and the mermaid `:::changed`/`Δ` classification derive from, so the two
// renderings can never disagree on what changed (guarded by a parity test). It is a
// VIEW, never a gate: the verdict stays with `groundwork review`; this makes the delta
// mechanically consumable and legible.
//
// Two blind spots the presence-only diff (added/removed sets) could not express, both
// fail-OPEN directions for artifacts derived from our output, are closed here:
//
//   - a surviving NODE whose tier drifted (the consumer's reported case: tier changed
//     between pins while set-based validation reported "additive only"), and
//   - a surviving (from,to) PAIR whose attribute records changed — a call that became
//     concurrent, gained/lost a `via`, or lost one of two records (a `go`-launched call
//     dropped while the plain call remains).
//
// Counting basis (Phase 0's record-identity rule, docs/groundwork/usage.md): edges are
// compared over UNIQUE (from,to) pairs, and a pair's ATTRIBUTE STATE is the SET of its
// DISTINCT (tier, boundary, concurrent, via) records. Comparing the whole set — not a
// single representative record — is what lets a multiplicity change (two records → one)
// register as a change; a single-record comparison would silently call it unchanged.
//
// Determinism: every output list is sorted on intrinsic keys (fqn; from,to,field), so
// the artifact is byte-identical across runs regardless of graph edge/node order.
//
// Relationship to groundwork review's own delta: `internal/groundwork/review`'s
// graphDelta (review/delta.go) is deliberately set-based and stays separate — it feeds a
// differently-scoped artifact (the review verdict's shape/touch labels), not this
// consumer-facing attribute report. The two are cross-referenced so they do not read as
// an accidental fork.

import (
	"slices"
	"sort"
	"strconv"
	"strings"
)

// GraphDelta is the serialized attribute-aware delta. Field names are snake_case to
// match the artifact the consumer currently hand-builds with jq. Every list is present
// (never null) even when empty, so a consumer can index it unconditionally; Base/Branch
// echo each side's substrate identity so the provenance skew the Caveats warn about is
// also legible structurally.
type GraphDelta struct {
	Base   DeltaProvenance `json:"base"`
	Branch DeltaProvenance `json:"branch"`

	NodesAdded   []string     `json:"nodes_added"`
	NodesRemoved []string     `json:"nodes_removed"`
	NodesChanged []NodeChange `json:"nodes_changed"`

	EdgesAdded   []EdgeEndpoints `json:"edges_added"`
	EdgesRemoved []EdgeEndpoints `json:"edges_removed"`
	EdgesChanged []EdgeChange    `json:"edges_changed"`

	// Caveats discloses a base↔branch SUBSTRATE skew (empty base, --algo/tool/reclaimer
	// mismatch) so a reader never mistakes a substrate difference for a code change — the
	// same honesty channel the mermaid diff carries (provenanceCaveats), reused verbatim.
	Caveats []string `json:"caveats"`
}

// DeltaProvenance echoes one side's substrate identity. Empty fields (a tool-stripped
// golden, a pre-Tool graph) omitempty away and mean "unrecorded", never "same as the
// other side".
type DeltaProvenance struct {
	Tool string `json:"tool,omitempty"`
	Algo string `json:"algo,omitempty"`
}

// NodeChange is a surviving node (present on both sides) whose tier drifted. Tier is the
// only node attribute the delta tracks: it is the computed salience the render and the
// consumer's tier tables read, and the FR's reported drift. Sig/Package/File/Line are
// disclosure-only positional facts (they locate a declaration, they are not a semantic
// attribute), so they are deliberately out of scope here rather than silently folded in.
type NodeChange struct {
	FQN   string `json:"fqn"`
	Field string `json:"field"` // "tier" (the only tracked node attribute today)
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// EdgeChange is one attribute that differs on a surviving (from,to) pair. Old/New carry
// the base/branch value with an UNSET attribute (concurrent=false, empty boundary/via)
// rendered as JSON null — the ∅ the mermaid view shows. A field whose value varies across
// the pair's records (rare: a multi-`via` pair) is carried as a JSON array. A pair with
// two changed fields yields two EdgeChange records; they sort together by (from,to,field).
type EdgeChange struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// EdgeEndpoints names an added or removed (from,to) pair — the unique-pair counting
// basis, so a pair present on only one side appears exactly once regardless of how many
// records it carried.
type EdgeEndpoints struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// attrTuple is one edge record's full attribute identity (Phase 0: record identity is the
// full attribute tuple, not (from,to)). A pair's attribute state is the SET of these.
type attrTuple struct {
	tier       int
	boundary   string
	concurrent bool
	via        string
}

// Delta computes the base→branch attribute-aware delta. Pure function of its two inputs.
func Delta(base, branch *Graph) GraphDelta {
	d := GraphDelta{
		Base:         DeltaProvenance{Tool: base.Tool, Algo: base.Algo},
		Branch:       DeltaProvenance{Tool: branch.Tool, Algo: branch.Algo},
		NodesAdded:   []string{},
		NodesRemoved: []string{},
		NodesChanged: []NodeChange{},
		EdgesAdded:   []EdgeEndpoints{},
		EdgesRemoved: []EdgeEndpoints{},
		EdgesChanged: []EdgeChange{},
		Caveats:      provenanceCaveats(base, branch),
	}
	if d.Caveats == nil {
		d.Caveats = []string{}
	}

	// Nodes: identity is FQN. Added/removed by presence; a surviving node with a drifted
	// tier is the third class the set-based diff cannot express.
	baseN, branchN := nodeIndex(base), nodeIndex(branch)
	for fqn := range branchN {
		if _, ok := baseN[fqn]; !ok {
			d.NodesAdded = append(d.NodesAdded, fqn)
		}
	}
	for fqn, bn := range baseN {
		brn, ok := branchN[fqn]
		if !ok {
			d.NodesRemoved = append(d.NodesRemoved, fqn)
			continue
		}
		if bn.Tier != brn.Tier {
			d.NodesChanged = append(d.NodesChanged, NodeChange{FQN: fqn, Field: "tier", Old: bn.Tier, New: brn.Tier})
		}
	}

	// Edges: identity is the unique (from,to) pair; a pair's attribute state is the SET of
	// its distinct records. Added/removed by pair presence; a surviving pair whose record
	// set differs is the third class.
	basePairs, branchPairs := pairTuples(base), pairTuples(branch)
	for k := range branchPairs {
		if _, ok := basePairs[k]; !ok {
			d.EdgesAdded = append(d.EdgesAdded, EdgeEndpoints{From: k.from, To: k.to})
		}
	}
	for k, bset := range basePairs {
		brset, ok := branchPairs[k]
		if !ok {
			d.EdgesRemoved = append(d.EdgesRemoved, EdgeEndpoints{From: k.from, To: k.to})
			continue
		}
		if !tupleSetsEqual(bset, brset) {
			d.EdgesChanged = append(d.EdgesChanged, edgeFieldChanges(k, bset, brset)...)
		}
	}

	sortDelta(&d)
	return d
}

// sortDelta totally orders every list on intrinsic, run-independent keys so the delta is
// byte-identical across runs (the determinism discipline the whole gating model rests on).
func sortDelta(d *GraphDelta) {
	sort.Strings(d.NodesAdded)
	sort.Strings(d.NodesRemoved)
	sort.Slice(d.NodesChanged, func(i, j int) bool { return d.NodesChanged[i].FQN < d.NodesChanged[j].FQN })
	sortEndpoints(d.EdgesAdded)
	sortEndpoints(d.EdgesRemoved)
	sort.Slice(d.EdgesChanged, func(i, j int) bool {
		a, b := d.EdgesChanged[i], d.EdgesChanged[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Field < b.Field
	})
}

func sortEndpoints(es []EdgeEndpoints) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].From != es[j].From {
			return es[i].From < es[j].From
		}
		return es[i].To < es[j].To
	})
}

// pairTuples groups a graph's edges by (from,to) pair to the SET of distinct attribute
// tuples that pair carries (Phase 0's record identity). Iteration order is irrelevant: it
// fills membership maps only.
func pairTuples(g *Graph) map[ekey]map[attrTuple]bool {
	out := map[ekey]map[attrTuple]bool{}
	for _, e := range g.Edges {
		k := ekey{e.From, e.To}
		if out[k] == nil {
			out[k] = map[attrTuple]bool{}
		}
		out[k][attrTuple{tier: e.Tier, boundary: e.Boundary, concurrent: e.Concurrent, via: e.Via}] = true
	}
	return out
}

func tupleSetsEqual(a, b map[attrTuple]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for t := range a {
		if !b[t] {
			return false
		}
	}
	return true
}

// edgeFieldChanges breaks a changed pair's record-set difference down into per-field
// old→new records. Each field is compared on the SET of values it takes across the pair's
// records: tier on its distinct tier values, concurrent on whether a concurrent record is
// present, boundary/via on their distinct non-empty labels. An unset attribute is nil
// (JSON null / the mermaid ∅).
//
// Soundness fallback: the "changed" classification itself is the full record-SET
// inequality above (tupleSetsEqual), which is exact. This per-field breakdown is a display
// derived from it and can, in a rare field-permuting multiplicity change, find no single
// field whose value-set differs even though the record sets do. When that happens we emit
// one record-level `records` change so a changed pair is NEVER invisible in the delta —
// disclosing the change we cannot cleanly attribute beats dropping it (tenets 2 and 3).
func edgeFieldChanges(k ekey, base, branch map[attrTuple]bool) []EdgeChange {
	var out []EdgeChange
	emit := func(field string, old, nw any) {
		out = append(out, EdgeChange{From: k.from, To: k.to, Field: field, Old: old, New: nw})
	}
	if o, n := tierSig(base), tierSig(branch); !slices.Equal(o, n) {
		emit("tier", scalarOrList(o), scalarOrList(n))
	}
	if o, n := labelSig(base, func(t attrTuple) string { return t.boundary }),
		labelSig(branch, func(t attrTuple) string { return t.boundary }); !slices.Equal(o, n) {
		emit("boundary", scalarOrList(o), scalarOrList(n))
	}
	if o, n := concurrentPresent(base), concurrentPresent(branch); o != n {
		emit("concurrent", boolOrNil(o), boolOrNil(n))
	}
	if o, n := labelSig(base, func(t attrTuple) string { return t.via }),
		labelSig(branch, func(t attrTuple) string { return t.via }); !slices.Equal(o, n) {
		emit("via", scalarOrList(o), scalarOrList(n))
	}
	if len(out) == 0 {
		emit("records", tupleSetRepr(base), tupleSetRepr(branch))
	}
	return out
}

// tierSig is the sorted distinct set of tier values across a pair's records.
func tierSig(set map[attrTuple]bool) []int {
	seen := map[int]bool{}
	var out []int
	for t := range set {
		if !seen[t.tier] {
			seen[t.tier] = true
			out = append(out, t.tier)
		}
	}
	sort.Ints(out)
	return out
}

// labelSig is the sorted distinct set of NON-EMPTY values a string attribute (boundary or
// via) takes across a pair's records — the empty string is the unset state and is folded
// out, so its old/new renders as ∅/null rather than as a literal "".
func labelSig(set map[attrTuple]bool, get func(attrTuple) string) []string {
	seen := map[string]bool{}
	var out []string
	for t := range set {
		if v := get(t); v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func concurrentPresent(set map[attrTuple]bool) bool {
	for t := range set {
		if t.concurrent {
			return true
		}
	}
	return false
}

// scalarOrList renders a distinct-value signature as JSON: nil for empty (the unset ∅), the
// bare value for a singleton (the common case — one record per pair), or a sorted array
// when a field genuinely varies across a pair's records.
func scalarOrList[T any](vals []T) any {
	switch len(vals) {
	case 0:
		return nil
	case 1:
		return vals[0]
	default:
		out := make([]any, len(vals))
		for i, v := range vals {
			out[i] = v
		}
		return out
	}
}

func boolOrNil(b bool) any {
	if b {
		return true
	}
	return nil
}

// tupleSetRepr renders a pair's whole record set as a sorted, deterministic label for the
// `records` fallback — only reached when the record sets differ but no single field's
// value-set does.
func tupleSetRepr(set map[attrTuple]bool) any {
	sigs := make([]string, 0, len(set))
	for t := range set {
		sigs = append(sigs, tupleSig(t))
	}
	sort.Strings(sigs)
	return scalarOrList(sigs)
}

func tupleSig(t attrTuple) string {
	parts := []string{"tier=" + strconv.Itoa(t.tier)}
	if t.concurrent {
		parts = append(parts, "concurrent")
	}
	if t.boundary != "" {
		parts = append(parts, "boundary="+t.boundary)
	}
	if t.via != "" {
		parts = append(parts, "via="+t.via)
	}
	return "(" + strings.Join(parts, ",") + ")"
}
