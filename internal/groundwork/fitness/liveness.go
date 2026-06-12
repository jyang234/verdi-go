package fitness

import (
	"fmt"
	"sort"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// PatternLiveness is one rule pattern's binding state against one graph — the
// rule-side half of the suppression audit. The checks themselves disclose a
// whole-From going inert as a Caution, but that surface is delta-diffed in
// review: a rule that was BORN inert cautions identically on base and branch
// and is suppressed by the new-findings filter forever. This listing is
// absolute — no base graph, no diff — so birth defects and steady-state rot
// stay visible.
//
// Info marks a To-side pattern, where deadness is ambiguous by design: a To
// that matches nothing may be success (the forbidden thing does not exist) or
// rot (the target renamed and the rule went vacuous). The audit states the
// fact and the reviewer judges; only From/Through deadness is an unambiguous
// defect (Through less so — a dead waypoint usually fails loud as bypasses,
// but is silent when no From→To path exists either).
type PatternLiveness struct {
	Source  string `json:"source"` // "must_not_reach:<rule>", "must_pass_through:<rule>", "no_concurrent_reach:<rule>"
	Field   string `json:"field"`  // "from", "through", "to"
	Pattern string `json:"pattern"`
	Dead    bool   `json:"dead,omitempty"`
	Info    bool   `json:"info,omitempty"` // To-side: dead may be success, not rot
}

// Liveness audits every pattern of every pattern-bearing rule against one
// graph. A From/Through pattern is live iff it binds at least one node (or is
// the entrypoint selector with at least one source); a To pattern is live iff
// it matches a node or a boundary effect label. Deterministic and read-only;
// like Exceptions, it informs review, it does not gate.
func Liveness(p *policy.Policy, ix *graph.Index) []PatternLiveness {
	// Rule-independent facts, hoisted once (the RF-7 discipline): the sorted
	// node list and the deduped boundary labels do not change per pattern, so
	// neither the Nodes() sort nor the edge scan is repeated inside the loops.
	nodes := ix.Nodes()
	var boundary []string
	seen := map[string]bool{}
	for _, e := range ix.Edges() {
		if e.IsBoundary() && !seen[e.To] {
			seen[e.To] = true
			boundary = append(boundary, e.To)
		}
	}

	nodeLive := func(pat string) bool {
		if pat == policy.EntrypointSelector {
			return len(ix.Sources()) > 0
		}
		for _, fqn := range nodes {
			if matchAny(fqn, []string{pat}) {
				return true
			}
		}
		return false
	}
	targetLive := func(pat string) bool {
		if nodeLive(pat) {
			return true
		}
		for _, l := range boundary {
			if matchAny(l, []string{pat}) {
				return true
			}
		}
		return false
	}

	var out []PatternLiveness
	add := func(source, field string, pats []string, live func(string) bool, info bool) {
		for _, pat := range pats {
			out = append(out, PatternLiveness{
				Source: source, Field: field, Pattern: pat,
				Dead: !live(pat), Info: info,
			})
		}
	}
	for _, r := range p.MustNotReach {
		add("must_not_reach:"+r.Name, "from", r.From, nodeLive, false)
		add("must_not_reach:"+r.Name, "to", r.To, targetLive, true)
	}
	for i := range p.MustPassThrough {
		r := &p.MustPassThrough[i]
		add("must_pass_through:"+r.Name, "from", r.From, nodeLive, false)
		add("must_pass_through:"+r.Name, "through", r.Through, nodeLive, false)
		add("must_pass_through:"+r.Name, "to", r.To, targetLive, true)
	}
	for _, r := range p.NoConcurrentReach {
		add("no_concurrent_reach:"+r.Name, "to", r.To, targetLive, true)
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		return a.Pattern < b.Pattern
	})
	return out
}

// DeadPatternCount tallies the unambiguously-dead patterns (From/Through; the
// To-side INFO entries are excluded) — the number the audit wants at zero.
func DeadPatternCount(ls []PatternLiveness) int {
	n := 0
	for _, l := range ls {
		if l.Dead && !l.Info {
			n++
		}
	}
	return n
}

// String renders one entry for the audit listing.
func (l PatternLiveness) String() string {
	state := "LIVE"
	switch {
	case l.Dead && l.Info:
		state = "INFO"
	case l.Dead:
		state = "DEAD"
	}
	s := fmt.Sprintf("[%s] %s %s %q", state, l.Source, l.Field, l.Pattern)
	if l.Dead && l.Info {
		s += " — matches nothing (may be success: the forbidden thing is absent — or a renamed target)"
	} else if l.Dead {
		s += " — binds nothing"
	}
	return s
}
