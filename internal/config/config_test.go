package config

import "testing"

func TestLoadExample(t *testing.T) {
	// The .flowmap.yaml from the worked example (artifacts §8): mostly defaults.
	const y = `
version: 1
classify:
  telemetry:  ["go.uber.org/zap"]
  busPublish: ["github.com/koalafi/eventbus#Publish"]
  busConsume: ["github.com/koalafi/eventbus#Subscribe"]
  db:         ["github.com/jackc/pgx/v5"]
`
	c, err := Load([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != 1 {
		t.Errorf("version = %d, want 1", c.Version)
	}
	if !c.UsesDefaults() {
		t.Error("UsesDefaults should default to true when absent")
	}
	if c.CatchAllTier() != 3 {
		t.Errorf("CatchAllTier = %d, want 3", c.CatchAllTier())
	}
	if len(c.Classify.DB) != 1 || c.Classify.DB[0] != "github.com/jackc/pgx/v5" {
		t.Errorf("db hints = %v", c.Classify.DB)
	}
}

func TestLoadTierLayer(t *testing.T) {
	const y = `
useDefaults: false
catchAll: 4
rules:
  - match: { effect: mutate, identity: "*ledger*" }
    tier: 1
pins:
  - { identity: "*health#Ping", tier: 4 }
`
	c, err := Load([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if c.UsesDefaults() {
		t.Error("useDefaults:false not honored")
	}
	if c.CatchAllTier() != 4 {
		t.Errorf("CatchAllTier = %d, want 4", c.CatchAllTier())
	}
	if len(c.Rules) != 1 || c.Rules[0].Match.Identity != "*ledger*" || c.Rules[0].Tier != 1 {
		t.Errorf("rules = %+v", c.Rules)
	}
	if len(c.Pins) != 1 || c.Pins[0].Identity != "*health#Ping" || c.Pins[0].Tier != 4 {
		t.Errorf("pins = %+v", c.Pins)
	}
}

func TestLoadServiceAndHTTPHints(t *testing.T) {
	const y = `
version: 1
service: loansvc
classify:
  http: ["example.com/loansvc/internal/client#Call"]
`
	c, err := Load([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if c.Service != "loansvc" {
		t.Errorf("service = %q, want loansvc", c.Service)
	}
	if len(c.Classify.HTTP) != 1 || c.Classify.HTTP[0] != "example.com/loansvc/internal/client#Call" {
		t.Errorf("http hints = %v", c.Classify.HTTP)
	}
}

func TestLoadEmpty(t *testing.T) {
	c, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !c.UsesDefaults() || c.CatchAllTier() != 3 {
		t.Errorf("empty config should be all-defaults, got %+v", c)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	if _, err := Load([]byte("nope: true\n")); err == nil {
		t.Fatal("expected error on unknown key")
	}
}

func TestLoadRejectsBadTier(t *testing.T) {
	if _, err := Load([]byte("rules:\n  - match: {effect: mutate}\n    tier: 9\n")); err == nil {
		t.Fatal("expected error on tier out of range")
	}
}

func TestCanonDefaults(t *testing.T) {
	c, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.SalienceThreshold(); got != 2 {
		t.Errorf("default SalienceThreshold = %d, want 2 (warn)", got)
	}
}

func TestCanonSalienceTier(t *testing.T) {
	const y = `
canon:
  salienceTier: info
  attributeAllowlist: ["loan.product"]
  redactKeys: ["customer.email"]
`
	c, err := Load([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.SalienceThreshold(); got != 3 {
		t.Errorf("info SalienceThreshold = %d, want 3", got)
	}
	if len(c.Canon.AttributeAllowlist) != 1 || c.Canon.AttributeAllowlist[0] != "loan.product" {
		t.Errorf("attributeAllowlist = %v", c.Canon.AttributeAllowlist)
	}
	if len(c.Canon.RedactKeys) != 1 {
		t.Errorf("redactKeys = %v", c.Canon.RedactKeys)
	}
}

func TestCanonRejectsBadSalienceTier(t *testing.T) {
	if _, err := Load([]byte("canon:\n  salienceTier: loud\n")); err == nil {
		t.Fatal("expected error on bad salienceTier")
	}
}

func TestFanOutThreshold(t *testing.T) {
	def, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := def.FanOutThreshold(); got != 8 {
		t.Errorf("default FanOutThreshold = %d, want 8", got)
	}
	custom, err := Load([]byte("static:\n  highFanOutThreshold: 20\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := custom.FanOutThreshold(); got != 20 {
		t.Errorf("custom FanOutThreshold = %d, want 20", got)
	}
	if _, err := Load([]byte("static:\n  highFanOutThreshold: -1\n")); err == nil {
		t.Fatal("expected error on negative highFanOutThreshold")
	}
}
