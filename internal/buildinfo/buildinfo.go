// Package buildinfo resolves a binary's self-reported version. The whole trust
// model — "same code → same graph, stamp it, verify with --expect" — leans on a
// binary that can name itself; a hard-coded "dev" sentinel that survives a real
// `go install module@version` quietly breaks that provenance story. So the
// resolver never settles for "dev" when the toolchain knows better: it prefers
// an ldflags-injected stamp, then the module version Go records for an installed
// build, then the embedded VCS revision of a build inside a checkout.
package buildinfo

import "runtime/debug"

// devSentinel is the placeholder both commands compile in as their default
// `var version`. It only ever reaches the user when nothing else is knowable.
const devSentinel = "dev"

// Version resolves the version to print, given the ldflags-injected value (the
// package's `var version`, "dev" when unset). Precedence:
//
//  1. an explicit ldflags stamp (a release build's -X main.version=...);
//  2. the module version Go records for `go install module@<version>` —
//     a tag or a pinned pseudo-version, exactly what scripts/groundwork-mcp.sh
//     installs;
//  3. the VCS revision embedded by `go build` inside a working tree, suffixed
//     -dirty when the tree had uncommitted changes;
//  4. the original sentinel, when the binary genuinely cannot name itself.
func Version(ldflags string) string {
	if ldflags != "" && ldflags != devSentinel {
		return ldflags // a release build stamped it explicitly
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fallback(ldflags)
	}
	// `go install module@v1.2.3` (or @<pseudo-version>) records the resolved
	// version here; a plain `go build` in a checkout leaves "(devel)".
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	if rev := vcsRevision(info); rev != "" {
		return rev
	}
	return fallback(ldflags)
}

// vcsRevision pulls the embedded commit (short) and dirty flag the Go toolchain
// stamps into a build made inside a VCS checkout, or "" when none was recorded.
func vcsRevision(info *debug.BuildInfo) string {
	var rev, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if rev == "" {
		return ""
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if modified == "true" {
		rev += "-dirty"
	}
	return rev
}

func fallback(ldflags string) string {
	if ldflags != "" {
		return ldflags
	}
	return devSentinel
}
