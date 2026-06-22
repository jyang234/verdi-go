// Package audit is a methods-ONLY domain package: it declares a type whose only
// functions are POINTER-RECEIVER (*T) methods, none of which any reachable code calls
// — so it contributes no call-graph node, yet it is NOT function-less.
//
// It is the regression guard for the omitted-package walk (Graph.OmittedPackages): the
// walk must count *T-receiver methods (it uses ssautil.AllFunctions, not pkg.Members /
// MethodSet(T), which omit them). If it ever reverted to a methods-omitting collection,
// audit would be miscounted as a types-only package and FALSELY disclosed as omitted.
// It is imported by the server component but stays unreachable.
package audit

// Recorder records audit events. Its method has a POINTER receiver — the dominant Go
// idiom MethodSet(T) drops — so a regression to a methods-omitting walk surfaces as
// audit being wrongly listed in the rollup's omitted set.
type Recorder struct{}

// Record is never called by reachable code, so Recorder has no graph node — but the
// method still makes audit a function-bearing package, not a types-only one.
func (r *Recorder) Record(event string) { _ = event }
