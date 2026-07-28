// Package ciparity holds self-checking parity guards between the Makefile (the
// local gate) and the CI workflow (the remote gate). CLAUDE.md's trust boundary
// requires "CI mirrors make verify exactly"; a pin that lives in two files as prose
// drifts silently. These tests turn that prose into an enforced invariant. The
// package imports nothing from the toolchain so it can read the raw config files.
package ciparity

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// repoRoot resolves the module root from this test file's location.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// TestGolangciLintVersionParity pins R-6: the golangci-lint version the Makefile
// pins (GOLANGCI_LINT_VERSION) and the version CI installs (the `@vX.Y.Z` on the
// `go install .../golangci-lint@...` line in gates.yml) must be byte-identical, so
// `make lint` and the CI lint step run the SAME linter build. The two were kept in
// step only by a comment on each side; this guard makes the invariant self-checking
// (CLAUDE.md: parity needs a guard, not just prose).
func TestGolangciLintVersionParity(t *testing.T) {
	root := repoRoot(t)

	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	mkMatch := regexp.MustCompile(`(?m)^GOLANGCI_LINT_VERSION\s*\?=\s*(v[0-9]+\.[0-9]+\.[0-9]+)`).FindSubmatch(mk)
	if mkMatch == nil {
		t.Fatal("Makefile: could not find GOLANGCI_LINT_VERSION pin")
	}
	mkVer := string(mkMatch[1])

	ci, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "gates.yml"))
	if err != nil {
		t.Fatalf("read gates.yml: %v", err)
	}
	ciMatch := regexp.MustCompile(`golangci-lint@(v[0-9]+\.[0-9]+\.[0-9]+)`).FindAllSubmatch(ci, -1)
	if len(ciMatch) == 0 {
		t.Fatal("gates.yml: could not find a golangci-lint@vX.Y.Z install pin")
	}
	for _, m := range ciMatch {
		if ciVer := string(m[1]); ciVer != mkVer {
			t.Errorf("golangci-lint version drift: Makefile pins %s but gates.yml installs %s — "+
				"`make lint` and CI would run different linter builds (R-6)", mkVer, ciVer)
		}
	}
}

// TestRaceTimeoutParity pins the -race per-package timeout the local gate and the
// remote gate use to the SAME value. Go's default is 600s per package; the static
// packages outgrew it (graphio 579s, cmd/flowmap 571s on the CI runners), so one
// commit passed on one runner and timed out on its twin. Raising the ceiling in
// one file only would restore precisely the split-brain this package forbids: a
// `make test` that cannot reproduce a CI timeout, or a CI that times out on a
// suite the author watched pass.
func TestRaceTimeoutParity(t *testing.T) {
	root := repoRoot(t)

	mk, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	mkMatch := regexp.MustCompile(`(?m)^RACE_TIMEOUT\s*\?=\s*(\S+)`).FindSubmatch(mk)
	if mkMatch == nil {
		t.Fatal("Makefile: no `RACE_TIMEOUT ?= ...` assignment found; the -race " +
			"timeout must stay a named, greppable pin so this guard can see it")
	}
	if !regexp.MustCompile(`go test -race -timeout \$\(RACE_TIMEOUT\) \./\.\.\.`).Match(mk) {
		t.Error("Makefile: the test target does not run " +
			"`go test -race -timeout $(RACE_TIMEOUT) ./...`; RACE_TIMEOUT is pinned but unused")
	}

	wf, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "gates.yml"))
	if err != nil {
		t.Fatalf("read gates.yml: %v", err)
	}
	wfMatch := regexp.MustCompile(`go test -race -timeout (\S+) \./\.\.\.`).FindSubmatch(wf)
	if wfMatch == nil {
		t.Fatal("gates.yml: no `go test -race -timeout ... ./...` step found")
	}

	if got, want := string(wfMatch[1]), string(mkMatch[1]); got != want {
		t.Errorf("race timeout drift: Makefile RACE_TIMEOUT=%s but gates.yml passes "+
			"-timeout=%s — `make test` and CI would enforce different ceilings", want, got)
	}
}
