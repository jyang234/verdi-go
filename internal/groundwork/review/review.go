package review

import (
	"sort"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/groundwork/fitness"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// Review computes the MR review artifact from the base and branch graphs under a
// policy. It is a pure function: identical inputs always yield an identical
// artifact, digest included — which is what makes the verdict a hard, repeatable
// gate rather than an opinion.
//
// The graphs MUST come from trusted CI (flowmap run on the respective code). An
// agent that supplies its own branch graph forges any verdict by omitting an
// edge; groundwork cannot detect that, and does not try to — the trust boundary
// is around graph generation, not here.
func Review(p *policy.Policy, base, branch *graph.Graph) Artifact {
	baseIx, branchIx := graph.NewIndex(base), graph.NewIndex(branch)
	d := diffGraphs(base, branch)

	newViolations, newCautions := newFindings(p, baseIx, branchIx)
	contract := contractChanges(d, baseIx, branchIx)
	effects := ioEffects(d)
	reach := reachExisting(d, baseIx, branchIx)

	a := Artifact{
		Service:       p.Service,
		Shape:         d.shape(),
		Touches:       d.pkgDeltas(),
		NewViolations: newViolations,
		Contract:      contract,
		Effects:       effects,
		Reach:         reach,
		NewCautions:   newCautions,
	}
	a.Verdict = verdict(d, newViolations, contract)
	a.Digest = digestOf(a)
	return a
}

// verdict applies the three-valued rule: an empty delta abstains; a new violation
// or a breaking contract change blocks; otherwise the structure is clear.
func verdict(d graphDelta, violations []Violation, contract []ContractChange) Verdict {
	if d.empty() {
		return NoStructuralSignal
	}
	if len(violations) > 0 || anyBreaking(contract) {
		return Block
	}
	return StructurallyClear
}

// newFindings runs fitness on both graphs and returns the findings present on the
// branch but not the base — the "report only newly-introduced" property.
func newFindings(p *policy.Policy, baseIx, branchIx *graph.Index) (violations, cautions []Violation) {
	baseRes := fitness.Check(p, baseIx)
	branchRes := fitness.Check(p, branchIx)

	baseKeys := map[string]bool{}
	for _, f := range baseRes.Findings {
		baseKeys[findingKey(f)] = true
	}
	for _, f := range branchRes.Findings {
		if baseKeys[findingKey(f)] {
			continue
		}
		v := Violation{Rule: f.Rule, Summary: f.Summary, From: f.From, To: f.To}
		if f.Severity == fitness.Violation {
			violations = append(violations, v)
		} else {
			cautions = append(cautions, v)
		}
	}
	return violations, cautions
}

func findingKey(f fitness.Finding) string {
	return strings.Join([]string{f.Rule, f.From, f.To, f.Summary}, "\x00")
}

// contractChanges reports inter-service surface movement: entrypoints (Sources)
// and bus/outbound effects added or removed. DB effects are excluded — the store
// is the service's own, not its contract. A removal is breaking.
func contractChanges(d graphDelta, baseIx, branchIx *graph.Index) []ContractChange {
	var out []ContractChange

	baseSrc := stringSet(baseIx.Sources())
	for _, s := range branchIx.Sources() {
		if !baseSrc[s] {
			out = append(out, ContractChange{Op: "+", Surface: "entrypoint", Name: fitness.ShortName(s)})
		}
	}
	branchSrc := stringSet(branchIx.Sources())
	for _, s := range baseIx.Sources() {
		if !branchSrc[s] {
			out = append(out, ContractChange{Op: "-", Surface: "entrypoint", Name: fitness.ShortName(s), Breaking: true})
		}
	}

	for _, e := range d.effectsAdded {
		if surface, name, ok := classifyContract(e); ok {
			out = append(out, ContractChange{Op: "+", Surface: surface, Name: name})
		}
	}
	for _, e := range d.effectsRemoved {
		if surface, name, ok := classifyContract(e); ok {
			out = append(out, ContractChange{Op: "-", Surface: surface, Name: name, Breaking: true})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Surface != out[j].Surface {
			return out[i].Surface < out[j].Surface
		}
		if out[i].Op != out[j].Op {
			return out[i].Op < out[j].Op
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// classifyContract maps a boundary effect to its inter-service surface, or
// reports ok=false for a DB effect (internal store, not contract).
func classifyContract(e graph.Edge) (surface, name string, ok bool) {
	f := strings.Fields(strings.TrimPrefix(e.To, "boundary:"))
	if len(f) < 2 {
		return "", "", false
	}
	switch f[0] {
	case "db":
		return "", "", false
	case "bus":
		switch strings.ToUpper(f[1]) {
		case "PUBLISH":
			return "publish", strings.Join(f[2:], " "), true
		case "CONSUME":
			return "consume", strings.Join(f[2:], " "), true
		}
		return "", "", false
	default:
		return "outbound", strings.Join(f, " "), true
	}
}

// ioEffects reports every external I/O effect added or removed, DB writes
// included, with the write flag.
func ioEffects(d graphDelta) []EffectChange {
	var out []EffectChange
	for _, e := range d.effectsAdded {
		out = append(out, EffectChange{Op: "+", Effect: strings.TrimPrefix(e.To, "boundary:"), Write: fitness.IsWrite(e)})
	}
	for _, e := range d.effectsRemoved {
		out = append(out, EffectChange{Op: "-", Effect: strings.TrimPrefix(e.To, "boundary:"), Write: fitness.IsWrite(e)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Effect != out[j].Effect {
			return out[i].Effect < out[j].Effect
		}
		return out[i].Op < out[j].Op
	})
	return out
}

// reachExisting returns the pre-existing entrypoints that the changed sites are
// now live behind — the blast surface the reviewer is now responsible for. New
// entrypoints introduced by the MR are reported in the contract section, not
// here; this is specifically "what that already exists is affected".
func reachExisting(d graphDelta, baseIx, branchIx *graph.Index) []string {
	existing := stringSet(baseIx.Sources())

	sites := map[string]bool{}
	for _, n := range d.nodesAdded {
		sites[n] = true
	}
	for _, e := range d.edgesAdded {
		sites[e[0]] = true
	}
	for _, e := range d.effectsAdded {
		sites[e.From] = true
	}

	hit := map[string]bool{}
	for site := range sites {
		for _, ep := range branchIx.EntrypointCover(site) {
			if existing[ep] {
				hit[ep] = true
			}
		}
	}
	return sortedKeys(hit)
}

func stringSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
