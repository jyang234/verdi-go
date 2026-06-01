// Package tiermap is flowmap's single salience classifier. Given the normalized
// features of an operation it returns a tier (1 = most consequential … 4 =
// debug). The same classifier is used three times: static edge tiering, canon
// salience filtering, and diff prioritization — so a logging call is tier 4 and a
// publish is tier 1 whether seen statically or at runtime.
//
// It is pure and deterministic: rules and pins are ordered slices (never maps),
// matching is first-match-wins, and a fixed config plus a given operation always
// yields exactly one tier.
package tiermap

import (
	"fmt"

	"github.com/jyang234/golang-code-graph/internal/config"
	"github.com/jyang234/golang-code-graph/internal/glob"
	"github.com/jyang234/golang-code-graph/internal/model"
)

// Classifier assigns tiers per a config.
type Classifier struct {
	cfg      *config.Config
	builtins []config.Rule
}

// New builds a classifier from cfg (nil => all defaults).
func New(cfg *config.Config) *Classifier {
	if cfg == nil {
		cfg = &config.Config{}
	}
	return &Classifier{cfg: cfg, builtins: BuiltinRules()}
}

// Default returns a zero-config classifier: built-in defaults on, catch-all 3.
func Default() *Classifier { return New(&config.Config{}) }

// Classify returns the tier for f and the identifier of the rule that decided it
// (for explainability). Precedence (tier-map spec §2): pins → user rules →
// built-in defaults → catch-all.
func (c *Classifier) Classify(f model.Features) (tier int, rule string) {
	for i, p := range c.cfg.Pins {
		if p.Identity != "" && glob.Match(p.Identity, f.Identity) {
			return p.Tier, fmt.Sprintf("pin[%d]", i)
		}
	}
	for i, r := range c.cfg.Rules {
		if matches(r.Match, f) {
			return r.Tier, name("rule", i, r.Name)
		}
	}
	if c.cfg.UsesDefaults() {
		for _, r := range c.builtins {
			if matches(r.Match, f) {
				return r.Tier, "builtin:" + r.Name
			}
		}
	}
	return c.cfg.CatchAllTier(), "catch-all"
}

func name(prefix string, i int, n string) string {
	if n != "" {
		return prefix + ":" + n
	}
	return fmt.Sprintf("%s[%d]", prefix, i)
}

func matches(m config.MatchSpec, f model.Features) bool {
	if m.Boundary != "" && m.Boundary != string(f.Boundary) {
		return false
	}
	if m.Effect != "" && m.Effect != string(f.Effect) {
		return false
	}
	if m.Origin != "" && m.Origin != string(f.Origin) {
		return false
	}
	if m.Fallible != nil && *m.Fallible != f.Fallible {
		return false
	}
	if m.Concurrent != nil && *m.Concurrent != f.Concurrent {
		return false
	}
	if m.Identity != "" && !glob.Match(m.Identity, f.Identity) {
		return false
	}
	return true
}

// BuiltinRules returns flowmap's default tier rules in first-match order
// (tier-map spec §4). Telemetry is ranked first so a logging call never falls
// through to the first-party rule. Note: effect=read denotes a DB read; an
// outbound call to a peer SERVICE is effect=io and so lands on ext-sync (tier 1),
// matching the example where every external dependency is tier 1 while the DB
// read is tier 2. (Feature extraction is responsible for setting effect=read only
// for DB reads.)
func BuiltinRules() []config.Rule {
	yes := true
	return []config.Rule{
		{Name: "telemetry", Match: config.MatchSpec{Effect: "telemetry"}, Tier: 4},
		{Name: "publish", Match: config.MatchSpec{Boundary: "outbound-async"}, Tier: 1},
		{Name: "inbound", Match: config.MatchSpec{Boundary: "inbound"}, Tier: 1},
		{Name: "mutate", Match: config.MatchSpec{Effect: "mutate"}, Tier: 1},
		{Name: "ext-read", Match: config.MatchSpec{Boundary: "outbound-sync", Effect: "read"}, Tier: 2},
		{Name: "ext-sync", Match: config.MatchSpec{Boundary: "outbound-sync"}, Tier: 1},
		{Name: "xpkg-fallible", Match: config.MatchSpec{Boundary: "cross-package", Origin: "first-party", Fallible: &yes}, Tier: 2},
		{Name: "first-party", Match: config.MatchSpec{Origin: "first-party"}, Tier: 3},
		{Name: "stdlib", Match: config.MatchSpec{Origin: "stdlib"}, Tier: 4},
	}
}
