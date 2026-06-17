// Package router is a tiny custom HTTP router — the kind of bespoke mini-framework
// real services grow in-house. Its Add method is NOT one of flowmap's recognized
// registration hints (net/http.HandleFunc, chi's per-method funcs, or a
// classify-listed bus subscribe), so a route mounted through it is a MISSED ROOT:
// the call-graph builder still connects ServeHTTP to the handler (the value is
// address-taken in the map, resolved by signature), so the handler's effects are
// reachable from main — but no DISCOVERED entrypoint owns them, and nothing
// discloses the gap (there is no recognized registrar for root discovery to flag
// as UnresolvedDispatch). That silent attribution hole is exactly the seam the
// behavioral-impeachment cell exists to catch.
package router

import "net/http"

// Router dispatches on "<METHOD> <path>" against a table built at mount time.
type Router struct {
	routes map[string]http.HandlerFunc
}

// New returns an empty Router.
func New() *Router { return &Router{routes: map[string]http.HandlerFunc{}} }

// Add registers h under "<method> <pattern>". This is the unhinted registrar: a
// plain method on a first-party type, invisible to root discovery.
func (r *Router) Add(method, pattern string, h http.HandlerFunc) {
	r.routes[method+" "+pattern] = h
}

// ServeHTTP dispatches by method+path prefix. The lookup is a func-value call, so
// the builder resolves it to every address-taken HandlerFunc by signature — the
// handlers ARE in the graph, reachable from main, just unattributed to a route.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	for key, h := range r.routes {
		if matches(key, req) {
			h(w, req)
			return
		}
	}
	http.NotFound(w, req)
}

// matches reports whether the request hits a registered "<method> <prefix>" key.
func matches(key string, req *http.Request) bool {
	want := req.Method + " " + req.URL.Path
	if want == key {
		return true
	}
	// Prefix match so "DELETE /admin/ledger" serves "DELETE /admin/ledger/L1".
	return len(want) > len(key) && want[:len(key)] == key && want[len(key)] == '/'
}
