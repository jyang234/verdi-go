// Package capture defines flowmap's internal span model and the harness's
// output: a complete, scoped trace for exactly one flow (trace-capture-harness
// spec §7). It is deliberately free of any OpenTelemetry dependency — the
// harness adapts real OTel spans into this model, so the canonicalizer and
// everything downstream consume a stable internal shape and never import OTel
// (decision D8).
//
// This package owns three responsibilities that do not need a tracing backend:
// the Span / CapturedFlow data types, scoping a span set to one flow by its
// correlation key, and the deterministic sibling-ordering decision (the
// caller-clock concurrency signal of harness §3 / canon §3.3). Tree assembly and
// the rest of normalization belong to the canonicalizer.
package capture

import (
	"sort"
	"time"

	"github.com/jyang234/golang-code-graph/ir"
)

// TriggerKind is how a flow begins: an inbound HTTP request or a consumed event.
type TriggerKind string

const (
	TriggerHTTP  TriggerKind = "http"
	TriggerEvent TriggerKind = "event"
)

// CaptureMode is the capture topology (harness §1). v1 is in-process; post-hoc
// is the deferred out-of-process path.
type CaptureMode string

const (
	ModeInProcess CaptureMode = "in-process"
	ModePostHoc   CaptureMode = "post-hoc"
)

// Status mirrors a span's OTel status, normalized to the three values the IR
// retains.
const (
	StatusUnset = "unset"
	StatusOK    = "ok"
	StatusError = "error"
)

// Span is one captured operation in flowmap's internal model. Start and End are
// caller-clock timestamps used transiently to decide sibling ordering (§3.3) and
// then discarded by the canonicalizer; they are never serialized into a golden.
type Span struct {
	ID       string
	ParentID string
	Name     string
	Kind     ir.Kind
	Attrs    map[string]string

	Status    string // unset|ok|error
	ErrorType string // normalized exception class; "" if none

	Start time.Time
	End   time.Time
}

// CapturedFlow is the harness's output and the canonicalizer's input
// (harness §7). Complete=false is a hard stop: the canonicalizer must refuse to
// snapshot a truncated trace.
type CapturedFlow struct {
	Flow     string
	Service  string // the self lifeline (OTel resource service.name)
	Trigger  TriggerKind
	Mode     CaptureMode
	Spans    []Span
	Root     *Span
	Complete bool
}

// Attr returns the value of key on s, or "" if absent.
func (s *Span) Attr(key string) string {
	if s == nil || s.Attrs == nil {
		return ""
	}
	return s.Attrs[key]
}

// Scope returns the spans carrying the given correlation id in test.run.id,
// reconstructs the root, and reports whether the set is internally consistent.
// In-process capture gets scoping for free (the in-memory exporter only holds
// this run's spans), but filtering by the correlation attribute is the robust
// backstop the spec mandates (§3): it survives an instrumentation that starts a
// fresh trace on entry. A runID of "" keeps every span (the in-process fast
// path).
//
// The returned root is the single span whose parent is outside the scoped set
// (the reconstructed entry — a server or consumer span). Multiple such spans, or
// none, signal a scoping/completeness problem the caller surfaces via Complete;
// canonicalization re-checks and attaches orphans to a synthetic root.
func Scope(spans []Span, runID string) (scoped []Span, root *Span) {
	scoped = make([]Span, 0, len(spans))
	for _, s := range spans {
		if runID == "" || s.Attr(CorrelationKey) == runID {
			scoped = append(scoped, s)
		}
	}
	sortSpans(scoped)

	ids := make(map[string]bool, len(scoped))
	for i := range scoped {
		ids[scoped[i].ID] = true
	}
	for i := range scoped {
		if !ids[scoped[i].ParentID] {
			if root == nil {
				root = &scoped[i]
			} else {
				// More than one in-scope root: ambiguous. Leave root as the
				// first; the caller treats this as incomplete.
				return scoped, nil
			}
		}
	}
	return scoped, root
}

// CorrelationKey is the span attribute the harness copies from baggage so a span
// can be attributed to exactly one test run (§3).
const CorrelationKey = "test.run.id"

// sortSpans imposes a deterministic, run-independent order on the flat span set:
// by start time (so assembly and ordering see a stable sequence), then by id as
// a tiebreaker. Ordering of *siblings in the tree* is decided separately by
// Order; this is only to keep the flat list reproducible.
func sortSpans(spans []Span) {
	sort.Slice(spans, func(i, j int) bool {
		if !spans[i].Start.Equal(spans[j].Start) {
			return spans[i].Start.Before(spans[j].Start)
		}
		return spans[i].ID < spans[j].ID
	})
}

// Relation is the happens-before relationship between two sibling spans, decided
// from the caller's single clock domain.
type Relation int

const (
	// Concurrent means the two intervals overlap (or ordering is otherwise
	// unreliable): determinism wins over fidelity-to-this-run, so they are a
	// race-ordered group (canon §3.3 rule 3).
	Concurrent Relation = iota
	// Before means a reliably precedes b (a.End <= b.Start beyond the guard band).
	Before
	// After means b reliably precedes a.
	After
)

// Order decides the ordering of two sibling spans from their caller-clock
// intervals with a guard band (harness §3 / canon §3.3). Because both spans were
// recorded in the *same* process clock (the in-process exporter, or one
// service's client spans), interval comparison is reliable here — unlike
// cross-service server spans, which live in separate clock domains and must
// never be compared this way. When intervals overlap within the guard band the
// relationship is treated as concurrent, which is also the default on any
// ambiguity.
func Order(a, b Span, guard time.Duration) Relation {
	switch {
	case !a.End.Add(guard).After(b.Start):
		// a.End + guard <= b.Start
		return Before
	case !b.End.Add(guard).After(a.Start):
		return After
	default:
		return Concurrent
	}
}
