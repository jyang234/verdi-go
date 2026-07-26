package claims

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/canonjson"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// MachineSchemaVersion identifies the versioned machine-report contract.
const MachineSchemaVersion = "groundwork.assert/v1"

// JSONReport is the public machine-report DTO.
type JSONReport struct {
	SchemaVersion string       `json:"schema_version"`
	Fixture       JSONFixture  `json:"fixture"`
	Results       []JSONResult `json:"results"`
	Summary       JSONSummary  `json:"summary"`
}

// JSONFixture records the graph provenance the report was evaluated against.
type JSONFixture struct {
	Stamp        string   `json:"stamp"`
	ProducerTool string   `json:"producer_tool"`
	Algo         string   `json:"algo"`
	Caveats      []string `json:"caveats"`
}

// JSONResult is one machine-report result.
type JSONResult struct {
	ID        string        `json:"id"`
	Kind      string        `json:"kind"`
	Outcome   string        `json:"outcome"`
	Reason    string        `json:"reason,omitempty"`
	Detail    string        `json:"detail,omitempty"`
	Bindings  *JSONBindings `json:"bindings,omitempty"`
	Witnesses []JSONWitness `json:"witnesses,omitempty"`
}

// JSONBindings carries canonical graph identities for a result.
type JSONBindings struct {
	From       []string `json:"from,omitempty"`
	To         []string `json:"to,omitempty"`
	Through    []string `json:"through,omitempty"`
	FQN        []string `json:"fqn,omitempty"`
	Of         []string `json:"of,omitempty"`
	Fn         []string `json:"fn,omitempty"`
	Entrypoint []string `json:"entrypoint,omitempty"`
	Obligation []string `json:"obligation,omitempty"`
}

// JSONWitness carries deterministic graph evidence for a result.
type JSONWitness struct {
	From      string   `json:"from,omitempty"`
	To        string   `json:"to,omitempty"`
	Path      []string `json:"path,omitempty"`
	BlindSite string   `json:"blind_site,omitempty"`
	Rule      string   `json:"rule,omitempty"`
	Fn        string   `json:"fn,omitempty"`
	Site      string   `json:"site,omitempty"`
	Status    string   `json:"status,omitempty"`
	Detail    string   `json:"detail,omitempty"`
}

// JSONSummary counts the outcomes and graph universe used by the report.
type JSONSummary struct {
	Passed      int `json:"passed"`
	Failed      int `json:"failed"`
	Errored     int `json:"errored"`
	Nodes       int `json:"nodes"`
	UniqueEdges int `json:"unique_edges"`
}

// MarshalMachine serializes report as the versioned, deterministic machine DTO.
// It rejects an invalid internal result state instead of widening the contract.
func MarshalMachine(g *graph.Graph, report Report) ([]byte, error) {
	if g == nil {
		return nil, fmt.Errorf("machine report requires a graph")
	}

	results := make([]JSONResult, len(report.Results))
	for i, result := range report.Results {
		machineResult, err := machineResult(result)
		if err != nil {
			return nil, fmt.Errorf("result %d: %w", i+1, err)
		}
		results[i] = machineResult
	}

	ix := graph.NewIndex(g)
	dto := JSONReport{
		SchemaVersion: MachineSchemaVersion,
		Fixture: JSONFixture{
			Stamp:        g.Stamp,
			ProducerTool: g.Tool,
			Algo:         g.Algo,
			Caveats:      canonicalStrings(ix.GateCaveats("")),
		},
		Results: results,
		Summary: JSONSummary{
			Passed:      report.Passed(),
			Failed:      report.Failed(),
			Errored:     report.Errored(),
			Nodes:       report.NumNodes,
			UniqueEdges: report.NumUniquePairs,
		},
	}
	return canonjson.Marshal(dto)
}

func machineResult(result Result) (JSONResult, error) {
	outcome, err := machineOutcome(result.Outcome, result.Reason)
	if err != nil {
		return JSONResult{}, err
	}

	bindings := machineBindings(result.Bindings)
	return JSONResult{
		ID:        result.ID,
		Kind:      result.Kind,
		Outcome:   outcome,
		Reason:    string(result.Reason),
		Detail:    result.Detail,
		Bindings:  bindings,
		Witnesses: canonicalWitnesses(result.Witnesses),
	}, nil
}

func machineOutcome(outcome Outcome, reason Reason) (string, error) {
	switch outcome {
	case Pass:
		if reason != "" {
			return "", fmt.Errorf("PASS result has reason %q", reason)
		}
		return "PASS", nil
	case Fail:
		if reason != "" {
			return "", fmt.Errorf("FAIL result has reason %q", reason)
		}
		return "FAIL", nil
	case Errored:
		if !knownReason(reason) {
			return "", fmt.Errorf("ERROR result has invalid reason %q", reason)
		}
		return "ERROR", nil
	default:
		return "", fmt.Errorf("unknown outcome %d", outcome)
	}
}

func knownReason(reason Reason) bool {
	switch reason {
	case ReasonUnresolved, ReasonAmbiguous, ReasonUnboundSelector, ReasonBlindFrontier,
		ReasonMalformedClaim, ReasonUnknownStatus, ReasonMissingGraphData, ReasonCantProve,
		ReasonUnmatched:
		return true
	default:
		return false
	}
}

func machineBindings(bindings Bindings) *JSONBindings {
	result := &JSONBindings{
		From:       canonicalStrings(bindings.From),
		To:         canonicalStrings(bindings.To),
		Through:    canonicalStrings(bindings.Through),
		FQN:        canonicalStrings(bindings.FQN),
		Of:         canonicalStrings(bindings.Of),
		Fn:         canonicalStrings(bindings.Fn),
		Entrypoint: canonicalStrings(bindings.Entrypoint),
		Obligation: canonicalStrings(bindings.Obligation),
	}
	if len(result.From) == 0 && len(result.To) == 0 && len(result.Through) == 0 &&
		len(result.FQN) == 0 && len(result.Of) == 0 && len(result.Fn) == 0 &&
		len(result.Entrypoint) == 0 && len(result.Obligation) == 0 {
		return nil
	}
	return result
}

// canonicalStrings returns a sorted, de-duplicated copy of values.
func canonicalStrings(values []string) []string {
	result := append(make([]string, 0, len(values)), values...)
	sort.Strings(result)
	if len(result) == 0 {
		return result
	}

	n := 1
	for _, value := range result[1:] {
		if value == result[n-1] {
			continue
		}
		result[n] = value
		n++
	}
	return result[:n]
}

func canonicalWitnesses(witnesses []Witness) []JSONWitness {
	result := make([]JSONWitness, len(witnesses))
	for i, witness := range witnesses {
		result[i] = JSONWitness{
			From:      witness.From,
			To:        witness.To,
			Path:      append([]string(nil), witness.Path...),
			BlindSite: witness.BlindSite,
			Rule:      witness.Rule,
			Fn:        witness.Fn,
			Site:      witness.Site,
			Status:    witness.Status,
			Detail:    witness.Detail,
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return compareWitnesses(result[i], result[j]) < 0
	})
	if len(result) == 0 {
		return result
	}

	n := 1
	for _, witness := range result[1:] {
		if compareWitnesses(witness, result[n-1]) == 0 {
			continue
		}
		result[n] = witness
		n++
	}
	return result[:n]
}

func compareWitnesses(left, right JSONWitness) int {
	for _, pair := range [][2]string{
		{left.From, right.From},
		{left.To, right.To},
	} {
		if cmp := strings.Compare(pair[0], pair[1]); cmp != 0 {
			return cmp
		}
	}
	if cmp := comparePaths(left.Path, right.Path); cmp != 0 {
		return cmp
	}
	for _, pair := range [][2]string{
		{left.BlindSite, right.BlindSite},
		{left.Rule, right.Rule},
		{left.Fn, right.Fn},
		{left.Site, right.Site},
		{left.Status, right.Status},
		{left.Detail, right.Detail},
	} {
		if cmp := strings.Compare(pair[0], pair[1]); cmp != 0 {
			return cmp
		}
	}
	return 0
}

func comparePaths(left, right []string) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		if cmp := strings.Compare(left[i], right[i]); cmp != 0 {
			return cmp
		}
	}
	switch {
	case len(left) < len(right):
		return -1
	case len(left) > len(right):
		return 1
	default:
		return 0
	}
}

// ValidateMachineIDs rejects missing or duplicate IDs in file order.
func ValidateMachineIDs(file *File) error {
	if file == nil {
		return fmt.Errorf("claims file is nil")
	}
	seen := make(map[string]int, len(file.Claims))
	for i, claim := range file.Claims {
		if strings.TrimSpace(claim.ID) == "" {
			return fmt.Errorf("claim %d has no non-whitespace id", i+1)
		}
		if first, ok := seen[claim.ID]; ok {
			return fmt.Errorf("claim %d duplicates id %q first used by claim %d", i+1, claim.ID, first)
		}
		seen[claim.ID] = i + 1
	}
	return nil
}
