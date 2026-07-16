package loader_test

import (
	"path/filepath"
	"runtime"
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
