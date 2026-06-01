package ssabuild_test

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/jyang234/golang-code-graph/internal/static/statictest"
)

func TestBuildServicePackages(t *testing.T) {
	prog, err := statictest.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if prog.ModulePath != "example.com/loansvc" {
		t.Fatalf("module path = %q", prog.ModulePath)
	}
	// One SSA package per first-party source package (main + 8 internal).
	const want = 9
	if len(prog.ServicePkgs) != want {
		t.Fatalf("ServicePkgs = %d, want %d", len(prog.ServicePkgs), want)
	}
	for _, p := range prog.ServicePkgs {
		if !prog.IsFirstParty(p) {
			t.Errorf("service package %q reported not first-party", p.Pkg.Path())
		}
	}
}

func TestIsFirstPartyExcludesDeps(t *testing.T) {
	prog, err := statictest.Build()
	if err != nil {
		t.Fatal(err)
	}
	// errgroup is a third-party dependency; its package must not be first-party.
	for fn := range ssautil.AllFunctions(prog.Prog) {
		if fn.Pkg == nil {
			continue
		}
		path := fn.Pkg.Pkg.Path()
		if strings.HasPrefix(path, "golang.org/x/sync") || path == "net/http" {
			if prog.IsFirstParty(fn.Pkg) {
				t.Fatalf("dependency %q reported as first-party", path)
			}
			return
		}
	}
	t.Skip("no dependency package observed to check")
}

func TestGenericInstantiationBuilt(t *testing.T) {
	prog, err := statictest.Build()
	if err != nil {
		t.Fatal(err)
	}
	// InstantiateGenerics must materialize codec.Decode[origination.Application].
	var found string
	for fn := range ssautil.AllFunctions(prog.Prog) {
		name := fn.String()
		if strings.Contains(name, "internal/codec.Decode[") {
			found = name
		}
	}
	if found == "" {
		t.Fatal("generic instantiation of codec.Decode[...] not found in SSA")
	}
	if !strings.Contains(found, "origination.Application") {
		t.Errorf("Decode instantiated as %q, want it specialized to origination.Application", found)
	}
}
