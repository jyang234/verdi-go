package impeach

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/groundwork/fitness"
)

// TestCanonFQNPackageParityWithFitness pins the one-source-of-truth parity
// (CLAUDE.md): canonFQN's package-boundary split (splitPkgSymbol / splitLastDot)
// and fitness.PkgOf implement the SAME rule (first '.' after the last '/'), so they
// must agree on every ssa spelling. canonFQN cannot simply CALL fitness.PkgOf (it
// needs the symbol remainder and also parses runtime spellings fitness never
// sees), so this test is what keeps the two copies from drifting.
func TestCanonFQNPackageParityWithFitness(t *testing.T) {
	for _, ssa := range []string{
		"example.com/svc/internal/origination.NewEvaluator",
		"(*example.com/svc/internal/client.Bureau).Score",
		"(example.com/svc/internal/client.Bureau).Score",
		"example.com/p.Func",
		"(*example.com/p.T).M",
	} {
		k, ok := canonFQN(ssa)
		if !ok {
			t.Fatalf("canonFQN(%q) = ⊥, want a key", ssa)
		}
		if want := fitness.PkgOf(ssa); k.Pkg != want {
			t.Errorf("package split drift on %q: canonFQN=%q fitness.PkgOf=%q", ssa, k.Pkg, want)
		}
	}
}

// TestCanonFQNParity is the one-source guard CLAUDE.md requires (§7): for a
// fixture function of each RECONCILABLE class, the ssa node spelling and the
// runtime tag spelling must canonFQN to the SAME key, and each ⊥ class must ⊥ on
// BOTH spellings. If Go ever changes either convention, this breaks before the
// span↔node map can mislocalize.
func TestCanonFQNParity(t *testing.T) {
	reconcilable := []struct {
		name         string
		ssa, runtime string
		want         FQNKey
	}{
		{
			name:    "package func",
			ssa:     "example.com/svc/internal/origination.NewEvaluator",
			runtime: "example.com/svc/internal/origination.NewEvaluator",
			want:    FQNKey{Pkg: "example.com/svc/internal/origination", Name: "NewEvaluator"},
		},
		{
			name:    "pointer method",
			ssa:     "(*example.com/svc/internal/client.Bureau).Score",
			runtime: "example.com/svc/internal/client.(*Bureau).Score",
			want:    FQNKey{Pkg: "example.com/svc/internal/client", Recv: "Bureau", Ptr: true, Name: "Score"},
		},
		{
			name:    "value method",
			ssa:     "(example.com/svc/internal/client.Bureau).Score",
			runtime: "example.com/svc/internal/client.Bureau.Score",
			want:    FQNKey{Pkg: "example.com/svc/internal/client", Recv: "Bureau", Ptr: false, Name: "Score"},
		},
	}
	for _, c := range reconcilable {
		t.Run(c.name, func(t *testing.T) {
			ks, ok := canonFQN(c.ssa)
			if !ok {
				t.Fatalf("ssa %q failed to parse", c.ssa)
			}
			kr, ok := canonFQN(c.runtime)
			if !ok {
				t.Fatalf("runtime %q failed to parse", c.runtime)
			}
			if ks != kr {
				t.Errorf("spellings disagree:\n  ssa     %q -> %+v\n  runtime %q -> %+v", c.ssa, ks, c.runtime, kr)
			}
			if ks != c.want {
				t.Errorf("key = %+v, want %+v", ks, c.want)
			}
		})
	}

	// The pointer and value methods of the same type are DIFFERENT functions: the
	// key must not collapse them (Ptr is load-bearing).
	pv, _ := canonFQN("(*example.com/p.T).M")
	vv, _ := canonFQN("(example.com/p.T).M")
	if pv == vv {
		t.Error("pointer and value receiver methods canonicalized to the same key")
	}

	// Each ⊥ class must ⊥ on BOTH spellings — never one side keying while the other
	// ⊥s (the asymmetry that would fabricate `absent-from-graph`, §7).
	bot := []struct {
		name         string
		ssa, runtime string
	}{
		{"closure", "example.com/svc.run$4", "example.com/svc.run.func1"},
		{"generic", "example.com/m.Map[int]", "example.com/m.Map[go.shape.int]"},
		{"method value", "(*example.com/p.T).M$bound", "example.com/p.(*T).M-fm"},
		{"package init", "example.com/p.init", "example.com/p.init"},
		{"empty", "", ""},
	}
	for _, c := range bot {
		t.Run("bot/"+c.name, func(t *testing.T) {
			if k, ok := canonFQN(c.ssa); ok {
				t.Errorf("ssa %q should be ⊥, got %+v", c.ssa, k)
			}
			if k, ok := canonFQN(c.runtime); ok {
				t.Errorf("runtime %q should be ⊥, got %+v", c.runtime, k)
			}
			if r := fqnBotReason(c.ssa); r == "" {
				t.Errorf("ssa %q ⊥ carries no reason", c.ssa)
			}
		})
	}
}

// TestCanonFQNDottedFinalSegment pins the §12.5 close: over a dotted-final-segment
// import path (gopkg.in/yaml.v3), ⊥ stays SYMMETRIC across the two spellings, so the
// sharp absent-from-graph signal cannot mint a phantom. A value method ⊥s on BOTH
// sides (the runtime spelling is genuinely ambiguous); a pointer method and a
// package func reconcile (robust markers / identical strings).
func TestCanonFQNDottedFinalSegment(t *testing.T) {
	// Value method: both ⊥ (the new SSA-side guard matches the runtime's >2-segment ⊥).
	if k, ok := canonFQN("(gopkg.in/yaml.v3.Cfg).Marshal"); ok {
		t.Errorf("ssa value method on dotted package should be ⊥, got %+v", k)
	}
	if k, ok := canonFQN("gopkg.in/yaml.v3.Cfg.Marshal"); ok {
		t.Errorf("runtime value method on dotted package should be ⊥, got %+v", k)
	}
	// Pointer method: both key to the SAME key (pkg keeps the dotted final segment).
	ssaPtr, okSSA := canonFQN("(*gopkg.in/yaml.v3.Cfg).Marshal")
	rtPtr, okRT := canonFQN("gopkg.in/yaml.v3.(*Cfg).Marshal")
	if !okSSA || !okRT || ssaPtr != rtPtr {
		t.Errorf("pointer method on dotted package must reconcile: ssa(%v,%v) runtime(%v,%v)", ssaPtr, okSSA, rtPtr, okRT)
	}
	if ssaPtr.Pkg != "gopkg.in/yaml.v3" || ssaPtr.Recv != "Cfg" || !ssaPtr.Ptr {
		t.Errorf("pointer method key = %+v, want pkg=gopkg.in/yaml.v3 recv=Cfg ptr", ssaPtr)
	}
	// Package func: identical spelling ⇒ identical key (consistent, a match not a phantom).
	fa, oka := canonFQN("gopkg.in/yaml.v3.Marshal")
	fb, okb := canonFQN("gopkg.in/yaml.v3.Marshal")
	if !oka || !okb || fa != fb {
		t.Errorf("package func on dotted package must key identically: %v/%v %v/%v", fa, oka, fb, okb)
	}
}

// TestCanonFQNTotalAndPure pins the §7 properties relied on for determinism: the
// function never panics on adversarial input (total) and returns the same result
// on repeated calls (pure). A parseable input also carries no ⊥ reason, and a ⊥
// input always does — the two views can never disagree.
func TestCanonFQNTotalAndPure(t *testing.T) {
	inputs := []string{
		"", ".", "..", "(", "()", "(*).", "(*p.T)", "a.b.c.d.e",
		"example.com/p.T.M", "/leadingslash.Func", "no-dot-at-all",
		"example.com/p.(*).M", "example.com/p.(*T).", "$", "[", "-fm", ".func9",
	}
	for _, in := range inputs {
		k1, ok1 := canonFQN(in)
		k2, ok2 := canonFQN(in)
		if k1 != k2 || ok1 != ok2 {
			t.Errorf("impure on %q: (%+v,%v) != (%+v,%v)", in, k1, ok1, k2, ok2)
		}
		if (fqnBotReason(in) == "") != ok1 {
			t.Errorf("reason/ok disagree on %q: reason=%q ok=%v", in, fqnBotReason(in), ok1)
		}
	}
}
