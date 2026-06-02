package ingest

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jyang234/golang-code-graph/ir"
)

// EffectSchemaVersion identifies the post-hoc behavioral golden's canonical form.
// It is deliberately distinct from the in-process trace schema (flowmap.trace/v1):
// the post-hoc gate asserts the boundary-effect *set*, not the ordered span tree
// (post-hoc design §3 / D-PH3), so the two artifacts are not interchangeable.
const EffectSchemaVersion = "flowmap.effects/v1"

// EffectGolden is the gated post-hoc artifact for one flow slug on one service:
// the set of boundary effects (published/consumed events, outbound HTTP/RPC
// dependencies) the flow exercised. Comparison is set-based with no-new-effects
// semantics (CompareEffects), which is what keeps the gate stable under a
// distributed run that legitimately under-exercises a flow.
type EffectGolden struct {
	SchemaVersion string   `json:"schema_version"`
	Flow          string   `json:"flow"`
	Service       string   `json:"service"`
	Effects       []string `json:"effects"` // sorted boundary-effect op keys
}

// NewEffectGolden builds a golden from an observed effect set (sorted, deduped).
func NewEffectGolden(flow, service string, effects []string) EffectGolden {
	return EffectGolden{
		SchemaVersion: EffectSchemaVersion,
		Flow:          flow,
		Service:       service,
		Effects:       sortedUnique(effects),
	}
}

// Marshal renders the golden as deterministic, newline-terminated JSON.
func (g EffectGolden) Marshal() ([]byte, error) {
	g.Effects = sortedUnique(g.Effects)
	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// LoadEffectGolden reads a committed effect golden.
func LoadEffectGolden(path string) (EffectGolden, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return EffectGolden{}, err
	}
	var g EffectGolden
	if err := json.Unmarshal(b, &g); err != nil {
		return EffectGolden{}, fmt.Errorf("%s: %w", path, err)
	}
	return g, nil
}

// BoundaryEffects returns the sorted, deduped set of canonical op keys in a
// trace that name a boundary effect — a published/consumed event or an outbound
// HTTP/RPC dependency. These are the keys the coverage join speaks (plan [H2]);
// internal compute and DB operations are not inter-service boundary effects and
// are omitted. This is the post-hoc assertion unit (design D-PH1: the slug's
// effect set, unioned across its traces).
func BoundaryEffects(root *ir.CanonicalSpan) []string {
	set := map[string]bool{}
	var walk func(*ir.CanonicalSpan)
	walk = func(n *ir.CanonicalSpan) {
		if n == nil {
			return
		}
		if isBoundaryOp(n.Op) {
			set[n.Op] = true
		}
		for _, g := range n.Children {
			for _, m := range g.Members {
				walk(m)
			}
		}
	}
	walk(root)
	return sortedSet(set)
}

func isBoundaryOp(op string) bool {
	for _, p := range []string{"PUBLISH ", "CONSUME ", "HTTP ", "RPC "} {
		if strings.HasPrefix(op, p) {
			return true
		}
	}
	return false
}

// CompareEffects implements the no-new-effects gate (design D-PH3). added is the
// effects observed but absent from the golden — the contract additions that fail
// the gate and route to review. missing is in the golden but not observed — an
// under-exercised effect, surfaced as information (coverage), never a failure: a
// distributed run legitimately does not exercise every path. Both inputs may be
// unsorted; the results are sorted.
func CompareEffects(golden, observed []string) (added, missing []string) {
	g := toSet(golden)
	o := toSet(observed)
	for k := range o {
		if !g[k] {
			added = append(added, k)
		}
	}
	for k := range g {
		if !o[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(added)
	sort.Strings(missing)
	return added, missing
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedUnique(ss []string) []string { return sortedSet(toSet(ss)) }
