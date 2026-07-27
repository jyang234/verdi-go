package fitness

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

func concurrentPolicy(rule policy.ConcurrentRule) *policy.Policy {
	return &policy.Policy{Service: "layeredsvc", Version: 1, NoConcurrentReach: []policy.ConcurrentRule{rule}}
}

// TestConcurrentCharacterization pins every Finding field emitted by
// no_concurrent_reach before its rule-independent surface moves to the shared
// facts package. Presentation changes require an explicit compatibility review.
//
// The two "canonical ... over shuffled input" cases are the ONE deliberate
// exception: they pin a post-extraction correction and therefore FAIL against
// the pre-extraction probe. See their case comments.
func TestConcurrentCharacterization(t *testing.T) {
	const (
		launcher = "svc.Launcher"
		worker   = "svc.Worker"
		target   = "svc.Target"
		update   = "boundary:db UPDATE users"
		publish  = "boundary:bus PUBLISH user.updated"
		dynamic  = "boundary:bus PUBLISH <dynamic>"
	)
	nodes := func(fqns ...string) []graph.Node {
		result := make([]graph.Node, len(fqns))
		for i, fqn := range fqns {
			result[i] = graph.Node{FQN: fqn}
		}
		return result
	}
	rule := func(to string) policy.ConcurrentRule {
		return policy.ConcurrentRule{Name: "no-async", To: []string{to}}
	}
	hit := func(from, to string) Finding {
		return Finding{
			Rule:     "no_concurrent_reach",
			Severity: Violation,
			Summary:  "no-async: " + ShortName(to) + " reachable on a concurrent path",
			From:     from,
			To:       to,
		}
	}

	tests := []struct {
		name string
		g    *graph.Graph
		rule policy.ConcurrentRule
		want []Finding
	}{
		{
			name: "direct concurrent boundary hit",
			g: &graph.Graph{
				Nodes: nodes(launcher),
				Edges: []graph.Edge{{
					From: launcher, To: publish, Boundary: "outbound-async", Concurrent: true,
				}},
			},
			rule: rule("boundary:bus PUBLISH"),
			want: []Finding{hit(launcher, publish)},
		},
		{
			name: "spawned function hit",
			g: &graph.Graph{
				Nodes: nodes(launcher, worker),
				Edges: []graph.Edge{{From: launcher, To: worker, Concurrent: true}},
			},
			rule: rule(worker),
			want: []Finding{hit("", worker)},
		},
		{
			name: "concurrent cone effect hit",
			g: &graph.Graph{
				Nodes: nodes(launcher, worker),
				Edges: []graph.Edge{
					{From: launcher, To: worker, Concurrent: true},
					{From: worker, To: update, Boundary: "outbound-sync"},
				},
			},
			rule: rule("boundary:db UPDATE"),
			want: []Finding{hit(worker, update)},
		},
		{
			name: "duplicate hit collapse",
			g: &graph.Graph{
				Nodes: nodes(launcher, worker),
				Edges: []graph.Edge{
					{From: launcher, To: worker, Concurrent: true},
					{From: worker, To: publish, Boundary: "outbound-async", Concurrent: true},
					{From: worker, To: publish, Boundary: "outbound-async", Concurrent: true},
				},
			},
			rule: rule("boundary:bus PUBLISH"),
			want: []Finding{hit(worker, publish)},
		},
		{
			name: "clean visible surface",
			g: &graph.Graph{
				Nodes: nodes(launcher, worker, target),
				Edges: []graph.Edge{{From: launcher, To: worker, Concurrent: true}},
			},
			rule: rule(target),
		},
		{
			name: "concurrent cone blind",
			g: &graph.Graph{
				Nodes:      nodes(launcher, worker, target),
				Edges:      []graph.Edge{{From: launcher, To: worker, Concurrent: true}},
				BlindSpots: []graph.BlindSpot{{Kind: "reflect", Site: worker, Detail: "opaque dispatch"}},
			},
			rule: rule(target),
			want: []Finding{{
				Rule:     "no_concurrent_reach",
				Severity: Caution,
				Summary:  "no-async: no concurrent path found, but the frontier is blind (reflect at svc.Worker) — cannot prove the concurrent cone avoids the target",
			}},
		},
		{
			name: "dynamic direct boundary blind",
			g: &graph.Graph{
				Nodes: nodes(launcher, target),
				Edges: []graph.Edge{
					{From: launcher, To: dynamic, Boundary: "outbound-async", Concurrent: true},
				},
			},
			rule: rule(target),
			want: []Finding{{
				Rule:     "no_concurrent_reach",
				Severity: Caution,
				Summary:  "no-async: no concurrent path found, but the frontier is blind (unresolved concurrent boundary effect " + dynamic + ") — cannot prove the concurrent cone avoids the target",
			}},
		},
		{
			name: "graph wide concurrent dispatch blind",
			g: &graph.Graph{
				Nodes:      nodes(launcher, target),
				BlindSpots: []graph.BlindSpot{{Kind: "ConcurrentDispatch", Site: launcher, Detail: "go f()"}},
			},
			rule: rule(target),
			want: []Finding{{
				Rule:     "no_concurrent_reach",
				Severity: Caution,
				Summary:  "no-async: no concurrent path found, but the frontier is blind (ConcurrentDispatch at svc.Launcher) — cannot prove the concurrent cone avoids the target",
			}},
		},
		{
			// Post-extraction correction, pinned deliberately. graph.Load does not
			// sort the blind-spot manifest, so the pre-extraction probe returned
			// whichever ConcurrentDispatch the PRODUCER happened to emit first
			// (svc.Zed here) — the named site moved with input order, churning the
			// base-vs-branch diff for a graph that is semantically identical. The
			// facts surface sorts the manifest before selecting, so the
			// representative is now a pure function of the graph's content.
			name: "canonical concurrent dispatch site over shuffled input",
			g: &graph.Graph{
				Nodes: nodes(launcher, target),
				BlindSpots: []graph.BlindSpot{
					{Kind: "ConcurrentDispatch", Site: "svc.Zed", Detail: "go z()"},
					{Kind: "ConcurrentDispatch", Site: "svc.Alpha", Detail: "go a()"},
				},
			},
			rule: rule(target),
			want: []Finding{{
				Rule:     "no_concurrent_reach",
				Severity: Caution,
				Summary:  "no-async: no concurrent path found, but the frontier is blind (ConcurrentDispatch at svc.Alpha) — cannot prove the concurrent cone avoids the target",
			}},
		},
		{
			// Post-extraction correction, pinned deliberately — the same input-order
			// dependence as above, on the dynamic direct concurrent boundary probe.
			// The pre-extraction probe walked ix.Edges() and named boundary:zzz;
			// canonicalBoundaryEdges sorts first, so the representative no longer
			// depends on producer emission order.
			name: "canonical dynamic direct boundary over shuffled input",
			g: &graph.Graph{
				Nodes: nodes(launcher, target),
				Edges: []graph.Edge{
					{From: launcher, To: "boundary:zzz PUBLISH <dynamic>", Boundary: "outbound-async", Concurrent: true},
					{From: launcher, To: "boundary:aaa PUBLISH <dynamic>", Boundary: "outbound-async", Concurrent: true},
				},
			},
			rule: rule(target),
			want: []Finding{{
				Rule:     "no_concurrent_reach",
				Severity: Caution,
				Summary:  "no-async: no concurrent path found, but the frontier is blind (unresolved concurrent boundary effect boundary:aaa PUBLISH <dynamic>) — cannot prove the concurrent cone avoids the target",
			}},
		},
		{
			name: "dead target",
			g:    &graph.Graph{Nodes: nodes(launcher)},
			rule: rule("svc.Missing"),
			want: []Finding{{
				Rule:     "no_concurrent_reach",
				Severity: Caution,
				Summary:  "no-async: to binds nothing in this graph — name a first-party sink it can bind, or this invariant is vacuous",
			}},
		},
		{
			name: "require proof escalates blind",
			g: &graph.Graph{
				Nodes:      nodes(launcher, worker, target),
				Edges:      []graph.Edge{{From: launcher, To: worker, Concurrent: true}},
				BlindSpots: []graph.BlindSpot{{Kind: "reflect", Site: worker, Detail: "opaque dispatch"}},
			},
			rule: policy.ConcurrentRule{
				Name: "no-async", To: []string{target}, RequireProof: true,
			},
			want: []Finding{{
				Rule:     "no_concurrent_reach",
				Severity: Violation,
				Summary:  "no-async: no concurrent path found, but the frontier is blind (reflect at svc.Worker) — require_proof is set and avoidance cannot be proven",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Check(concurrentPolicy(tt.rule), graph.NewIndex(tt.g)).Findings
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("concurrent findings mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

// A goroutine-spawned function whose cone hits a forbidden boundary fires; the
// same call on the synchronous path does not.
func TestConcurrentReachDetected(t *testing.T) {
	rule := policy.ConcurrentRule{Name: "no-async-writes", To: []string{"boundary:db UPDATE"}}

	// Synchronous base: UpdateUser writes via the normal path — no finding.
	g := loadGraph(t, "layeredsvc.graph.json")
	if res := Check(concurrentPolicy(rule), graph.NewIndex(g)); len(res.Findings) != 0 {
		t.Fatalf("synchronous writes must not fire the rule; got %v", res.Findings)
	}

	// Spawn the store call on a goroutine: the same write is now concurrent.
	g = loadGraph(t, "layeredsvc.graph.json")
	g.Edges = append(g.Edges, graph.Edge{
		From: "(*example.com/layeredsvc/internal/app.Service).UpdateProfile",
		To:   "(*example.com/layeredsvc/internal/store.Store).UpdateUser",
		Tier: 2, Concurrent: true,
	})
	res := Check(concurrentPolicy(rule), graph.NewIndex(g))
	v := res.Violations()
	if len(v) != 1 || v[0].Rule != "no_concurrent_reach" {
		t.Fatalf("want 1 no_concurrent_reach violation, got %v", res.Findings)
	}
	if v[0].To != "boundary:db UPDATE users" {
		t.Errorf("violation target = %q", v[0].To)
	}
}

// A concurrent boundary edge IS the target directly (`go publish(...)`).
func TestConcurrentDirectEffect(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	g.Edges = append(g.Edges, graph.Edge{
		From: "(*example.com/layeredsvc/internal/app.Service).UpdateProfile",
		To:   "boundary:bus PUBLISH user.updated", Tier: 1,
		Boundary: "outbound-async", Concurrent: true,
	})
	rule := policy.ConcurrentRule{Name: "no-async-publish", To: []string{"boundary:bus PUBLISH"}}
	res := Check(concurrentPolicy(rule), graph.NewIndex(g))
	if v := res.Violations(); len(v) != 1 || !strings.Contains(v[0].Summary, "concurrent path") {
		t.Fatalf("want the direct concurrent publish violation, got %v", res.Findings)
	}
}

// Issue 1a: a no_concurrent_reach rule whose To binds nothing is a dead selector
// (a typo'd or stale label), disclosed exactly like an unbindable must_not_reach
// target — a Caution by default, escalated under require_proof — never a silent
// "enforced" pass. This is the parity the issue asked for.
func TestConcurrentUnbindableTargetIsDisclosed(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	rule := policy.ConcurrentRule{Name: "no-async-zzz", To: []string{"boundary:db ZZZ_NONEXISTENT"}}
	res := Check(concurrentPolicy(rule), graph.NewIndex(g))
	c := res.Cautions()
	if len(res.Violations()) != 0 || len(c) != 1 || !strings.Contains(c[0].Summary, "to binds nothing") {
		t.Fatalf("an unbindable no_concurrent_reach To must be a disclosed caution, got %v", res.Findings)
	}

	rule.RequireProof = true
	res = Check(concurrentPolicy(rule), graph.NewIndex(g))
	if v := res.Violations(); len(v) != 1 || !strings.Contains(v[0].Summary, "require_proof") {
		t.Fatalf("require_proof must escalate an unbindable concurrent target, got %v", res.Findings)
	}
}

// No concurrent path over a blind frontier: caution, escalated by require_proof.
func TestConcurrentBlindFrontier(t *testing.T) {
	g := loadGraph(t, "blindsvc.graph.json")
	// Spawn something concurrent so the cone exists and crosses blind territory.
	var anyNode string
	for _, n := range g.Nodes {
		if strings.Contains(n.FQN, "handler") {
			anyNode = n.FQN
			break
		}
	}
	g.Edges = append(g.Edges, graph.Edge{From: anyNode, To: anyNode, Tier: 2, Concurrent: true})
	// The target must BIND somewhere (else the to-binds-nothing check fires first),
	// but NOT on the concurrent cone — so an isolated node reaches it. The concurrent
	// cone stays blind and never reaches it, exercising the blind-frontier caution.
	g.Nodes = append(g.Nodes, graph.Node{FQN: "example.com/blindsvc/internal/store.isolatedDeleter"})
	g.Edges = append(g.Edges, graph.Edge{From: "example.com/blindsvc/internal/store.isolatedDeleter", To: "boundary:db DELETE", Tier: 1, Boundary: "db"})
	rule := policy.ConcurrentRule{Name: "no-async-deletes", To: []string{"boundary:db DELETE"}}
	res := Check(concurrentPolicy(rule), graph.NewIndex(g))
	if c := res.Cautions(); len(c) != 1 || !strings.Contains(c[0].Summary, "frontier is blind") {
		t.Fatalf("want one blind-frontier caution, got %v", res.Findings)
	}

	rule.RequireProof = true
	res = Check(concurrentPolicy(rule), graph.NewIndex(g))
	if v := res.Violations(); len(v) != 1 || !strings.Contains(v[0].Summary, "require_proof") {
		t.Fatalf("require_proof must escalate, got %v", res.Findings)
	}
}

// TestConcurrentUnresolvedGoDispatchBlinds is the C-6 regression: a graph with a
// bindable boundary target, ZERO concurrent edges, and a ConcurrentDispatch blind
// spot (an unresolved `go f()` at a spawning function that never enters the
// concurrent cone) must not pass vacuously. The old blind probe surveyed only the
// resolved cone, so it saw nothing and returned silent green even under
// require_proof; the probe now surveys ConcurrentDispatch blind spots graph-wide.
func TestConcurrentUnresolvedGoDispatchBlinds(t *testing.T) {
	mk := func() *graph.Graph {
		return &graph.Graph{
			Algo: "rta",
			Nodes: []graph.Node{
				{FQN: "example.com/x/app.Handle"},
				{FQN: "example.com/x/store.deleteRows"},
			},
			Edges: []graph.Edge{
				// A bindable db DELETE reached only synchronously — no concurrent edge.
				{From: "example.com/x/store.deleteRows", To: "boundary:db DELETE rows", Tier: 1, Boundary: "db"},
			},
			BlindSpots: []graph.BlindSpot{
				// Unresolved `go someFuncValue()`: produces no edge, so nothing enters
				// the concurrent cone — only this blind spot at the spawning function.
				{Kind: "ConcurrentDispatch", Site: "example.com/x/app.Handle", Detail: "go f()"},
			},
		}
	}
	rule := policy.ConcurrentRule{Name: "no-async-deletes", To: []string{"boundary:db DELETE"}}

	res := Check(concurrentPolicy(rule), graph.NewIndex(mk()))
	if c := res.Cautions(); len(c) != 1 || !strings.Contains(c[0].Summary, "frontier is blind") {
		t.Fatalf("an unresolved go-dispatch must blind the concurrent probe (caution), got %v", res.Findings)
	}
	if len(res.Violations()) != 0 {
		t.Fatalf("without require_proof the blind dispatch is a caution, not a violation: %v", res.Findings)
	}

	rule.RequireProof = true
	res = Check(concurrentPolicy(rule), graph.NewIndex(mk()))
	if v := res.Violations(); len(v) != 1 || !strings.Contains(v[0].Summary, "require_proof") {
		t.Fatalf("require_proof must escalate the blind concurrent dispatch, got %v", res.Findings)
	}
}

// RF-2: findings are a set, not a multiset. The same function spawned from two
// goroutine sites — and its boundary effect reached both directly and through
// the cone — is one finding per (from, target) pair.
func TestConcurrentDuplicateSpawnsSingleFinding(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	const sUpdate = "(*example.com/layeredsvc/internal/store.Store).UpdateUser"
	g.Edges = append(g.Edges,
		graph.Edge{From: "(*example.com/layeredsvc/internal/app.Service).UpdateProfile", To: sUpdate, Tier: 2, Concurrent: true},
		graph.Edge{From: "(*example.com/layeredsvc/internal/handler.Server).UpdateUser", To: sUpdate, Tier: 2, Concurrent: true},
	)
	rule := policy.ConcurrentRule{Name: "no-async-writes", To: []string{"boundary:db UPDATE"}}
	res := Check(concurrentPolicy(rule), graph.NewIndex(g))
	if v := res.Violations(); len(v) != 1 {
		t.Fatalf("two spawn sites of one function must yield one finding, got %v", v)
	}
}
