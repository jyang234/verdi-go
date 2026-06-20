package graphio

import (
	"testing"

	"github.com/jyang234/golang-code-graph/internal/config"
	"github.com/jyang234/golang-code-graph/internal/static/blindspots"
)

func annCfg(anns ...config.Annotation) *config.Config {
	c := &config.Config{}
	c.Static.Annotations = anns
	return c
}

// TestMergeAnnotationsBinds covers the happy path: an annotation matching a blind
// spot by (site, kind) binds, an omitted kind adopts a single-kind site's kind,
// and the result is sorted by (Site, Kind).
func TestMergeAnnotationsBinds(t *testing.T) {
	manifest := []blindspots.BlindSpot{
		{Kind: blindspots.ExternalBoundaryCall, Site: "ex.com/svc.Send", Detail: "hands off to acme"},
		{Kind: blindspots.ConcurrentDispatch, Site: "ex.com/svc.Run", Detail: "goroutine"},
	}
	cfg := annCfg(
		config.Annotation{Site: "ex.com/svc.Send", Kind: "ExternalBoundaryCall", Note: "POSTs to acme.example.com", By: "dev@x"},
		config.Annotation{Site: "ex.com/svc.Run", Note: "spawns the dispatch worker"}, // kind omitted, single-kind site
	)
	got, skipped, err := mergeAnnotations(manifest, cfg)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("happy path must skip nothing, got %+v", skipped)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 bound annotations, got %d: %+v", len(got), got)
	}
	// Sorted by (Site, Kind): ex.com/svc.Run < ex.com/svc.Send.
	if got[0].Site != "ex.com/svc.Run" || got[0].Kind != "ConcurrentDispatch" {
		t.Errorf("omitted kind should have adopted ConcurrentDispatch; got %+v", got[0])
	}
	if got[1].Site != "ex.com/svc.Send" || got[1].Note != "POSTs to acme.example.com" || got[1].By != "dev@x" {
		t.Errorf("Send annotation bound wrong: %+v", got[1])
	}
}

// TestMergeAnnotationsOrphanFails pins the fail-closed contract: an annotation
// that matches no detected blind spot is refused (a stale FQN is drift), never a
// silent drop. A mismatch on an algo-STABLE kind at a live site is likewise refused
// — only an algo-FRAGILE kind earns the warn-and-skip (see the fragile test below).
func TestMergeAnnotationsOrphanFails(t *testing.T) {
	manifest := []blindspots.BlindSpot{{Kind: blindspots.Reflect, Site: "ex.com/svc.Decode", Detail: "reflect"}}
	if _, _, err := mergeAnnotations(manifest, annCfg(config.Annotation{Site: "ex.com/svc.Gone", Note: "x"})); err == nil {
		t.Fatal("an annotation at a site with no blind spot must be rejected")
	}
	// Site exists but the named kind does not, and the named kind is algo-STABLE
	// (ExternalBoundaryCall — a known external leaf, present under any --algo): a
	// mismatch on it is a real typo, so it must still fail closed, never warn-and-skip.
	if blindspots.AlgoFragile(blindspots.ExternalBoundaryCall) {
		t.Fatal("test premise: ExternalBoundaryCall must be algo-stable")
	}
	if _, _, err := mergeAnnotations(manifest, annCfg(config.Annotation{Site: "ex.com/svc.Decode", Kind: "ExternalBoundaryCall", Note: "x"})); err == nil {
		t.Fatal("an annotation naming an algo-stable kind absent at the site must be rejected")
	}
}

// TestMergeAnnotationsAlgoFragileKindSkips pins the §22 tolerance: an annotation
// naming an algo-FRAGILE kind (its presence flips with --algo) absent at a site that
// is otherwise LIVE (carries another blind spot — so it is an --algo skew, not a
// stale FQN) is warn-and-skipped into the skip list, NOT failed. The build must not
// hard-error on a disclosure-only note that a different --algo would surface.
func TestMergeAnnotationsAlgoFragileKindSkips(t *testing.T) {
	if !blindspots.AlgoFragile(blindspots.UnresolvedCall) {
		t.Fatal("test premise: UnresolvedCall must be algo-fragile")
	}
	// Live site: it carries a (stable) ExternalBoundaryCall; the annotation names the
	// fragile UnresolvedCall, which this manifest does not surface (as under rta).
	manifest := []blindspots.BlindSpot{{Kind: blindspots.ExternalBoundaryCall, Site: "ex.com/svc.Send", Detail: "ext"}}
	got, skipped, err := mergeAnnotations(manifest, annCfg(
		config.Annotation{Site: "ex.com/svc.Send", Kind: "UnresolvedCall", Note: "behind the func value, under vta"},
	))
	if err != nil {
		t.Fatalf("a fragile kind absent at a live site must warn-and-skip, not fail: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("the skewed annotation must not bind; got %+v", got)
	}
	if len(skipped) != 1 {
		t.Fatalf("want exactly one skipped annotation, got %d: %+v", len(skipped), skipped)
	}
	if s := skipped[0]; s.Site != "ex.com/svc.Send" || s.Kind != "UnresolvedCall" || len(s.Present) != 1 || s.Present[0] != "ExternalBoundaryCall" {
		t.Errorf("skipped record wrong: %+v", skipped[0])
	}

	// Limit of the relaxation (§22): the skip requires the site to stay LIVE. A fragile
	// kind at a site with NO blind spot at all (the fragile kind was its only one, now
	// dropped by this algo) is indistinguishable from a stale FQN, so it takes the
	// orphan path and FAILS — never warn-and-skipped. This pins the fail-closed boundary
	// so a future relaxation cannot silently swallow a genuine typo at a dead site.
	if _, _, err := mergeAnnotations(nil, annCfg(
		config.Annotation{Site: "ex.com/svc.Gone", Kind: "UnresolvedCall", Note: "fragile kind, but the site is dead"},
	)); err == nil {
		t.Fatal("a fragile kind at a site with no blind spot must still fail (orphan), not warn-and-skip")
	}
}

// TestMergeAnnotationsAmbiguousKindFails pins that a site carrying more than one
// blind-spot kind requires an explicit kind, so context can never silently attach
// to the wrong shape.
func TestMergeAnnotationsAmbiguousKindFails(t *testing.T) {
	manifest := []blindspots.BlindSpot{
		{Kind: blindspots.ExternalBoundaryCall, Site: "ex.com/svc.Mixed", Detail: "ext"},
		{Kind: blindspots.ConcurrentDispatch, Site: "ex.com/svc.Mixed", Detail: "go"},
	}
	if _, _, err := mergeAnnotations(manifest, annCfg(config.Annotation{Site: "ex.com/svc.Mixed", Note: "which one?"})); err == nil {
		t.Fatal("an omitted kind on a multi-kind site must be rejected as ambiguous")
	}
	// With the kind given, it binds.
	got, _, err := mergeAnnotations(manifest, annCfg(config.Annotation{Site: "ex.com/svc.Mixed", Kind: "ConcurrentDispatch", Note: "the worker"}))
	if err != nil || len(got) != 1 || got[0].Kind != "ConcurrentDispatch" {
		t.Fatalf("disambiguated annotation should bind ConcurrentDispatch; got %+v err=%v", got, err)
	}
}

// TestMergeAnnotationsDedupTieBreakIsIntrinsic pins that two annotations colliding
// on (site, kind) collapse to the lexically-smallest note — an intrinsic tie-break,
// not arrival order — so the bound set is byte-identical run-to-run.
func TestMergeAnnotationsDedupTieBreakIsIntrinsic(t *testing.T) {
	manifest := []blindspots.BlindSpot{{Kind: blindspots.Reflect, Site: "ex.com/svc.Decode", Detail: "reflect"}}
	mk := func(n1, n2 string) Annotation {
		got, _, err := mergeAnnotations(manifest, annCfg(
			config.Annotation{Site: "ex.com/svc.Decode", Kind: "reflect", Note: n1},
			config.Annotation{Site: "ex.com/svc.Decode", Kind: "reflect", Note: n2},
		))
		if err != nil || len(got) != 1 {
			t.Fatalf("collision must collapse to one: %+v err=%v", got, err)
		}
		return got[0]
	}
	if fwd, rev := mk("aaa", "zzz"), mk("zzz", "aaa"); fwd != rev || fwd.Note != "aaa" {
		t.Fatalf("dedup tie-break is arrival-dependent or not lexically-smallest: %+v vs %+v", fwd, rev)
	}

	// EQUAL notes must still tie-break on intrinsic content (By, then Claim), never
	// on config-array position — otherwise the kept By/Claim would depend on file order.
	eq := func(by1, by2 string) Annotation {
		got, _, err := mergeAnnotations(manifest, annCfg(
			config.Annotation{Site: "ex.com/svc.Decode", Kind: "reflect", Note: "same", By: by1},
			config.Annotation{Site: "ex.com/svc.Decode", Kind: "reflect", Note: "same", By: by2},
		))
		if err != nil || len(got) != 1 {
			t.Fatalf("collision must collapse to one: %+v err=%v", got, err)
		}
		return got[0]
	}
	if fwd, rev := eq("alice", "bob"), eq("bob", "alice"); fwd != rev || fwd.By != "alice" {
		t.Fatalf("equal-note tie-break is arrival-dependent or not lexically-smallest By: %+v vs %+v", fwd, rev)
	}
}

// TestMergeAnnotationsEmptyConfig: no annotations ⇒ nil, never an error.
func TestMergeAnnotationsEmptyConfig(t *testing.T) {
	manifest := []blindspots.BlindSpot{{Kind: blindspots.Reflect, Site: "ex.com/svc.Decode", Detail: "reflect"}}
	got, skipped, err := mergeAnnotations(manifest, &config.Config{})
	if err != nil || got != nil || skipped != nil {
		t.Fatalf("empty config must yield (nil, nil, nil), got %+v skipped=%+v err=%v", got, skipped, err)
	}
}
