package ssabuild_test

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/jyang234/golang-code-graph/internal/static/ssabuild"
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

// TestBuildExtraInitialPackages pins the wrapper-descent horizon widening AND its
// soundness guard. Offering a dependency ("errors") as an extra initial package
// MATERIALIZES its function bodies (so a pass can descend into it) but must NEVER leak
// it into ServicePkgs — ServicePkgs feeds roots.Discover (MainPackages, init rooting,
// the library-export fallback), and a dependency there manufactures false reachability
// (CLAUDE.md "Sound scope"). The materialized extra also stays a dependency, not
// first-party: the widening adds bodies, not first-partyness.
func TestBuildExtraInitialPackages(t *testing.T) {
	svc, err := statictest.Load()
	if err != nil {
		t.Fatal(err)
	}
	extras := svc.ExtraInitialPackages([]string{"errors"})
	if len(extras) != 1 || extras[0].PkgPath != "errors" {
		t.Fatalf("ExtraInitialPackages([errors]) = %v, want exactly [errors]", extras)
	}

	p1, err := ssabuild.Build(svc) // un-widened
	if err != nil {
		t.Fatal(err)
	}
	p2, err := ssabuild.Build(svc, extras...) // widened with "errors"
	if err != nil {
		t.Fatal(err)
	}

	// (a) No leakage: the extra must not enter ServicePkgs. Same package-path set as
	// the un-widened build, and "errors" must not be among them.
	pkgSet := func(p *ssabuild.Program) map[string]bool {
		m := map[string]bool{}
		for _, sp := range p.ServicePkgs {
			m[sp.Pkg.Path()] = true
		}
		return m
	}
	s1, s2 := pkgSet(p1), pkgSet(p2)
	if len(s1) != len(s2) {
		t.Fatalf("ServicePkgs count changed with an extra: %d -> %d (leakage)", len(s1), len(s2))
	}
	for path := range s2 {
		if !s1[path] {
			t.Errorf("ServicePkgs gained %q with the extra (leakage)", path)
		}
	}
	if s2["errors"] {
		t.Error(`the extra "errors" leaked into ServicePkgs — roots.Discover would root it (false reachability)`)
	}

	// (b) Bodies: "errors" is a bodiless dependency in p1 but its bodies are built in p2.
	hasBody := func(p *ssabuild.Program) bool {
		ep := p.Prog.ImportedPackage("errors")
		if ep == nil {
			t.Fatal(`"errors" package not created in the SSA program`)
		}
		for _, m := range ep.Members {
			if fn, ok := m.(*ssa.Function); ok && len(fn.Blocks) > 0 {
				return true
			}
		}
		return false
	}
	if hasBody(p1) {
		t.Error(`"errors" should be a BODILESS dependency without the extra (p1)`)
	}
	if !hasBody(p2) {
		t.Error(`"errors" bodies must be materialized when it is offered as an extra (p2)`)
	}

	// (c) The materialized extra is still a DEPENDENCY, not first-party.
	if ep := p2.Prog.ImportedPackage("errors"); ep == nil || p2.IsFirstParty(ep) {
		t.Error(`the materialized extra "errors" must not be reclassified as first-party`)
	}
}
