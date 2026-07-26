package graphio

import (
	"bytes"
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
