package fitness

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/graph"
)

// A method-named outbound effect's (blob/cache/rpc) write-ness is the callee method
// name, which the budget does NOT read as a verb (a method-name heuristic could be
// silently wrong). So IsWrite must return false for it — it is never counted as a
// write — and UnclassifiedEffectLabel must return true, so the budget discloses it
// as an unenforceable write frontier instead of passing it silently.
func TestMethodNamedOutboundWriteness(t *testing.T) {
	cases := []struct {
		to           string
		isWrite      bool
		isUnclassEff bool
	}{
		{"boundary:blob PutObject", false, true},
		{"boundary:blob GetObject", false, true},
		{"boundary:blob Delete", false, true}, // a method NAMED Delete must not be read as an HTTP DELETE write
		{"boundary:cache Set", false, true},
		{"boundary:cache Get", false, true},
		{"boundary:rpc Charge", false, true},
		{"boundary:db INSERT users", true, false},
		{"boundary:bus PUBLISH user.created", true, false},
		{"boundary:checkout POST /charge", true, false}, // HTTP label is "<peer> <METHOD> <route>"
		{"boundary:checkout GET /status", false, false},
	}
	for _, c := range cases {
		e := graph.Edge{To: c.to, Boundary: "outbound-sync"}
		if got := IsWrite(e); got != c.isWrite {
			t.Errorf("IsWrite(%q) = %v, want %v", c.to, got, c.isWrite)
		}
		if _, got := UnclassifiedEffectLabel(e); got != c.isUnclassEff {
			t.Errorf("UnclassifiedEffectLabel(%q) ok = %v, want %v", c.to, got, c.isUnclassEff)
		}
	}
}
