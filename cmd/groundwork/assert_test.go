package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/canonjson"
	"github.com/jyang234/golang-code-graph/internal/groundwork/claims"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

const assertAllPassJSON = `{
  "schema_version": "groundwork.assert/v1",
  "fixture": {
    "stamp": "",
    "producer_tool": "groundwork test",
    "algo": "rta",
    "caveats": []
  },
  "results": [
    {
      "id": "pass",
      "kind": "edge",
      "outcome": "PASS",
      "bindings": {
        "from": [
          "pkg.A"
        ],
        "to": [
          "pkg.B"
        ]
      }
    }
  ],
  "summary": {
    "passed": 1,
    "failed": 0,
    "errored": 0,
    "nodes": 2,
    "unique_edges": 1
  }
}
`

// TestAssertLoansvcAcceptance runs the committed seven-claim file over the
// pinned loansvc graph, exercising all four outcome classes at once — PASS,
// FAIL, AMBIGUOUS, and UNRESOLVED — with the report asserted byte-for-byte and
// the exit CLASS pinned to a verdictError (a FAIL takes precedence over the
// errored claims, exit 1). This fixture is kept alongside the byte-pinned
// spec-acceptance case (TestAssertSpecAcceptance) because it exercises cases the
// spec file does not: a `no_node` claim and a boundary-endpoint edge claim. Its
// claims carry no `id`, so it also pins the id-less fallback label (the
// endpoint-derived label) in the report shape.
func TestAssertLoansvcAcceptance(t *testing.T) {
	const want = `FAIL  store.Loans).SelectLoan -> handler.App).Create [edge] 0 edge(s)
ERROR .Score [node] AMBIGUOUS: '.Score' matches 3: (*example.com/loansvc/internal/client.Bureau).Score; (*example.com/loansvc/internal/scoring.Remote).Score; (*example.com/loansvc/internal/scoring.Stub).Score
ERROR handler.App).Delete [node] UNRESOLVED: 'handler.App).Delete' matches no node
assert: 4 passed, 1 failed, 2 errored (graph: 40 nodes, 49 unique edges)
`
	args := []string{"assert",
		"../../testdata/groundwork/goldens/loansvc.graph.json",
		"../../testdata/groundwork/claims/loansvc-acceptance.claims.json"}

	var err error
	got := captureStdout(t, func() { err = run(args) })
	if got != want {
		t.Errorf("assert report:\n got:\n%s\nwant:\n%s", got, want)
	}
	// A FAIL is a verdictError (exit 1), taking precedence over the errored claims.
	var v verdictError
	if !errors.As(err, &v) {
		t.Errorf("run(assert) = %v (%T), want a verdictError (exit 1)", err, err)
	}
}

// TestAssertSpecAcceptance is the phase's byte-for-byte pin of the companion
// spec's §1.6 acceptance case: the verbatim seven-claim file (ids L1–L7, `fn`
// aliases, `to_matching` on L3) over the pinned loansvc graph must reproduce the
// spec's expected output exactly, and the exit CLASS is a verdictError (a FAIL
// takes precedence over the errored claims, exit 1). The byte-pin is the point —
// if this diverges the schema/report-shape closure regressed, not the golden.
func TestAssertSpecAcceptance(t *testing.T) {
	const want = `FAIL  L5-deliberate-fail [edge] 0 edge(s)
ERROR L6-ambiguous-name [node] AMBIGUOUS: 'Score' matches 3: (*example.com/loansvc/internal/client.Bureau).Score; (*example.com/loansvc/internal/scoring.Remote).Score; (*example.com/loansvc/internal/scoring.Stub).Score
ERROR L7-unresolved-name [edge] UNRESOLVED: 'handler.App).Delete' matches no node/endpoint
assert: 4 passed, 1 failed, 2 errored (graph: 40 nodes, 49 unique edges)
`
	args := []string{"assert",
		"../../testdata/groundwork/goldens/loansvc.graph.json",
		"../../testdata/groundwork/claims/loansvc-spec-acceptance.claims.json"}

	var err error
	got := captureStdout(t, func() { err = run(args) })
	if got != want {
		t.Errorf("assert report:\n got:\n%s\nwant:\n%s", got, want)
	}
	// A FAIL is a verdictError (exit 1), taking precedence over the errored claims.
	var v verdictError
	if !errors.As(err, &v) {
		t.Errorf("run(assert) = %v (%T), want a verdictError (exit 1)", err, err)
	}
}

// TestAssertEntrypointAcceptance byte-pins the FR's six-claim entrypoint
// acceptance file over the pinned loansvc graph — the kind that pins the
// route/topic → handler join the node/edge kinds cannot reach. It exercises the
// PASS cases (exact join L1, entry_kind-filtered consumer L2, observed-path
// wildcard L3) silently and the non-passing set loudly: a wrong-handler FAIL
// (L4), a zero-match FAIL on a removed route (L5), and an AMBIGUOUS `fn`
// resolution ERROR (L6). Exit CLASS is a verdictError (a FAIL takes precedence
// over the errored claim, exit 1).
func TestAssertEntrypointAcceptance(t *testing.T) {
	const want = `FAIL  L4-wrong-fn [entrypoint] handled by (*example.com/loansvc/internal/handler.App).Create
FAIL  L5-absent-route [entrypoint] no entrypoint matches 'POST /loan-application/archive'
ERROR L6-ambiguous-fn [entrypoint] AMBIGUOUS: 'Score' matches 3: (*example.com/loansvc/internal/client.Bureau).Score; (*example.com/loansvc/internal/scoring.Remote).Score; (*example.com/loansvc/internal/scoring.Stub).Score
assert: 3 passed, 2 failed, 1 errored (graph: 40 nodes, 49 unique edges)
`
	args := []string{"assert",
		"../../testdata/groundwork/goldens/loansvc.graph.json",
		"../../testdata/groundwork/claims/loansvc-entrypoint-acceptance.claims.json"}

	var err error
	got := captureStdout(t, func() { err = run(args) })
	if got != want {
		t.Errorf("assert report:\n got:\n%s\nwant:\n%s", got, want)
	}
	// A FAIL is a verdictError (exit 1), taking precedence over the errored claim.
	var v verdictError
	if !errors.As(err, &v) {
		t.Errorf("run(assert) = %v (%T), want a verdictError (exit 1)", err, err)
	}
}

// TestAssertExitClasses pins the three-way exit split on minimal graphs: a
// clean pass (nil), a FAIL (verdictError, exit 1), and an errored-only run
// (plain operational error, exit 2 — the claim's gate could not run).
func TestAssertExitClasses(t *testing.T) {
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "g.json")
	if err := os.WriteFile(graphPath, []byte(`{
	  "algo":"rta",
	  "nodes":[{"fqn":"pkg.A","sig":"func()","tier":1},{"fqn":"pkg.B","sig":"func()","tier":2}],
	  "edges":[{"from":"pkg.A","to":"pkg.B","tier":2}],
	  "blind_spots":[]
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	write := func(claims string) string {
		p := filepath.Join(dir, "c.json")
		if err := os.WriteFile(p, []byte(claims), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// All pass → nil.
	if err := run([]string{"assert", graphPath, write(`{"claims":[{"kind":"edge","from":"pkg.A","to":"pkg.B"}]}`)}); err != nil {
		t.Errorf("clean pass = %v, want nil", err)
	}
	// A FAIL → verdictError (exit 1).
	err := run([]string{"assert", graphPath, write(`{"claims":[{"kind":"edge","from":"pkg.B","to":"pkg.A"}]}`)})
	var v verdictError
	if !errors.As(err, &v) {
		t.Errorf("FAIL run = %v (%T), want verdictError", err, err)
	}
	// Errored-only (no FAIL) → plain operational error (exit 2).
	err = run([]string{"assert", graphPath, write(`{"claims":[{"kind":"node","fqn":"pkg.Missing"}]}`)})
	if err == nil || errors.As(err, &v) {
		t.Errorf("errored-only run = %v (%T), want a non-verdict error", err, err)
	}
}

// TestAssertJSONCommandContract pins JSON mode at the CLI boundary: it emits a
// complete machine report before returning the exit-classifying error. The
// all-pass report is byte-pinned; the remaining cases decode only to inspect
// their distinct exit classifications and reported outcomes.
func TestAssertJSONCommandContract(t *testing.T) {
	graphPath, writeClaims := assertMachineFiles(t)

	allPassClaims := `{"claims":[{"id":"pass","kind":"edge","from":"pkg.A","to":"pkg.B"}]}`

	tests := []struct {
		name          string
		claims        string
		wantError     bool
		wantVerdict   bool
		wantOutcomes  []string
		wantExactJSON string
	}{
		{
			name:          "all pass",
			claims:        allPassClaims,
			wantOutcomes:  []string{"PASS"},
			wantExactJSON: assertAllPassJSON,
		},
		{
			name:         "with FAIL",
			claims:       `{"claims":[{"id":"fail","kind":"edge","from":"pkg.B","to":"pkg.A"}]}`,
			wantError:    true,
			wantVerdict:  true,
			wantOutcomes: []string{"FAIL"},
		},
		{
			name:         "with ERROR only",
			claims:       `{"claims":[{"id":"error","kind":"node","fqn":"pkg.Missing"}]}`,
			wantError:    true,
			wantOutcomes: []string{"ERROR"},
		},
		{
			name:         "with FAIL and ERROR",
			claims:       `{"claims":[{"id":"fail","kind":"edge","from":"pkg.B","to":"pkg.A"},{"id":"error","kind":"node","fqn":"pkg.Missing"}]}`,
			wantError:    true,
			wantVerdict:  true,
			wantOutcomes: []string{"FAIL", "ERROR"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var runErr error
			got := captureStdout(t, func() {
				runErr = run([]string{"assert", graphPath, writeClaims(tt.claims), "--json"})
			})
			var verdict verdictError
			if gotVerdict := errors.As(runErr, &verdict); gotVerdict != tt.wantVerdict {
				t.Fatalf("run error verdict class = %t (%v), want %t", gotVerdict, runErr, tt.wantVerdict)
			}
			if gotError := runErr != nil; gotError != tt.wantError {
				t.Fatalf("run error = %v, want error=%t", runErr, tt.wantError)
			}
			if tt.wantExactJSON != "" {
				if got != tt.wantExactJSON {
					t.Fatalf("JSON output:\n got:\n%s\nwant:\n%s", got, tt.wantExactJSON)
				}
				return
			}

			var report claims.JSONReport
			if err := json.Unmarshal([]byte(got), &report); err != nil {
				t.Fatalf("decode JSON report: %v\n%s", err, got)
			}
			if len(report.Results) != len(tt.wantOutcomes) {
				t.Fatalf("reported %d results, want %d", len(report.Results), len(tt.wantOutcomes))
			}
			for i, want := range tt.wantOutcomes {
				if report.Results[i].Outcome != want {
					t.Errorf("result %d outcome = %q, want %q", i, report.Results[i].Outcome, want)
				}
			}
		})
	}
}

// TestAssertJSONCanonicalAcrossWholeGraphPermutations pins the command boundary:
// each graph collection may arrive in a different order, but no single
// collection's order may alter the machine report for the same claims order.
func TestAssertJSONCanonicalAcrossWholeGraphPermutations(t *testing.T) {
	dir := t.TempDir()
	claimsPath := filepath.Join(dir, "claims.json")
	if err := os.WriteFile(claimsPath, []byte(`{"claims":[{"id":"edge","kind":"edge","from":"pkg.A","to":"pkg.B"},{"id":"node","kind":"node","fqn":"pkg.C"},{"id":"entrypoint","kind":"entrypoint","name":"GET /a","fn":"pkg.A"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	base := graph.Graph{
		Stamp:   "sha-test",
		Tool:    "flowmap-test",
		Algo:    "vta",
		Caveats: []string{"zeta", "alpha", "alpha"},
		Nodes: []graph.Node{
			{FQN: "pkg.A", Sig: "func()", Tier: 1},
			{FQN: "pkg.B", Sig: "func()", Tier: 2},
			{FQN: "pkg.C", Sig: "func()", Tier: 3},
		},
		Edges: []graph.Edge{
			{From: "pkg.A", To: "pkg.B", Tier: 2},
			{From: "pkg.A", To: "pkg.B", Tier: 2},
			{From: "pkg.B", To: "pkg.C", Tier: 3},
		},
		BlindSpots: []graph.BlindSpot{
			{Kind: "reflect", Site: "pkg.A", Detail: "a"},
			{Kind: "unsafe", Site: "pkg.B", Detail: "b"},
			{Kind: "cgo", Site: "pkg.C", Detail: "c"},
		},
		Obligations: []graph.Obligation{
			{Rule: "r1", Kind: "must_pass_through", Fn: "pkg.A", Site: "a", Status: "SATISFIED"},
			{Rule: "r2", Kind: "must_not_reach", Fn: "pkg.B", Site: "b", Status: "VIOLATED"},
			{Rule: "r3", Kind: "must_not_reach", Fn: "pkg.C", Site: "c", Status: "CANT-PROVE"},
		},
		Entrypoints: []graph.Entrypoint{
			{Kind: "http", Name: "GET /a", Fn: "pkg.A"},
			{Kind: "consumer", Name: "events.b", Fn: "pkg.B"},
			{Kind: "worker", Name: "worker-c", Fn: "pkg.C"},
		},
	}

	want := assertMachineJSON(t, writeAssertGraph(t, dir, "baseline", base), claimsPath)
	for _, tt := range []struct {
		name  string
		graph graph.Graph
	}{
		{name: "nodes", graph: graphWithPermutedNodes(base)},
		{name: "edges", graph: graphWithPermutedEdges(base)},
		{name: "blind spots", graph: graphWithPermutedBlindSpots(base)},
		{name: "obligations", graph: graphWithPermutedObligations(base)},
		{name: "entrypoints", graph: graphWithPermutedEntrypoints(base)},
		{name: "caveats", graph: graphWithPermutedCaveats(base)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := assertMachineJSON(t, writeAssertGraph(t, dir, tt.name, tt.graph), claimsPath)
			if !bytes.Equal(got, want) {
				t.Fatalf("%s permutation changed JSON report:\n%s\nwant:\n%s", tt.name, got, want)
			}
		})
	}

	var report claims.JSONReport
	if err := json.Unmarshal(want, &report); err != nil {
		t.Fatalf("decode canonical report: %v", err)
	}
	if got, want := report.Fixture.Caveats, []string{"alpha", "zeta"}; !slices.Equal(got, want) {
		t.Fatalf("fixture caveats = %q, want %q", got, want)
	}
	if report.Summary.UniqueEdges != 2 {
		t.Fatalf("unique edges = %d, want 2 after duplicate-edge deduplication", report.Summary.UniqueEdges)
	}
	if report.Results[2].Bindings == nil {
		t.Fatal("entrypoint result omitted bindings")
	}
	if got := report.Results[2].Bindings.Entrypoint; len(got) != 1 {
		t.Fatalf("entrypoint binding = %q, want one canonical entrypoint identity", got)
	}
}

func graphWithPermutedNodes(base graph.Graph) graph.Graph {
	g := base
	g.Nodes = permuteAssertSlice(base.Nodes, 1, true)
	return g
}

func graphWithPermutedEdges(base graph.Graph) graph.Graph {
	g := base
	g.Edges = permuteAssertSlice(base.Edges, 2, false)
	return g
}

func graphWithPermutedBlindSpots(base graph.Graph) graph.Graph {
	g := base
	g.BlindSpots = permuteAssertSlice(base.BlindSpots, 2, true)
	return g
}

func graphWithPermutedObligations(base graph.Graph) graph.Graph {
	g := base
	g.Obligations = permuteAssertSlice(base.Obligations, 1, true)
	return g
}

func graphWithPermutedEntrypoints(base graph.Graph) graph.Graph {
	g := base
	g.Entrypoints = permuteAssertSlice(base.Entrypoints, 2, true)
	return g
}

func graphWithPermutedCaveats(base graph.Graph) graph.Graph {
	g := base
	g.Caveats = permuteAssertSlice(base.Caveats, 1, true)
	return g
}

func permuteAssertSlice[T any](values []T, offset int, reverse bool) []T {
	out := make([]T, len(values))
	for i := range values {
		j := (i + offset) % len(values)
		if reverse {
			j = len(values) - 1 - j
		}
		out[i] = values[j]
	}
	return out
}

func writeAssertGraph(t *testing.T, dir, name string, g graph.Graph) string {
	t.Helper()
	b, err := canonjson.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%s.graph.json", name))
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertMachineJSON(t *testing.T, graphPath, claimsPath string) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := cmdAssertTo([]string{graphPath, claimsPath, "--json"}, &out); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// TestAssertMachineIDValidation rejects an incomplete machine identity set
// before evaluation, so consumers never receive a partial report they could
// mistake for a complete assertion suite.
func TestAssertMachineIDValidation(t *testing.T) {
	graphPath, writeClaims := assertMachineFiles(t)
	tests := []struct {
		name   string
		claims string
	}{
		{name: "missing ID", claims: `{"claims":[{"kind":"edge","from":"pkg.A","to":"pkg.B"}]}`},
		{name: "duplicate ID", claims: `{"claims":[{"id":"same","kind":"edge","from":"pkg.A","to":"pkg.B"},{"id":"same","kind":"edge","from":"pkg.A","to":"pkg.B"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var runErr error
			got := captureStdout(t, func() {
				runErr = run([]string{"assert", graphPath, writeClaims(tt.claims), "--json"})
			})
			if runErr == nil {
				t.Fatal("run error = nil, want invalid-machine-ID error")
			}
			var verdict verdictError
			if errors.As(runErr, &verdict) {
				t.Fatalf("run error = %v, want operational error", runErr)
			}
			if got != "" {
				t.Fatalf("stdout = %q, want no report", got)
			}
		})
	}
}

// TestAssertStampCommandContract checks that assert binds a report to the
// supplied graph identity before claim evaluation or report emission.
func TestAssertStampCommandContract(t *testing.T) {
	graphPath, writeClaims := assertMachineFiles(t)
	claimsPath := writeClaims(`{"claims":[{"id":"pass","kind":"edge","from":"pkg.A","to":"pkg.B"}]}`)
	stamped := stampedGraphFile(t, graphPath, "sha-good")

	var runErr error
	got := captureStdout(t, func() {
		runErr = run([]string{"assert", stamped, claimsPath, "--expect", "sha-good"})
	})
	if runErr != nil {
		t.Fatalf("matching --expect: %v", runErr)
	}
	if !strings.Contains(got, "assert: 1 passed, 0 failed, 0 errored") {
		t.Fatalf("matching --expect report = %q", got)
	}

	for _, tt := range []struct {
		name string
		args []string
		set  bool
	}{
		{name: "mismatched expect", args: []string{"assert", stamped, claimsPath, "--expect", "sha-bad"}},
		{name: "missing graph stamp", args: []string{"assert", graphPath, claimsPath, "--expect", "sha-good"}},
		{name: "require stamp", args: []string{"assert", graphPath, claimsPath}, set: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(requireStampEnv, "1")
			}
			var err error
			out := captureStdout(t, func() { err = run(tt.args) })
			if err == nil {
				t.Fatal("run error = nil, want operational stamp error")
			}
			var verdict verdictError
			if errors.As(err, &verdict) {
				t.Fatalf("run error = %v, want operational error", err)
			}
			if out != "" {
				t.Fatalf("stdout = %q, want no report", out)
			}
		})
	}
}

// TestAssertReportWriteErrors uses an injected writer rather than replacing
// process-wide os.Stdout, so it remains isolated from tests that may run in
// parallel. Report delivery failures are operational even when evaluation
// itself passes.
func TestAssertReportWriteErrors(t *testing.T) {
	graphPath, writeClaims := assertMachineFiles(t)
	textClaims := writeClaims(`{"claims":[{"kind":"edge","from":"pkg.A","to":"pkg.B"}]}`)
	jsonClaims := writeClaims(`{"claims":[{"id":"pass","kind":"edge","from":"pkg.A","to":"pkg.B"}]}`)
	writeErr := errors.New("stdout unavailable")

	for _, tt := range []struct {
		name string
		args []string
		out  io.Writer
	}{
		{
			name: "text write error",
			args: []string{graphPath, textClaims},
			out:  errorWriter{err: writeErr},
		},
		{
			name: "JSON write error",
			args: []string{graphPath, jsonClaims, "--json"},
			out:  errorWriter{err: writeErr},
		},
		{
			name: "short write",
			args: []string{graphPath, textClaims},
			out:  shortWriter{},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := cmdAssertTo(tt.args, tt.out)
			if err == nil {
				t.Fatal("cmdAssertTo error = nil, want operational write error")
			}
			var verdict verdictError
			if errors.As(err, &verdict) {
				t.Fatalf("cmdAssertTo error = %v, want operational error", err)
			}
			if !strings.Contains(err.Error(), "write report") {
				t.Fatalf("cmdAssertTo error = %v, want write-report context", err)
			}
			if tt.name == "short write" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("cmdAssertTo error = %v, want io.ErrShortWrite", err)
			}
		})
	}
}

// TestAssertDoubleDashCompatibility preserves the flag-package-era invocation
// while proving the new flags remain movable on either side of the delimiter.
func TestAssertDoubleDashCompatibility(t *testing.T) {
	graphPath, writeClaims := assertMachineFiles(t)
	textClaims := writeClaims(`{"claims":[{"kind":"edge","from":"pkg.A","to":"pkg.B"}]}`)
	jsonClaims := writeClaims(`{"claims":[{"id":"pass","kind":"edge","from":"pkg.A","to":"pkg.B"}]}`)
	stamped := stampedGraphFile(t, graphPath, "sha-good")

	for _, tt := range []struct {
		name    string
		args    []string
		wantOut string
	}{
		{
			name:    "text delimiter",
			args:    []string{"--", graphPath, textClaims},
			wantOut: "assert: 1 passed, 0 failed, 0 errored (graph: 2 nodes, 1 unique edges)\n",
		},
		{
			name:    "JSON flag after positionals",
			args:    []string{"--", graphPath, jsonClaims, "--json"},
			wantOut: assertAllPassJSON,
		},
		{
			name:    "expect flag before delimiter",
			args:    []string{"--expect", "sha-good", "--", stamped, textClaims},
			wantOut: "assert: 1 passed, 0 failed, 0 errored (graph: 2 nodes, 1 unique edges)\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := cmdAssertTo(tt.args, &out); err != nil {
				t.Fatalf("cmdAssertTo: %v", err)
			}
			if got := out.String(); got != tt.wantOut {
				t.Fatalf("report bytes:\n got: %q\nwant: %q", got, tt.wantOut)
			}
		})
	}
}

func TestGateHelpUsesStampTerminology(t *testing.T) {
	if strings.Contains(usageBody, "--expect <sha>") {
		t.Fatal("usageBody contains --expect <sha>; all gate help must use --expect <stamp>")
	}
	for _, command := range []string{"fitness", "review", "verify", "assert", "verify-artifact"} {
		err := run([]string{command})
		if err == nil {
			t.Fatalf("%s wrong-arity invocation returned nil", command)
		}
		if !strings.Contains(err.Error(), "--expect <stamp>") {
			t.Errorf("%s usage error = %q, want --expect <stamp>", command, err)
		}
	}
}

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

func assertMachineFiles(t *testing.T) (string, func(string) string) {
	t.Helper()
	dir := t.TempDir()
	graphPath := filepath.Join(dir, "graph.json")
	if err := os.WriteFile(graphPath, []byte(`{
  "algo":"rta",
  "tool":"groundwork test",
  "nodes":[{"fqn":"pkg.A","sig":"func()","tier":1},{"fqn":"pkg.B","sig":"func()","tier":2}],
  "edges":[{"from":"pkg.A","to":"pkg.B","tier":2}],
  "blind_spots":[]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return graphPath, func(contents string) string {
		path := filepath.Join(dir, "claims.json")
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
}

// richGraph is the purpose-built graph the committed assert-rich claims file is
// graded against. Every rich kind gets both a passing and a failing case over the
// SAME graph, so the fixture also proves the four families do not interfere:
// there are no blind spots, so a blind frontier can never be the reason a
// negative claim passes, and the single abstaining claim abstains for an
// obligation reason the graph carries explicitly.
func richGraph() graph.Graph {
	const (
		handler  = "example.com/svc/internal/handler.Handle"
		process  = "example.com/svc/internal/app.Service.Process"
		publish  = "example.com/svc/internal/outbox.Lifecycle.Publish"
		update   = "example.com/svc/internal/store.Store.Update"
		orphan   = "example.com/svc/internal/isolated.Orphan"
		sink     = "example.com/svc/internal/unrelated.Sink"
		spawner  = "example.com/svc/internal/worker.Spawner.Start"
		audit    = "example.com/svc/internal/worker.Audit.Record"
		busTopic = "boundary:bus PUBLISH order.created"
		dbUpdate = "boundary:db UPDATE orders"
		dbInsert = "boundary:db INSERT audit"
	)
	node := func(fqn string, tier int) graph.Node {
		return graph.Node{FQN: fqn, Sig: "func()", Tier: tier}
	}
	return graph.Graph{
		Stamp:   "sha-rich",
		Tool:    "flowmap-test",
		Algo:    "vta",
		Caveats: []string{"zeta", "alpha", "alpha"},
		Nodes: []graph.Node{
			node(handler, 1), node(process, 2), node(publish, 2), node(update, 3),
			node(orphan, 3), node(sink, 3), node(spawner, 1), node(audit, 2),
		},
		Edges: []graph.Edge{
			{From: handler, To: process, Tier: 2},
			{From: process, To: publish, Tier: 2},
			{From: publish, To: busTopic, Tier: 2, Boundary: "outbound-async"},
			{From: process, To: update, Tier: 3},
			{From: update, To: dbUpdate, Tier: 3, Boundary: "db"},
			{From: spawner, To: audit, Tier: 2, Concurrent: true},
			{From: audit, To: dbInsert, Tier: 2, Boundary: "db"},
		},
		// Sited off every cone this fixture evaluates (sink is reached by nothing;
		// spawner is a concurrent SOURCE, not part of the spawned cone), so the
		// permutation subtest is non-vacuous without turning a proven absence into
		// an abstention. Blindness reaching a verdict is covered by the unit tests.
		BlindSpots: []graph.BlindSpot{
			{Kind: "reflect", Site: sink, Detail: "opaque dispatch"},
			{Kind: "unsafe", Site: spawner, Detail: "pointer arithmetic"},
		},
		Obligations: []graph.Obligation{
			{Rule: "tx-must-close", Kind: "must-release", Fn: update, Site: "store.go:31", Status: "SATISFIED", Detail: "closed on every path"},
			{Rule: "tx-must-close", Kind: "must-release", Fn: process, Site: "app.go:14", Status: "SATISFIED", Detail: "closed on every path"},
			{Rule: "lock-must-release", Kind: "must-release", Fn: audit, Site: "audit.go:9", Status: "CANT-PROVE", Detail: "handoff is unprovable"},
		},
		Entrypoints: []graph.Entrypoint{
			{Kind: "http", Name: "POST /orders", Fn: handler},
			{Kind: "consumer", Name: "orders.audit", Fn: spawner},
		},
	}
}

const richClaimsPath = "../../testdata/groundwork/claims/assert-rich.claims.json"

// TestAssertRichMixedFixture grades the committed nine-claim rich file at the
// command boundary. It asserts the machine contract structurally rather than by
// golden bytes: result order, outcome per claim, the reason-only-on-ERROR rule,
// and the canonical shape of every binding and witness. The byte-level pin is
// TestAssertRichCanonicalAcrossWholeGraphPermutations, which requires identical
// bytes for the same claims over six independently shuffled graphs.
func TestAssertRichMixedFixture(t *testing.T) {
	dir := t.TempDir()
	graphPath := writeAssertGraph(t, dir, "rich", richGraph())

	var runErr error
	var out bytes.Buffer
	runErr = cmdAssertTo([]string{graphPath, richClaimsPath, "--json"}, &out)

	// A FAIL outranks the errored claim: the exit class is a verdict (exit 1),
	// not an operational error (exit 2).
	var verdict verdictError
	if !errors.As(runErr, &verdict) {
		t.Fatalf("run error = %v (%T), want a verdictError — FAIL must outrank ERROR", runErr, runErr)
	}

	var report claims.JSONReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode machine report: %v\n%s", err, out.String())
	}

	// Result order equals CLAIM FILE order, read from the file rather than
	// restated here: a reordered report would otherwise still pass a hardcoded
	// list that happened to be written in the report's order.
	file, err := claims.LoadFile(richClaimsPath)
	if err != nil {
		t.Fatalf("load claims fixture: %v", err)
	}
	if len(report.Results) != len(file.Claims) {
		t.Fatalf("reported %d results for %d claims", len(report.Results), len(file.Claims))
	}
	for i, claim := range file.Claims {
		if report.Results[i].ID != claim.ID {
			t.Fatalf("result %d id = %q, want %q — results must stay in claims-file order", i, report.Results[i].ID, claim.ID)
		}
	}

	wantOutcome := map[string]struct{ outcome, reason string }{
		"R1-handler-reaches-store":                    {outcome: "PASS"},
		"R2-isolated-reaches-nothing":                 {outcome: "PASS"},
		"R3-deliberate-fail-handler-does-reach-store": {outcome: "FAIL"},
		"P1-publishes-go-through-outbox":              {outcome: "PASS"},
		"P2-deliberate-fail-writes-bypass-outbox":     {outcome: "FAIL"},
		"C1-no-concurrent-publish":                    {outcome: "PASS"},
		"C2-deliberate-fail-concurrent-audit-insert":  {outcome: "FAIL"},
		"O1-transaction-closes":                       {outcome: "PASS"},
		"O2-lock-release-unprovable":                  {outcome: "ERROR", reason: "CANT_PROVE"},
	}
	if len(wantOutcome) != len(file.Claims) {
		t.Fatalf("expectation table covers %d claims, fixture has %d", len(wantOutcome), len(file.Claims))
	}

	// Every classification below is read from `outcome` and `reason` ONLY. No
	// assertion in this test parses `detail`: a machine consumer must be able to
	// grade the report without reading human prose.
	knownReasons := map[string]bool{
		"UNRESOLVED": true, "AMBIGUOUS": true, "UNBOUND_SELECTOR": true,
		"BLIND_FRONTIER": true, "MALFORMED_CLAIM": true, "UNKNOWN_STATUS": true,
		"MISSING_GRAPH_DATA": true, "CANT_PROVE": true, "UNMATCHED": true,
	}
	var passed, failed, errored int
	for _, result := range report.Results {
		want, ok := wantOutcome[result.ID]
		if !ok {
			t.Fatalf("unexpected result id %q", result.ID)
		}
		if result.Outcome != want.outcome {
			t.Errorf("%s outcome = %q, want %q", result.ID, result.Outcome, want.outcome)
		}
		if result.Reason != want.reason {
			t.Errorf("%s reason = %q, want %q", result.ID, result.Reason, want.reason)
		}
		switch result.Outcome {
		case "PASS":
			passed++
		case "FAIL":
			failed++
		case "ERROR":
			errored++
			if !knownReasons[result.Reason] {
				t.Errorf("%s ERROR reason %q is outside the closed v1 vocabulary", result.ID, result.Reason)
			}
		default:
			t.Errorf("%s outcome %q is outside the closed v1 vocabulary", result.ID, result.Outcome)
		}
		// Reason is carried by ERROR and by nothing else, so its presence alone
		// tells a consumer the result did not evaluate.
		if (result.Reason != "") != (result.Outcome == "ERROR") {
			t.Errorf("%s: outcome %q with reason %q violates reason-only-on-ERROR", result.ID, result.Outcome, result.Reason)
		}
		assertCanonicalEvidence(t, result)
	}

	if passed != 5 || failed != 3 || errored != 1 {
		t.Errorf("outcome counts = %d/%d/%d, want 5 passed, 3 failed, 1 errored", passed, failed, errored)
	}
	if report.Summary.Passed != passed || report.Summary.Failed != failed || report.Summary.Errored != errored {
		t.Errorf("summary %+v disagrees with the results it summarizes", report.Summary)
	}

	// Each family's PASS carries the evidence that justified it, so a reader can
	// tell a real proof from a vacuous one.
	byID := map[string]claims.JSONResult{}
	for _, result := range report.Results {
		byID[result.ID] = result
	}
	if got := byID["R1-handler-reaches-store"].Witnesses; len(got) == 0 || len(got[0].Path) < 2 {
		t.Errorf("positive reach PASS carries no path witness: %+v", got)
	}
	if got := byID["C2-deliberate-fail-concurrent-audit-insert"].Witnesses; len(got) == 0 {
		t.Error("concurrent FAIL carries no hit witness")
	}
	if got := byID["O1-transaction-closes"].Witnesses; len(got) != 2 {
		t.Errorf("obligation PASS exposes %d record witnesses, want 2", len(got))
	}
	if got := byID["O2-lock-release-unprovable"].Bindings; got == nil || len(got.Obligation) != 1 {
		t.Errorf("abstaining obligation lost its rule binding: %+v", got)
	}
}

// assertCanonicalEvidence checks the two properties the v1 contract promises of
// every evidence array: each binding family is sorted and de-duplicated, and
// witnesses are sorted on their complete intrinsic tuple. A witness PATH is an
// ordered BFS sequence and is deliberately NOT sorted.
func assertCanonicalEvidence(t *testing.T, result claims.JSONResult) {
	t.Helper()
	if b := result.Bindings; b != nil {
		for name, values := range map[string][]string{
			"from": b.From, "to": b.To, "through": b.Through, "fqn": b.FQN,
			"of": b.Of, "fn": b.Fn, "entrypoint": b.Entrypoint, "obligation": b.Obligation,
		} {
			for i := 1; i < len(values); i++ {
				if values[i-1] >= values[i] {
					t.Errorf("%s bindings.%s not sorted+deduplicated: %q", result.ID, name, values)
					break
				}
			}
		}
	}
	for i := 1; i < len(result.Witnesses); i++ {
		if witnessTuple(result.Witnesses[i-1]) > witnessTuple(result.Witnesses[i]) {
			t.Errorf("%s witnesses not sorted: %q then %q",
				result.ID, witnessTuple(result.Witnesses[i-1]), witnessTuple(result.Witnesses[i]))
			break
		}
	}
}

func witnessTuple(w claims.JSONWitness) string {
	return strings.Join(append([]string{w.From, w.To}, append(append([]string(nil), w.Path...),
		w.BlindSite, w.Rule, w.Fn, w.Site, w.Status, w.Detail)...), "\x00")
}

// richMachineJSON is assertMachineJSON for a fixture that deliberately contains
// FAILs: the report is still complete and printed, so only a non-verdict error
// is a test failure.
func richMachineJSON(t *testing.T, graphPath, claimsPath string) []byte {
	t.Helper()
	var out bytes.Buffer
	err := cmdAssertTo([]string{graphPath, claimsPath, "--json"}, &out)
	var verdict verdictError
	if err != nil && !errors.As(err, &verdict) {
		t.Fatal(err)
	}
	return out.Bytes()
}

// TestAssertRichCanonicalAcrossWholeGraphPermutations is the byte-level pin for
// the rich kinds: six independently shuffled graphs, one claims order, identical
// report bytes. Reach paths, pass-through bypasses, concurrent hits, and
// obligation witnesses are all evidence derived from graph traversal, so this is
// where an ordering leak in any of the four fact families would surface.
func TestAssertRichCanonicalAcrossWholeGraphPermutations(t *testing.T) {
	dir := t.TempDir()
	base := richGraph()
	want := richMachineJSON(t, writeAssertGraph(t, dir, "rich-baseline", base), richClaimsPath)

	for _, tt := range []struct {
		name  string
		graph graph.Graph
	}{
		{name: "nodes", graph: graphWithPermutedNodes(base)},
		{name: "edges", graph: graphWithPermutedEdges(base)},
		{name: "blind spots", graph: graphWithPermutedBlindSpots(base)},
		{name: "obligations", graph: graphWithPermutedObligations(base)},
		{name: "entrypoints", graph: graphWithPermutedEntrypoints(base)},
		{name: "caveats", graph: graphWithPermutedCaveats(base)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := richMachineJSON(t, writeAssertGraph(t, dir, "rich-"+strings.ReplaceAll(tt.name, " ", "-"), tt.graph), richClaimsPath)
			if !bytes.Equal(got, want) {
				t.Fatalf("%s permutation changed the report:\n%s\nwant:\n%s", tt.name, got, want)
			}
		})
	}

	// Duplicate EDGE records are not evidence — the graph carries the same call
	// twice — so they must collapse to the same bytes. (A duplicate OBLIGATION
	// record is different: two producer verdicts are two records, and the report
	// says so; that asymmetry is pinned in facts.)
	duped := base
	duped.Edges = append(append([]graph.Edge(nil), base.Edges...), base.Edges[0], base.Edges[len(base.Edges)-1])
	if got := richMachineJSON(t, writeAssertGraph(t, dir, "rich-duplicate-edges", duped), richClaimsPath); !bytes.Equal(got, want) {
		t.Fatalf("duplicate edge records changed the report:\n%s\nwant:\n%s", got, want)
	}
}

// TestAssertReachPathStableUnderEdgeOrder pins BFS tie-breaking: with two
// equal-length paths to the same target, the reported witness path must be a
// function of the graph's content, not of which edge the producer emitted first.
func TestAssertReachPathStableUnderEdgeOrder(t *testing.T) {
	const (
		source = "svc.Source"
		viaA   = "svc.AlphaHop"
		viaZ   = "svc.ZetaHop"
		target = "svc.Target"
	)
	build := func(edges []graph.Edge) graph.Graph {
		return graph.Graph{
			Tool: "flowmap-test", Algo: "vta",
			Nodes: []graph.Node{
				{FQN: source, Sig: "func()"}, {FQN: viaA, Sig: "func()"},
				{FQN: viaZ, Sig: "func()"}, {FQN: target, Sig: "func()"},
			},
			Edges: edges,
		}
	}
	forward := []graph.Edge{
		{From: source, To: viaA}, {From: viaA, To: target},
		{From: source, To: viaZ}, {From: viaZ, To: target},
	}
	reversed := []graph.Edge{
		{From: source, To: viaZ}, {From: viaZ, To: target},
		{From: source, To: viaA}, {From: viaA, To: target},
	}

	dir := t.TempDir()
	claimsPath := filepath.Join(dir, "claims.json")
	if err := os.WriteFile(claimsPath, []byte(
		`{"claims":[{"id":"tie","kind":"reach","from":["svc.Source"],"to":["svc.Target"],"expect":"present"}]}`,
	), 0o644); err != nil {
		t.Fatal(err)
	}

	first := richMachineJSON(t, writeAssertGraph(t, dir, "forward", build(forward)), claimsPath)
	second := richMachineJSON(t, writeAssertGraph(t, dir, "reversed", build(reversed)), claimsPath)
	if !bytes.Equal(first, second) {
		t.Fatalf("edge order changed the chosen shortest path:\n%s\nwant:\n%s", second, first)
	}

	var report claims.JSONReport
	if err := json.Unmarshal(first, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 1 || len(report.Results[0].Witnesses) == 0 {
		t.Fatalf("no path witness in %s", first)
	}
	if got, want := report.Results[0].Witnesses[0].Path, []string{source, viaA, target}; !slices.Equal(got, want) {
		t.Fatalf("tie-broken path = %q, want the lexicographically first hop %q", got, want)
	}
}
