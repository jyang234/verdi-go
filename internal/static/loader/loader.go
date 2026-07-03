// Package loader is the front of the static pipeline: it type-checks one service
// unit with go/packages and hands the result to SSA construction. The unit is a
// single service directory (its "./..." packages); first-party siblings in a
// monorepo resolve through the active go.work / go.mod, while the bus and peer
// services remain the boundary.
//
// It fails loudly. A load error, or any type-check error anywhere in the loaded
// graph, aborts — every downstream stage assumes a complete, well-typed program,
// so a partial load must never be silently analyzed.
package loader

import (
	"errors"
	"fmt"
	"go/version"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// loadMode is the fixed set of facts the static pipeline needs from every service
// unit: identity, files, the full import closure, and complete type information
// plus syntax so SSA can be built.
const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo |
	packages.NeedModule

// Service is one loaded, type-checked service unit.
type Service struct {
	// Dir is the directory the unit was loaded from.
	Dir string
	// Module is the unit's module (nil only if the toolchain reported none).
	Module *packages.Module
	// Packages are the unit's first-party packages, sorted by import path for
	// determinism. Their dependencies are reachable through the import graph but
	// are not listed here.
	Packages []*packages.Package
}

// Load type-checks the packages rooted at dir (the "./..." pattern relative to
// dir) and returns them as one service unit.
func Load(dir string) (*Service, error) {
	cfg := &packages.Config{
		Mode:  loadMode,
		Dir:   dir,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("loader: load %q: %w", dir, err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("loader: no packages found under %q", dir)
	}
	if err := collectErrors(pkgs); err != nil {
		return nil, err
	}

	// Keep only packages with production code. A directory holding solely
	// *_test.go files (e.g. an adopter's flows/ behavioral-gate package) loads as
	// a package with no compiled files; it contributes nothing to the call graph
	// or boundary and must not count toward the analyzed service unit.
	unit := pkgs[:0]
	for _, p := range pkgs {
		if len(p.CompiledGoFiles) > 0 {
			unit = append(unit, p)
		}
	}
	if len(unit) == 0 {
		return nil, fmt.Errorf("loader: no packages with Go source found under %q", dir)
	}

	// packages.Load does not guarantee a stable order; sort so every downstream
	// stage sees the same sequence.
	sort.Slice(unit, func(i, j int) bool { return unit[i].PkgPath < unit[j].PkgPath })

	return &Service{Dir: dir, Module: moduleOf(unit), Packages: unit}, nil
}

// collectErrors walks the whole loaded graph and returns a single error
// summarizing any package or type-check failure. Walking the closure (not just
// the roots) catches a broken dependency that would otherwise corrupt SSA.
func collectErrors(roots []*packages.Package) error {
	var msgs []string
	seen := make(map[string]bool)
	packages.Visit(roots, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			key := p.PkgPath + "|" + e.Error()
			if seen[key] {
				continue
			}
			seen[key] = true
			msgs = append(msgs, fmt.Sprintf("%s: %s", p.PkgPath, e))
		}
	})
	if len(msgs) == 0 {
		return nil
	}
	sort.Strings(msgs)
	const max = 10
	shown := msgs
	suffix := ""
	if len(shown) > max {
		shown = shown[:max]
		suffix = fmt.Sprintf("\n\t(+%d more)", len(msgs)-max)
	}
	summary := fmt.Sprintf("loader: %d type-check/load error(s):\n\t%s%s",
		len(msgs), strings.Join(shown, "\n\t"), suffix)
	// A toolchain-skew load failure is repeated once per importing package and reads
	// like a defect in the analyzed code, when the fix is to rebuild the analyzer.
	// Lead with one actionable remediation line so the wall of identical errors below
	// it is context, not the whole message. Scanned over the FULL msgs (before the
	// display cap) so the class is still recognized when its errors sort past max.
	if hint := toolchainSkewHint(msgs); hint != "" {
		return fmt.Errorf("%s\n%s", hint, summary)
	}
	return errors.New(summary)
}

// skewRE matches the go/packages loader's toolchain-skew error, e.g.
// "package requires newer Go version go1.26 (application built with go1.25)". The
// built-with clause is optional: older/newer x/tools phrasings may omit it, so the
// second group is best-effort and the hint degrades gracefully when it is absent.
var skewRE = regexp.MustCompile(`requires newer Go version (go[0-9]+(?:\.[0-9]+)+)(?: \(application built with (go[0-9]+(?:\.[0-9]+)+)\))?`)

// toolchainSkewHint returns a single actionable remediation line when any collected
// loader message is the toolchain-skew class (the target's dependencies declare a
// higher `go` directive than the Go version this analyzer binary was built with), or
// "" otherwise. The failure is inherent to the x/tools type-checker — types must be
// checked at the binary's own language version — so the ONLY remedy is rebuilding the
// analyzer with a matching toolchain, which is what the line tells the operator to do
// (the confusing per-package wall of errors otherwise names no fix).
//
// It reports the HIGHEST required version across all skewed packages, not the first
// one encountered: different dependencies can declare different `go` directives, and a
// GOTOOLCHAIN that satisfies only the lowest of them would still fail to load the rest,
// so recommending anything below the maximum sends the operator to rebuild with a
// toolchain that does not resolve the failure. The built-with version is the analyzer
// binary's own and is identical in every message, so first-seen suffices for it.
func toolchainSkewHint(msgs []string) string {
	var required, built string
	for _, m := range msgs {
		sub := skewRE.FindStringSubmatch(m)
		if sub == nil {
			continue
		}
		if required == "" || version.Compare(sub[1], required) > 0 {
			required = sub[1]
		}
		if built == "" && sub[2] != "" {
			built = sub[2]
		}
	}
	if required == "" {
		return ""
	}
	builtClause := "an older Go"
	if built != "" {
		builtClause = built
	}
	return fmt.Sprintf(
		"loader: analyzer/target Go toolchain skew — this binary was built with %s but the target requires %s; "+
			"types are checked at the binary's own Go version, so rebuild the analyzer with a matching toolchain "+
			"(e.g. GOTOOLCHAIN=%s go install <analyzer-cmd>@<version>) and re-run",
		builtClause, required, suggestToolchain(required))
}

// suggestToolchain renders v as a GOTOOLCHAIN name: a bare major.minor (go1.26, which
// skewRE has already validated as a go-prefixed dotted version) is patch-qualified to
// go1.26.0 (the concrete toolchain GOTOOLCHAIN resolves), while a version that already
// carries a patch is used as-is. "No patch component" is exactly "one dot".
func suggestToolchain(v string) string {
	if strings.Count(v, ".") == 1 {
		return v + ".0"
	}
	return v
}

// moduleOf returns the unit's own module: the main module if the toolchain marked
// one, else the module of the shortest-path package (the unit root). Returns nil
// only if no package reported a module.
func moduleOf(pkgs []*packages.Package) *packages.Module {
	var fallback *packages.Module
	var fallbackPkg string
	for _, p := range pkgs {
		if p.Module == nil {
			continue
		}
		if p.Module.Main {
			return p.Module
		}
		// Pick the module of the shortest-path package (the unit root). Compare
		// package path against package path — not the module path, a different
		// namespace — and break ties lexicographically so the result does not
		// depend on the order packages.Load happened to return.
		if fallback == nil || len(p.PkgPath) < len(fallbackPkg) ||
			(len(p.PkgPath) == len(fallbackPkg) && p.PkgPath < fallbackPkg) {
			fallback = p.Module
			fallbackPkg = p.PkgPath
		}
	}
	return fallback
}
