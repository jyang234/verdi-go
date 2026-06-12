package policy

import "testing"

// PC-3: an effect-ratchet allow entry with an empty target would prefix-match
// every write label — one entry silently disabling the whole ratchet, the
// inert-guardrail failure mode the load validator exists to catch.
func TestEffectRatchetEmptyTargetRejected(t *testing.T) {
	p := &Policy{Service: "svc", Version: 1, EffectRatchet: &EffectRatchet{
		Allow: []EffectException{{Target: "  ", Reason: "oops"}},
	}}
	if err := p.Validate(); err == nil {
		t.Fatal("an empty effect_ratchet allow target must fail validation")
	}
}

func TestEffectRatchetAllows(t *testing.T) {
	r := &EffectRatchet{Allow: []EffectException{{Target: "db INSERT audit"}}}
	cases := map[string]bool{
		"db INSERT audit_log": true,  // prefix
		"db INSERT audit":     true,  // exact
		"db INSERT other":     false, //
		"db DELETE audit":     false, //
	}
	for label, want := range cases {
		if got := r.Allows(label); got != want {
			t.Errorf("Allows(%q) = %v, want %v", label, got, want)
		}
	}
	var nilRatchet *EffectRatchet
	if nilRatchet.Allows("db INSERT x") {
		t.Error("a nil ratchet must allow nothing")
	}
}
