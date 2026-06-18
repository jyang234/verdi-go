// Command otlpgen emits authoritative OTLP/JSON samples marshaled with the OTel
// Collector's own ptrace.JSONMarshaler — the exact encoder the collector `file`
// exporter (format: json) uses — so flowmap's reader is pinned to collector output
// rather than a hand-authored guess, without needing any real trace store or
// proprietary data. Two samples, selected by the first argument:
//
//   - loansvc (default): the single-service loan flow →
//     testdata/otlp/loansvc.collector.otlp.json (the format the otlpjson decoder
//     is validated against, TestDecodeCollectorSample).
//   - crossservice: a TWO-resource trace (impeachsvc → peersvc) whose peer owns the
//     DB writes and runs on a clock skewed behind the caller →
//     testdata/otlp/cross_service_peer.otlp.json (the cross-service impeachment
//     fixture; collector-marshaled so its wire format is real, not author-drawn).
//
// It is a standalone module (own go.mod, deliberately NOT in go.work): the heavy
// pdata dependency stays entirely out of the engine's module graph and off the
// public harness/flow/ir surface. Regenerate with:
//
//	cd testdata/otlpgen && GOWORK=off go run . > ../otlp/loansvc.collector.otlp.json
//	cd testdata/otlpgen && GOWORK=off go run . crossservice > ../otlp/cross_service_peer.otlp.json
package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

func main() {
	which := "loansvc"
	if len(os.Args) > 1 {
		which = os.Args[1]
	}
	var traces ptrace.Traces
	switch which {
	case "loansvc":
		traces = loansvcTraces()
	case "crossservice":
		traces = crossServiceTraces()
	default:
		fmt.Fprintln(os.Stderr, "otlpgen: unknown sample", which, "(want loansvc|crossservice)")
		os.Exit(1)
	}
	b, err := (&ptrace.JSONMarshaler{}).MarshalTraces(traces)
	if err != nil {
		fmt.Fprintln(os.Stderr, "otlpgen:", err)
		os.Exit(1)
	}
	os.Stdout.Write(append(b, '\n'))
}

func loansvcTraces() ptrace.Traces {
	traces := ptrace.NewTraces()
	rs := traces.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", "loansvc")
	rs.Resource().Attributes().PutStr("host.name", "pod-7f9c") // resource noise the allowlist must drop
	ss := rs.ScopeSpans().AppendEmpty()
	ss.Scope().SetName("loansvc")

	base := time.Unix(1700000000, 0).UTC()
	var nextID byte = 1
	spanID := func() pcommon.SpanID {
		id := pcommon.SpanID{0, 0, 0, 0, 0, 0, 0, nextID}
		nextID++
		return id
	}
	traceID := pcommon.TraceID{0x5b, 0x8e, 0xff, 0xf7, 0x98, 0x03, 0x81, 0x03, 0xd2, 0x69, 0xb6, 0x33, 0x81, 0x3f, 0xc6, 0x0c}

	type span struct {
		id, parent pcommon.SpanID
		name       string
		kind       ptrace.SpanKind
		startMS    int
		status     ptrace.StatusCode
		attrs      map[string]any
	}

	root := spanID()
	root00 := pcommon.SpanID{} // empty => the inbound entry's caller is outside this capture
	mk := func(name string, kind ptrace.SpanKind, parent pcommon.SpanID, startMS int, status ptrace.StatusCode, attrs map[string]any) span {
		return span{id: spanID(), parent: parent, name: name, kind: kind, startMS: startMS, status: status, attrs: attrs}
	}

	spans := []span{
		{id: root, parent: root00, name: "POST /loan-application", kind: ptrace.SpanKindServer, startMS: 0, status: ptrace.StatusCodeOk,
			attrs: map[string]any{"http.request.method": "POST", "http.route": "/loan-application", "http.response.status_code": int64(200)}},
		mk("query applicants", ptrace.SpanKindClient, root, 5, ptrace.StatusCodeUnset,
			map[string]any{"db.system": "postgresql", "db.statement": "SELECT name, income FROM applicants WHERE id = $1"}),
		mk("GET", ptrace.SpanKindClient, root, 6, ptrace.StatusCodeOk,
			map[string]any{"http.request.method": "GET", "peer.service": "credit-bureau", "http.target": "/score/8412"}),
		mk("charge", ptrace.SpanKindClient, root, 20, ptrace.StatusCodeOk,
			map[string]any{"http.request.method": "POST", "peer.service": "payment-gw", "http.target": "/charge/8412"}),
		mk("loan.approved send", ptrace.SpanKindProducer, root, 25, ptrace.StatusCodeOk,
			map[string]any{"messaging.destination.name": "loan.approved", "messaging.operation": "publish"}),
		mk("ledger insert", ptrace.SpanKindClient, root, 28, ptrace.StatusCodeUnset,
			map[string]any{"db.system": "postgres", "db.statement": "INSERT INTO ledger (loan_id, amount) VALUES ($1, $2)"}),
		mk("audit insert", ptrace.SpanKindClient, root, 32, ptrace.StatusCodeUnset,
			map[string]any{"db.system": "postgres", "db.statement": "INSERT INTO audit_log (loan_id) VALUES ($1)"}),
	}

	for _, s := range spans {
		sp := ss.Spans().AppendEmpty()
		sp.SetTraceID(traceID)
		sp.SetSpanID(s.id)
		if s.parent != root00 {
			sp.SetParentSpanID(s.parent)
		}
		sp.SetName(s.name)
		sp.SetKind(s.kind)
		sp.SetStartTimestamp(pcommon.NewTimestampFromTime(base.Add(time.Duration(s.startMS) * time.Millisecond)))
		sp.SetEndTimestamp(pcommon.NewTimestampFromTime(base.Add(time.Duration(s.startMS+2) * time.Millisecond)))
		sp.Status().SetCode(s.status)
		// flowmap.flow is the per-flow tag a baggagecopy span processor promotes
		// onto every span out of process.
		sp.Attributes().PutStr("flowmap.flow", "loan-application")
		putAttrs(sp, s.attrs)
	}
	return traces
}

// crossServiceTraces builds the two-resource cross-service impeachment fixture:
// impeachsvc handles POST /admin/federate and calls peersvc, whose server span fans
// out to two DB DELETEs (peer_ledger, peer_audit) it OWNS. Two adversarial properties
// the in-process harness cannot produce, both load-bearing for the §17 cross-service
// closure:
//
//   - the peer's spans carry peersvc's resource service.name (so the DB effects are
//     owned by a FOREIGN service → the service-scope rung downgrades to CROSS-SERVICE);
//   - the peer's clock is skewed ~0.5s behind the caller's, and its two sibling DB
//     writes are timestamped peer_ledger-BEFORE-peer_audit — the REVERSE of canonical
//     op-key order — so a sound out-of-process canonicalization must order them by op
//     key, never by the misleading cross-clock-domain intervals.
func crossServiceTraces() ptrace.Traces {
	traces := ptrace.NewTraces()
	traceID := pcommon.TraceID{0xa1, 0xa1, 0xa1, 0xa1, 0xa1, 0xa1, 0xa1, 0xa1, 0xa1, 0xa1, 0xa1, 0xa1, 0xa1, 0xa1, 0xa1, 0xa1}
	sid := func(b byte) pcommon.SpanID { return pcommon.SpanID{0, 0, 0, 0, 0, 0, 0, b} }
	var none pcommon.SpanID

	emit := func(ss ptrace.ScopeSpans, id, parent pcommon.SpanID, name string, kind ptrace.SpanKind, base time.Time, startMS, durMS int, status ptrace.StatusCode, attrs map[string]any) {
		sp := ss.Spans().AppendEmpty()
		sp.SetTraceID(traceID)
		sp.SetSpanID(id)
		if parent != none {
			sp.SetParentSpanID(parent)
		}
		sp.SetName(name)
		sp.SetKind(kind)
		sp.SetStartTimestamp(pcommon.NewTimestampFromTime(base.Add(time.Duration(startMS) * time.Millisecond)))
		sp.SetEndTimestamp(pcommon.NewTimestampFromTime(base.Add(time.Duration(startMS+durMS) * time.Millisecond)))
		sp.Status().SetCode(status)
		sp.Attributes().PutStr("flowmap.flow", "admin-federate")
		putAttrs(sp, attrs)
	}

	id1, id2, id3, id4, id5 := sid(1), sid(2), sid(3), sid(4), sid(5)

	// impeachsvc resource — the caller's clock.
	callerBase := time.Unix(1700000000, 0).UTC()
	rs1 := traces.ResourceSpans().AppendEmpty()
	rs1.Resource().Attributes().PutStr("service.name", "impeachsvc")
	ss1 := rs1.ScopeSpans().AppendEmpty()
	ss1.Scope().SetName("impeachsvc")
	emit(ss1, id1, none, "POST /admin/federate", ptrace.SpanKindServer, callerBase, 0, 90, ptrace.StatusCodeOk,
		map[string]any{"http.request.method": "POST", "http.route": "/admin/federate", "http.response.status_code": int64(202)})
	emit(ss1, id2, id1, "POST peersvc.replicate", ptrace.SpanKindClient, callerBase, 10, 70, ptrace.StatusCodeOk,
		map[string]any{"http.request.method": "POST", "peer.service": "peersvc", "http.target": "/replicate"})

	// peersvc resource — clock skewed ~0.5s BEHIND the caller; the two DB siblings are
	// timestamped to REVERSE op-key order (peer_ledger earlier than peer_audit).
	peerBase := time.Unix(1699999999, 500000000).UTC()
	rs2 := traces.ResourceSpans().AppendEmpty()
	rs2.Resource().Attributes().PutStr("service.name", "peersvc")
	ss2 := rs2.ScopeSpans().AppendEmpty()
	ss2.Scope().SetName("peersvc")
	emit(ss2, id3, id2, "POST /replicate", ptrace.SpanKindServer, peerBase, 0, 95, ptrace.StatusCodeOk,
		map[string]any{"http.request.method": "POST", "http.route": "/replicate", "http.response.status_code": int64(204)})
	emit(ss2, id4, id3, "ledger delete", ptrace.SpanKindClient, peerBase, 10, 10, ptrace.StatusCodeUnset,
		map[string]any{"db.system": "postgres", "db.statement": "DELETE FROM peer_ledger WHERE loan_id = $1"})
	emit(ss2, id5, id3, "audit delete", ptrace.SpanKindClient, peerBase, 60, 10, ptrace.StatusCodeUnset,
		map[string]any{"db.system": "postgres", "db.statement": "DELETE FROM peer_audit WHERE loan_id = $1"})

	return traces
}

// putAttrs writes attrs onto sp in SORTED key order, so the marshaled bytes are a
// deterministic function of the inputs (Go map iteration order is not).
func putAttrs(sp ptrace.Span, attrs map[string]any) {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch t := attrs[k].(type) {
		case string:
			sp.Attributes().PutStr(k, t)
		case int64:
			sp.Attributes().PutInt(k, t)
		default:
			panic(fmt.Sprintf("unsupported attr type for %s", k))
		}
	}
}
