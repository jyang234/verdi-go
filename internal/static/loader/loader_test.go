package loader_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/loader"
)

func fixtureDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", "loansvc")
}

func TestLoadFixture(t *testing.T) {
	svc, err := loader.Load(fixtureDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if svc.Module == nil || svc.Module.Path != "example.com/loansvc" {
		t.Fatalf("module = %+v, want path example.com/loansvc", svc.Module)
	}
	// main + 8 internal packages.
	const want = 9
	if len(svc.Packages) != want {
		var got []string
		for _, p := range svc.Packages {
			got = append(got, p.PkgPath)
		}
		t.Fatalf("loaded %d packages, want %d: %v", len(svc.Packages), want, got)
	}
	// Packages must be sorted by import path.
	for i := 1; i < len(svc.Packages); i++ {
		if svc.Packages[i-1].PkgPath > svc.Packages[i].PkgPath {
			t.Errorf("packages not sorted: %q before %q", svc.Packages[i-1].PkgPath, svc.Packages[i].PkgPath)
		}
	}
	// The unit must carry full type information.
	for _, p := range svc.Packages {
		if p.Types == nil || p.TypesInfo == nil {
			t.Errorf("package %q missing type info", p.PkgPath)
		}
	}
}

// TestExtraInitialPackages checks the wrapper-descent horizon-widening helper. A
// stdlib dependency anywhere in the import closure is returned (so ssabuild can later
// materialize its bodies); a first-party path already in the unit is EXCLUDED (it is
// already initial and built); a path with no loaded package is silently OMITTED (a
// client outside the import graph can never be called, so widening it is meaningless —
// fail-closed). The result is sorted by import path for a deterministic downstream
// initial-package order. Loader-only, no SSA.
func TestExtraInitialPackages(t *testing.T) {
	svc, err := loader.Load(fixtureDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	const (
		// "errors" is transitive (fmt/context/net/http all import it); "context" is a
		// direct import. Both are certain to be in loansvc's closure. "context" < "errors"
		// pins the sort.
		depTransitive = "errors"
		depDirect     = "context"
		firstParty    = "example.com/loansvc/internal/handler" // already in the unit
		missing       = "example.com/nope/not/loaded"          // no loaded package
	)
	// Requested in a deliberately UNSORTED order to prove the helper sorts its output.
	got := svc.ExtraInitialPackages([]string{firstParty, missing, depTransitive, depDirect})

	var paths []string
	present := map[string]bool{}
	for _, p := range got {
		if p == nil {
			t.Fatalf("ExtraInitialPackages returned a nil package")
		}
		paths = append(paths, p.PkgPath)
		present[p.PkgPath] = true
	}
	// Exactly the two closure dependencies, sorted; first-party and missing omitted.
	want := []string{depDirect, depTransitive} // "context", "errors"
	if len(paths) != len(want) {
		t.Fatalf("ExtraInitialPackages = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("ExtraInitialPackages = %v, want %v (sorted by import path)", paths, want)
		}
	}
	if present[firstParty] {
		t.Errorf("a first-party path already in the unit must be excluded, got %q", firstParty)
	}
	if present[missing] {
		t.Errorf("a path with no loaded package must be omitted, got %q", missing)
	}

	// The returned packages carry syntax + type info (loadMode loaded the full closure),
	// which is what makes them buildable when re-offered as extra initial packages.
	for _, p := range got {
		if p.Syntax == nil || p.TypesInfo == nil {
			t.Errorf("dependency %q missing syntax/type info; cannot materialize bodies", p.PkgPath)
		}
	}

	// Empty input is a nil no-op (the feature-inert default path).
	if out := svc.ExtraInitialPackages(nil); out != nil {
		t.Errorf("ExtraInitialPackages(nil) = %v, want nil", out)
	}
}

func TestLoadMissingDirFails(t *testing.T) {
	if _, err := loader.Load(filepath.Join(fixtureDir(), "does-not-exist")); err == nil {
		t.Fatal("Load of a missing directory should fail")
	}
}

func TestLoadDeterministicPackageOrder(t *testing.T) {
	first, err := loader.Load(fixtureDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		again, err := loader.Load(fixtureDir())
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Packages) != len(first.Packages) {
			t.Fatalf("package count drifted: %d vs %d", len(again.Packages), len(first.Packages))
		}
		for j := range first.Packages {
			if first.Packages[j].PkgPath != again.Packages[j].PkgPath {
				t.Fatalf("package order drifted at %d: %q vs %q", j, first.Packages[j].PkgPath, again.Packages[j].PkgPath)
			}
		}
	}
}

// TestLoadExcludesTestOnlyPackages guards the rule that a directory holding only
// *_test.go files (the fixture's flows/ behavioral-gate package) is not part of
// the analyzed service unit: it has no production code and must not perturb the
// call graph or boundary.
func TestLoadExcludesTestOnlyPackages(t *testing.T) {
	svc, err := loader.Load(fixtureDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, p := range svc.Packages {
		if p.PkgPath == "example.com/loansvc/flows" {
			t.Errorf("test-only package %q should not be in the service unit", p.PkgPath)
		}
		if len(p.CompiledGoFiles) == 0 {
			t.Errorf("package %q has no production Go files and should have been filtered", p.PkgPath)
		}
	}
}

// TestLoadExcludesInPackageTestVariants pins Load's Tests: false behaviorally,
// not by reading the field. Under Tests: true go/packages also returns the
// in-package test variant, which reports the SAME PkgPath over the SAME files at
// the SAME byte offsets as the real package — a pair the function-local type
// discriminator cannot separate, because its declaration sites are qualified by
// package PATH. That would be a new, correctly fail-closed collision class, so
// the flip must be a deliberate decision with the design revisited, never a
// drive-by. See "Physical position" in
// docs/superpowers/specs/2026-07-26-local-generic-type-identity-design.md.
//
// It reads inpkgtestsvc, NOT loansvc, so that flipping the field fails on the
// assertions below rather than on something incidental. loansvc's behavioral-gate
// tests pull dependencies its go.mod does not declare for test loading, so
// `go list -test` there can fail with "updates to go.mod needed" — a tripwire that
// fires, but with a message pointing at the wrong thing. inpkgtestsvc declares no
// dependencies and its test file imports only "testing", and — unlike every other
// fixture — its test file is IN-PACKAGE, which is what produces the same-PkgPath
// variant this test is actually about.
func TestLoadExcludesInPackageTestVariants(t *testing.T) {
	dir := inPkgTestFixtureDir()
	assertInPackageTestFile(t, dir)

	svc, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	byPath := make(map[string]string, len(svc.Packages))
	for _, p := range svc.Packages {
		if strings.Contains(p.ID, ".test") || strings.Contains(p.ID, "[") {
			t.Errorf("loaded test-variant package %q; Load must set Tests: false", p.ID)
		}
		if prev, ok := byPath[p.PkgPath]; ok {
			t.Errorf("package path %q loaded twice, as %q and %q", p.PkgPath, prev, p.ID)
		}
		byPath[p.PkgPath] = p.ID
	}
}

func inPkgTestFixtureDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", "inpkgtestsvc")
}

// assertInPackageTestFile keeps the tripwire ARMED. Its whole mechanism is that
// the fixture holds a test file in the PRODUCTION package: only that yields the
// `pkg [pkg.test]` variant whose PkgPath duplicates the real one. Moving the file
// to an external `_test` package, or deleting it, would leave a fixture that
// still loads, still passes, and proves nothing — a silently disarmed guard.
func assertInPackageTestFile(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read fixture dir: %v", err)
	}
	fset := token.NewFileSet()
	var prod, inPkg []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.PackageClauseOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if !strings.HasSuffix(name, "_test.go") {
			prod = append(prod, f.Name.Name)
			continue
		}
		if !strings.HasSuffix(f.Name.Name, "_test") {
			inPkg = append(inPkg, name)
		}
	}
	if len(prod) == 0 {
		t.Fatalf("fixture %s has no production package", dir)
	}
	if len(inPkg) == 0 {
		t.Fatalf("fixture %s has no IN-PACKAGE test file; the Tests tripwire is disarmed", dir)
	}
}
