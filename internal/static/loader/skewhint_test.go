package loader

// Finding 3 (flowmap field report): a toolchain-skew load failure — the analyzer
// binary built with an older Go than the target's declared `go` directive — is
// correct but reads like a defect in the analyzed code and names no remedy. These
// pin the one actionable remediation line collectErrors now leads with.

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestToolchainSkewHint(t *testing.T) {
	tests := []struct {
		name    string
		msgs    []string
		want    bool // expect a non-empty hint
		require []string
	}{
		{
			name:    "full message with both versions",
			msgs:    []string{"pkg/x: pkg requires newer Go version go1.26 (application built with go1.25)"},
			want:    true,
			require: []string{"go1.26", "go1.25", "GOTOOLCHAIN=go1.26.0", "rebuild"},
		},
		{
			name:    "patch-qualified required version is not re-suffixed",
			msgs:    []string{"pkg requires newer Go version go1.26.3 (application built with go1.25.1)"},
			want:    true,
			require: []string{"GOTOOLCHAIN=go1.26.3", "go1.25.1"},
		},
		{
			name:    "built-with clause absent still yields a hint",
			msgs:    []string{"pkg requires newer Go version go1.26"},
			want:    true,
			require: []string{"go1.26", "GOTOOLCHAIN=go1.26.0"},
		},
		{
			name: "ordinary type errors produce no hint",
			msgs: []string{"main.go:4:2: cannot use \"s\" as int", "undefined: foo"},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toolchainSkewHint(tc.msgs)
			if tc.want != (got != "") {
				t.Fatalf("toolchainSkewHint(%v) = %q; wanted non-empty=%v", tc.msgs, got, tc.want)
			}
			for _, want := range tc.require {
				if !strings.Contains(got, want) {
					t.Errorf("hint missing %q:\n%s", want, got)
				}
			}
		})
	}
}

// TestCollectErrorsLeadsWithSkewHint pins the integration: when the loaded graph
// carries the skew error, collectErrors leads with the remediation line AND still
// includes the underlying error summary as evidence.
func TestCollectErrorsLeadsWithSkewHint(t *testing.T) {
	pkg := &packages.Package{
		PkgPath: "example.com/target/internal/structerr",
		Errors: []packages.Error{
			{Msg: "package requires newer Go version go1.26 (application built with go1.25)"},
		},
	}
	err := collectErrors([]*packages.Package{pkg})
	if err == nil {
		t.Fatal("collectErrors returned nil for a package carrying a load error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "toolchain skew") {
		t.Errorf("summary did not lead with the skew remediation line:\n%s", msg)
	}
	if !strings.Contains(msg, "type-check/load error(s)") {
		t.Errorf("summary dropped the underlying error evidence:\n%s", msg)
	}
	// The remediation line must precede the error wall so it is read first.
	if hi, wi := strings.Index(msg, "toolchain skew"), strings.Index(msg, "type-check/load error(s)"); hi > wi {
		t.Errorf("remediation line came after the error wall (%d > %d):\n%s", hi, wi, msg)
	}
}
