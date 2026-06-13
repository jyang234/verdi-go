package buildinfo

import (
	"runtime/debug"
	"strings"
	"testing"
)

// An explicit ldflags stamp always wins — a release build named itself.
func TestExplicitStampWins(t *testing.T) {
	if got := Version("v1.4.2"); got != "v1.4.2" {
		t.Fatalf("Version(ldflags) = %q, want the ldflags value", got)
	}
}

// The "dev" sentinel never survives verbatim when the toolchain can do better:
// the binary under `go test` carries embedded build info, so the resolver must
// return something other than the bare sentinel. (We assert "not dev" rather
// than a fixed value because the embedded module version/VCS stamp varies by
// build environment.)
func TestSentinelFallsBackToBuildInfo(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		t.Skip("no build info embedded in this binary")
	}
	got := Version("dev")
	resolvable := (info.Main.Version != "" && info.Main.Version != "(devel)") || vcsRevision(info) != ""
	if resolvable && got == "dev" {
		t.Fatalf("Version(\"dev\") = %q, want the recovered module/VCS stamp", got)
	}
	if got == "" {
		t.Fatalf("Version returned empty")
	}
}

// A dirty VCS revision is rendered short with a -dirty suffix.
func TestVCSRevisionShape(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
		{Key: "vcs.modified", Value: "true"},
	}}
	got := vcsRevision(info)
	if !strings.HasSuffix(got, "-dirty") {
		t.Fatalf("rev = %q, want a -dirty suffix", got)
	}
	if rev := strings.TrimSuffix(got, "-dirty"); len(rev) != 12 {
		t.Fatalf("short rev = %q (len %d), want 12 hex chars", rev, len(rev))
	}
}
