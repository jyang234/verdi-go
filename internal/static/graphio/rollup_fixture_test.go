package graphio_test

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/jyang234/golang-code-graph/internal/static/analyze"
	"github.com/jyang234/golang-code-graph/internal/static/graphio"
)

func eventbussvcDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "fixtures", "eventbussvc")
}

// TestRollupCompositionRootWiringEndToEnd is the end-to-end guard for the
// composition-root wiring classification, run against the eventbussvc fixture — a
// hand-written stand-in for oapi-codegen's strict server built WITH error-handler
// options. The fixture's `main` injects two closures (newHandler$1/$2) into the
// generated `api` handler, which invokes them on the error path, producing the
// back-edge api.strictHandler.<Op> -> eventbussvc.newHandler$N — the exact pattern
// the field report observed on event-bus. The DEFAULT (RTA) build resolves the
// func-value calls, so no --reclaim/--algo is needed.
//
// It pins, on real analyzer output (not a hand-built graph), that:
//   - the `package main` component is flagged role="composition_root";
//   - the api -> main back-edge is classified "wiring", NOT a domain "call";
//   - the api -> server domain dependency is still a plain "call" (the wiring
//     reclassification does not swallow real dependencies);
//   - no edge points the wrong way as a CODE call into the composition root (the
//     C3 inversion the wiring class exists to prevent).
func TestRollupCompositionRootWiringEndToEnd(t *testing.T) {
	res, err := analyze.Analyze(eventbussvcDir())
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	g, err := graphio.Build(res, "")
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	r := g.RollupByPackage()

	const (
		root   = "example.com/eventbussvc"
		api    = "example.com/eventbussvc/api"
		server = "example.com/eventbussvc/server"
		store  = "example.com/eventbussvc/store"
	)

	var rootComp *graphio.Component
	for i := range r.Components {
		if r.Components[i].Package == root {
			rootComp = &r.Components[i]
		}
	}
	if rootComp == nil {
		t.Fatalf("composition-root component %q missing from rollup: %+v", root, r.Components)
	}
	if rootComp.Role != graphio.RollupRoot {
		t.Errorf("component %q Role = %q, want %q", root, rootComp.Role, graphio.RollupRoot)
	}

	// Edge classes the fixture must exercise. The DOMAIN surface (server's bus
	// publish and its store→db write) is asserted too, so the fixture's store/bus
	// packages are intentionally exercised rather than decorative: the point of the
	// test is that the api→root wiring edge is split apart from these real
	// dependencies and effects, not just that one wiring edge exists.
	var sawWiring, sawDomainCall, sawServerStore, sawStoreDB, sawServerBus bool
	for _, e := range r.Edges {
		switch {
		case e.From == api && e.To == root:
			if e.Kind != graphio.RollupWiring {
				t.Errorf("api->main back-edge Kind = %q, want %q (wiring): %+v", e.Kind, graphio.RollupWiring, e)
			}
			sawWiring = true
		case e.From == api && e.To == server:
			if e.Kind != graphio.RollupCall {
				t.Errorf("api->server domain edge Kind = %q, want %q (call): %+v", e.Kind, graphio.RollupCall, e)
			}
			sawDomainCall = true
		case e.From == server && e.To == store:
			if e.Kind != graphio.RollupCall {
				t.Errorf("server->store domain edge Kind = %q, want %q (call): %+v", e.Kind, graphio.RollupCall, e)
			}
			sawServerStore = true
		case e.From == store && e.To == "db":
			if e.Kind != graphio.RollupEffect {
				t.Errorf("store->db edge Kind = %q, want %q (effect): %+v", e.Kind, graphio.RollupEffect, e)
			}
			sawStoreDB = true
		case e.From == server && e.To == "bus":
			if e.Kind != graphio.RollupEffect {
				t.Errorf("server->bus edge Kind = %q, want %q (effect): %+v", e.Kind, graphio.RollupEffect, e)
			}
			sawServerBus = true
		}
		// No edge may read as a CODE call INTO the composition root — that is the
		// architecture inversion the wiring class exists to prevent.
		if e.To == root && e.Kind == graphio.RollupCall {
			t.Errorf("an edge reads as a domain call into the composition root (inverted): %+v", e)
		}
	}
	if !sawWiring {
		t.Errorf("expected an api->main wiring back-edge; rollup edges = %+v", r.Edges)
	}
	if !sawDomainCall {
		t.Errorf("expected an api->server domain call; rollup edges = %+v", r.Edges)
	}
	if !sawServerStore || !sawStoreDB || !sawServerBus {
		t.Errorf("expected the domain effect surface (server->store call, store->db effect, server->bus effect); rollup edges = %+v", r.Edges)
	}
}

// TestRollupDisclosesOmittedPackageEndToEnd is the end-to-end guard for the
// imported-but-omitted no-function disclosure (the C3 orientation gap from the field
// report), on real analyzer output. The eventbussvc fixture's `participant` package is
// types/consts only (no functions, so no call-graph node) yet IMPORTED by the `server`
// component — exactly the internal domain package a reader orienting on the architecture
// would otherwise have no signal exists. It pins that:
//   - `participant` is disclosed in the rollup's Omitted list, with its full import path;
//   - the Mermaid render carries the footnote naming it;
//   - the disclosure is anchored to a real import: a package no component imports (and
//     every function-bearing component) is NOT listed;
//   - the omitted set is deterministic across two independent builds (a sorted path, per
//     CLAUDE.md's determinism discipline for a new ordering surface).
func TestRollupDisclosesOmittedPackageEndToEnd(t *testing.T) {
	const participant = "example.com/eventbussvc/participant"

	res, err := analyze.Analyze(eventbussvcDir())
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	g, err := graphio.Build(res, "")
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	r := g.RollupByPackage()

	found := false
	for _, p := range r.Omitted {
		if p == participant {
			found = true
		}
		// A function-bearing component must never be listed as omitted.
		for _, c := range r.Components {
			if p == c.Package {
				t.Errorf("a rendered component %q must not appear as an omitted package", p)
			}
		}
	}
	if !found {
		t.Errorf("types-only imported package %q missing from rollup Omitted = %v", participant, r.Omitted)
	}

	// The footnote names it (abbreviated to the last two path segments, like the boxes).
	m := g.RollupMermaid(graphio.RollupMermaidOptions{})
	if !strings.Contains(m, "omitted (imported, no functions)") || !strings.Contains(m, "eventbussvc/participant") {
		t.Errorf("rollup Mermaid missing the omitted-packages footnote for %q:\n%s", participant, m)
	}

	// Determinism: a second independent build yields the same omitted set.
	res2, err := analyze.Analyze(eventbussvcDir())
	if err != nil {
		t.Fatalf("analyze (2): %v", err)
	}
	g2, err := graphio.Build(res2, "")
	if err != nil {
		t.Fatalf("build graph (2): %v", err)
	}
	if !reflect.DeepEqual(g2.RollupByPackage().Omitted, r.Omitted) {
		t.Errorf("omitted set not deterministic across builds:\n%v\nvs\n%v", g2.RollupByPackage().Omitted, r.Omitted)
	}
}
