package glob

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"*ledger#Post", "example.com/loansvc/internal/ledger#Post", true}, // '*' crosses '/'
		{"*health#Ping", "svc/health#Ping", true},
		{"*ledger*", "a/b/ledger/c#X", true},
		{"*decisioning#Evaluate", "x/decisioning#Evaluate", true},
		{"exact", "exact", true},
		{"exact", "exacts", false},
		{"a*b*c", "a__b__c", true},
		{"a*b*c", "axbyc", true},
		{"a*b*c", "abc", true},
		{"a*b*c", "ac", false},
		{"*", "anything/at#all", true},
		{"", "", true},
		{"", "x", false},
		{"no*star", "nostar", true},
		{"*#Post", "p/q#Post", true},
		{"*#Post", "p/q#Get", false},
		{"a*", "a", true},
		{"*z", "z", true},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.s); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}
