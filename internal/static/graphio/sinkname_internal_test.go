package graphio

import "testing"

// TestSinkMethodNameNilSite pins H-4's latent sibling: sinkMethodName is the
// fallback label for a DB call whose statement cannot be read, and a synthetic
// edge carries a nil site (callgraph.Edge.Site is documented nil for synthetic
// edges). It must return the opaque "call" label rather than dereference a nil
// Common() and panic — fail closed to the safe label.
func TestSinkMethodNameNilSite(t *testing.T) {
	if got := sinkMethodName(nil); got != "call" {
		t.Fatalf("sinkMethodName(nil) = %q, want %q", got, "call")
	}
}
