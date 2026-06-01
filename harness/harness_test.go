package harness_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/jyang234/golang-code-graph/harness"
	"github.com/jyang234/golang-code-graph/internal/canon"
	"github.com/jyang234/golang-code-graph/internal/capture"
	"github.com/jyang234/golang-code-graph/ir"
)

// instrumentedSUT is a miniature loan service wired with real OTel spans, used to
// exercise the harness end to end. The applicant read and the credit-score call
// run concurrently; an audit write fires from a goroutine after the response, so
// the harness must drain it before declaring completeness. slowLeg lengthens the
// credit-bureau call to prove the concurrency classification is timing-stable.
func instrumentedSUT(slowLeg time.Duration) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /loan-application", func(w http.ResponseWriter, r *http.Request) {
		tr := otel.Tracer("sut")
		ctx := r.Context()

		evalCtx, eval := tr.Start(ctx, "evaluateApplication")
		var wg sync.WaitGroup
		wg.Add(2)
		// The two legs are dispatched together and both perform real I/O, so their
		// spans overlap in time — a genuine race the canonicalizer reads as
		// concurrent. (Instantaneous spans that happen not to overlap are the
		// ambiguous case the determinism self-test is meant to surface.)
		go func() {
			defer wg.Done()
			_, sp := tr.Start(evalCtx, "select", oteltrace.WithSpanKind(oteltrace.SpanKindClient))
			sp.SetAttributes(
				attribute.String("db.system", "postgresql"),
				attribute.String("db.statement", "SELECT name, income FROM applicants WHERE id = $1"),
			)
			time.Sleep(10 * time.Millisecond)
			sp.End()
		}()
		go func() {
			defer wg.Done()
			scCtx, sc := tr.Start(evalCtx, "scorer.Score")
			_, b := tr.Start(scCtx, "GET /score", oteltrace.WithSpanKind(oteltrace.SpanKindClient))
			b.SetAttributes(
				attribute.String("http.request.method", "GET"),
				attribute.String("peer.service", "credit-bureau"),
				attribute.String("http.target", "/score/8412"),
			)
			time.Sleep(10*time.Millisecond + slowLeg)
			b.End()
			sc.End()
		}()
		wg.Wait()
		eval.End()

		_, ch := tr.Start(ctx, "charge", oteltrace.WithSpanKind(oteltrace.SpanKindClient))
		ch.SetAttributes(
			attribute.String("http.request.method", "POST"),
			attribute.String("peer.service", "payment-gw"),
			attribute.String("http.target", "/charge/8412"),
		)
		ch.End()

		_, pub := tr.Start(ctx, "publish", oteltrace.WithSpanKind(oteltrace.SpanKindProducer))
		pub.SetAttributes(attribute.String("messaging.destination.name", "loan.approved"))
		pub.End()

		_, led := tr.Start(ctx, "ledger", oteltrace.WithSpanKind(oteltrace.SpanKindClient))
		led.SetAttributes(
			attribute.String("db.system", "postgres"),
			attribute.String("db.statement", "INSERT INTO ledger (loan_id, amount) VALUES ($1, $2)"),
		)
		led.End()

		// Fire-and-forget audit: starts a span after the response is on its way.
		auditCtx := context.WithoutCancel(ctx)
		go func() {
			time.Sleep(5 * time.Millisecond)
			_, au := tr.Start(auditCtx, "audit", oteltrace.WithSpanKind(oteltrace.SpanKindClient))
			au.SetAttributes(
				attribute.String("db.system", "postgres"),
				attribute.String("db.statement", "INSERT INTO audit_log (loan_id) VALUES ($1)"),
			)
			au.End()
		}()

		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func happyMarkers() []string {
	return []string{
		"HTTP GET credit-bureau /score/{id}",
		"PUBLISH loan.approved",
		"DB postgres INSERT ledger",
		"DB postgres INSERT audit_log",
	}
}

func TestHTTPCaptureComplete(t *testing.T) {
	app := harness.NewInProcess(t, instrumentedSUT(0), harness.WithService("loansvc"))
	cf, err := app.HTTP("POST", "/loan-application", []byte(`{"id":"8412"}`)).Capture(harness.CaptureOptions{
		Markers: happyMarkers(),
		Quiet:   10 * time.Millisecond,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if !cf.Complete {
		t.Fatal("expected Complete=true")
	}
	if cf.Root == nil || cf.Root.Kind != ir.KindServer {
		t.Fatalf("root = %+v, want a server span", cf.Root)
	}
	if cf.Service != "loansvc" {
		t.Errorf("service = %q", cf.Service)
	}
	// The fire-and-forget audit span must have been drained in.
	if !hasSpanWithAttr(cf.Spans, "db.statement", "INSERT INTO audit_log (loan_id) VALUES ($1)") {
		t.Error("audit span from the goroutine was not captured")
	}
	// Sanity: a producer publish and a client DB read are present.
	if countKind(cf.Spans, ir.KindProducer) != 1 {
		t.Error("expected exactly one producer (publish) span")
	}
	if countKind(cf.Spans, ir.KindClient) < 3 {
		t.Error("expected the DB/client/charge spans")
	}
}

func TestHTTPCaptureCanonicalizes(t *testing.T) {
	app := harness.NewInProcess(t, instrumentedSUT(0), harness.WithService("loansvc"))
	cf, err := app.HTTP("POST", "/loan-application", nil).Capture(harness.CaptureOptions{
		Markers: happyMarkers(),
		Quiet:   10 * time.Millisecond,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	tr, err := canon.Canonicalize(*cf, nil)
	if err != nil {
		t.Fatalf("Canonicalize: %v", err)
	}
	if tr.Root.Op != "HTTP POST /loan-application" {
		t.Errorf("root op = %q", tr.Root.Op)
	}
	// The tier-3 evaluator/scorer are contracted away; the applicant read and the
	// credit-bureau call surface as a concurrent pair directly under the root.
	g0 := tr.Root.Children[0]
	if !g0.Concurrent || len(g0.Members) != 2 {
		t.Fatalf("first group should be the promoted concurrent pair, got %+v", g0)
	}
	if g0.Members[0].Op != "DB postgresql SELECT applicants" || g0.Members[1].Op != "HTTP GET credit-bureau /score/{id}" {
		t.Errorf("concurrent members = %q, %q", g0.Members[0].Op, g0.Members[1].Op)
	}
}

// TestTimingStableConcurrency is the fast-vs-slow check: lengthening one errgroup
// leg must not change the concurrent grouping (canon §3.3 rule 3).
func TestTimingStableConcurrency(t *testing.T) {
	fast := canonStructure(t, 0)
	slow := canonStructure(t, 40*time.Millisecond)
	if fast != slow {
		t.Errorf("concurrency classification changed with timing:\n--- fast ---\n%s\n--- slow ---\n%s", fast, slow)
	}
}

func canonStructure(t *testing.T, slow time.Duration) string {
	t.Helper()
	app := harness.NewInProcess(t, instrumentedSUT(slow), harness.WithService("loansvc"))
	cf, err := app.HTTP("POST", "/loan-application", nil).Capture(harness.CaptureOptions{
		Markers: happyMarkers(),
		Quiet:   10 * time.Millisecond,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	tr, err := canon.Canonicalize(*cf, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := tr.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestTruncatedCaptureFailsLoudly drives a flow whose declared exit never occurs;
// the harness must report incompleteness rather than surface a partial trace.
func TestTruncatedCaptureFailsLoudly(t *testing.T) {
	app := harness.NewInProcess(t, instrumentedSUT(0), harness.WithService("loansvc"))
	cf, err := app.HTTP("POST", "/loan-application", nil).Capture(harness.CaptureOptions{
		Markers: []string{"PUBLISH nonexistent.event"}, // never published
		Quiet:   5 * time.Millisecond,
		Timeout: 150 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected a truncation error")
	}
	if cf.Complete {
		t.Fatal("expected Complete=false")
	}
	// And canonicalization refuses it as the last line of defense.
	if _, cerr := canon.Canonicalize(*cf, nil); cerr != canon.ErrIncomplete {
		t.Errorf("canon error = %v, want ErrIncomplete", cerr)
	}
}

// TestEventCaptureConsumerRoot drives a consumer-rooted flow: the consumer span
// becomes the root.
func TestEventCaptureConsumerRoot(t *testing.T) {
	app := harness.NewInProcess(t, http.NewServeMux(), harness.WithService("loansvc"))
	deliver := func(ctx context.Context) {
		tr := otel.Tracer("sut")
		ctx, root := tr.Start(ctx, "consume payment.settled", oteltrace.WithSpanKind(oteltrace.SpanKindConsumer))
		root.SetAttributes(attribute.String("messaging.destination", "payment.settled"))
		_, up := tr.Start(ctx, "markPaid", oteltrace.WithSpanKind(oteltrace.SpanKindClient))
		up.SetAttributes(
			attribute.String("db.system", "postgres"),
			attribute.String("db.statement", "UPDATE loans SET status = 'paid' WHERE id = $1"),
		)
		up.End()
		root.End()
	}
	cf, err := app.Event("payment.settled", deliver).Capture(harness.CaptureOptions{
		Markers: []string{"DB postgres UPDATE loans"},
		Quiet:   10 * time.Millisecond,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if cf.Root == nil || cf.Root.Kind != ir.KindConsumer {
		t.Fatalf("root = %+v, want a consumer span", cf.Root)
	}
	tr, err := canon.Canonicalize(*cf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Root.Op != "CONSUME payment.settled" {
		t.Errorf("root op = %q", tr.Root.Op)
	}
}

func hasSpanWithAttr(spans []capture.Span, key, val string) bool {
	for i := range spans {
		if spans[i].Attrs[key] == val {
			return true
		}
	}
	return false
}

func countKind(spans []capture.Span, k ir.Kind) int {
	n := 0
	for i := range spans {
		if spans[i].Kind == k {
			n++
		}
	}
	return n
}
