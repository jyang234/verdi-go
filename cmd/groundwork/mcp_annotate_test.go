package main

import (
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

func toolResult(t *testing.T, r map[string]any) (text string, isErr bool) {
	t.Helper()
	_, isErr = r["isError"]
	content, ok := r["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatalf("malformed tool result: %+v", r)
	}
	return content[0]["text"].(string), isErr
}

// TestAnnotateTool covers the read-only annotate proposer: it validates a proposed
// annotation against the live blind-spot manifest and returns ready-to-commit YAML,
// failing closed on an orphan site or an ambiguous kind. It writes nothing.
func TestAnnotateTool(t *testing.T) {
	g := &graph.Graph{
		Algo:  "rta",
		Nodes: []graph.Node{{FQN: "svc.Send"}, {FQN: "svc.Mixed"}},
		BlindSpots: []graph.BlindSpot{
			{Kind: "ExternalBoundaryCall", Site: "svc.Send", Detail: "hands off to external package acme"},
			{Kind: "ExternalBoundaryCall", Site: "svc.Mixed", Detail: "ext"},
			{Kind: "ConcurrentDispatch", Site: "svc.Mixed", Detail: "goroutine"},
		},
	}
	srv := &mcpServer{path: "t", ix: graph.NewIndex(g)}

	// Single-kind site, kind omitted → adopts it; returns the YAML snippet grounded
	// in the matched disclosure's detail.
	txt, isErr := toolResult(t, srv.call("annotate", toolArgs{Site: "svc.Send", Note: "POSTs to acme.example.com", By: "dev@x"}))
	if isErr {
		t.Fatalf("expected success, got error: %s", txt)
	}
	for _, want := range []string{
		"static:", "annotations:", "site: svc.Send", "kind: ExternalBoundaryCall",
		"POSTs to acme.example.com", "by: dev@x", "hands off to external package acme",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("annotate output missing %q:\n%s", want, txt)
		}
	}
	// Read-only: the snippet tells the caller to add it themselves.
	if !strings.Contains(txt, "writes nothing") {
		t.Errorf("annotate must disclose that it writes nothing:\n%s", txt)
	}
	// ExternalBoundaryCall is algo-STABLE: no algorithm-dependence flag on its card.
	if strings.Contains(txt, "algorithm-dependent") {
		t.Errorf("a stable-kind annotation must NOT be flagged algorithm-dependent:\n%s", txt)
	}

	// Orphan site is refused (fail closed, even for a disclosure-only channel).
	if txt, isErr := toolResult(t, srv.call("annotate", toolArgs{Site: "svc.Ghost", Note: "x"})); !isErr {
		t.Errorf("orphan site must error, got: %s", txt)
	}

	// Ambiguous: a multi-kind site with no kind is refused, naming the present kinds.
	txt, isErr = toolResult(t, srv.call("annotate", toolArgs{Site: "svc.Mixed", Note: "x"}))
	if !isErr {
		t.Errorf("ambiguous site must error, got: %s", txt)
	}
	for _, want := range []string{"ConcurrentDispatch", "ExternalBoundaryCall"} {
		if !strings.Contains(txt, want) {
			t.Errorf("ambiguity error should name present kinds; missing %q in: %s", want, txt)
		}
	}

	// With the kind supplied, the same multi-kind site binds. ConcurrentDispatch is
	// algo-FRAGILE, so the card flags it as algorithm-dependent (§22): the binding
	// holds under this graph's rta but a build under a different --algo would
	// warn-and-skip it. The proposer still succeeds — it never fails the build.
	txt, isErr = toolResult(t, srv.call("annotate", toolArgs{Site: "svc.Mixed", Kind: "ConcurrentDispatch", Note: "the worker"}))
	if isErr {
		t.Errorf("disambiguated annotate must succeed, got error: %s", txt)
	}
	if !strings.Contains(txt, "algorithm-dependent") {
		t.Errorf("a fragile-kind (ConcurrentDispatch) annotation must be flagged algorithm-dependent:\n%s", txt)
	}

	// A missing note is refused (the schema requires it; defense in depth).
	if _, isErr := toolResult(t, srv.call("annotate", toolArgs{Site: "svc.Send"})); !isErr {
		t.Error("missing note must error")
	}
}
