package graph

import (
	"strings"
	"testing"
)

func TestLoadRejectsUnknownFields(t *testing.T) {
	const j = `{"nodes":[],"edges":[],"blind_spots":[],"surprise":1}`
	if _, err := Load(strings.NewReader(j)); err == nil {
		t.Fatal("expected an error for an unknown field, got nil")
	}
}

func TestLoadRequiresNodes(t *testing.T) {
	const j = `{"edges":[],"blind_spots":[]}`
	if _, err := Load(strings.NewReader(j)); err == nil {
		t.Fatal("expected an error for a graph with no nodes key, got nil")
	}
}

func TestLoadEmptyGraph(t *testing.T) {
	const j = `{"nodes":[],"edges":[],"blind_spots":[]}`
	g, err := Load(strings.NewReader(j))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(g.Nodes) != 0 {
		t.Fatalf("want 0 nodes, got %d", len(g.Nodes))
	}
}

func TestEdgeClassification(t *testing.T) {
	cases := []struct {
		to       string
		boundary bool
		dynamic  bool
	}{
		{"example.com/svc/internal/app.Do", false, false},
		{"boundary:db INSERT users", true, false},
		{"boundary:bus PUBLISH user.created", true, false},
		{"boundary:bus PUBLISH <dynamic>", true, true},
	}
	for _, c := range cases {
		e := Edge{To: c.to}
		if got := e.IsBoundary(); got != c.boundary {
			t.Errorf("IsBoundary(%q)=%v, want %v", c.to, got, c.boundary)
		}
		if got := e.IsDynamic(); got != c.dynamic {
			t.Errorf("IsDynamic(%q)=%v, want %v", c.to, got, c.dynamic)
		}
	}
}
