package fitness

import (
	"fmt"
	"sort"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
	"github.com/jyang234/golang-code-graph/internal/groundwork/policy"
)

// ExceptionStatus is one active suppression and whether it still suppresses
// anything. Allow-lists accumulate; unaudited, the framework's honesty
// migrates into an unreviewed exception graveyard. A DEAD entry matches
// nothing in the current graph — the violation it once excused is gone, and
// the entry should be deleted before it silently excuses something new.
type ExceptionStatus struct {
	Source string `json:"source"` // "layering", "must_pass_through:<rule>", "blind_spot_ratchet"
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Site   string `json:"site,omitempty"`
	Reason string `json:"reason,omitempty"`
	Dead   bool   `json:"dead,omitempty"`
}

// Exceptions audits every allow-list in the policy against one graph. For the
// finding-suppressing lists (layering, must_pass_through) liveness is decided
// differentially: re-run the checks with the single entry removed — if no new
// finding appears, the entry suppresses nothing here. A blind-spot exception
// is live while the graph still carries a matching blind spot. Deterministic
// and read-only; it informs review, it does not gate.
func Exceptions(p *policy.Policy, ix *graph.Index) []ExceptionStatus {
	baseline := len(Check(p, ix).Findings)
	var out []ExceptionStatus

	if p.Layering != nil {
		for i, a := range p.Layering.Allow {
			p2 := *p
			lay := *p.Layering
			lay.Allow = dropException(p.Layering.Allow, i)
			p2.Layering = &lay
			out = append(out, ExceptionStatus{
				Source: "layering", From: a.From, To: a.To, Reason: a.Reason,
				Dead: len(Check(&p2, ix).Findings) == baseline,
			})
		}
	}

	for ri := range p.MustPassThrough {
		rule := &p.MustPassThrough[ri]
		for i, a := range rule.Allow {
			p2 := *p
			rules := append([]policy.PassRule{}, p.MustPassThrough...)
			rules[ri].Allow = dropException(rule.Allow, i)
			p2.MustPassThrough = rules
			out = append(out, ExceptionStatus{
				Source: "must_pass_through:" + rule.Name, From: a.From, To: a.To, Reason: a.Reason,
				Dead: len(Check(&p2, ix).Findings) == baseline,
			})
		}
	}

	if p.BlindSpotRatchet != nil {
		for _, a := range p.BlindSpotRatchet.Allow {
			live := false
			for _, b := range ix.BlindSpots() {
				one := &policy.BlindSpotRatchet{Allow: []policy.BlindSpotException{a}}
				if one.Allows(b.Kind, b.Site) {
					live = true
					break
				}
			}
			out = append(out, ExceptionStatus{
				Source: "blind_spot_ratchet", Kind: a.Kind, Site: a.Site, Reason: a.Reason,
				Dead: !live,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Site < b.Site
	})
	return out
}

func dropException(xs []policy.Exception, i int) []policy.Exception {
	out := make([]policy.Exception, 0, len(xs)-1)
	out = append(out, xs[:i]...)
	return append(out, xs[i+1:]...)
}

// DeadCount tallies the dead entries — the number the audit wants at zero.
func DeadCount(xs []ExceptionStatus) int {
	n := 0
	for _, x := range xs {
		if x.Dead {
			n++
		}
	}
	return n
}

// String renders one entry for the audit listing.
func (x ExceptionStatus) String() string {
	state := "LIVE"
	if x.Dead {
		state = "DEAD"
	}
	what := x.Site
	if x.From != "" || x.To != "" {
		what = x.From + " → " + x.To
	}
	if x.Kind != "" {
		what = x.Kind + " " + what
	}
	s := fmt.Sprintf("[%s] %s — %s", state, x.Source, what)
	if x.Reason != "" {
		s += " (" + x.Reason + ")"
	}
	return s
}
