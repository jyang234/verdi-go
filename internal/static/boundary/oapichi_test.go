package boundary_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/boundary"
)

func oapisvcDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", "oapisvc")
}

// TestOapiChiBoundary proves the static pipeline discovers HTTP entry points
// registered the way oapi-codegen's chi server does: through the chi.Router
// interface (an interface-method invoke, not a static call), with the HTTP method
// implied by the function name (r.Post/r.Get) and the route built as
// `baseURL + "/path"`. It also proves the handler is wired into the call graph —
// the publish reached only through the generated wrapper and the ServerInterface
// shows up in the contract.
func TestOapiChiBoundary(t *testing.T) {
	res, err := analyze.Analyze(oapisvcDir())
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	c := boundary.Extract(res)

	wantRoutes := map[string]string{
		"/loan-application":             "POST",
		"/loan-application/{id}/status": "GET",
	}
	got := map[string]string{}
	for _, e := range c.EntryPoints.HTTP {
		got[e.Route] = e.Method
	}
	for route, method := range wantRoutes {
		if got[route] != method {
			t.Errorf("entrypoint %s: got method %q, want %q (all: %+v)", route, got[route], method, c.EntryPoints.HTTP)
		}
	}

	// The publish is reachable only through wrapper → ServerInterface → *Server,
	// so finding it proves the chi-registered root is connected to the graph.
	found := false
	for _, e := range c.Published {
		if e.Event == "loan.created" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected published event loan.created reachable from the chi handler; got %+v", c.Published)
	}
}
