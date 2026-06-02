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
