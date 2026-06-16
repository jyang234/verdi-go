package review

import "testing"

// withinScope must treat scope entries as identifier-boundary prefixes: a scope
// of "...internal/app" admits its sub-packages but must NOT admit the sibling
// "...internal/application", which a bare strings.HasPrefix would silently let
// escape the scope-creep gate (a fail-open).
func TestWithinScopeBoundary(t *testing.T) {
	scope := []string{"example.com/svc/internal/app"}
	if !withinScope("example.com/svc/internal/app", scope) {
		t.Error("exact scope package must be in scope")
	}
	if !withinScope("example.com/svc/internal/app/sub", scope) {
		t.Error("a sub-package of the scope must be in scope")
	}
	if withinScope("example.com/svc/internal/application", scope) {
		t.Error("a sibling package must be OUT of scope — scope gate fails open otherwise")
	}
}
