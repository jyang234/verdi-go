package review

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// PC-3 effect ratchet: no new boundary write target base→branch without an
// allow-list entry. Observe-only by default; merge-blocking when the policy
// sets effect_ratchet.gate — the blind-spot ratchet's lifecycle exactly.

func withEffectRatchet(t *testing.T, r *policy.EffectRatchet) *policy.Policy {
	t.Helper()
	p := loadPolicy(t)
	p.EffectRatchet = r
	return p
}

func branchWithWrite(t *testing.T, from, label string) *graph.Graph {
	t.Helper()
	g := loadGraph(t, "layeredsvc.graph.json")
	g.Edges = append(g.Edges, graph.Edge{From: from, To: "boundary:" + label, Tier: 1, Boundary: "outbound-sync"})
	return g
}

// The write is a bus publish: the fixture policy's must_not_reach rule guards
// GetUser against DB mutations, and these tests want the Block attributable to
// the ratchet alone, not to a coincidental reach violation.
func TestGateBlocksOnNewWriteTarget(t *testing.T) {
	p := withEffectRatchet(t, &policy.EffectRatchet{Gate: true})
	base := loadGraph(t, "layeredsvc.graph.json")
	branch := branchWithWrite(t, aGetProfile, "bus PUBLISH audit.event")

	a := Review(p, base, branch)
	if len(a.NewViolations) != 0 {
		t.Fatalf("unexpected violations %v — the Block must come from the ratchet alone", a.NewViolations)
	}
	if a.Verdict != Block {
		t.Fatalf("review verdict = %s, want BLOCK with gate: true", a.Verdict)
	}
	g := Gate(p, base, branch, nil)
	if g.Pass {
		t.Fatal("gate passed despite a gated new write target")
	}
	if len(g.NewWriteTargets) != 1 || g.NewWriteTargets[0] != "bus PUBLISH audit.event" {
		t.Fatalf("gate write targets = %v, want [bus PUBLISH audit.event]", g.NewWriteTargets)
	}
}

func TestEffectAllowSuppressesExactlyThatTarget(t *testing.T) {
	p := withEffectRatchet(t, &policy.EffectRatchet{
		Gate:  true,
		Allow: []policy.EffectException{{Target: "bus PUBLISH audit.event", Reason: "Q3 audit feature"}},
	})
	base := loadGraph(t, "layeredsvc.graph.json")
	branch := branchWithWrite(t, aGetProfile, "bus PUBLISH audit.event")
	branch.Edges = append(branch.Edges, graph.Edge{
		From: aGetProfile, To: "boundary:bus PUBLISH other.event", Tier: 1, Boundary: "outbound-async",
	})

	a := Review(p, base, branch)
	if len(a.NewWriteTargets) != 1 || a.NewWriteTargets[0] != "bus PUBLISH other.event" {
		t.Fatalf("new write targets = %v, want only the unallowed one", a.NewWriteTargets)
	}
	if g := Gate(p, base, branch, nil); g.Pass {
		t.Fatal("gate passed despite an unallowed new write target")
	}
}

func TestNewReadIsNotAWriteTarget(t *testing.T) {
	p := withEffectRatchet(t, &policy.EffectRatchet{Gate: true})
	base := loadGraph(t, "layeredsvc.graph.json")
	branch := branchWithWrite(t, aGetProfile, "db SELECT log")

	a := Review(p, base, branch)
	if len(a.NewWriteTargets) != 0 {
		t.Fatalf("new write targets = %v; reads are out of the ratchet's scope (a named v1 limitation)", a.NewWriteTargets)
	}
	if a.Verdict == Block {
		t.Error("a new read must not trip the write-surface gate")
	}
}

// A new dynamic write label is a finding like any other.
func TestNewDynamicWriteLabelIsATarget(t *testing.T) {
	p := withEffectRatchet(t, &policy.EffectRatchet{Gate: true})
	base := loadGraph(t, "layeredsvc.graph.json")
	branch := loadGraph(t, "layeredsvc.graph.json")
	branch.Edges = append(branch.Edges, graph.Edge{
		From: aGetProfile, To: "boundary:bus PUBLISH <dynamic>", Tier: 1, Boundary: "outbound-async",
	})

	a := Review(p, base, branch)
	if len(a.NewWriteTargets) != 1 || a.NewWriteTargets[0] != "bus PUBLISH <dynamic>" {
		t.Fatalf("new write targets = %v, want the dynamic publish", a.NewWriteTargets)
	}
}

// The documented GX-2 dependency, as a test instead of a comment: laundering a
// new write through an EXISTING dynamic label produces no new effect label —
// the effect ratchet cannot see it — but the new dispatch site is a new blind
// spot, and the blind-spot ratchet is what fires. Gating PC-3 without gating
// GX-2 leaves this escape open.
func TestDynamicLaunderingCaughtByBlindSpotRatchet(t *testing.T) {
	const busPub = "example.com/layeredsvc/internal/bus.Publish"
	withBus := func() *graph.Graph {
		g := loadGraph(t, "layeredsvc.graph.json")
		g.Nodes = append(g.Nodes, graph.Node{FQN: busPub, Sig: "func()", Tier: 2})
		g.Edges = append(g.Edges,
			graph.Edge{From: aUpdateProfile, To: busPub, Tier: 2},
			graph.Edge{From: busPub, To: "boundary:bus PUBLISH <dynamic>", Tier: 1, Boundary: "outbound-async"})
		return g
	}
	p := withEffectRatchet(t, &policy.EffectRatchet{Gate: true})
	p.BlindSpotRatchet = &policy.BlindSpotRatchet{Gate: true}

	base := withBus()
	branch := withBus()
	branch.Edges = append(branch.Edges, graph.Edge{From: aGetProfile, To: busPub, Tier: 2})
	branch.BlindSpots = append(branch.BlindSpots, graph.BlindSpot{
		Kind: "HighFanOut", Site: aGetProfile, Detail: "dispatch through an interface",
	})

	a := Review(p, base, branch)
	if len(a.NewWriteTargets) != 0 {
		t.Fatalf("new write targets = %v; the laundered write reuses an existing label", a.NewWriteTargets)
	}
	if len(a.NewBlindSpots) != 1 {
		t.Fatalf("new blind spots = %v; the dispatch site is what catches the laundering", a.NewBlindSpots)
	}
	if a.Verdict != Block {
		t.Errorf("verdict = %s, want BLOCK via the blind-spot ratchet", a.Verdict)
	}
}

func TestEffectRatchetDeterministic(t *testing.T) {
	p := withEffectRatchet(t, &policy.EffectRatchet{Gate: true})
	base := loadGraph(t, "layeredsvc.graph.json")
	branch := branchWithWrite(t, aGetProfile, "db INSERT log")
	if a, b := Review(p, base, branch), Review(p, base, branch); a.Digest != b.Digest {
		t.Fatalf("non-deterministic digest: %s vs %s", a.Digest, b.Digest)
	}
}
