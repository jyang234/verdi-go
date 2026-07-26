package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/claims"
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
