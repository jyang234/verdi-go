package tiermap

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/config"
	"github.com/jyang234/golang-code-graph/internal/model"
)

func ptr[T any](v T) *T { return &v }

// One case per built-in default rule (tier-map spec §4), under zero config.
func TestBuiltinDefaults(t *testing.T) {
	c := Default()
	cases := []struct {
		name string
		f    model.Features
		tier int
		rule string
	}{
		{"telemetry-beats-first-party",
			model.Features{Effect: model.EffectTelemetry, Origin: model.OriginFirstParty}, 4, "builtin:telemetry"},
		{"publish",
			model.Features{Boundary: model.BoundaryOutboundAsync}, 1, "builtin:publish"},
		{"inbound-entry",
			model.Features{Boundary: model.BoundaryInbound}, 1, "builtin:inbound"},
		{"db-write-mutate",
			model.Features{Boundary: model.BoundaryOutboundSync, Effect: model.EffectMutate}, 1, "builtin:mutate"},
		{"db-read",
			model.Features{Boundary: model.BoundaryOutboundSync, Effect: model.EffectRead}, 2, "builtin:ext-read"},
		{"service-call",
			model.Features{Boundary: model.BoundaryOutboundSync, Effect: model.EffectIO}, 1, "builtin:ext-sync"},
		{"xpkg-first-party-fallible",
			model.Features{Boundary: model.BoundaryCrossPackage, Origin: model.OriginFirstParty, Fallible: true}, 2, "builtin:xpkg-fallible"},
		{"first-party-internal",
			model.Features{Boundary: model.BoundaryInternal, Origin: model.OriginFirstParty}, 3, "builtin:first-party"},
		{"stdlib",
			model.Features{Boundary: model.BoundaryInternal, Origin: model.OriginStdlib}, 4, "builtin:stdlib"},
		{"catch-all-unknown",
			model.Features{Origin: model.OriginThirdParty}, 3, "catch-all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, rule := c.Classify(tc.f)
			if tier != tc.tier || rule != tc.rule {
				t.Errorf("Classify(%+v) = (%d, %q), want (%d, %q)", tc.f, tier, rule, tc.tier, tc.rule)
			}
		})
	}
}

func TestPinsEscalateAndDemote(t *testing.T) {
	c := New(&config.Config{Pins: []config.Pin{
		{Identity: "*decisioning#Evaluate", Tier: 1}, // escalate an internal fn
		{Identity: "*health#Ping", Tier: 4},          // demote a noisy probe
	}})
	// Would be tier 3 (first-party internal) without the pin.
	if tier, _ := c.Classify(model.Features{Identity: "x/decisioning#Evaluate", Boundary: model.BoundaryInternal, Origin: model.OriginFirstParty}); tier != 1 {
		t.Errorf("escalate pin: got %d, want 1", tier)
	}
	// Would be tier 1 (inbound) without the pin.
	if tier, _ := c.Classify(model.Features{Identity: "svc/health#Ping", Boundary: model.BoundaryInbound}); tier != 4 {
		t.Errorf("demote pin: got %d, want 4", tier)
	}
}

func TestUserRuleOverridesDefault(t *testing.T) {
	// A user rule that demotes all first-party calls to tier 2 wins over the
	// built-in first-party→3, because user rules run before built-ins.
	c := New(&config.Config{Rules: []config.Rule{
		{Name: "fp2", Match: config.MatchSpec{Origin: "first-party"}, Tier: 2},
	}})
	if tier, rule := c.Classify(model.Features{Boundary: model.BoundaryInternal, Origin: model.OriginFirstParty}); tier != 2 || rule != "rule:fp2" {
		t.Errorf("got (%d,%q), want (2,\"rule:fp2\")", tier, rule)
	}
}

func TestUseDefaultsFalse(t *testing.T) {
	c := New(&config.Config{UseDefaults: ptr(false)})
	// Without built-ins and no user rules, everything falls to catch-all.
	if tier, rule := c.Classify(model.Features{Effect: model.EffectTelemetry}); tier != 3 || rule != "catch-all" {
		t.Errorf("got (%d,%q), want (3,\"catch-all\")", tier, rule)
	}
}

func TestShadowingFirstMatchWins(t *testing.T) {
	// Broad rule placed before the narrow one hides it (first-match).
	c := New(&config.Config{
		UseDefaults: ptr(false),
		Rules: []config.Rule{
			{Name: "broad", Match: config.MatchSpec{Origin: "first-party"}, Tier: 3},
			{Name: "narrow", Match: config.MatchSpec{Origin: "first-party", Effect: "mutate"}, Tier: 1},
		},
	})
	if tier, rule := c.Classify(model.Features{Origin: model.OriginFirstParty, Effect: model.EffectMutate}); tier != 3 || rule != "rule:broad" {
		t.Errorf("shadowing: got (%d,%q), want (3,\"rule:broad\")", tier, rule)
	}
}

func TestGlobPin(t *testing.T) {
	c := New(&config.Config{Pins: []config.Pin{{Identity: "*ledger*", Tier: 1}}})
	if tier, _ := c.Classify(model.Features{Identity: "a/b/ledger/c#Post", Origin: model.OriginFirstParty}); tier != 1 {
		t.Errorf("glob pin did not match")
	}
}

func TestDeterministic(t *testing.T) {
	c := Default()
	f := model.Features{Boundary: model.BoundaryOutboundSync, Effect: model.EffectRead, Origin: model.OriginThirdParty}
	first, _ := c.Classify(f)
	for i := 0; i < 1000; i++ {
		if tier, _ := c.Classify(f); tier != first {
			t.Fatalf("non-deterministic at %d: %d != %d", i, tier, first)
		}
	}
}

func TestBuiltinRuleOrderFrozen(t *testing.T) {
	want := []string{"telemetry", "publish", "inbound", "mutate", "ext-read", "ext-sync", "xpkg-fallible", "first-party", "stdlib"}
	got := BuiltinRules()
	if len(got) != len(want) {
		t.Fatalf("builtin rule count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Errorf("builtin[%d] = %q, want %q", i, got[i].Name, want[i])
		}
	}
}
