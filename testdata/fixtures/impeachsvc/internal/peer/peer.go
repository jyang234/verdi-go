// Package peer models a DOWNSTREAM service whose instrumented spans propagate into
// impeachsvc's distributed trace. Replicate emits a span tagged with the peer's OWN
// service.name (the OTel resource attribute a collector folds per service onto each
// span), carrying a DB effect that therefore belongs to "peersvc", not impeachsvc.
// Crucially it is a RAW OTel span, not a database/sql call: the statement runs in the
// peer's code, so impeachsvc's static graph models no emitter for it. That is the
// cross-service shape — an effect behaviorally observed on a foreign service's span
// that this service's analysis cannot, and must not, claim as its own negative. It
// exists so a REAL harness capture drives the service-scope rung (§4 rung 4), instead
// of a hand-authored trace.
package peer

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Replicate records the downstream peer's DB write as it appears in the caller's
// trace: a client-kind span whose service.name is the peer's, with constant SQL so
// canon reads a NAMED "DB postgres DELETE peer_ledger" owned by "peersvc". No
// database/sql call — the write is the peer's, invisible to impeachsvc's graph.
func Replicate(ctx context.Context, loanID string) {
	_, span := otel.Tracer("impeachsvc").Start(ctx, "peer.replicate", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	span.SetAttributes(
		attribute.String("service.name", "peersvc"), // foreign owner: the cross-service signal
		attribute.String("db.system", "postgres"),
		attribute.String("db.statement", "DELETE FROM peer_ledger WHERE loan_id = $1"),
	)
}
