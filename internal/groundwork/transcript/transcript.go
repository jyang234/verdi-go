// Package transcript reads and summarizes the MCP server's --log file
// (calls.jsonl) — the E4 measurement apparatus, and the evidence the MCP
// tiers 2–3 plan-of-record defers to: per-session query counts, the tool and
// service mix, whether agents make cross-service hops mid-session, and how
// often a tool error is followed by a corrected call.
//
// The summary counts usage; it cannot grade value. Whether an agent's
// conclusions cite card facts — E4's qualitative half — stays human-judged,
// and the rendered card says so rather than implying otherwise.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Entry is one transcript line: a session boundary (Init, written when MCP
// initialize mints a session id) or one tool call with its resolution.
// Service is the name of the service that answered, "*" for a fleet-wide
// answer, and absent when resolution failed — the sentinels cannot collide
// with real names because the server validates --service names at startup.
// Session is the id the call belongs to; attribution rides it, never line
// order, because a shared team server interleaves concurrent clients' lines.
// Lines written by servers older than any of these fields decode fine — all
// are optional, and sessionless lines fall back to positional init-marker
// grouping.
//
// Log v2 (cmd/groundwork/mcp.go logFormatVersion) adds two optional fields, both
// tolerated here so this reader loads v1 AND v2 logs:
//   - Log: the format marker on the init line ("log":2); absent (0) on v1 logs.
//   - Result: the structured verdict on a verdict-bearing call line (fitness),
//     {"violated":[…],"cautions":N[,"truncated":true]}. Absent on v1 and on every
//     non-verdict or errored call.
type Entry struct {
	Init    bool            `json:"init,omitempty"`
	Log     int             `json:"log,omitempty"`
	Call    json.RawMessage `json:"call,omitempty"`
	Service string          `json:"service,omitempty"`
	Session string          `json:"session,omitempty"`
	IsError bool            `json:"isError,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// verdictResult is the decoded shape of a v2 call line's "result" object — the
// structured fitness verdict (cmd/groundwork/mcp.go fitnessLogResult). Decoded
// strictly (validateVerdict) so a malformed or future-shaped result fails loudly
// at Load, never skews the verdict tally silently (tenet 6).
type verdictResult struct {
	Violated  []string `json:"violated"`
	Cautions  int      `json:"cautions"`
	Truncated bool     `json:"truncated,omitempty"`
}

// toolName decodes the called tool's name from raw MCP call params. Unknown
// fields are tolerated (the params carry far more than the name), but malformed
// JSON is an error the caller MUST surface, never swallow — a swallowed decode
// skews the very usage counts this package exists to produce (tenet 6).
func toolName(raw json.RawMessage) (string, error) {
	var c struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return "", fmt.Errorf("malformed call params: %w", err)
	}
	return c.Name, nil
}

// Tool extracts the called tool's name from the raw call params. Load has already
// validated (fail-loud) that every entry's call params decode, so the only case
// left here is a call that carried no name field — surfaced as "(unnamed)" rather
// than dropped. A decode error is unreachable for entries Load produced.
func (e Entry) Tool() string {
	name, err := toolName(e.Call)
	if err != nil || name == "" {
		return "(unnamed)"
	}
	return name
}

// validateVerdict strictly decodes a v2 call line's "result" object. Unknown or
// missing-shaped fields are an error the caller MUST surface — the same fail-loud
// posture Load applies to call params (tenet 6), so a future or garbled verdict
// result can never be silently mis-tallied as "no violation surfaced". Returns the
// decoded verdict for the caller to reuse.
func validateVerdict(raw json.RawMessage) (verdictResult, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var v verdictResult
	if err := dec.Decode(&v); err != nil {
		return verdictResult{}, fmt.Errorf("malformed verdict result: %w", err)
	}
	return v, nil
}

// Load decodes a transcript, strictly: the format is this toolset's own, so
// a line it does not recognize is corruption (or a future field this reader
// has not been taught), and must fail loudly rather than skew the counts.
func Load(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		var e Entry
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("transcript: %s:%d: %w", path, lineNo, err)
		}
		if !e.Init && e.Call == nil {
			return nil, fmt.Errorf("transcript: %s:%d: neither an init marker nor a call", path, lineNo)
		}
		// Strictly validate the call params decode HERE (fail-loud), so Tool() never
		// has to swallow a decode error into a silent "(unnamed)" that would skew the
		// counts (tenet 6). Init markers carry no call payload and are exempt.
		if e.Call != nil {
			if _, err := toolName(e.Call); err != nil {
				return nil, fmt.Errorf("transcript: %s:%d: %w", path, lineNo, err)
			}
		}
		// A v2 "result" object is validated the same way: a malformed or
		// future-shaped verdict fails loudly here, so Summarize can tally it
		// without re-checking and a garbled result never reads as "no violation".
		if len(e.Result) > 0 {
			if _, err := validateVerdict(e.Result); err != nil {
				return nil, fmt.Errorf("transcript: %s:%d: %w", path, lineNo, err)
			}
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Count is one tool's or service's usage tally.
type Count struct {
	Name   string `json:"name"`
	Calls  int    `json:"calls"`
	Errors int    `json:"errors"`
}

// Summary is the deterministic reading of one transcript.
//
// The three trailing fields are the log v2 verdict surface (absent/zero on a v1
// log). They are omitempty so a v1 transcript's JSON summary is byte-identical to
// what it was before v2 existed — the added fields simply do not appear.
type Summary struct {
	Sessions              int     `json:"sessions"`
	Calls                 int     `json:"calls"`
	Errors                int     `json:"errors"`
	ErrorsCorrected       int     `json:"errors_corrected"`
	CallsPerSessionMin    int     `json:"calls_per_session_min"`
	CallsPerSessionMedian float64 `json:"calls_per_session_median"`
	CallsPerSessionMax    int     `json:"calls_per_session_max"`
	Tools                 []Count `json:"tools"`
	Services              []Count `json:"services"`
	CrossServiceHops      int     `json:"cross_service_hops"`
	SessionsWithHop       int     `json:"sessions_with_hop"`
	// MaxLogVersion is the highest "log" marker seen (log v2 ⇒ 2; a v1 log ⇒ 0,
	// omitted). VerdictResults counts call lines carrying a structured verdict
	// result; VerdictsSurfaced counts the subset whose violated[] was non-empty —
	// the "a verdict was surfaced to the agent" signal the funnel consumer reads.
	MaxLogVersion    int `json:"max_log_version,omitempty"`
	VerdictResults   int `json:"verdict_results,omitempty"`
	VerdictsSurfaced int `json:"verdicts_surfaced,omitempty"`
}

// fleetLabel and unresolvedLabel name the two non-service resolutions in the
// summary itself, so the JSON and the rendered card agree.
const (
	fleetLabel      = "(fleet-wide)"
	unresolvedLabel = "(unresolved)"
)

// call is one flattened tool call: the raw params are unmarshaled exactly
// once, here, however many passes the statistics make afterwards.
type call struct {
	tool, service string
	isError       bool
}

// Summarize computes the transcript's reading. A call belongs to the session
// whose id it carries; sessionless lines (older servers, clients that never
// echoed their id) fall back to positional grouping — split on unlabeled
// init markers, with calls before the first marker forming an implicit
// leading session, exactly as before ids existed. A cross-service hop is a
// call answered by a different concrete service than the session's previous
// concrete answer — fleet-wide and unresolved calls between them neither
// make nor break a hop. An error counts corrected when the session's next
// call of the same tool succeeds (counted in one pass: per tool, an
// error→success transition is exactly that).
func Summarize(entries []Entry) Summary {
	var order []string
	sessions := map[string][]call{}
	register := func(key string) {
		if _, ok := sessions[key]; !ok {
			sessions[key] = []call{}
			order = append(order, key)
		}
	}
	posN := 0
	posKey := "" // current positional session; registered lazily so an empty leading session does not count
	// Log v2 verdict tally, transcript-global (independent of session grouping):
	// the format marker, the count of structured verdict results, and how many
	// surfaced ≥1 violation. On a v1 log every one stays 0.
	var maxLogVer, verdictResults, verdictsSurfaced int
	for _, e := range entries {
		if e.Log > maxLogVer {
			maxLogVer = e.Log
		}
		if len(e.Result) > 0 {
			verdictResults++
			// Load already validated every Result strictly, so this decode cannot
			// fail on Load-produced entries; the err guard is defensive for entries
			// built by other means, and never counts a garbled result as surfaced.
			if v, err := validateVerdict(e.Result); err == nil && len(v.Violated) > 0 {
				verdictsSurfaced++
			}
		}
		if e.Init {
			if e.Session != "" {
				register("id:" + e.Session)
			} else {
				posN++
				posKey = "pos:" + strconv.Itoa(posN)
				register(posKey)
			}
			continue
		}
		key := "id:" + e.Session
		if e.Session == "" {
			if posKey == "" {
				posKey = "pos:0"
			}
			key = posKey
		}
		register(key)
		sessions[key] = append(sessions[key], call{tool: e.Tool(), service: e.Service, isError: e.IsError})
	}

	s := Summary{Sessions: len(order), MaxLogVersion: maxLogVer, VerdictResults: verdictResults, VerdictsSurfaced: verdictsSurfaced}
	tools, services := map[string]*Count{}, map[string]*Count{}
	tally := func(m map[string]*Count, name string, isErr bool) {
		c := m[name]
		if c == nil {
			c = &Count{Name: name}
			m[name] = c
		}
		c.Calls++
		if isErr {
			c.Errors++
		}
	}
	var perSession []int
	for _, key := range order {
		ses := sessions[key]
		perSession = append(perSession, len(ses))
		lastConcrete := ""
		hopped := false
		pendingErr := map[string]bool{}
		for _, c := range ses {
			s.Calls++
			if c.isError {
				s.Errors++
			}
			if pendingErr[c.tool] && !c.isError {
				s.ErrorsCorrected++
			}
			pendingErr[c.tool] = c.isError
			tally(tools, c.tool, c.isError)
			switch c.service {
			case "*":
				tally(services, fleetLabel, c.isError)
			case "":
				tally(services, unresolvedLabel, c.isError)
			default:
				tally(services, c.service, c.isError)
				if lastConcrete != "" && lastConcrete != c.service {
					s.CrossServiceHops++
					hopped = true
				}
				lastConcrete = c.service
			}
		}
		if hopped {
			s.SessionsWithHop++
		}
	}
	sort.Ints(perSession)
	if n := len(perSession); n > 0 {
		s.CallsPerSessionMin = perSession[0]
		s.CallsPerSessionMax = perSession[n-1]
		if n%2 == 1 {
			s.CallsPerSessionMedian = float64(perSession[n/2])
		} else {
			s.CallsPerSessionMedian = float64(perSession[n/2-1]+perSession[n/2]) / 2
		}
	}
	s.Tools = freeze(tools)
	s.Services = freeze(services)
	return s
}

func freeze(m map[string]*Count) []Count {
	out := make([]Count, 0, len(m))
	for _, c := range m {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Render prints the summary card.
func Render(s Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "sessions: %d   tool calls: %d   errors: %d (%d corrected by a later same-tool call)\n",
		s.Sessions, s.Calls, s.Errors, s.ErrorsCorrected)
	if s.Sessions > 0 {
		fmt.Fprintf(&b, "calls per session: min %d, median %g, max %d\n",
			s.CallsPerSessionMin, s.CallsPerSessionMedian, s.CallsPerSessionMax)
	}
	if len(s.Tools) > 0 {
		b.WriteString("\ntool                 calls  errors\n")
		for _, c := range s.Tools {
			fmt.Fprintf(&b, "%-20s %5d  %6d\n", c.Name, c.Calls, c.Errors)
		}
	}
	if len(s.Services) > 0 {
		b.WriteString("\nservice                          calls  errors\n")
		for _, c := range s.Services {
			fmt.Fprintf(&b, "%-32s %5d  %6d\n", c.Name, c.Calls, c.Errors)
		}
	}
	fmt.Fprintf(&b, "\ncross-service hops: %d, in %d of %d session(s)\n", s.CrossServiceHops, s.SessionsWithHop, s.Sessions)
	// The log v2 verdict surface, shown only when the log actually carried one — a
	// v1 log (VerdictResults 0) renders byte-identically to before v2 existed.
	if s.VerdictResults > 0 {
		fmt.Fprintf(&b, "\nverdict results (log v2): %d call(s) carried a structured verdict, %d surfaced ≥1 violation\n",
			s.VerdictResults, s.VerdictsSurfaced)
	}
	b.WriteString("\nThese counts measure usage, not value: whether conclusions cite card\nfacts (E4's qualitative half) stays human-judged.\n")
	return b.String()
}
