package fitness

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
)

const goldensDir = "../../../testdata/groundwork/goldens"

const (
	hGetUser      = "(*example.com/layeredsvc/internal/handler.Server).GetUser"
	hUpdateUser   = "(*example.com/layeredsvc/internal/handler.Server).UpdateUser"
	sSelectUser   = "(*example.com/layeredsvc/internal/store.Store).SelectUser"
	svcMain       = "example.com/layeredsvc.main"
	blindsvcStart = "(*example.com/blindsvc/internal/handler.Server).Create"
)

func loadGraph(t *testing.T, name string) *graph.Graph {
	t.Helper()
	g, err := graph.LoadFile(filepath.Join(goldensDir, name))
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return g
}

// layeredPolicy is the strict handler→app→store policy, declared in code so a
// test can vary one field without editing the committed JSON.
func layeredPolicy() *policy.Policy {
	return &policy.Policy{
		Service: "layeredsvc", Version: 1,
		Layers: []policy.Layer{
			{Name: "handler", Packages: []string{"example.com/layeredsvc/internal/handler"}},
			{Name: "app", Packages: []string{"example.com/layeredsvc/internal/app"}},
			{Name: "store", Packages: []string{"example.com/layeredsvc/internal/store"}},
		},
		Layering: &policy.Layering{Roots: []string{"example.com/layeredsvc"}},
	}
}

// The policy-vs-graph substrate mismatch is a DISCLOSURE (a caveat on the
// substrate line), never a fitness Finding: Check is shared with review/verify,
// so a finding here would leak into their base-vs-branch diff and flip a verdict.
// Check must therefore stay silent on a mismatch — the caveat channel
// (cmdFitness, provenanceCaveats) carries it. The caveat text itself is covered by
// graph.TestSubstrateMismatchCaveat and review.TestReviewFlagsPolicyGraphSubstrateMismatch.
func TestCheckEmitsNoSubstrateFinding(t *testing.T) {
	g := &graph.Graph{Algo: "rta", Nodes: []graph.Node{{FQN: "svc.A", Tier: 1}}}
	res := Check(&policy.Policy{Service: "svc", Version: 1, Substrate: "vta"}, graph.NewIndex(g))
	for _, f := range res.Findings {
		if f.Rule == "substrate" {
			t.Errorf("Check must not emit a substrate finding (it leaks into review's diff); got %+v", f)
		}
	}
}

func TestPkgOf(t *testing.T) {
	cases := map[string]string{
		hGetUser: "example.com/layeredsvc/internal/handler",
		"example.com/layeredsvc/internal/handler.writeJSON":         "example.com/layeredsvc/internal/handler",
		"example.com/layeredsvc.run":                                "example.com/layeredsvc",
		"example.com/layeredsvc.main":                               "example.com/layeredsvc",
		"(example.com/x/pkg.T).M":                                   "example.com/x/pkg",
		"example.com/x/gen.Decode[example.com/x/other.Application]": "example.com/x/gen",
	}
	for fqn, want := range cases {
		if got := PkgOf(fqn); got != want {
			t.Errorf("PkgOf(%q) = %q, want %q", fqn, got, want)
		}
	}
}

func TestLayeringCleanBase(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	res := Check(layeredPolicy(), graph.NewIndex(g))
	if !res.OK() {
		t.Fatalf("clean base should pass; got violations: %v", res.Violations())
	}
}

func TestLayeringSkipDetected(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	g.Edges = append(g.Edges, graph.Edge{From: hGetUser, To: sSelectUser, Tier: 2})
	res := Check(layeredPolicy(), graph.NewIndex(g))

	v := res.Violations()
	if len(v) != 1 || v[0].Rule != "layering" {
		t.Fatalf("want 1 layering violation, got %v", v)
	}
	if v[0].From != hGetUser || v[0].To != sSelectUser {
		t.Errorf("violation names edge %s→%s, want %s→%s", v[0].From, v[0].To, hGetUser, sSelectUser)
	}
	if v[0].Summary != "handler → store skips 1 layer(s)" {
		t.Errorf("summary = %q", v[0].Summary)
	}
}

// A skip routed through an UNASSIGNED helper package (handler → codec → store)
// must be caught: the gate's core promise is that a layer cannot be skipped, and
// before the effective-edge logic this bounce evaded it entirely.
func TestLayeringSkipThroughUnassignedPackage(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	const codec = "example.com/layeredsvc/internal/codec.Decode" // not in any declared layer
	g.Nodes = append(g.Nodes, graph.Node{FQN: codec, Sig: "func()", Tier: 3})
	g.Edges = append(g.Edges,
		graph.Edge{From: hGetUser, To: codec, Tier: 3},
		graph.Edge{From: codec, To: sSelectUser, Tier: 2})

	res := Check(layeredPolicy(), graph.NewIndex(g))
	v := res.Violations()
	if len(v) != 1 || v[0].Rule != "layering" {
		t.Fatalf("a skip bounced through an unassigned package must be caught; got %v", res.Findings)
	}
	if v[0].From != hGetUser || v[0].To != sSelectUser {
		t.Errorf("effective edge = %s → %s, want %s → %s", v[0].From, v[0].To, hGetUser, sSelectUser)
	}
}

// The legitimate spine handler → app → store must NOT be flagged: an intermediate
// layer (app) absorbs the descent, so handler does not "effectively" reach store.
func TestLayeringSpineThroughLayerAbsorbed(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json") // base IS handler → app → store
	if res := Check(layeredPolicy(), graph.NewIndex(g)); !res.OK() {
		t.Fatalf("the layered spine must pass; got %v", res.Violations())
	}
}

func TestLayeringUpwardDetected(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	// store calling back up into handler is an upward violation.
	g.Edges = append(g.Edges, graph.Edge{From: sSelectUser, To: hGetUser, Tier: 2})
	res := Check(layeredPolicy(), graph.NewIndex(g))
	v := res.Violations()
	if len(v) != 1 || v[0].Summary != "store → handler calls upward" {
		t.Fatalf("want one upward violation, got %v", v)
	}
}

func TestLayeringAllowListSuppresses(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	g.Edges = append(g.Edges, graph.Edge{From: hGetUser, To: sSelectUser, Tier: 2})
	p := layeredPolicy()
	p.Layering.Allow = []policy.Exception{{From: hGetUser, To: sSelectUser, Reason: "reviewed"}}
	res := Check(p, graph.NewIndex(g))
	if !res.OK() {
		t.Fatalf("allow-listed edge should be suppressed; got %v", res.Violations())
	}
}

func TestMustNotReachProvenAbsentIsSilent(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	p := layeredPolicy()
	p.MustNotReach = []policy.ReachRule{{
		Name: "read-only", From: []string{hGetUser},
		To: []string{"boundary:db INSERT", "boundary:db UPDATE", "boundary:db DELETE"},
	}}
	res := Check(p, graph.NewIndex(g))
	if len(res.Findings) != 0 {
		t.Fatalf("a proven-absent rule must be silent; got %v", res.Findings)
	}
}

// TestReachCharacterization pins every Finding field emitted by must_not_reach
// before its binding, traversal, and blind-frontier facts move to the shared
// facts package. Presentation changes require an explicit compatibility review.
//
// The three "canonical ... within one owner/site" cases are the deliberate
// exceptions and therefore FAIL against the pre-extraction probe: each pins a
// declared post-extraction correction that the shuffle-invariance contract
// required (an intrinsic key where the old probe read producer emission order).
// See their case comments. Every other case is base parity.
func TestReachCharacterization(t *testing.T) {
	const (
		source = "svc.A"
		mid    = "svc.Mid"
		other  = "svc.Other"
		target = "boundary:db UPDATE users"
	)

	baseGraph := func() *graph.Graph {
		return &graph.Graph{
			Nodes: []graph.Node{
				{FQN: source},
				{FQN: mid},
				{FQN: other},
			},
			Edges: []graph.Edge{
				{From: source, To: mid},
				{From: other, To: target, Boundary: "outbound-sync"},
			},
		}
	}
	rule := func() policy.ReachRule {
		return policy.ReachRule{
			Name: "no-write",
			From: []string{source},
			To:   []string{"boundary:db UPDATE"},
		}
	}

	tests := []struct {
		name string
		g    func() *graph.Graph
		rule func() policy.ReachRule
		want []Finding
	}{
		{
			name: "reachable violation",
			g: func() *graph.Graph {
				g := baseGraph()
				g.Edges = append(g.Edges, graph.Edge{
					From: mid, To: target, Boundary: "outbound-sync",
				})
				return g
			},
			rule: rule,
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Violation,
				Summary:  "no-write: svc.A reaches boundary:db UPDATE users",
				From:     source,
				To:       target,
				Detail:   "",
			}},
		},
		{
			name: "function target precedes a nearer boundary target",
			g: func() *graph.Graph {
				return &graph.Graph{
					Nodes: []graph.Node{
						{FQN: source},
						{FQN: mid},
						{FQN: "svc.Target"},
					},
					Edges: []graph.Edge{
						{From: source, To: target, Boundary: "outbound-sync"},
						{From: source, To: mid},
						{From: mid, To: "svc.Target"},
					},
				}
			},
			rule: func() policy.ReachRule {
				r := rule()
				r.To = []string{"boundary:db UPDATE", "svc.Target"}
				return r
			},
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Violation,
				Summary:  "no-write: svc.A reaches svc.Target",
				From:     source,
				To:       "svc.Target",
				Detail:   "",
			}},
		},
		{
			name: "lexicographic reachable target precedes BFS discovery order",
			g: func() *graph.Graph {
				return &graph.Graph{
					Nodes: []graph.Node{
						{FQN: source},
						{FQN: "svc.AParent"},
						{FQN: "svc.ZParent"},
						{FQN: "svc.targets.ATarget"},
						{FQN: "svc.targets.ZTarget"},
					},
					Edges: []graph.Edge{
						{From: source, To: "svc.AParent"},
						{From: "svc.AParent", To: "svc.targets.ZTarget"},
						{From: source, To: "svc.ZParent"},
						{From: "svc.ZParent", To: "svc.targets.ATarget"},
					},
				}
			},
			rule: func() policy.ReachRule {
				r := rule()
				r.To = []string{"svc.targets"}
				return r
			},
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Violation,
				Summary:  "no-write: svc.A reaches svc.targets.ATarget",
				From:     source,
				To:       "svc.targets.ATarget",
				Detail:   "",
			}},
		},
		{
			// Base-parity pin (F7). Two owners carry a matching effect at different
			// BFS depths. Precedence across owners is CONE order — the source, then
			// the reachable functions lexicographically — so svc.ADeep wins even
			// though svc.ZNear is one hop nearer. The labels are chosen so cone
			// order, BFS-level order and a global label sort all disagree: a
			// level-first walk answers "alpha", a global (To, From) sort answers
			// "alpha", only cone order answers "zeta". The cone is sorted, so this
			// precedence is already independent of the graph's input order and
			// nothing about shuffle-invariance licensed changing it.
			name: "cone owner order decides between boundary effects at different depths",
			g: func() *graph.Graph {
				return &graph.Graph{
					Nodes: []graph.Node{
						{FQN: source}, {FQN: "svc.ZNear"}, {FQN: "svc.ADeep"},
					},
					Edges: []graph.Edge{
						{From: source, To: "svc.ZNear"},
						{From: "svc.ZNear", To: "boundary:db UPDATE alpha", Boundary: "outbound-sync"},
						{From: "svc.ZNear", To: "svc.ADeep"},
						{From: "svc.ADeep", To: "boundary:db UPDATE zeta", Boundary: "outbound-sync"},
					},
				}
			},
			rule: rule,
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Violation,
				Summary:  "no-write: svc.A reaches boundary:db UPDATE zeta",
				From:     source,
				To:       "boundary:db UPDATE zeta",
				Detail:   "",
			}},
		},
		{
			// Post-extraction correction, pinned deliberately. graph.Load does not
			// sort the edge list, so the pre-extraction walk named whichever of one
			// owner's matching effects the PRODUCER happened to emit first (zeta
			// here) — the witness moved with input order for a graph that is
			// semantically identical, which the plan's shuffle-invariance
			// requirement forbids. Within one owner the canonical (To, From)
			// minimum is taken instead, so the witness is a pure function of the
			// graph's content. Cross-owner precedence is untouched (see above).
			name: "canonical effect within one owner over reversed declaration order",
			g: func() *graph.Graph {
				return &graph.Graph{
					Nodes: []graph.Node{{FQN: source}, {FQN: mid}},
					Edges: []graph.Edge{
						{From: source, To: mid},
						{From: mid, To: "boundary:db UPDATE zeta", Boundary: "outbound-sync"},
						{From: mid, To: "boundary:db UPDATE alpha", Boundary: "outbound-sync"},
					},
				}
			},
			rule: rule,
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Violation,
				Summary:  "no-write: svc.A reaches boundary:db UPDATE alpha",
				From:     source,
				To:       "boundary:db UPDATE alpha",
				Detail:   "",
			}},
		},
		{
			// Post-extraction correction, pinned deliberately — the same input-order
			// dependence as above, on the blind-spot manifest. graph.Load does not
			// sort BlindSpots, so the pre-extraction probe named the first
			// non-disclosure spot the PRODUCER emitted at the site ("unsafe" here).
			// Selection within one site is now the canonical (Kind, Site, Detail,
			// Location) minimum, matching the precedent the concurrent path already
			// declared. Which SITE is chosen still follows cone order.
			name: "canonical blind spot within one site over adversarial manifest order",
			g: func() *graph.Graph {
				g := baseGraph()
				g.BlindSpots = []graph.BlindSpot{
					{Kind: "unsafe", Site: mid, Detail: "zeta"},
					{Kind: "reflect", Site: mid, Detail: "alpha"},
				}
				return g
			},
			rule: rule,
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Caution,
				Summary:  "no-write: no path found, but the frontier is blind (reflect at svc.Mid) — cannot prove absence",
				From:     source,
				To:       "",
				Detail:   "",
			}},
		},
		{
			// Post-extraction correction, pinned deliberately — the third and last
			// site the shuffle-invariance contract required an intrinsic key at, and
			// the only one that had no case. blindForCone's doc declares it ("the
			// dynamic loop below owns the second"), but nothing enforced it: replacing
			// the dynamic witness selection with the site's MAXIMUM left the whole
			// ./internal/groundwork/... suite green. Two dynamic effects at ONE owner,
			// declared largest-first: the pre-extraction probe returned the first
			// dynamic edge in an unsorted edge list, so the disclosed site moved with
			// producer emission order on a semantically identical graph. WHICH owner is
			// chosen is untouched — that stays cone order, pinned by the case below.
			name: "canonical dynamic effect within one owner over reversed declaration order",
			g: func() *graph.Graph {
				g := baseGraph()
				g.Edges = append(g.Edges,
					graph.Edge{From: mid, To: "boundary:db Exec <dynamic>", Boundary: "outbound-sync"},
					graph.Edge{From: mid, To: "boundary:db Call <dynamic>", Boundary: "outbound-sync"},
				)
				return g
			},
			rule: rule,
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Caution,
				Summary:  "no-write: no path found, but the frontier is blind (unresolved boundary effect boundary:db Call <dynamic>) — cannot prove absence",
				From:     source,
				To:       "",
				Detail:   "",
			}},
		},
		{
			// Base-parity pin (F7/F8). With every function and package site visible,
			// the dynamic-effect fallback picks the FIRST owner in cone order that
			// makes one — the source before any callee — not the globally smallest
			// (Site, Detail). The source sorts LAST here, so a global sort would
			// name svc.AMid's effect instead.
			name: "source dynamic effect precedes a callee's",
			g: func() *graph.Graph {
				return &graph.Graph{
					Nodes: []graph.Node{
						{FQN: "svc.ZSrc"}, {FQN: "svc.AMid"}, {FQN: other},
					},
					Edges: []graph.Edge{
						{From: "svc.ZSrc", To: "svc.AMid"},
						{From: "svc.ZSrc", To: "boundary:db Exec <dynamic>", Boundary: "outbound-sync"},
						{From: "svc.AMid", To: "boundary:db Scan <dynamic>", Boundary: "outbound-sync"},
						{From: other, To: target, Boundary: "outbound-sync"},
					},
				}
			},
			rule: func() policy.ReachRule {
				r := rule()
				r.From = []string{"svc.ZSrc"}
				return r
			},
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Caution,
				Summary:  "no-write: no path found, but the frontier is blind (unresolved boundary effect boundary:db Exec <dynamic>) — cannot prove absence",
				From:     "svc.ZSrc",
				To:       "",
				Detail:   "",
			}},
		},
		{
			name: "proven absence",
			g:    baseGraph,
			rule: rule,
			want: nil,
		},
		{
			name: "blind frontier caution",
			g: func() *graph.Graph {
				g := baseGraph()
				g.BlindSpots = []graph.BlindSpot{{
					Kind: "reflect", Site: mid, Detail: "opaque dispatch",
				}}
				return g
			},
			rule: rule,
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Caution,
				Summary:  "no-write: no path found, but the frontier is blind (reflect at svc.Mid) — cannot prove absence",
				From:     source,
				To:       "",
				Detail:   "",
			}},
		},
		{
			name: "lexicographic cone function chooses blind evidence before BFS distance",
			g: func() *graph.Graph {
				return &graph.Graph{
					Nodes: []graph.Node{
						{FQN: source},
						{FQN: "svc.ZNear"},
						{FQN: "svc.ADeep"},
						{FQN: other},
					},
					Edges: []graph.Edge{
						{From: source, To: "svc.ZNear"},
						{From: "svc.ZNear", To: "svc.ADeep"},
						{From: other, To: target, Boundary: "outbound-sync"},
					},
					BlindSpots: []graph.BlindSpot{
						{Kind: "unsafe", Site: "svc.ZNear", Detail: "near"},
						{Kind: "reflect", Site: "svc.ADeep", Detail: "deep"},
					},
				}
			},
			rule: rule,
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Caution,
				Summary:  "no-write: no path found, but the frontier is blind (reflect at svc.ADeep) — cannot prove absence",
				From:     source,
				To:       "",
				Detail:   "",
			}},
		},
		{
			name: "require proof blind frontier violation",
			g: func() *graph.Graph {
				g := baseGraph()
				g.BlindSpots = []graph.BlindSpot{{
					Kind: "reflect", Site: mid, Detail: "opaque dispatch",
				}}
				return g
			},
			rule: func() policy.ReachRule {
				r := rule()
				r.RequireProof = true
				return r
			},
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Violation,
				Summary:  "no-write: no path found, but the frontier is blind (reflect at svc.Mid) — require_proof is set and absence cannot be proven",
				From:     source,
				To:       "",
				Detail:   "",
			}},
		},
		{
			name: "unbound source",
			g:    baseGraph,
			rule: func() policy.ReachRule {
				r := rule()
				r.From = []string{"svc.Missing"}
				return r
			},
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Caution,
				Summary:  "no-write: from binds nothing in this graph — inert rule",
				From:     "",
				To:       "",
				Detail:   "",
			}},
		},
		{
			name: "unbound target",
			g:    baseGraph,
			rule: func() policy.ReachRule {
				r := rule()
				r.To = []string{"boundary:db DELETE"}
				return r
			},
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Caution,
				Summary:  "no-write: to binds nothing in this graph — name a first-party sink it can bind, or this invariant is vacuous",
				From:     "",
				To:       "",
				Detail:   "",
			}},
		},
		{
			name: "entrypoint star",
			g: func() *graph.Graph {
				return &graph.Graph{
					Nodes: []graph.Node{{FQN: source}, {FQN: mid}},
					Edges: []graph.Edge{{From: source, To: mid}},
				}
			},
			rule: func() policy.ReachRule {
				return policy.ReachRule{
					Name: "entrypoint-no-mid",
					From: []string{policy.EntrypointSelector},
					To:   []string{mid},
				}
			},
			want: []Finding{{
				Rule:     "must_not_reach",
				Severity: Violation,
				Summary:  "entrypoint-no-mid: svc.A reaches svc.Mid",
				From:     source,
				To:       mid,
				Detail:   "",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &policy.Policy{
				Service: "svc", Version: 1,
				MustNotReach: []policy.ReachRule{tt.rule()},
			}
			got := Check(p, graph.NewIndex(tt.g())).Findings
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("findings changed\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

func TestMustNotReachReachableViolation(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	p := layeredPolicy()
	p.MustNotReach = []policy.ReachRule{{
		Name: "update-must-not-write", From: []string{hUpdateUser},
		To: []string{"boundary:db UPDATE"},
	}}
	res := Check(p, graph.NewIndex(g))
	v := res.Violations()
	if len(v) != 1 || v[0].Rule != "must_not_reach" {
		t.Fatalf("want a reachable violation, got %v", res.Findings)
	}
	if v[0].From != hUpdateUser || v[0].To != "boundary:db UPDATE users" {
		t.Errorf("violation = %s → %s", v[0].From, v[0].To)
	}
}

// TestMustNotReachBoundaryEmptyFieldStillBinds pins H-5: an edge whose To
// carries the boundary: prefix but whose Boundary FIELD is empty must still be
// recognized as a bindable target by the same predicate evalReach walks on
// (IsBoundary(), the To-prefix). Before the fix bindsAnyTarget keyed on
// `Boundary != ""` and short-circuited the rule to a "binds nothing" caution,
// silently masking the real reachable violation. (graph.Load would now reject
// this shape; the in-memory graph exercises the fitness predicate directly.)
func TestMustNotReachBoundaryEmptyFieldStillBinds(t *testing.T) {
	const from = "(*example.com/svc/internal/handler.Server).Purge"
	g := &graph.Graph{
		Nodes: []graph.Node{{FQN: from, Sig: "func()", Tier: 1}},
		Edges: []graph.Edge{
			// boundary: To prefix, but the Boundary field was left empty.
			{From: from, To: "boundary:db DELETE ledger", Tier: 1},
		},
	}
	p := &policy.Policy{Service: "svc", Version: 1, MustNotReach: []policy.ReachRule{{
		Name: "no-delete", From: []string{from}, To: []string{"boundary:db DELETE"},
	}}}
	res := Check(p, graph.NewIndex(g))
	v := res.Violations()
	if len(v) != 1 || v[0].Rule != "must_not_reach" {
		t.Fatalf("want a reachable must_not_reach violation, got findings %v", res.Findings)
	}
	for _, c := range res.Cautions() {
		if c.Rule == "must_not_reach" && strings.Contains(c.Summary, "binds nothing") {
			t.Fatalf("target was masked as unbindable: %s", c.Summary)
		}
	}
}

func TestMustNotReachNoPathFoundIsCaution(t *testing.T) {
	g := loadGraph(t, "blindsvc.graph.json")
	p := &policy.Policy{
		Service: "blindsvc", Version: 1,
		MustNotReach: []policy.ReachRule{{
			Name: "create-no-dynamic", From: []string{"(*example.com/blindsvc/internal/handler.Server).Create"},
			To: []string{"boundary:bus PUBLISH <dynamic>"},
		}},
	}
	res := Check(p, graph.NewIndex(g))
	if !res.OK() {
		t.Fatalf("a no-path-found caution must not fail the gate; got %v", res.Violations())
	}
	c := res.Cautions()
	if len(c) != 1 || c[0].Rule != "must_not_reach" {
		t.Fatalf("want one caution, got %v", res.Findings)
	}
}

func TestMustNotReachReachableThroughDynamic(t *testing.T) {
	g := loadGraph(t, "blindsvc.graph.json")
	p := &policy.Policy{
		Service: "blindsvc", Version: 1,
		MustNotReach: []policy.ReachRule{{
			Name: "publish-no-dynamic", From: []string{"(*example.com/blindsvc/internal/handler.Server).Publish"},
			To: []string{"boundary:bus PUBLISH <dynamic>"},
		}},
	}
	res := Check(p, graph.NewIndex(g))
	v := res.Violations()
	if len(v) != 1 || v[0].To != "boundary:bus PUBLISH <dynamic>" {
		t.Fatalf("want a reachable-through-dynamic violation, got %v", res.Findings)
	}
}

// TestExternalBoundaryCallIsDisclosureOnly pins the consumer half of EBC's
// contract: an ExternalBoundaryCall on the reachable cone must NOT blind a
// must_not_reach proof, while a genuinely-blinding kind (reflect) at the same site
// must. The external callee is the same out-of-module leaf the index already stops
// at, so it hides no in-scope path — flagging it must not silently turn a PROVEN
// into a CANT-PROVE. The two halves of the assertion share one graph so the only
// variable is the blind-spot kind.
func TestExternalBoundaryCallIsDisclosureOnly(t *testing.T) {
	// svc.From -> svc.Mid only; the forbidden effect hangs off svc.Other, so it
	// EXISTS in the graph (the target binds) but is unreachable from svc.From. The
	// blind spot sits on svc.Mid, on From's cone — so it governs whether the no-path
	// result is a proof or an abstention.
	graphWith := func(kind string) *graph.Graph {
		return &graph.Graph{
			Algo: "rta",
			Nodes: []graph.Node{
				{FQN: "svc.From", Tier: 1}, {FQN: "svc.Mid", Tier: 2}, {FQN: "svc.Other", Tier: 1},
			},
			Edges: []graph.Edge{
				{From: "svc.From", To: "svc.Mid"},
				{From: "svc.Other", To: "boundary:bus PUBLISH topic", Boundary: "outbound-async"},
			},
			BlindSpots: []graph.BlindSpot{{
				Kind: kind, Site: "svc.Mid",
				Detail: "handoff at the leaf on the reachable cone",
			}},
		}
	}
	p := &policy.Policy{
		Service: "svc", Version: 1,
		MustNotReach: []policy.ReachRule{{
			Name: "from-no-reach", From: []string{"svc.From"},
			To: []string{"boundary:bus PUBLISH topic"},
		}},
	}
	mustNotReachCautions := func(g *graph.Graph) int {
		res := Check(p, graph.NewIndex(g))
		n := 0
		for _, c := range res.Cautions() {
			if c.Rule == "must_not_reach" {
				n++
			}
		}
		return n
	}

	// ExternalBoundaryCall on the cone: no path AND the frontier is not blind, so
	// the rule is provenAbsent — a real proof, emitting no caution.
	if n := mustNotReachCautions(graphWith(string(blindspots.ExternalBoundaryCall))); n != 0 {
		t.Fatalf("ExternalBoundaryCall must be disclosure-only: want 0 must_not_reach cautions, got %d", n)
	}
	// reflect on the SAME cone genuinely blinds: no path, but the frontier is blind,
	// so the rule is noPathFound — a caution. This proves the carve-out is the cause,
	// not an unreachable target.
	if n := mustNotReachCautions(graphWith(string(blindspots.Reflect))); n != 1 {
		t.Fatalf("a blinding kind (reflect) on the cone must abstain: want 1 must_not_reach caution, got %d", n)
	}
}

func TestIOBudget(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	ix := graph.NewIndex(g)

	// Budget of 2 passes: the UpdateUser route does exactly two writes.
	p := layeredPolicy()
	p.IOBudget = &policy.IOBudget{MaxWritesPerRoute: 2}
	if res := Check(p, ix); !res.OK() {
		t.Fatalf("budget 2 should pass; got %v", res.Violations())
	}

	// Budget of 1 fails on the UpdateUser route.
	p.IOBudget = &policy.IOBudget{MaxWritesPerRoute: 1}
	res := Check(p, ix)
	v := res.Violations()
	if len(v) != 1 || v[0].Rule != "io_budget" || v[0].From != hUpdateUser {
		t.Fatalf("want one io_budget violation on the UpdateUser route, got %v", res.Findings)
	}
}

// A route whose DB mutations flow through a wrapper (non-constant SQL, labeled
// "db call") writes ZERO classified writes, so it trivially passes any budget —
// but that pass is not a proof the write surface is bounded. The budget check
// discloses it as a caution (advisory, exit 0), never a silent green (F1).
func TestIOBudgetCautionsOnUnclassifiedDB(t *testing.T) {
	const route = "(*example.com/svc/internal/handler.Server).CreateThing"
	const store = "(*example.com/svc/internal/store.Store).Insert"
	g := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: route, Sig: "func()", Tier: 1},
			{FQN: store, Sig: "func() error", Tier: 1},
		},
		Edges: []graph.Edge{
			{From: route, To: store, Tier: 2},
			// The store mutates, but the labeler could not read the verb.
			{From: store, To: "boundary:db call", Tier: 1, Boundary: "outbound-sync"},
		},
	}
	p := &policy.Policy{Service: "svc", Version: 1, IOBudget: &policy.IOBudget{MaxWritesPerRoute: 0}}
	res := Check(p, graph.NewIndex(g))

	if !res.OK() {
		t.Fatalf("an unclassified-DB caution must not fail the gate; got %v", res.Violations())
	}
	var found bool
	for _, c := range res.Cautions() {
		if c.Rule == "io_budget" && strings.Contains(c.Summary, "unenforceable") && strings.Contains(c.Summary, "db call") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want an io_budget unenforceable caution naming `db call`, got %v", res.Findings)
	}
}

// A route whose forward cone touches a blind frontier (here a <dynamic> bus
// publish, the shape an oapi-codegen dispatch seam produces) has a write count
// that is only a lower bound — the budget discloses it as such, the half of F1
// that fires when forward reach is starved rather than mislabeled.
func TestIOBudgetCautionsOnBlindFrontier(t *testing.T) {
	const route = "(*example.com/svc/internal/handler.Server).Publish"
	const fan = "(*example.com/svc/internal/app.Bus).Fanout"
	g := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: route, Sig: "func()", Tier: 1},
			{FQN: fan, Sig: "func() error", Tier: 1},
		},
		Edges: []graph.Edge{
			{From: route, To: fan, Tier: 2},
			{From: fan, To: "boundary:bus PUBLISH <dynamic>", Tier: 1, Boundary: "outbound-async"},
		},
	}
	p := &policy.Policy{Service: "svc", Version: 1, IOBudget: &policy.IOBudget{MaxWritesPerRoute: 2}}
	res := Check(p, graph.NewIndex(g))
	if !res.OK() {
		t.Fatalf("a blind-frontier caution must not fail the gate; got %v", res.Violations())
	}
	var found bool
	for _, c := range res.Cautions() {
		if c.Rule == "io_budget" && strings.Contains(c.Summary, "lower bound") && strings.Contains(c.Summary, "blind") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want an io_budget lower-bound caution naming the blind frontier, got %v", res.Findings)
	}
}

// A graph whose every DB write is classified (db INSERT/UPDATE/...) produces no
// unenforceable caution — the disclosure fires only where the labeler is blind.
func TestIOBudgetNoCautionWhenClassified(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	p := layeredPolicy()
	p.IOBudget = &policy.IOBudget{MaxWritesPerRoute: 2}
	res := Check(p, graph.NewIndex(g))
	for _, c := range res.Cautions() {
		if c.Rule == "io_budget" {
			t.Fatalf("classified DB writes must not raise an io_budget caution; got %q", c.Summary)
		}
	}
}

// UnclassifiedDBLabel buckets only labels that MIGHT be an unproven write.
// Driver/transaction control (Ping*, Begin*, Commit, Rollback), statement prep
// and connection/stat methods (Prepare*, Conn, Stats), and pool/session setters
// (Set*) reach the DB but cannot mutate a row, so they are excluded; "db call",
// ExecContext, and Query* stay in (a Postgres INSERT…RETURNING rides
// QueryContext, so a Query* might mutate). This is the R2 bucket narrowing.
func TestUnclassifiedDBLabelExcludesNonMutating(t *testing.T) {
	cases := map[string]bool{
		"boundary:db call":              true,  // opaque write path — kept
		"boundary:db ExecContext":       true,  // exec of non-constant SQL — kept
		"boundary:db QueryContext":      true,  // may be INSERT … RETURNING — kept
		"boundary:db QueryRowContext":   true,  // may be INSERT … RETURNING — kept
		"boundary:db PingContext":       false, // readiness probe — excluded
		"boundary:db BeginTx":           false, // transaction control — excluded
		"boundary:db Commit":            false, // transaction control — excluded
		"boundary:db Rollback":          false, // transaction control — excluded
		"boundary:db Prepare":           false, // statement prep, no row mutation — excluded
		"boundary:db PrepareContext":    false, // statement prep, no row mutation — excluded
		"boundary:db Conn":              false, // connection acquisition — excluded
		"boundary:db Stats":             false, // pool statistics — excluded
		"boundary:db SetMaxOpenConns":   false, // pool config — excluded
		"boundary:db Settle":            true,  // mutating method, not a Set* pool setter — must not be swallowed by a bare SET prefix
		"boundary:db INSERT ledger":     false, // classified write, not unclassified
		"boundary:db SELECT applicants": false, // classified read, not unclassified
		"boundary:bus PUBLISH topic":    false, // not a DB effect
	}
	for to, want := range cases {
		if _, got := UnclassifiedDBLabel(graph.Edge{To: to, Boundary: "outbound-sync"}); got != want {
			t.Errorf("UnclassifiedDBLabel(%q) = %v, want %v", to, got, want)
		}
	}
}

// A service with a genuine opaque "db call" write route plus a health probe
// (db PingContext) and a read (db QueryRowContext) raises the unenforceable
// caution for exactly ONE route — the opaque write — not the probe or the read.
// Pre-R2 the probe and read were bucketed too, over-firing the caution 3×.
func TestIOBudgetUnenforceableExcludesProbesAndReads(t *testing.T) {
	const (
		writeRoute  = "(*example.com/svc/internal/inbound.Handler).Handle"
		writeStore  = "(*example.com/svc/internal/store.Store).Save"
		healthRoute = "(*example.com/svc/internal/server.Server).Healthcheck"
		readRoute   = "(*example.com/svc/internal/store.Store).GetEntity"
	)
	g := &graph.Graph{
		Nodes: []graph.Node{
			{FQN: writeRoute, Sig: "func()", Tier: 1},
			{FQN: writeStore, Sig: "func() error", Tier: 1},
			{FQN: healthRoute, Sig: "func()", Tier: 1},
			{FQN: readRoute, Sig: "func() error", Tier: 1},
		},
		Edges: []graph.Edge{
			{From: writeRoute, To: writeStore, Tier: 2},
			{From: writeStore, To: "boundary:db call", Tier: 1, Boundary: "outbound-sync"},
			{From: healthRoute, To: "boundary:db PingContext", Tier: 1, Boundary: "outbound-sync"},
			{From: readRoute, To: "boundary:db QueryRowContext", Tier: 1, Boundary: "outbound-sync"},
		},
	}
	p := &policy.Policy{Service: "svc", Version: 1, IOBudget: &policy.IOBudget{MaxWritesPerRoute: 1}}
	routes := RouteWrites(p, graph.NewIndex(g))

	hasUnclassified := func(route string) bool { return len(routes[route].Unclassified) > 0 }
	if !hasUnclassified(writeRoute) {
		t.Errorf("the opaque `db call` write route must remain unenforceable")
	}
	if hasUnclassified(healthRoute) {
		t.Errorf("a db PingContext health probe must not be flagged unenforceable: %v", routes[healthRoute].Unclassified)
	}
	// QueryRowContext stays in the bucket on purpose (INSERT … RETURNING rides it).
	if !hasUnclassified(readRoute) {
		t.Errorf("a db QueryRowContext route must stay in the bucket (may be INSERT … RETURNING)")
	}
}

// The composition root (main) is an entrypoint but not a route; its startup
// writes must not be charged against the per-route I/O budget.
func TestIOBudgetExcludesCompositionRoot(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	// main performs three startup writes — more than any route.
	for _, tbl := range []string{"a", "b", "c"} {
		g.Edges = append(g.Edges, graph.Edge{
			From: svcMain, To: "boundary:db INSERT " + tbl, Tier: 1, Boundary: "outbound-sync",
		})
	}
	p := layeredPolicy()
	p.IOBudget = &policy.IOBudget{MaxWritesPerRoute: 2} // routes do ≤2; main does 3
	res := Check(p, graph.NewIndex(g))
	if !res.OK() {
		t.Fatalf("the composition root must be exempt from io_budget; got %v", res.Violations())
	}
}

// require_proof escalates an unprovable must_not_reach rule from caution to
// violation — fail-closed for high-stakes safety invariants.
func TestMustNotReachRequireProof(t *testing.T) {
	g := loadGraph(t, "blindsvc.graph.json")
	rule := policy.ReachRule{
		Name: "create-no-dynamic", From: []string{blindsvcStart},
		To: []string{"boundary:bus PUBLISH <dynamic>"},
	}

	// Default: an unprovable rule is a non-blocking caution.
	p := &policy.Policy{Service: "blindsvc", Version: 1, MustNotReach: []policy.ReachRule{rule}}
	if res := Check(p, graph.NewIndex(g)); !res.OK() {
		t.Fatalf("without require_proof the rule must remain a caution; got %v", res.Violations())
	}

	// With require_proof: the same unprovable rule blocks.
	rule.RequireProof = true
	p.MustNotReach = []policy.ReachRule{rule}
	res := Check(p, graph.NewIndex(g))
	v := res.Violations()
	if len(v) != 1 || v[0].Rule != "must_not_reach" {
		t.Fatalf("require_proof must turn the unprovable rule into a violation; got %v", res.Findings)
	}
}

func TestIsWrite(t *testing.T) {
	cases := map[string]bool{
		"boundary:db SELECT users":          false,
		"boundary:db INSERT audit_log":      true,
		"boundary:db UPDATE users":          true,
		"boundary:bus PUBLISH user.created": true,
		"boundary:bus PUBLISH <dynamic>":    true,
		"boundary:bus CONSUME payment":      false,
		"boundary:peer GET /score/{id}":     false,
		"boundary:peer POST /charge/{id}":   true,
	}
	for to, want := range cases {
		if got := IsWrite(graph.Edge{To: to, Boundary: "outbound-sync"}); got != want {
			t.Errorf("IsWrite(%q) = %v, want %v", to, got, want)
		}
	}
}

func TestCheckDeterministic(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	g.Edges = append(g.Edges, graph.Edge{From: hGetUser, To: sSelectUser, Tier: 2})
	p := layeredPolicy()
	p.IOBudget = &policy.IOBudget{MaxWritesPerRoute: 1}
	a := Check(p, graph.NewIndex(g))
	b := Check(p, graph.NewIndex(g))
	if len(a.Findings) != len(b.Findings) {
		t.Fatalf("non-deterministic finding count: %d vs %d", len(a.Findings), len(b.Findings))
	}
	for i := range a.Findings {
		if a.Findings[i] != b.Findings[i] {
			t.Errorf("finding %d differs: %v vs %v", i, a.Findings[i], b.Findings[i])
		}
	}
}
