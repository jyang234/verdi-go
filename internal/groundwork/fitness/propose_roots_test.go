package fitness

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// A graph spanning two binaries has two composition roots. proposeLayers must
// exempt and record BOTH — keying on the last .main iterated (ix.Nodes() is
// sorted) silently dropped all but the alphabetically-final root.
func TestProposeLayersMultipleRoots(t *testing.T) {
	const (
		serverMain = "example.com/svc/cmd/server.main"
		workerMain = "example.com/svc/cmd/worker.main"
		appRun     = "example.com/svc/internal/app.Run"
		storeSave  = "example.com/svc/internal/store.Save"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: serverMain, Sig: "func()", Tier: 3},
			{FQN: workerMain, Sig: "func()", Tier: 3},
			{FQN: appRun, Sig: "func()", Tier: 3},
			{FQN: storeSave, Sig: "func() error", Tier: 3},
		},
		Edges: []graph.Edge{
			{From: serverMain, To: appRun, Tier: 3},
			{From: workerMain, To: appRun, Tier: 3},
			{From: appRun, To: storeSave, Tier: 3},
		},
	}
	p, _ := Propose(graph.NewIndex(g), "svc")
	if p.Layering == nil {
		t.Fatal("expected a layering proposal")
	}
	got := map[string]bool{}
	for _, r := range p.Layering.Roots {
		got[r] = true
	}
	for _, want := range []string{"example.com/svc/cmd/server", "example.com/svc/cmd/worker"} {
		if !got[want] {
			t.Errorf("root %q missing from proposed roots %v", want, p.Layering.Roots)
		}
	}
}
