package claims

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

func TestMarshalMachineContract(t *testing.T) {
	g := &graph.Graph{
		Stamp: "789abc",
		Tool:  "v0.0.0-test",
		Algo:  "vta",
		Nodes: []graph.Node{
			{FQN: "example.com/p.A", Sig: "func()"},
			{FQN: "example.com/p.B", Sig: "func()"},
		},
		Edges: []graph.Edge{
			{From: "example.com/p.A", To: "example.com/p.B"},
			{From: "example.com/p.A", To: "example.com/p.B"},
		},
	}
	report := Report{
		Results: []Result{
			{
				ID: "present", Kind: "edge", Outcome: Pass,
				Bindings: Bindings{
					From: []string{"example.com/p.A"},
					To:   []string{"example.com/p.B"},
				},
			},
			{ID: "absent", Kind: "node", Outcome: Fail, Detail: "tier mismatch"},
			{
				ID: "unknown", Kind: "edge", Outcome: Errored,
				Reason: ReasonUnresolved, Detail: "does not resolve",
			},
		},
		NumNodes: 2, NumUniquePairs: 1,
	}
	got, err := MarshalMachine(g, report)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{
  "schema_version": "groundwork.assert/v1",
  "fixture": {
    "stamp": "789abc",
    "producer_tool": "v0.0.0-test",
    "algo": "vta",
    "caveats": []
  },
  "results": [
    {
      "id": "present",
      "kind": "edge",
      "outcome": "PASS",
      "bindings": {
        "from": [
          "example.com/p.A"
        ],
        "to": [
          "example.com/p.B"
        ]
      }
    },
    {
      "id": "absent",
      "kind": "node",
      "outcome": "FAIL",
      "detail": "tier mismatch"
    },
    {
      "id": "unknown",
      "kind": "edge",
      "outcome": "ERROR",
      "reason": "UNRESOLVED",
      "detail": "does not resolve"
    }
  ],
  "summary": {
    "passed": 1,
    "failed": 1,
    "errored": 1,
    "nodes": 2,
    "unique_edges": 1
  }
}
`
	if string(got) != want {
		t.Fatalf("machine JSON mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestMarshalMachineCanonicalizesSets(t *testing.T) {
	first := machineCanonicalizationInput(false)
	second := machineCanonicalizationInput(true)
	firstReport := machineCanonicalizationReport(false)
	secondReport := machineCanonicalizationReport(true)
	wantFirstGraph := machineCanonicalizationInput(false)
	wantSecondGraph := machineCanonicalizationInput(true)
	wantFirstReport := machineCanonicalizationReport(false)
	wantSecondReport := machineCanonicalizationReport(true)

	gotFirst, err := MarshalMachine(first, firstReport)
	if err != nil {
		t.Fatal(err)
	}
	gotSecond, err := MarshalMachine(second, secondReport)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotFirst) != string(gotSecond) {
		t.Fatalf("canonical machine JSON differs\nfirst:  %s\nsecond: %s", gotFirst, gotSecond)
	}
	if !reflect.DeepEqual(first, wantFirstGraph) || !reflect.DeepEqual(second, wantSecondGraph) ||
		!reflect.DeepEqual(firstReport, wantFirstReport) || !reflect.DeepEqual(secondReport, wantSecondReport) {
		t.Fatal("MarshalMachine mutated caller-owned graph or report data")
	}
}

func TestMarshalMachineWitnessContract(t *testing.T) {
	path := []string{"z", "a", "m"}
	witnesses := []Witness{
		{From: "same-from", To: "same-to", Path: path, BlindSite: "b"},
		{From: "same-from", To: "same-to", Path: path, Status: "b"},
		{From: "same-from", To: "same-to", Path: path, Rule: "a"},
		{From: "same-from", To: "same-to", Path: path, Detail: "b"},
		{From: "same-from", To: "same-to", Path: path, Fn: "a"},
		{From: "same-from", To: "same-to", Path: path, Site: "b"},
		{From: "same-from", To: "same-to", Path: path, BlindSite: "a"},
		{From: "same-from", To: "same-to", Path: path, Detail: "a"},
		{From: "same-from", To: "same-to", Path: path, Status: "a"},
		{From: "same-from", To: "same-to", Path: path, Site: "a"},
		{From: "same-from", To: "same-to", Path: path, Fn: "b"},
		{From: "same-from", To: "same-to", Path: path, Rule: "b"},
		{From: "same-from", To: "same-to", Path: path, Detail: "b"},
	}
	report := Report{Results: []Result{{
		ID: "witnesses", Kind: "edge", Outcome: Pass, Witnesses: witnesses,
	}}}

	got, err := MarshalMachine(&graph.Graph{}, report)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{
  "schema_version": "groundwork.assert/v1",
  "fixture": {
    "stamp": "",
    "producer_tool": "",
    "algo": "",
    "caveats": []
  },
  "results": [
    {
      "id": "witnesses",
      "kind": "edge",
      "outcome": "PASS",
      "witnesses": [
        {
          "from": "same-from",
          "to": "same-to",
          "path": [
            "z",
            "a",
            "m"
          ],
          "detail": "a"
        },
        {
          "from": "same-from",
          "to": "same-to",
          "path": [
            "z",
            "a",
            "m"
          ],
          "detail": "b"
        },
        {
          "from": "same-from",
          "to": "same-to",
          "path": [
            "z",
            "a",
            "m"
          ],
          "status": "a"
        },
        {
          "from": "same-from",
          "to": "same-to",
          "path": [
            "z",
            "a",
            "m"
          ],
          "status": "b"
        },
        {
          "from": "same-from",
          "to": "same-to",
          "path": [
            "z",
            "a",
            "m"
          ],
          "site": "a"
        },
        {
          "from": "same-from",
          "to": "same-to",
          "path": [
            "z",
            "a",
            "m"
          ],
          "site": "b"
        },
        {
          "from": "same-from",
          "to": "same-to",
          "path": [
            "z",
            "a",
            "m"
          ],
          "fn": "a"
        },
        {
          "from": "same-from",
          "to": "same-to",
          "path": [
            "z",
            "a",
            "m"
          ],
          "fn": "b"
        },
        {
          "from": "same-from",
          "to": "same-to",
          "path": [
            "z",
            "a",
            "m"
          ],
          "rule": "a"
        },
        {
          "from": "same-from",
          "to": "same-to",
          "path": [
            "z",
            "a",
            "m"
          ],
          "rule": "b"
        },
        {
          "from": "same-from",
          "to": "same-to",
          "path": [
            "z",
            "a",
            "m"
          ],
          "blind_site": "a"
        },
        {
          "from": "same-from",
          "to": "same-to",
          "path": [
            "z",
            "a",
            "m"
          ],
          "blind_site": "b"
        }
      ]
    }
  ],
  "summary": {
    "passed": 1,
    "failed": 0,
    "errored": 0,
    "nodes": 0,
    "unique_edges": 0
  }
}
`
	if string(got) != want {
		t.Fatalf("machine witness JSON mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

func TestValidateMachineIDs(t *testing.T) {
	tests := []struct {
		name    string
		ids     []string
		wantErr string
	}{
		{name: "missing ID", ids: []string{""}, wantErr: "claim 1 has no non-whitespace id"},
		{name: "empty ID", ids: []string{"valid", ""}, wantErr: "claim 2 has no non-whitespace id"},
		{name: "whitespace only ID", ids: []string{"valid", " \t\n"}, wantErr: "claim 2 has no non-whitespace id"},
		{name: "exact duplicate", ids: []string{"one", "one"}, wantErr: "claim 2 duplicates id \"one\" first used by claim 1"},
		{name: "byte distinct case variant", ids: []string{"One", "one"}},
		{name: "valid unique IDs", ids: []string{"first", "second", " third "}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := &File{Claims: make([]Claim, len(tt.ids))}
			for i, id := range tt.ids {
				file.Claims[i].ID = id
			}

			err := ValidateMachineIDs(file)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateMachineIDs() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateMachineIDs() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestMarshalMachineRejectsInvalidResultState(t *testing.T) {
	tests := []struct {
		name    string
		result  Result
		wantErr string
	}{
		{name: "unknown outcome", result: Result{Outcome: Outcome(99)}, wantErr: "unknown outcome 99"},
		{name: "PASS reason", result: Result{Outcome: Pass, Reason: ReasonUnresolved}, wantErr: "PASS result has reason"},
		{name: "FAIL reason", result: Result{Outcome: Fail, Reason: ReasonUnresolved}, wantErr: "FAIL result has reason"},
		{name: "ERROR missing reason", result: Result{Outcome: Errored}, wantErr: "ERROR result has invalid reason"},
		{name: "ERROR unknown reason", result: Result{Outcome: Errored, Reason: Reason("OTHER")}, wantErr: "ERROR result has invalid reason"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalMachine(&graph.Graph{}, Report{Results: []Result{tt.result}})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("MarshalMachine() error = %v, want %q", err, tt.wantErr)
			}
			if got != nil {
				t.Fatalf("MarshalMachine() returned bytes with invalid state: %s", got)
			}
		})
	}
}

func TestMarshalMachineReasonVocabulary(t *testing.T) {
	tests := []struct {
		name   string
		reason Reason
		valid  bool
	}{
		{name: "UNRESOLVED", reason: ReasonUnresolved, valid: true},
		{name: "AMBIGUOUS", reason: ReasonAmbiguous, valid: true},
		{name: "UNBOUND_SELECTOR", reason: ReasonUnboundSelector, valid: true},
		{name: "BLIND_FRONTIER", reason: ReasonBlindFrontier, valid: true},
		{name: "MALFORMED_CLAIM", reason: ReasonMalformedClaim, valid: true},
		{name: "UNKNOWN_STATUS", reason: ReasonUnknownStatus, valid: true},
		{name: "MISSING_GRAPH_DATA", reason: ReasonMissingGraphData, valid: true},
		{name: "CANT_PROVE", reason: ReasonCantProve, valid: true},
		{name: "UNMATCHED", reason: ReasonUnmatched, valid: true},
		{name: "empty", reason: "", valid: false},
		{name: "unknown", reason: Reason("OTHER"), valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalMachine(&graph.Graph{}, Report{Results: []Result{{
				ID: "reason", Kind: "edge", Outcome: Errored, Reason: tt.reason,
			}}})
			if tt.valid {
				if err != nil {
					t.Fatalf("MarshalMachine() error = %v", err)
				}
				if got == nil {
					t.Fatal("MarshalMachine() returned nil bytes for accepted reason")
				}
				return
			}
			if err == nil {
				t.Fatalf("MarshalMachine() accepted invalid reason %q", tt.reason)
			}
			if got != nil {
				t.Fatalf("MarshalMachine() returned bytes for invalid reason %q: %s", tt.reason, got)
			}
		})
	}
}

func machineCanonicalizationInput(reverse bool) *graph.Graph {
	caveats := []string{"beta", "alpha", "beta"}
	if reverse {
		caveats = []string{"beta", "beta", "alpha"}
	}
	return &graph.Graph{
		Stamp:   "stamp",
		Tool:    "tool",
		Algo:    "vta",
		Caveats: caveats,
	}
}

func machineCanonicalizationReport(reverse bool) Report {
	bindings := Bindings{
		From:       []string{"from-b", "from-a", "from-b"},
		To:         []string{"to-b", "to-a", "to-b"},
		Through:    []string{"through-b", "through-a", "through-b"},
		FQN:        []string{"fqn-b", "fqn-a", "fqn-b"},
		Of:         []string{"of-b", "of-a", "of-b"},
		Fn:         []string{"fn-b", "fn-a", "fn-b"},
		Entrypoint: []string{"entry-b", "entry-a", "entry-b"},
		Obligation: []string{"obligation-b", "obligation-a", "obligation-b"},
	}
	witnesses := []Witness{
		{From: "b", To: "a", Path: []string{"b", "a"}, BlindSite: "blind-b", Rule: "rule-b", Fn: "fn-b", Site: "site-b", Status: "status-b", Detail: "detail-b"},
		{From: "a", To: "b", Path: []string{"a", "b"}, BlindSite: "blind-a", Rule: "rule-a", Fn: "fn-a", Site: "site-a", Status: "status-a", Detail: "detail-a"},
		{From: "a", To: "b", Path: []string{"a", "b"}, BlindSite: "blind-a", Rule: "rule-a", Fn: "fn-a", Site: "site-a", Status: "status-a", Detail: "detail-a"},
	}
	if reverse {
		bindings = Bindings{
			From:       []string{"from-b", "from-b", "from-a"},
			To:         []string{"to-b", "to-b", "to-a"},
			Through:    []string{"through-b", "through-b", "through-a"},
			FQN:        []string{"fqn-b", "fqn-b", "fqn-a"},
			Of:         []string{"of-b", "of-b", "of-a"},
			Fn:         []string{"fn-b", "fn-b", "fn-a"},
			Entrypoint: []string{"entry-b", "entry-b", "entry-a"},
			Obligation: []string{"obligation-b", "obligation-b", "obligation-a"},
		}
		witnesses = []Witness{witnesses[2], witnesses[1], witnesses[0]}
	}
	return Report{Results: []Result{{
		ID: "result", Kind: "edge", Outcome: Pass, Bindings: bindings, Witnesses: witnesses,
	}}}
}
