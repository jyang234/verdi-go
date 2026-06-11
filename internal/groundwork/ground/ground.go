// Package ground assembles the pre-edit grounding card (GX-5): everything an
// agent (or human) should know BEFORE touching a function — its identity and
// neighborhood, the external effects it can reach, the rules that demonstrably
// bind it, and the blind spots touching any claim on the card. Deterministic
// prevention is strictly cheaper than deterministic rejection: the same rules
// that gate the merge are surfaced at generation time, so the edit loop
// becomes ground → edit → verify with one rule set at both ends.
//
// The card never promises a guardrail that does not bind: binding rules are
// derived with the same matchers the checks use (fitness.MatchesAny, the
// graph-carried obligation and effect-order facts), and a card whose claims
// cross a blind spot says so.
package ground

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jyang234/golang-code-graph/internal/groundwork/fitness"
	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
	"github.com/jyang234/golang-code-graph/internal/groundwork/setutil"
)

// Card is the grounding evidence for one function. Pure function of
// (graph, policy, fqn); every list is sorted.
type Card struct {
	FQN         string            `json:"fqn"`
	Sig         string            `json:"sig,omitempty"`
	Tier        int               `json:"tier,omitempty"`
	Layer       string            `json:"layer,omitempty"` // the policy layer owning its package
	Callers     []string          `json:"callers,omitempty"`
	Callees     []string          `json:"callees,omitempty"`
	Entrypoints []string          `json:"entrypoints,omitempty"` // cover: what it is live behind
	Effects     []string          `json:"effects,omitempty"`     // reachable boundary effects
	Binding     []string          `json:"binding_rules,omitempty"`
	BlindSpots  []graph.BlindSpot `json:"blind_spots,omitempty"`
}

// For assembles the card. The policy may be nil (graph-only grounding); the
// binding-rules section then carries only the graph-borne facts (obligations,
// effect order).
func For(ix *graph.Index, p *policy.Policy, fqn string) (Card, error) {
	node, ok := ix.Node(fqn)
	if !ok {
		return Card{}, fmt.Errorf("no function %q in graph", fqn)
	}
	c := Card{FQN: fqn, Sig: node.Sig, Tier: node.Tier}
	c.Callers = append([]string{}, ix.Callers(fqn)...)
	c.Callees = append([]string{}, ix.Callees(fqn)...)
	c.Entrypoints = ix.EntrypointCover(fqn)

	cone := append([]string{fqn}, ix.Reachable(fqn)...)
	effects := map[string]bool{}
	for _, e := range ix.Effects(cone...) {
		effects[e.To] = true
	}
	c.Effects = setutil.SortedKeys(effects)

	c.Binding = bindingRules(ix, p, fqn, &c)

	seen := map[string]bool{}
	for _, fn := range cone {
		for _, b := range append(ix.BlindSpotsAt(fn), ix.BlindSpotsAt(fitness.PkgOf(fn))...) {
			k := b.Kind + "\x00" + b.Site
			if !seen[k] {
				seen[k] = true
				c.BlindSpots = append(c.BlindSpots, b)
			}
		}
	}
	sort.Slice(c.BlindSpots, func(i, j int) bool {
		if c.BlindSpots[i].Kind != c.BlindSpots[j].Kind {
			return c.BlindSpots[i].Kind < c.BlindSpots[j].Kind
		}
		return c.BlindSpots[i].Site < c.BlindSpots[j].Site
	})
	return c, nil
}

// bindingRules names the rules that demonstrably bind fqn, with the same
// matchers the checks themselves use.
func bindingRules(ix *graph.Index, p *policy.Policy, fqn string, c *Card) []string {
	var out []string

	// Graph-borne facts bind regardless of policy.
	for _, o := range ix.Obligations() {
		if o.Fn == fqn {
			out = append(out, fmt.Sprintf("obligation %s (%s): %s at %s", o.Rule, o.Kind, o.Status, o.Site))
		}
	}
	for _, f := range ix.EffectOrder() {
		if f.Fn == fqn {
			mode := "can precede"
			if f.Always {
				mode = "always precedes"
			}
			out = append(out, fmt.Sprintf("partial-effect: %s %s the fallible call to %s", f.Effect, mode, fitness.ShortName(f.Callee)))
		}
	}

	if p != nil {
		if layer := p.LayerOf(fitness.PkgOf(fqn)); layer != "" {
			c.Layer = layer
			out = append(out, fmt.Sprintf("layering: package is in layer %q — calls may descend one layer, never skip or rise", layer))
		}
		for _, r := range p.MustNotReach {
			if fitness.MatchesAny(fqn, r.From) {
				out = append(out, fmt.Sprintf("must_not_reach %s: this function must never reach %s", r.Name, strings.Join(r.To, ", ")))
			}
		}
		for _, r := range p.MustPassThrough {
			if fitness.MatchesAny(fqn, r.Through) {
				out = append(out, fmt.Sprintf("must_pass_through %s: this function is the waypoint guarding %s", r.Name, strings.Join(r.To, ", ")))
			}
		}
		for _, r := range p.NoConcurrentReach {
			if fitness.MatchesAny(fqn, r.To) {
				out = append(out, fmt.Sprintf("no_concurrent_reach %s: must not be reached on a concurrent path", r.Name))
			}
		}
		if p.IOBudget != nil && len(ix.Callers(fqn)) == 0 {
			out = append(out, fmt.Sprintf("io_budget: routes may reach at most %d external write(s)", p.IOBudget.MaxWritesPerRoute))
		}
	}
	sort.Strings(out)
	return out
}

// Render is the human/agent-facing text card.
func (c Card) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", c.FQN)
	if c.Sig != "" {
		fmt.Fprintf(&b, "  %s\n", c.Sig)
	}
	if c.Layer != "" {
		fmt.Fprintf(&b, "  tier %d · layer %s\n", c.Tier, c.Layer)
	} else {
		fmt.Fprintf(&b, "  tier %d\n", c.Tier)
	}
	b.WriteString("\n")
	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s (%d)\n", title, len(items))
		for _, it := range items {
			fmt.Fprintf(&b, "- %s\n", it)
		}
		b.WriteString("\n")
	}
	section("Binding rules — these gate any edit here", c.Binding)
	section("Callers", c.Callers)
	section("Callees", c.Callees)
	section("Live behind entrypoints", c.Entrypoints)
	section("Reachable boundary effects", c.Effects)
	if len(c.BlindSpots) > 0 {
		fmt.Fprintf(&b, "🕳️  Blind spots touching this card's claims (%d)\n", len(c.BlindSpots))
		for _, s := range c.BlindSpots {
			fmt.Fprintf(&b, "- %s %s\n", s.Kind, s.Site)
		}
	}
	return b.String()
}
