package roots_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/loader"
	"github.com/jyang234/golang-code-graph/internal/static/roots"
	"github.com/jyang234/golang-code-graph/internal/static/ssabuild"
	"github.com/jyang234/golang-code-graph/internal/static/statictest"
)

func discoverFixture(t *testing.T) *roots.Result {
	t.Helper()
	prog, err := statictest.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return roots.Discover(prog, statictest.Registrars())
}

func TestDiscoverFixtureRoots(t *testing.T) {
	res := discoverFixture(t)

	type rk struct{ kind, name, fqn string }
	got := make(map[rk]bool)
	var primary []rk
	initRoots := 0
	for _, r := range res.Roots {
		key := rk{string(r.Kind), r.Name, r.FQN()}
		got[key] = true
		if r.Kind == roots.KindInit {
			// Every first-party package contributes its synthesized init root,
			// always with an empty Name; the count is the package count, asserted
			// loosely (>0) so the test does not churn when a package is added.
			initRoots++
			if r.Name != "" {
				t.Errorf("init root has a non-empty Name: %+v", r)
			}
			continue
		}
		primary = append(primary, key)
	}

	// The PRIMARY (non-init) root set is pinned exactly: mains, HTTP handlers, and
	// bus consumers. Init roots are partitioned out above.
	want := []rk{
		{"main", "", "example.com/loansvc.main"},
		{"http", "POST /loan-application", "(*example.com/loansvc/internal/handler.App).Create"},
		{"http", "GET /loan-application/{id}/status", "(*example.com/loansvc/internal/handler.App).Status"},
		{"consumer", "payment.settled", "(*example.com/loansvc/internal/consumer.Payments).OnSettled"},
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing root %+v", w)
		}
	}
	if len(primary) != len(want) {
		t.Errorf("got %d primary roots, want %d: %+v", len(primary), len(want), primary)
	}
	// init() is always rooted (it runs unconditionally before main); the fixture
	// spans several first-party packages, so there must be at least one.
	if initRoots == 0 {
		t.Errorf("expected init roots (init runs before main); got none in %+v", res.Roots)
	}
	// The fixture's registrations are all statically resolvable.
	if len(res.BlindSpots) != 0 {
		t.Errorf("unexpected blind spots: %+v", res.BlindSpots)
	}
}

func TestDiscoverResolvesMethodValueToRealMethod(t *testing.T) {
	res := discoverFixture(t)
	for _, r := range res.Roots {
		if r.Kind == roots.KindHTTP && (r.FQN() == "" || containsBound(r.FQN())) {
			t.Errorf("handler root not resolved to real method: %q", r.FQN())
		}
	}
}

func containsBound(s string) bool {
	for i := 0; i+6 <= len(s); i++ {
		if s[i:i+6] == "$bound" {
			return true
		}
	}
	return false
}

func TestDiscoverDeterministic(t *testing.T) {
	prog, err := statictest.Build()
	if err != nil {
		t.Fatal(err)
	}
	first := roots.Discover(prog, statictest.Registrars())
	for i := 0; i < 5; i++ {
		again := roots.Discover(prog, statictest.Registrars())
		if len(again.Roots) != len(first.Roots) {
			t.Fatalf("root count drifted: %d vs %d", len(again.Roots), len(first.Roots))
		}
		for j := range first.Roots {
			if first.Roots[j].FQN() != again.Roots[j].FQN() || first.Roots[j].Kind != again.Roots[j].Kind {
				t.Fatalf("root order drifted at %d: %+v vs %+v", j, first.Roots[j], again.Roots[j])
			}
		}
	}
}

// TestDiscoverBlindSpots synthesizes a module whose handler arguments cannot be
// resolved to concrete functions and checks they are disclosed, not dropped.
func TestDiscoverBlindSpots(t *testing.T) {
	t.Setenv("GOWORK", "off") // analyze the temp module on its own, not via the repo workspace
	dir := t.TempDir()
	write(t, dir, "go.mod", "module dyn\n\ngo 1.24\n")
	write(t, dir, "reg/reg.go", `package reg
type Handler func()
func Register(route string, h Handler) {}
`)
	write(t, dir, "main.go", `package main
import "dyn/reg"
var registry = map[string]reg.Handler{}
func choose() reg.Handler { return func() {} }
func main() {
	reg.Register("GET /a", registry["a"]) // map lookup: unresolvable
	reg.Register("GET /b", choose())      // call result: unresolvable
}
`)

	svc, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	prog, err := ssabuild.Build(svc)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	res := roots.Discover(prog, []roots.Registrar{
		{PkgPath: "dyn/reg", Name: "Register", Kind: roots.KindHTTP, NameArg: 0, HandlerArg: 1},
	})

	if len(res.BlindSpots) != 2 {
		t.Fatalf("got %d blind spots, want 2: %+v", len(res.BlindSpots), res.BlindSpots)
	}
	for _, bs := range res.BlindSpots {
		if bs.Registrar != "dyn/reg.Register" {
			t.Errorf("blind spot registrar = %q", bs.Registrar)
		}
		if bs.Pos == "" {
			t.Errorf("blind spot missing source position: %+v", bs)
		}
	}
	// main is still a root.
	var hasMain bool
	for _, r := range res.Roots {
		if r.Kind == roots.KindMain {
			hasMain = true
		}
	}
	if !hasMain {
		t.Error("main root missing")
	}
}

// TestDiscoverSharedHandlerKeepsEveryRoute synthesizes a module where two routes
// register the same handler function: both must survive as roots. Dedupe keyed
// on the function alone dropped one route from the gated contract, with the
// survivor picked by map iteration order.
func TestDiscoverSharedHandlerKeepsEveryRoute(t *testing.T) {
	res := discoverShared(t)

	got := make(map[string]string, len(res.Roots))
	for _, r := range res.Roots {
		if r.Kind == roots.KindHTTP {
			got[r.Name] = r.FQN()
		}
	}
	for _, route := range []string{"GET /health", "GET /ready"} {
		if got[route] != "dyn.ok" {
			t.Errorf("route %q: got handler %q, want dyn.ok", route, got[route])
		}
	}
	// The shared handler must still be a single graph root.
	fns := res.Funcs()
	seen := make(map[string]bool, len(fns))
	for _, fn := range fns {
		if seen[fn.String()] {
			t.Errorf("Funcs returned duplicate root function %q", fn)
		}
		seen[fn.String()] = true
	}
}

// TestDiscoverSharedHandlerDeterministic re-discovers the shared-handler module:
// the registrations live in two different functions, so a survivor chosen during
// AllFunctions map iteration would differ between runs.
func TestDiscoverSharedHandlerDeterministic(t *testing.T) {
	first := discoverShared(t)
	for i := 0; i < 5; i++ {
		again := discoverShared(t)
		if len(again.Roots) != len(first.Roots) {
			t.Fatalf("root count drifted: %d vs %d", len(again.Roots), len(first.Roots))
		}
		for j := range first.Roots {
			if first.Roots[j].Name != again.Roots[j].Name || first.Roots[j].FQN() != again.Roots[j].FQN() {
				t.Fatalf("root %d drifted: %+v vs %+v", j, first.Roots[j], again.Roots[j])
			}
		}
	}
}

// discoverShared builds a module whose two HTTP routes share one handler,
// registered from two separate functions.
func discoverShared(t *testing.T) *roots.Result {
	t.Helper()
	t.Setenv("GOWORK", "off")
	dir := t.TempDir()
	write(t, dir, "go.mod", "module dyn\n\ngo 1.24\n")
	write(t, dir, "reg/reg.go", `package reg
type Handler func()
func Register(route string, h Handler) {}
`)
	write(t, dir, "main.go", `package main
import "dyn/reg"
func ok() {}
func registerHealth() { reg.Register("GET /health", ok) }
func registerReady()  { reg.Register("GET /ready", ok) }
func main() {
	registerHealth()
	registerReady()
}
`)

	svc, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	prog, err := ssabuild.Build(svc)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return roots.Discover(prog, []roots.Registrar{
		{PkgPath: "dyn/reg", Name: "Register", Kind: roots.KindHTTP, NameArg: 0, HandlerArg: 1},
	})
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
