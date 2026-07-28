package graphio

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestSortGraphNodesUsesEverySerializedField(t *testing.T) {
	base := Node{
		FQN:     "example.com/p.F",
		Sig:     "func()",
		Package: "example.com/p",
		File:    "p.go",
		Line:    10,
	}
	nodes := []Node{
		func() Node { n := base; n.EndLine = 11; return n }(),
		func() Node { n := base; n.EndLine = 12; return n }(),
		func() Node { n := base; n.EndLine = 12; n.Tier = 1; return n }(),
		func() Node { n := base; n.EndLine = 12; n.Tier = 1; n.Fallible = true; return n }(),
	}
	var want []byte
	for _, perm := range nodePermutations(nodes) {
		g := &Graph{Nodes: append([]Node(nil), perm...)}
		sortGraph(g)
		got, err := g.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if want == nil {
			want = got
		} else if !bytes.Equal(got, want) {
			t.Fatalf("node permutation changed canonical bytes\nwant: %s\ngot:  %s", want, got)
		}
	}
}

func nodePermutations(nodes []Node) [][]Node {
	if len(nodes) == 0 {
		return [][]Node{{}}
	}
	permutations := nodePermutations(nodes[1:])
	out := make([][]Node, 0, len(nodes)*len(permutations))
	for _, permutation := range permutations {
		for i := range len(nodes) {
			candidate := make([]Node, 0, len(nodes))
			candidate = append(candidate, permutation[:i]...)
			candidate = append(candidate, nodes[0])
			candidate = append(candidate, permutation[i:]...)
			out = append(out, candidate)
		}
	}
	return out
}

// TestNodeLessEqualImpliesByteIdenticalRecord pins nodeLess's load-bearing doc claim —
// "equal comparator keys therefore mean byte-identical serialized node records". sortGraph
// leaves comparator-equal nodes in whatever order they arrived in, so that claim is exactly
// what makes the residue harmless. Were it to stop holding, input order would become
// visible in the canonical bytes and the graph would no longer be a pure function of its
// inputs — the determinism the whole gating model rests on.
//
// This pins the claim on a concrete pair; the ENFORCING half is
// TestNodeLessOrdersEverySerializedNodeField below, which proves no serialized field can
// be perturbed while leaving the two nodes comparator-equal in the first place.
func TestNodeLessEqualImpliesByteIdenticalRecord(t *testing.T) {
	a := Node{
		FQN: "example.com/p.report[example.com/p.result]", Sig: "func p.report[T any](v T) string",
		Tier: 2, Package: "example.com/p", Fallible: true, File: "p.go", Line: 5, EndLine: 7,
	}
	b := Node{
		FQN: "example.com/p.report[example.com/p.result]", Sig: "func p.report[T any](v T) string",
		Tier: 2, Package: "example.com/p", Fallible: true, File: "p.go", Line: 5, EndLine: 7,
	}
	if nodeLess(a, b) || nodeLess(b, a) {
		t.Fatal("premise broken: the two nodes must be comparator-equal for this pin to mean anything")
	}
	ab, err := (&Graph{Nodes: []Node{a}}).Marshal()
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	bb, err := (&Graph{Nodes: []Node{b}}).Marshal()
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	if !bytes.Equal(ab, bb) {
		t.Errorf("comparator-equal nodes serialized differently, so nodeLess's doc claim is false\na: %s\nb: %s", ab, bb)
	}
}

// TestNodeLessOrdersEverySerializedNodeField is the reflective field guard for Node, the
// node twin of TestAttrTupleMirrorsEdgeIdentity: every field encoding/json will serialize
// must participate in nodeLess. A serialized field the comparator ignores makes two nodes
// that differ in it compare EQUAL, so sortGraph leaves them in arrival order while the
// bytes differ — the canonical form silently stops being a pure function of the input, and
// the diff's per-FQN maximum stops being unique. The field set is read from the struct's
// json tags, never a hand-list, so adding a field fails here instead of drifting.
func TestNodeLessOrdersEverySerializedNodeField(t *testing.T) {
	rt := reflect.TypeOf(Node{})
	for i := range rt.NumField() {
		f := rt.Field(i)
		if f.PkgPath != "" {
			continue // unexported: encoding/json never emits it, so it cannot reach the bytes
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue // deliberately unserialized
		}
		var a, b Node
		bv := reflect.ValueOf(&b).Elem().Field(i)
		switch f.Type.Kind() {
		case reflect.String:
			bv.SetString("z")
		case reflect.Int:
			bv.SetInt(1)
		case reflect.Bool:
			bv.SetBool(true)
		default:
			t.Fatalf("Node.%s has kind %s, which this guard cannot perturb: extend the perturbation "+
				"here (and nodeLess) before adding a serialized field of this kind", f.Name, f.Type.Kind())
		}
		if nodeLess(a, b) == nodeLess(b, a) {
			t.Errorf("nodeLess does not order Node.%s (json %q): two nodes differing ONLY in that "+
				"field compare equal, so sortGraph leaves them in arrival order while their "+
				"serialized records differ", f.Name, name)
		}
	}
}
