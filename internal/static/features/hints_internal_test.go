package features

import "testing"

// TestBuiltinTelemetryIncludesZap pins zap as a built-in telemetry library: a
// service should not need a per-service telemetry hint just to keep its zap
// logging calls out of the tier-1..3 bands. The check is white-box because the
// fixture does not import zap, so there is no zap *ssa.Function to classify
// end-to-end — the guarantee is that the hint is registered by default.
func TestBuiltinTelemetryIncludesZap(t *testing.T) {
	hs := NewHintSet(nil) // nil cfg => only built-ins
	want := map[string]bool{
		"go.uber.org/zap":         false,
		"go.uber.org/zap/zapcore": false,
	}
	for _, h := range hs.telemetry {
		if _, ok := want[h.pkgPath]; ok && h.name == "" { // a bare-path hint matches any call into the package
			want[h.pkgPath] = true
		}
	}
	for pkg, found := range want {
		if !found {
			t.Errorf("built-in telemetry hints missing a bare-path entry for %q", pkg)
		}
	}
}

// TestBuiltinExternalBoundaryExemptsOTel pins OpenTelemetry as a built-in exempt
// prefix: even with nil config its span/attribute packages must not surface as an
// ExternalBoundaryCall, or every instrumented function would disclose one.
func TestBuiltinExternalBoundaryExemptsOTel(t *testing.T) {
	hs := NewHintSet(nil) // nil cfg => only built-ins
	for _, p := range []string{"go.opentelemetry.io/otel", "go.opentelemetry.io/otel/trace", "go.opentelemetry.io/otel/attribute"} {
		if !prefixExempt(p, hs.externalExempt) {
			t.Errorf("OpenTelemetry package %q should be exempt by default", p)
		}
	}
}

// TestPrefixExempt pins the segment-boundary matching the externalBoundaryExempt
// list relies on: a bare entry matches itself and its subpackages but not a
// look-alike sibling; a trailing-slash entry matches the whole family.
func TestPrefixExempt(t *testing.T) {
	prefixes := []string{"github.com/go-chi/chi/v5", "go.opentelemetry.io/"}
	cases := []struct {
		path string
		want bool
	}{
		{"github.com/go-chi/chi/v5", true},            // exact
		{"github.com/go-chi/chi/v5/middleware", true}, // subpackage at a segment boundary
		{"github.com/go-chi/chi/v52", false},          // look-alike sibling, not a boundary
		{"github.com/go-chi/chi", false},              // parent is not under the prefix
		{"go.opentelemetry.io/otel", true},            // trailing-slash family
		{"go.opentelemetry.io/otel/trace", true},
		{"golang.org/x/sync/errgroup", false}, // unrelated
		{"", false},                           // synthetic / no package
	}
	for _, c := range cases {
		if got := prefixExempt(c.path, prefixes); got != c.want {
			t.Errorf("prefixExempt(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
