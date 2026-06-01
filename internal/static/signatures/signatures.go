// Package signatures renders a function's type signature as a canonical,
// package-qualified string for the non-gated call-graph view (static-extractor
// spec §6). It captures the receiver, generic type parameters, parameters, and
// results — the "what each function accepts and returns" half of the relationship
// map. Signatures are not gated, so their churn under refactoring never reaches a
// gate.
package signatures

import (
	"go/types"

	"golang.org/x/tools/go/ssa"
)

// Of renders fn's signature. Declared functions and methods render through
// go/types' object form, which includes the receiver and any generic type
// parameters; synthetic functions without a type object fall back to their bare
// signature type.
func Of(fn *ssa.Function) string {
	if obj := fn.Object(); obj != nil {
		return types.ObjectString(obj, qualifier)
	}
	if fn.Signature == nil {
		return ""
	}
	return types.TypeString(fn.Signature, qualifier)
}

// qualifier renders packages by name (context.Context, http.ResponseWriter)
// rather than by full import path, the conventional and readable Go form. It is
// deterministic given the program.
func qualifier(p *types.Package) string { return p.Name() }
