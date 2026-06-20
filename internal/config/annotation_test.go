package config

import (
	"errors"
	"strings"
	"testing"
)

// TestResolveAnnotationKind pins the single-source binding rule shared by the
// producer merge (graphio) and the read-only MCP annotate proposer: orphan and
// ambiguity fail closed, a single-kind site adopts its kind, and an error names the
// kinds actually present so a caller can correct without another round-trip.
func TestResolveAnnotationKind(t *testing.T) {
	// Orphan: nothing detected at the site.
	if _, err := ResolveAnnotationKind("svc.Gone", "", nil); err == nil {
		t.Error("a site with no blind spot must be an error")
	}

	// Single kind, requested kind omitted → adopt it (duplicates of one kind are
	// still a single distinct kind, not ambiguous).
	if k, err := ResolveAnnotationKind("svc.Send", "", []string{"ExternalBoundaryCall", "ExternalBoundaryCall"}); err != nil || k != "ExternalBoundaryCall" {
		t.Errorf("single-kind site should adopt; got %q err=%v", k, err)
	}

	// Multiple kinds, omitted → ambiguous, error names both (sorted, deterministic).
	_, err := ResolveAnnotationKind("svc.Mixed", "", []string{"ExternalBoundaryCall", "ConcurrentDispatch"})
	if err == nil {
		t.Fatal("a multi-kind site with no requested kind must be ambiguous")
	}
	if !strings.Contains(err.Error(), "ConcurrentDispatch") || !strings.Contains(err.Error(), "ExternalBoundaryCall") {
		t.Errorf("ambiguity error must name present kinds: %v", err)
	}

	// Named kind present at the site → binds.
	if k, err := ResolveAnnotationKind("svc.Mixed", "ConcurrentDispatch", []string{"ExternalBoundaryCall", "ConcurrentDispatch"}); err != nil || k != "ConcurrentDispatch" {
		t.Errorf("named present kind should bind; got %q err=%v", k, err)
	}

	// Named kind absent at the site → a TYPED *KindAbsentError (the site exists, the
	// kind does not). It is typed distinctly from orphan/ambiguity so a caller can
	// downgrade only this case to a warn-and-skip when the kind is algo-fragile (§22);
	// the message and the Present list must be carried for the caller to act on.
	_, err = ResolveAnnotationKind("svc.Send", "Reflect", []string{"ExternalBoundaryCall"})
	if err == nil {
		t.Fatal("a kind absent at the site must be an error")
	}
	var ka *KindAbsentError
	if !errors.As(err, &ka) {
		t.Fatalf("a present-but-different kind must yield *KindAbsentError, got %T: %v", err, err)
	}
	if ka.Site != "svc.Send" || ka.RequestedKind != "Reflect" || len(ka.Present) != 1 || ka.Present[0] != "ExternalBoundaryCall" {
		t.Errorf("KindAbsentError carries wrong fields: %+v", ka)
	}
	if !strings.Contains(ka.Error(), "ExternalBoundaryCall") || !strings.Contains(ka.Error(), "svc.Send") {
		t.Errorf("KindAbsentError message must name the site and present kinds: %v", ka)
	}

	// The orphan (no blind spot at the site) must NOT be a *KindAbsentError — a caller
	// must never mistake a stale FQN for a tolerable algo skew.
	_, orphanErr := ResolveAnnotationKind("svc.Gone", "Reflect", nil)
	if orphanErr == nil || errors.As(orphanErr, &ka) {
		t.Errorf("an orphan site must be a non-KindAbsentError; got %v", orphanErr)
	}
}
