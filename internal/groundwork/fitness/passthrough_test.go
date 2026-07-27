package fitness

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// must_pass_through (GX-1): every path from a From source to a To target must
// pass through a Through waypoint. In layeredsvc the app layer is the waypoint:
// every entrypoint-to-DB path legitimately runs handler → app.Service → store.

const (
	appService   = "(*example.com/layeredsvc/internal/app.Service)"
	hGetUserFast = "(*example.com/layeredsvc/internal/handler.Server).GetUserFast"
	v2Export     = "(*example.com/layeredsvc/internal/handlerv2.Server).Export"
)

func passPolicy(rule policy.PassRule) *policy.Policy {
	return &policy.Policy{Service: "layeredsvc", Version: 1, MustPassThrough: []policy.PassRule{rule}}
}

func appGuardsDB() policy.PassRule {
	return policy.PassRule{
		Name:    "app-guards-db",
		From:    []string{policy.EntrypointSelector},
		To:      []string{"boundary:db"},
		Through: []string{appService},
	}
}

func TestPassThroughGuardedBase(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	res := Check(passPolicy(appGuardsDB()), graph.NewIndex(g))
	if len(res.Findings) != 0 {
		t.Fatalf("clean base should be guarded (proven, no caution); got %v", res.Findings)
	}
}

func TestPassThroughBypassDetected(t *testing.T) {
	// The skip fixture wires a handler straight to the store — the same edge the
	// layering check catches, seen here as an unguarded path to the DB.
	g := loadGraph(t, "layeredsvc.branch-skip.graph.json")
	res := Check(passPolicy(appGuardsDB()), graph.NewIndex(g))

	v := res.Violations()
	if len(v) != 1 || v[0].Rule != "must_pass_through" {
		t.Fatalf("want 1 must_pass_through violation, got %v", res.Findings)
	}
	if v[0].From != hGetUserFast || v[0].To != "boundary:db SELECT users" {
		t.Errorf("violation = %s → %s, want %s → boundary:db SELECT users", v[0].From, v[0].To, hGetUserFast)
	}
	if !strings.Contains(v[0].Detail, "store.Store.SelectUser") || !strings.Contains(v[0].Detail, "boundary:db SELECT users") {
		t.Errorf("detail = %q, want the witness path through SelectUser to the effect", v[0].Detail)
	}
}

// The defining property of the entrypoint:* selector: a brand-new handler
// package with an unguarded route fires the rule with NO policy change. An
// FQN-glob From (naming the existing handler package) could not do this.
func TestPassThroughNewHandlerPackageStillBound(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	g.Nodes = append(g.Nodes, graph.Node{FQN: v2Export, Sig: "func()", Tier: 1})
	g.Edges = append(g.Edges, graph.Edge{From: v2Export, To: sSelectUser, Tier: 2})

	res := Check(passPolicy(appGuardsDB()), graph.NewIndex(g))
	v := res.Violations()
	if len(v) != 1 || v[0].From != v2Export {
		t.Fatalf("new handler package must be bound by entrypoint:*; got %v", res.Findings)
	}
}

func TestPassThroughAllowSuppressesExactPair(t *testing.T) {
	// Two bypasses; one allow-listed. Exactly the other must fire.
	g := loadGraph(t, "layeredsvc.branch-skip.graph.json")
	g.Nodes = append(g.Nodes, graph.Node{FQN: v2Export, Sig: "func()", Tier: 1})
	g.Edges = append(g.Edges, graph.Edge{From: v2Export, To: sSelectUser, Tier: 2})

	rule := appGuardsDB()
	rule.Allow = []policy.Exception{{From: hGetUserFast, Reason: "read-only fast path, reviewed"}}
	res := Check(passPolicy(rule), graph.NewIndex(g))

	v := res.Violations()
	if len(v) != 1 || v[0].From != v2Export {
		t.Fatalf("allow must suppress exactly the listed pair; got %v", res.Findings)
	}
}

func TestPassThroughSourceIsWaypoint(t *testing.T) {
	// A source that itself matches Through is trivially guarded.
	g := loadGraph(t, "layeredsvc.graph.json")
	rule := policy.PassRule{
		Name:    "handler-is-its-own-guard",
		From:    []string{hGetUser},
		To:      []string{"boundary:db"},
		Through: []string{hGetUser},
	}
	res := Check(passPolicy(rule), graph.NewIndex(g))
	if len(res.Findings) != 0 {
		t.Fatalf("source matching through must be trivially guarded; got %v", res.Findings)
	}
}

func TestPassThroughBlindFrontierCaution(t *testing.T) {
	// blindsvc, from Publish: the bound target user.created is unreached from
	// Publish (only Create publishes it), but Publish's cone holds a reflect
	// site (encode.Marshal), so a hidden edge could skirt the waypoint —
	// "guarded" is unprovable. The To binds (Create publishes it), so this is
	// the genuine blind-frontier caution, not the unbindable-target case.
	g := loadGraph(t, "blindsvc.graph.json")
	rule := policy.PassRule{
		Name:    "publish-guarded",
		From:    []string{"(*example.com/blindsvc/internal/handler.Server).Publish"},
		To:      []string{"boundary:bus PUBLISH user.created"},
		Through: []string{"(*example.com/blindsvc/internal/notify.Notifier).Created"},
	}
	res := Check(passPolicy(rule), graph.NewIndex(g))
	c := res.Cautions()
	if len(res.Violations()) != 0 || len(c) != 1 {
		t.Fatalf("want exactly one caution on the blind frontier, got %v", res.Findings)
	}
	if !strings.Contains(c[0].Summary, "cannot prove every path is guarded") {
		t.Errorf("caution summary = %q", c[0].Summary)
	}

	rule.RequireProof = true
	res = Check(passPolicy(rule), graph.NewIndex(g))
	if v := res.Violations(); len(v) != 1 || !strings.Contains(v[0].Summary, "require_proof") {
		t.Fatalf("require_proof must escalate the blind frontier to a violation; got %v", res.Findings)
	}
}

// The unbindable-target fix on the must_pass_through side: a To matching no
// node and no effect anywhere (blindsvc has no DELETE) is disclosed as
// vacuous, not silently held — the field's cgate trust gap, mirrored.
func TestPassThroughUnbindableTo(t *testing.T) {
	g := loadGraph(t, "blindsvc.graph.json")
	rule := policy.PassRule{
		Name:    "nothing-deletes-unguarded",
		From:    []string{policy.EntrypointSelector},
		To:      []string{"boundary:db DELETE"},
		Through: []string{"example.com/blindsvc/internal/audit.Check"},
	}
	res := Check(passPolicy(rule), graph.NewIndex(g))
	c := res.Cautions()
	if len(res.Violations()) != 0 || len(c) != 1 || !strings.Contains(c[0].Summary, "to binds nothing") {
		t.Fatalf("an unbindable To must be a disclosed caution, got %v", res.Findings)
	}

	rule.RequireProof = true
	res = Check(passPolicy(rule), graph.NewIndex(g))
	if v := res.Violations(); len(v) != 1 || !strings.Contains(v[0].Summary, "require_proof") {
		t.Fatalf("require_proof must escalate an unbindable To to a violation, got %v", res.Findings)
	}
}

func TestPassThroughDeterministic(t *testing.T) {
	g := loadGraph(t, "layeredsvc.branch-skip.graph.json")
	p := passPolicy(appGuardsDB())
	a, b := Check(p, graph.NewIndex(g)), Check(p, graph.NewIndex(g))
	if len(a.Findings) != len(b.Findings) {
		t.Fatalf("non-deterministic finding count")
	}
	for i := range a.Findings {
		if a.Findings[i] != b.Findings[i] {
			t.Fatalf("non-deterministic finding %d: %v vs %v", i, a.Findings[i], b.Findings[i])
		}
	}
}

func TestPassThroughCharacterization(t *testing.T) {
	const (
		sourceA  = "svc.ASource"
		sourceZ  = "svc.ZSource"
		target   = "svc.Target"
		guard    = "svc.Guard"
		blind    = "svc.Blind"
		other    = "svc.Other"
		users    = "boundary:db UPDATE users"
		audit    = "boundary:db UPDATE users_audit"
		missing  = "svc.Missing"
		ruleName = "guarded"
		// Two boundary labels whose punctuation ShortName would mangle: it strips
		// everything before the last "/", deletes "*" and ")", and trims a leading
		// "(" — so these would render "{id}" and "count(" if a summary shortened a
		// boundary target the way it shortens an FQN.
		peerCharge = "boundary:peer POST /charge/{id}"
		dbCount    = "boundary:db SELECT count(*)"
	)
	nodes := func(fqns ...string) []graph.Node {
		result := make([]graph.Node, len(fqns))
		for i, fqn := range fqns {
			result[i] = graph.Node{FQN: fqn}
		}
		return result
	}
	pass := func(from, to, through []string) policy.PassRule {
		return policy.PassRule{
			Name: ruleName, From: from, To: to, Through: through,
		}
	}
	// violation spells the expected summary from the case's OWN constants and
	// never recomputes it through ShortName: an expectation derived from the
	// implementation it grades cannot catch a drift in that implementation (the
	// tautological pin that let the boundary-label mangling through). Every
	// constant this helper is called with is a ShortName no-op, so the assembled
	// string is a literal in all but syntax; the boundary-punctuation case below
	// pins the labels ShortName would actually change.
	violation := func(from, to, through, detail string) Finding {
		return Finding{
			Rule:     "must_pass_through",
			Severity: Violation,
			Summary:  ruleName + ": " + from + " reaches " + to + " without passing " + through,
			From:     from,
			To:       to,
			Detail:   detail,
		}
	}

	tests := []struct {
		name string
		g    *graph.Graph
		rule policy.PassRule
		want []Finding
	}{
		{
			name: "function bypass",
			g: &graph.Graph{
				Nodes: nodes(sourceA, target, guard),
				Edges: []graph.Edge{{From: sourceA, To: target}},
			},
			rule: pass([]string{sourceA}, []string{target}, []string{guard}),
			want: []Finding{violation(
				sourceA, target, guard,
				sourceA+" → "+target,
			)},
		},
		{
			name: "direct boundary bypass",
			g: &graph.Graph{
				Nodes: nodes(sourceA, guard),
				Edges: []graph.Edge{{From: sourceA, To: users, Boundary: "outbound-sync"}},
			},
			rule: pass([]string{sourceA}, []string{users}, []string{guard}),
			want: []Finding{violation(
				sourceA, users, guard,
				sourceA+" → "+users,
			)},
		},
		{
			// Base-parity pin (F6). A boundary label is a canonical string a human
			// must read verbatim to act on it — the route template and the SQL
			// statement ARE the evidence. ShortName is an FQN shortener; applied to
			// a label it silently rewrites "boundary:peer POST /charge/{id}" to
			// "{id}" and "boundary:db SELECT count(*)" to "count(". Summary is part
			// of Finding.Key(), so that rewrite also churns the base-vs-branch diff
			// and can reorder Result.sort(). These literals are the bytes the
			// pre-extraction evaluator emitted.
			name: "boundary targets keep their punctuation in the summary",
			g: &graph.Graph{
				Nodes: nodes(sourceA, guard),
				Edges: []graph.Edge{
					{From: sourceA, To: peerCharge, Boundary: "outbound-sync"},
					{From: sourceA, To: dbCount, Boundary: "outbound-sync"},
				},
			},
			rule: pass(
				[]string{sourceA},
				[]string{"boundary:db", "boundary:peer"},
				[]string{guard},
			),
			want: []Finding{
				{
					Rule:     "must_pass_through",
					Severity: Violation,
					Summary:  "guarded: svc.ASource reaches boundary:db SELECT count(*) without passing svc.Guard",
					From:     "svc.ASource",
					To:       "boundary:db SELECT count(*)",
					Detail:   "svc.ASource → boundary:db SELECT count(*)",
				},
				{
					Rule:     "must_pass_through",
					Severity: Violation,
					Summary:  "guarded: svc.ASource reaches boundary:peer POST /charge/{id} without passing svc.Guard",
					From:     "svc.ASource",
					To:       "boundary:peer POST /charge/{id}",
					Detail:   "svc.ASource → boundary:peer POST /charge/{id}",
				},
			},
		},
		{
			name: "multiple bypass pairs",
			g: &graph.Graph{
				Nodes: nodes(sourceZ, target, guard, sourceA),
				Edges: []graph.Edge{
					{From: sourceZ, To: audit, Boundary: "outbound-sync"},
					{From: sourceA, To: target},
					{From: sourceA, To: users, Boundary: "outbound-sync"},
				},
			},
			rule: pass(
				[]string{sourceZ, sourceA},
				[]string{target, "boundary:db UPDATE"},
				[]string{guard},
			),
			want: []Finding{
				violation(sourceA, users, guard, sourceA+" → "+users),
				violation(sourceA, target, guard, sourceA+" → "+target),
				violation(sourceZ, audit, guard, sourceZ+" → "+audit),
			},
		},
		{
			name: "multiple boundary paths preserve finding multiplicity",
			g: &graph.Graph{
				Nodes: nodes(sourceA, "svc.Left", "svc.Right", guard),
				Edges: []graph.Edge{
					{From: sourceA, To: "svc.Right"},
					{From: "svc.Right", To: users, Boundary: "outbound-sync"},
					{From: sourceA, To: "svc.Left"},
					{From: "svc.Left", To: users, Boundary: "outbound-sync"},
				},
			},
			rule: pass([]string{sourceA}, []string{users}, []string{guard}),
			want: []Finding{
				violation(sourceA, users, guard, sourceA+" → svc.Left → "+users),
				violation(sourceA, users, guard, sourceA+" → svc.Right → "+users),
			},
		},
		{
			name: "boundary occurrences preserve owner order before path length",
			g: &graph.Graph{
				Nodes: nodes(sourceA, "svc.AMid", "svc.AOwner", "svc.ZOwner", guard),
				Edges: []graph.Edge{
					{From: sourceA, To: "svc.ZOwner"},
					{From: "svc.ZOwner", To: users, Boundary: "outbound-sync"},
					{From: sourceA, To: "svc.AMid"},
					{From: "svc.AMid", To: "svc.AOwner"},
					{From: "svc.AOwner", To: users, Boundary: "outbound-sync"},
				},
			},
			rule: pass([]string{sourceA}, []string{users}, []string{guard}),
			want: []Finding{
				violation(sourceA, users, guard, sourceA+" → svc.AMid → svc.AOwner → "+users),
				violation(sourceA, users, guard, sourceA+" → svc.ZOwner → "+users),
			},
		},
		{
			name: "allow suppresses only its boundary pair",
			g: &graph.Graph{
				Nodes: nodes(sourceA, guard),
				Edges: []graph.Edge{
					{From: sourceA, To: audit, Boundary: "outbound-sync"},
					{From: sourceA, To: users, Boundary: "outbound-sync"},
				},
			},
			rule: func() policy.PassRule {
				rule := pass(
					[]string{sourceA},
					[]string{"boundary:db UPDATE"},
					[]string{guard},
				)
				rule.Allow = []policy.Exception{{From: sourceA, To: users}}
				return rule
			}(),
			want: []Finding{violation(
				sourceA, audit, guard,
				sourceA+" → "+audit,
			)},
		},
		{
			name: "guarded",
			g: &graph.Graph{
				Nodes: nodes(sourceA, guard, target),
				Edges: []graph.Edge{
					{From: sourceA, To: guard},
					{From: guard, To: target},
				},
			},
			rule: pass([]string{sourceA}, []string{target}, []string{guard}),
		},
		{
			name: "source is waypoint",
			g: &graph.Graph{
				Nodes: nodes(sourceA),
				Edges: []graph.Edge{{From: sourceA, To: users, Boundary: "outbound-sync"}},
			},
			rule: pass([]string{sourceA}, []string{users}, []string{sourceA}),
		},
		{
			name: "blind frontier cautions",
			g: &graph.Graph{
				Nodes: nodes(sourceA, blind, target, guard),
				Edges: []graph.Edge{{From: sourceA, To: blind}},
				BlindSpots: []graph.BlindSpot{{
					Kind: "reflect", Site: blind, Detail: "opaque dispatch",
				}},
			},
			rule: pass([]string{sourceA}, []string{target}, []string{guard}),
			want: []Finding{{
				Rule:     "must_pass_through",
				Severity: Caution,
				Summary:  ruleName + ": no bypass found, but the frontier is blind (reflect at " + blind + ") — cannot prove every path is guarded",
				From:     sourceA,
			}},
		},
		{
			name: "blind frontier require proof violates",
			g: &graph.Graph{
				Nodes: nodes(sourceA, blind, target, guard),
				Edges: []graph.Edge{{From: sourceA, To: blind}},
				BlindSpots: []graph.BlindSpot{{
					Kind: "reflect", Site: blind, Detail: "opaque dispatch",
				}},
			},
			rule: func() policy.PassRule {
				rule := pass([]string{sourceA}, []string{target}, []string{guard})
				rule.RequireProof = true
				return rule
			}(),
			want: []Finding{{
				Rule:     "must_pass_through",
				Severity: Violation,
				Summary:  ruleName + ": no bypass found, but the frontier is blind (reflect at " + blind + ") — require_proof is set and guarding cannot be proven",
				From:     sourceA,
			}},
		},
		{
			name: "blind witness preserves sorted cone precedence",
			g: &graph.Graph{
				Nodes: nodes(sourceZ, blind, target, guard),
				Edges: []graph.Edge{{From: sourceZ, To: blind}},
				BlindSpots: []graph.BlindSpot{
					{Kind: "unsafe", Site: sourceZ, Detail: "source blind"},
					{Kind: "reflect", Site: blind, Detail: "reachable blind"},
				},
			},
			rule: pass([]string{sourceZ}, []string{target}, []string{guard}),
			want: []Finding{{
				Rule:     "must_pass_through",
				Severity: Caution,
				Summary:  ruleName + ": no bypass found, but the frontier is blind (reflect at " + blind + ") — cannot prove every path is guarded",
				From:     sourceZ,
			}},
		},
		{
			name: "unbound from cautions before target",
			g:    &graph.Graph{Nodes: nodes(target, guard)},
			rule: pass([]string{missing}, []string{target}, []string{guard}),
			want: []Finding{{
				Rule:     "must_pass_through",
				Severity: Caution,
				Summary:  ruleName + ": from binds nothing in this graph — inert rule",
			}},
		},
		{
			name: "unbound from require proof violates",
			g:    &graph.Graph{Nodes: nodes(target, guard)},
			rule: func() policy.PassRule {
				rule := pass([]string{missing}, []string{target}, []string{guard})
				rule.RequireProof = true
				return rule
			}(),
			want: []Finding{{
				Rule:     "must_pass_through",
				Severity: Violation,
				Summary:  ruleName + ": from binds nothing in this graph — require_proof is set and an inert rule guards nothing",
			}},
		},
		{
			name: "unbound to cautions",
			g:    &graph.Graph{Nodes: nodes(sourceA, guard)},
			rule: pass([]string{sourceA}, []string{missing}, []string{guard}),
			want: []Finding{{
				Rule:     "must_pass_through",
				Severity: Caution,
				Summary:  ruleName + ": to binds nothing in this graph — name a first-party sink it can bind, or this invariant is vacuous",
			}},
		},
		{
			name: "unbound to require proof violates",
			g:    &graph.Graph{Nodes: nodes(sourceA, guard)},
			rule: func() policy.PassRule {
				rule := pass([]string{sourceA}, []string{missing}, []string{guard})
				rule.RequireProof = true
				return rule
			}(),
			want: []Finding{{
				Rule:     "must_pass_through",
				Severity: Violation,
				Summary:  ruleName + ": to binds nothing in this graph — require_proof is set and an unbindable target cannot be proven absent",
			}},
		},
		{
			// The parity pin for the per-selector Unbound* disclosure: standing
			// fitness grades a From family that binds through ANY of its selectors,
			// so a rule naming one live and one dead source still reports the live
			// source's bypass. Reading a per-selector dead set as the inert-rule
			// trigger would silently drop this real violation.
			name: "partially bound from still reports the live source's bypass",
			g: &graph.Graph{
				Nodes: nodes(sourceA, target, guard),
				Edges: []graph.Edge{{From: sourceA, To: target}},
			},
			rule: pass([]string{sourceA, missing}, []string{target}, []string{guard}),
			want: []Finding{violation(
				sourceA, target, guard,
				sourceA+" → "+target,
			)},
		},
		{
			// The same parity for the To side: a target family with one live and one
			// dead selector is still graded, not disclosed as an unbindable target.
			name: "partially bound to still reports the live target's bypass",
			g: &graph.Graph{
				Nodes: nodes(sourceA, target, guard),
				Edges: []graph.Edge{{From: sourceA, To: target}},
			},
			rule: pass([]string{sourceA}, []string{target, missing}, []string{guard}),
			want: []Finding{violation(
				sourceA, target, guard,
				sourceA+" → "+target,
			)},
		},
		{
			name: "dead waypoint with path bypasses",
			g: &graph.Graph{
				Nodes: nodes(sourceA, target),
				Edges: []graph.Edge{{From: sourceA, To: target}},
			},
			rule: pass([]string{sourceA}, []string{target}, []string{missing}),
			want: []Finding{violation(
				sourceA, target, missing,
				sourceA+" → "+target,
			)},
		},
		{
			name: "dead waypoint without path is silent",
			g: &graph.Graph{
				Nodes: nodes(sourceA, target, other),
				Edges: []graph.Edge{{From: other, To: target}},
			},
			rule: pass([]string{sourceA}, []string{target}, []string{missing}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Check(passPolicy(tt.rule), graph.NewIndex(tt.g)).Findings
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("pass-through findings mismatch\nwant: %#v\ngot:  %#v", tt.want, got)
			}
		})
	}
}

// RF-1: entrypoint:* is one selector language for every From position. In a
// must_not_reach rule it expands to the graph sources instead of silently
// matching nothing (which read as proof of absence).
func TestEntrypointSelectorInMustNotReach(t *testing.T) {
	g := loadGraph(t, "layeredsvc.graph.json")
	p := &policy.Policy{Service: "layeredsvc", Version: 1, MustNotReach: []policy.ReachRule{{
		Name: "no-entrypoint-reads",
		From: []string{policy.EntrypointSelector},
		To:   []string{"boundary:db SELECT"},
	}}}
	res := Check(p, graph.NewIndex(g))
	if v := res.Violations(); len(v) != 1 || v[0].Rule != "must_not_reach" {
		t.Fatalf("entrypoint:* must bind in must_not_reach (GetUser reaches SELECT users); got %v", res.Findings)
	}

	// And on a blind graph with require_proof, unprovability fails closed
	// instead of silently passing over an empty from-set.
	blind := loadGraph(t, "blindsvc.graph.json")
	p.MustNotReach[0].To = []string{"boundary:db DELETE"}
	p.MustNotReach[0].RequireProof = true
	res = Check(p, graph.NewIndex(blind))
	if v := res.Violations(); len(v) != 1 {
		t.Fatalf("require_proof over a blind frontier must fail closed; got %v", res.Findings)
	}
}
