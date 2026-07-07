package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "calls.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The defining reading: two marked sessions plus an implicit leading one
// (lines from before the init marker existed), a cross-service hop made
// through a fleet-wide orientation call, an error corrected by the next
// same-tool call, and one that never was.
func TestSummarize(t *testing.T) {
	path := write(t,
		`{"call":{"name":"ground","arguments":{"fqn":"x"}},"service":"loans","isError":true}`, // implicit session; never corrected
		`{"init":true}`,
		`{"call":{"name":"entrypoints","arguments":{}},"service":"*"}`,
		`{"call":{"name":"ground","arguments":{"fqn":"y"}},"isError":true}`,                      // unresolved service
		`{"call":{"name":"ground","arguments":{"service":"loans","fqn":"y"}},"service":"loans"}`, // corrects it
		`{"call":{"name":"fleet-events","arguments":{}},"service":"*"}`,
		`{"call":{"name":"triage","arguments":{"service":"oblig","event":"e"}},"service":"oblig"}`, // the hop: loans → oblig
		`{"init":true}`,
		`{"call":{"name":"reach","arguments":{"fqn":"z"}},"service":"loans"}`,
	)
	entries, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s := Summarize(entries)

	if s.Sessions != 3 {
		t.Errorf("Sessions = %d, want 3 (implicit leading + two marked)", s.Sessions)
	}
	if s.Calls != 7 || s.Errors != 2 || s.ErrorsCorrected != 1 {
		t.Errorf("Calls/Errors/Corrected = %d/%d/%d, want 7/2/1", s.Calls, s.Errors, s.ErrorsCorrected)
	}
	if s.CallsPerSessionMin != 1 || s.CallsPerSessionMedian != 1 || s.CallsPerSessionMax != 5 {
		t.Errorf("per-session min/median/max = %d/%g/%d, want 1/1/5",
			s.CallsPerSessionMin, s.CallsPerSessionMedian, s.CallsPerSessionMax)
	}
	if s.CrossServiceHops != 1 || s.SessionsWithHop != 1 {
		t.Errorf("hops = %d in %d sessions, want 1 in 1 (fleet-wide calls neither make nor break a hop)",
			s.CrossServiceHops, s.SessionsWithHop)
	}
	find := func(cs []Count, name string) Count {
		for _, c := range cs {
			if c.Name == name {
				return c
			}
		}
		t.Errorf("missing count %q in %v", name, cs)
		return Count{}
	}
	if c := find(s.Tools, "ground"); c.Calls != 3 || c.Errors != 2 {
		t.Errorf("ground = %+v, want 3 calls 2 errors", c)
	}
	if c := find(s.Services, "loans"); c.Calls != 3 {
		t.Errorf("loans = %+v, want 3 calls", c)
	}
	if c := find(s.Services, "(fleet-wide)"); c.Calls != 2 {
		t.Errorf("fleet-wide = %+v, want 2 calls", c)
	}
	if c := find(s.Services, "(unresolved)"); c.Calls != 1 || c.Errors != 1 {
		t.Errorf("unresolved = %+v, want 1 call 1 error", c)
	}

	card := Render(s)
	for _, want := range []string{"sessions: 3", "cross-service hops: 1, in 1 of 3", "human-judged"} {
		if !strings.Contains(card, want) {
			t.Errorf("card missing %q:\n%s", want, card)
		}
	}
}

// Session ids beat line order: two clients of a shared team server
// interleave their lines, and every call must land in the session whose id
// it carries — positional grouping would put all three calls in session "2".
func TestSummarizeInterleavedSessions(t *testing.T) {
	path := write(t,
		`{"init":true,"session":"1"}`,
		`{"init":true,"session":"2"}`,
		`{"call":{"name":"ground","arguments":{}},"service":"loans","session":"1"}`,
		`{"call":{"name":"reach","arguments":{}},"service":"oblig","session":"2"}`,
		`{"call":{"name":"triage","arguments":{}},"service":"oblig","session":"1"}`,
	)
	entries, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s := Summarize(entries)
	if s.Sessions != 2 || s.Calls != 3 {
		t.Errorf("Sessions/Calls = %d/%d, want 2/3", s.Sessions, s.Calls)
	}
	// Session 1 walked loans → oblig (a hop); session 2 made one call (no hop).
	if s.CrossServiceHops != 1 || s.SessionsWithHop != 1 {
		t.Errorf("hops = %d in %d sessions, want 1 in 1 — interleaved lines must not fake or hide hops",
			s.CrossServiceHops, s.SessionsWithHop)
	}
	if s.CallsPerSessionMin != 1 || s.CallsPerSessionMax != 2 {
		t.Errorf("per-session min/max = %d/%d, want 1/2", s.CallsPerSessionMin, s.CallsPerSessionMax)
	}
}

// A session that initialized and asked nothing is evidence, and must count.
func TestSummarizeEmptySession(t *testing.T) {
	entries, err := Load(write(t, `{"init":true}`))
	if err != nil {
		t.Fatal(err)
	}
	s := Summarize(entries)
	if s.Sessions != 1 || s.Calls != 0 || s.CallsPerSessionMin != 0 || s.CallsPerSessionMax != 0 {
		t.Errorf("empty session reading = %+v", s)
	}
}

// The format is our own: an unrecognized line is corruption (or a field this
// reader was never taught) and must fail loudly, never skew counts silently.
func TestLoadFailsClosed(t *testing.T) {
	for _, bad := range []string{
		`{"call":{"name":"x"},"surprise":1}`,
		`{"service":"loans"}`,
		`not json`,
		// A call payload that is not a decodable params object must fail at Load
		// (fail-loud), not be silently mislabeled "(unnamed)" by Tool() (tenet 6).
		`{"call":"oops"}`,
		`{"call":42}`,
	} {
		if _, err := Load(write(t, bad)); err == nil {
			t.Errorf("Load accepted %q", bad)
		}
	}
}

// TestLoadValidatesCallParams pins the R-6/§4 tenet-6 fix: a malformed call
// payload is rejected at Load time so Tool() never swallows a decode error, while
// a well-formed call with no name field is still accepted (Tool → "(unnamed)").
func TestLoadValidatesCallParams(t *testing.T) {
	// Well-formed object with no name: accepted; Tool reports "(unnamed)".
	entries, err := Load(write(t, `{"call":{"args":{}}}`))
	if err != nil {
		t.Fatalf("Load rejected a well-formed name-less call: %v", err)
	}
	if got := entries[0].Tool(); got != "(unnamed)" {
		t.Errorf("Tool() = %q, want %q for a name-less call", got, "(unnamed)")
	}
}

// Log v2: the reader loads a log carrying the "log":2 marker and per-call "result"
// verdicts (tolerant of the new fields), and surfaces the verdict tally — the count
// of structured verdicts and how many surfaced ≥1 violation.
func TestLoadAndSummarizeV2(t *testing.T) {
	path := write(t,
		`{"init":true,"log":2,"session":"1"}`,
		`{"call":{"name":"fitness","arguments":{}},"service":"svc","session":"1","result":{"violated":["io_budget|api.H|"],"cautions":1}}`,
		`{"call":{"name":"fitness","arguments":{}},"service":"svc","session":"1","result":{"violated":[],"cautions":0}}`,
		`{"call":{"name":"ground","arguments":{"fqn":"x"}},"service":"svc","session":"1"}`,
	)
	entries, err := Load(path)
	if err != nil {
		t.Fatalf("Load rejected a v2 log: %v", err)
	}
	s := Summarize(entries)
	if s.MaxLogVersion != 2 {
		t.Errorf("MaxLogVersion = %d, want 2", s.MaxLogVersion)
	}
	// Two calls carried a structured result; one of them surfaced a violation.
	if s.VerdictResults != 2 || s.VerdictsSurfaced != 1 {
		t.Errorf("verdict tally = %d results / %d surfaced, want 2/1", s.VerdictResults, s.VerdictsSurfaced)
	}
	// The call counting is unchanged by the additive fields (the ground call counts too).
	if s.Calls != 3 {
		t.Errorf("Calls = %d, want 3", s.Calls)
	}
	card := Render(s)
	if !strings.Contains(card, "verdict results (log v2): 2 call(s) carried a structured verdict, 1 surfaced ≥1 violation") {
		t.Errorf("card missing the v2 verdict surface:\n%s", card)
	}
}

// A malformed v2 "result" (a shape this reader was never taught) fails LOUDLY at
// Load — the same fail-closed posture the call-params validation holds (tenet 6),
// so a garbled verdict can never be silently tallied as "no violation surfaced".
func TestLoadFailsClosedOnMalformedVerdict(t *testing.T) {
	// An unknown field inside result → DisallowUnknownFields rejects it.
	if _, err := Load(write(t, `{"call":{"name":"fitness"},"result":{"violated":[],"cautions":0,"surprise":1}}`)); err == nil {
		t.Error("Load accepted a result with an unknown field")
	}
	// A mis-typed verdict (violated must be an array of strings).
	if _, err := Load(write(t, `{"call":{"name":"fitness"},"result":{"violated":"nope"}}`)); err == nil {
		t.Error("Load accepted a mis-typed verdict result")
	}
}

// v1 unchanged: a log with no "log" marker and no "result" reads exactly as before —
// no verdict version, no verdict tally, and no verdict line in the rendered card.
func TestV1LogUnchangedByV2Reader(t *testing.T) {
	path := write(t,
		`{"init":true,"session":"1"}`,
		`{"call":{"name":"fitness","arguments":{}},"service":"svc","session":"1"}`,
	)
	entries, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s := Summarize(entries)
	if s.MaxLogVersion != 0 || s.VerdictResults != 0 || s.VerdictsSurfaced != 0 {
		t.Errorf("a v1 log produced non-zero v2 fields: %+v", s)
	}
	if strings.Contains(Render(s), "verdict results") {
		t.Error("a v1 log must not render a verdict line")
	}
}
