// Package ssabuild turns a loaded service unit into an SSA program. It builds
// with InstantiateGenerics so calls made through generic code are materialized as
// concrete instantiations and become visible to the call graph.
package ssabuild

import (
	"fmt"
	"sort"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/jyang234/golang-code-graph/internal/static/features"
	"github.com/jyang234/golang-code-graph/internal/static/loader"
)

// Program is the built SSA form of one service unit.
type Program struct {
	// Prog is the whole SSA program: the service packages plus every dependency,
	// fully built.
	Prog *ssa.Program
	// ServicePkgs are the SSA packages for the unit's own first-party packages,
	// sorted by import path. Dependencies live in Prog but are not listed here.
	ServicePkgs []*ssa.Package
	// ModulePath is the unit's module path, used to tell first-party code from
	// dependencies.
	ModulePath string
}

// Build constructs and builds the SSA program for svc. The optional extra packages
// are ADDITIONAL initial packages whose function bodies are materialized alongside
// the service's own — the openapi wrapper-descent horizon widening (a dependency-
// module client package is a bodiless external declaration until re-offered here as
// an extra initial). Extras are de-duplicated against svc.Packages and against each
// other by import path (nils dropped); they arrive sorted from
// loader.ExtraInitialPackages and that order is preserved. With no extras, Build is
// byte-identical to the pre-widening single-argument call.
//
// SOUNDNESS INVARIANT: an extra gets a built body but must NEVER enter ServicePkgs.
// ServicePkgs feeds roots.Discover — ssautil.MainPackages, the package-init rooting
// loop, and the library-export fallback that roots every exported member — so a
// dependency leaking into it manufactures reachability the service never has (the
// false-PROVEN class CLAUDE.md's "Sound scope" section says bit this codebase twice).
// ssautil.Packages returns ssaPkgs INDEX-ALIGNED with its input, so ServicePkgs is
// taken from ssaPkgs[:len(svc.Packages)] ONLY — the service prefix — never from the
// extras that follow it. First-party classification is module-path based
// (IsFirstPartyPath), so a materialized extra is still correctly a dependency: the
// widening adds bodies, not first-partyness.
func Build(svc *loader.Service, extra ...*packages.Package) (*Program, error) {
	// initial = svc.Packages ++ deduped extras. Extras go AFTER the service packages so
	// the index-aligned ssaPkgs keeps the service packages at the [:len(svc.Packages)]
	// prefix (see the ServicePkgs invariant above).
	serviceCount := len(svc.Packages)
	initial := make([]*packages.Package, serviceCount, serviceCount+len(extra))
	copy(initial, svc.Packages)
	seen := make(map[string]bool, serviceCount+len(extra))
	for _, p := range svc.Packages {
		seen[p.PkgPath] = true // an extra duplicating a service package is dropped
	}
	for _, p := range extra {
		if p == nil || seen[p.PkgPath] {
			continue // drop nils and dedup extras against the unit and each other
		}
		seen[p.PkgPath] = true
		initial = append(initial, p)
	}

	// ssautil.Packages returns one SSA package per initial package, aligned with
	// `initial`; an entry is nil for a package that had no syntax (none of the
	// service's own packages do). Dependencies not listed in `initial` are created in
	// the program but bodiless and not returned in this slice.
	prog, ssaPkgs := ssautil.Packages(initial, ssa.InstantiateGenerics)
	prog.Build()

	// Only the service prefix becomes ServicePkgs — the extras (which follow it) are
	// built for descent but deliberately excluded, so roots.Discover never roots them.
	service := make([]*ssa.Package, 0, serviceCount)
	for _, p := range ssaPkgs[:serviceCount] {
		if p != nil {
			service = append(service, p)
		}
	}
	if len(service) == 0 {
		return nil, fmt.Errorf("ssabuild: no SSA packages built for %q", svc.Dir)
	}
	sort.Slice(service, func(i, j int) bool {
		return service[i].Pkg.Path() < service[j].Pkg.Path()
	})

	modPath := ""
	if svc.Module != nil {
		modPath = svc.Module.Path
	}
	return &Program{Prog: prog, ServicePkgs: service, ModulePath: modPath}, nil
}

// IsFirstParty reports whether pkg belongs to the service unit's module (as
// opposed to stdlib or a third-party dependency). A nil package — synthetic
// functions such as wrappers — is treated as not first-party.
func (p *Program) IsFirstParty(pkg *ssa.Package) bool {
	if pkg == nil || pkg.Pkg == nil || p.ModulePath == "" {
		return false
	}
	path := pkg.Pkg.Path()
	return path == p.ModulePath || hasPathPrefix(path, p.ModulePath)
}

// IsFirstPartyFunc reports whether fn belongs to the service unit's module,
// resolving the nil-fn.Pkg synthetic functions (generic instances,
// $bound/$thunk method-value wrappers) through features.EffectivePkgPath. It is
// the function-level first-party predicate every SOUNDNESS decision must use:
// IsFirstParty(fn.Pkg) returns false for those synthetics, silently severing
// reachable first-party behavior from the graph with no blind spot (C-1).
func (p *Program) IsFirstPartyFunc(fn *ssa.Function) bool {
	return p.IsFirstPartyPath(features.EffectivePkgPath(fn))
}

// IsFirstPartyPath reports whether an import path belongs to the service unit's
// module — the string-keyed counterpart of IsFirstParty, for callers that hold a
// package path rather than an *ssa.Package.
func (p *Program) IsFirstPartyPath(path string) bool {
	if path == "" || p.ModulePath == "" {
		return false
	}
	return path == p.ModulePath || hasPathPrefix(path, p.ModulePath)
}

// hasPathPrefix reports whether importPath is the module path followed by a "/"
// segment boundary, so "example.com/loansvc" matches its sub-packages but not an
// unrelated "example.com/loansvc-extra".
func hasPathPrefix(importPath, modulePath string) bool {
	return len(importPath) > len(modulePath) &&
		importPath[:len(modulePath)] == modulePath &&
		importPath[len(modulePath)] == '/'
}
