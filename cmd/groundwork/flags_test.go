package main

import (
	"slices"
	"testing"
)

// TestTakeValueFlags pins the scanning contract seven subcommands depend on.
// Nothing tested it directly: the entire `--flag=value` branch could be deleted
// with `go test ./cmd/groundwork/` green, because every existing caller-level
// test happens to spell its flags in the space-separated form. The `=` form is
// the one a human types most often on a shell that autocompletes, so its
// silent loss would have shown up first in a user's hands, not in CI.
//
// The contract, from takeValueFlags' doc: every occurrence is removed FROM ANY
// POSITION (stdlib flag.Parse stops at the first positional, which is the whole
// reason this helper exists), values come back in order of appearance, and
// everything else is returned untouched and in order.
func TestTakeValueFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		names      []string
		wantValues []string
		wantRest   []string
	}{
		{
			name:       "space separated",
			args:       []string{"graph.json", "--policy", "p.json", "claims.json"},
			names:      []string{"--policy", "-policy"},
			wantValues: []string{"p.json"},
			wantRest:   []string{"graph.json", "claims.json"},
		},
		{
			name:       "equals form",
			args:       []string{"graph.json", "--policy=p.json", "claims.json"},
			names:      []string{"--policy", "-policy"},
			wantValues: []string{"p.json"},
			wantRest:   []string{"graph.json", "claims.json"},
		},
		{
			name:       "single dash spelling",
			args:       []string{"-policy", "p.json"},
			names:      []string{"--policy", "-policy"},
			wantValues: []string{"p.json"},
			wantRest:   nil,
		},
		{
			name:       "after a positional, where flag.Parse would stop",
			args:       []string{"graph.json", "claims.json", "--policy", "p.json"},
			names:      []string{"--policy", "-policy"},
			wantValues: []string{"p.json"},
			wantRest:   []string{"graph.json", "claims.json"},
		},
		{
			name:       "repeated occurrences come back in order of appearance",
			args:       []string{"--service", "a=1.json", "x", "--service=b=2.json", "-service", "c=3.json"},
			names:      []string{"--service", "-service"},
			wantValues: []string{"a=1.json", "b=2.json", "c=3.json"},
			wantRest:   []string{"x"},
		},
		{
			name:       "absent flag leaves args untouched",
			args:       []string{"graph.json", "claims.json", "--json"},
			names:      []string{"--policy", "-policy"},
			wantValues: nil,
			wantRest:   []string{"graph.json", "claims.json", "--json"},
		},
		{
			name:       "empty args",
			args:       nil,
			names:      []string{"--policy", "-policy"},
			wantValues: nil,
			wantRest:   nil,
		},
		{
			// An explicit empty value is a VALUE, not an absent flag: the caller's
			// `found` bool must still be true so "" can be rejected on its own terms
			// rather than read as "the user did not ask".
			name:       "equals form with an empty value",
			args:       []string{"--expect="},
			names:      []string{"--expect", "-expect"},
			wantValues: []string{""},
			wantRest:   nil,
		},
		{
			// A dangling flag has no value to take, so it is NOT consumed — it falls
			// through to the subcommand's flag.FlagSet, which reports the missing
			// argument with the usage text this helper has no access to.
			name:       "trailing flag with no value is left for flag.Parse",
			args:       []string{"graph.json", "--policy"},
			names:      []string{"--policy", "-policy"},
			wantValues: nil,
			wantRest:   []string{"graph.json", "--policy"},
		},
		{
			// The next token is taken verbatim, even when it looks like a flag: the
			// scanner has no schema and must not guess that the user meant to omit a
			// value. Documented here so a future "skip values starting with -" idea
			// is a deliberate change, not an unnoticed one.
			name:       "a flag-shaped value is taken verbatim",
			args:       []string{"--expect", "--json"},
			names:      []string{"--expect", "-expect"},
			wantValues: []string{"--json"},
			wantRest:   nil,
		},
		{
			// CHARACTERIZATION, not an endorsement: takeValueFlags has no notion of
			// the "--" end-of-flags terminator, so it keeps scanning past one and the
			// "--" itself survives into rest, where the subcommand's flag.Parse does
			// honor it. No groundwork usage string documents "--", so nothing depends
			// on the other behavior today; this case exists so a change here is
			// visible rather than silent.
			name:       "the -- terminator does not stop the scan",
			args:       []string{"graph.json", "--", "--policy", "p.json"},
			names:      []string{"--policy", "-policy"},
			wantValues: []string{"p.json"},
			wantRest:   []string{"graph.json", "--"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, rest := takeValueFlags(tt.args, tt.names...)
			if !slices.Equal(values, tt.wantValues) {
				t.Errorf("values = %q, want %q", values, tt.wantValues)
			}
			if !slices.Equal(rest, tt.wantRest) {
				t.Errorf("rest = %q, want %q", rest, tt.wantRest)
			}
		})
	}
}

// TestTakeValueFlag pins the singular wrapper's two additions to the plural
// form: LAST-WINS on a repeated flag (the contract mcp.go's dedup comment cites
// by name), and a `found` bool that distinguishes an absent flag from an
// explicitly empty one. Both matter to callers that treat "" as a real value.
func TestTakeValueFlag(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		names     []string
		wantValue string
		wantFound bool
		wantRest  []string
	}{
		{
			name:      "single occurrence",
			args:      []string{"--expect", "sha-1", "graph.json"},
			names:     []string{"--expect", "-expect"},
			wantValue: "sha-1",
			wantFound: true,
			wantRest:  []string{"graph.json"},
		},
		{
			name:      "repeated flags are last-wins and every occurrence is removed",
			args:      []string{"--expect", "sha-1", "graph.json", "--expect=sha-2", "-expect", "sha-3"},
			names:     []string{"--expect", "-expect"},
			wantValue: "sha-3",
			wantFound: true,
			wantRest:  []string{"graph.json"},
		},
		{
			name:      "absent flag is not found and yields the empty value",
			args:      []string{"graph.json", "claims.json"},
			names:     []string{"--expect", "-expect"},
			wantValue: "",
			wantFound: false,
			wantRest:  []string{"graph.json", "claims.json"},
		},
		{
			// The discriminating pair with the case above: same value, opposite
			// `found`. A caller reading only the string cannot tell "--expect=" from
			// no flag at all, which is why the bool exists.
			name:      "an explicitly empty value is found",
			args:      []string{"--expect="},
			names:     []string{"--expect", "-expect"},
			wantValue: "",
			wantFound: true,
			wantRest:  nil,
		},
		{
			name:      "a dangling flag is neither found nor consumed",
			args:      []string{"graph.json", "--expect"},
			names:     []string{"--expect", "-expect"},
			wantValue: "",
			wantFound: false,
			wantRest:  []string{"graph.json", "--expect"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, found, rest := takeValueFlag(tt.args, tt.names...)
			if value != tt.wantValue || found != tt.wantFound {
				t.Errorf("takeValueFlag() = %q, %v; want %q, %v", value, found, tt.wantValue, tt.wantFound)
			}
			if !slices.Equal(rest, tt.wantRest) {
				t.Errorf("rest = %q, want %q", rest, tt.wantRest)
			}
		})
	}
}
