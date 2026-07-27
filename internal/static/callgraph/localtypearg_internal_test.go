package callgraph

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"

	xcg "golang.org/x/tools/go/callgraph"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"

	"github.com/jyang234/golang-code-graph/internal/static/features"
)

const collidingLocalTypeArgsSrc = `package localtypes
func instantiate[T any]() {}
func use() { { type result struct{ A int }; instantiate[result]() }; { type result struct{ A int }; instantiate[result]() } }
func main() { use() }
`

// buildLocalTypeCallGraph returns the main and init roots for a program whose
// same-line local types have identical display strings but distinct declarations.
func buildLocalTypeCallGraph(t *testing.T) []*ssa.Function {
	t.Helper()
	return buildCallGraphRoots(t, collidingLocalTypeArgsSrc)
}

// swappedLocalTypeArgsSrc reproduces a residual collision class the
// local-sites/v1 discriminator cannot separate: the pooled site set is
// position-blind, so pair[A, B] and pair[B, A] over the same two local types
// share both the display FQN and the discriminator. The local `result` inside
// the generic `first` additionally contributes no site at all (an instantiated
// local TypeName has a nil Parent), so both instances reduce to the same single
// site. It exists to prove finalize's duplicate-key guard fires on EVERY run.
const swappedLocalTypeArgsSrc = `package localtypes
func pair[A any, B any]() {}
func swap[A any, B any]() { pair[A, B](); pair[B, A]() }
func first[X any]() { type result struct{ N int }; swap[result, X]() }
func second() { type result struct{ N int }; first[result]() }
func main() { second() }
`

// buildCallGraphRoots returns the main and init roots for src.
func buildCallGraphRoots(t *testing.T, src string) []*ssa.Function {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "local.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkg := types.NewPackage("example.com/localtypes", "")
	spkg, _, err := ssautil.BuildPackage(
		&types.Config{Importer: importer.Default()}, fset, pkg, []*ast.File{f},
		ssa.SanityCheckFunctions|ssa.InstantiateGenerics)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	spkg.Prog.Build()

	var roots []*ssa.Function
	if main := spkg.Func("main"); main != nil {
		roots = append(roots, main)
	}
	if init := spkg.Func("init"); init != nil {
		roots = append(roots, init)
	}
	return roots
}

// TestFinalizePanicsOnEverySurvivingKeyCollision pins the guarantee that a surviving
// duplicate sort key ALWAYS panics, over repeated runs so a probabilistic guard cannot
// flake green. The guard used to live inside the sort.Slice comparator, which is not
// required to compare every equal pair — sort.Slice promises a sorted result, nothing
// about which pairs it visits. Measured on go1.25.5 the comparator did in fact see this
// pair on every one of 20000 fromX runs, so the old placement was not observably broken;
// it was resting on an implementation detail instead of on the documented postcondition
// that panicOnDuplicateSortKey now uses. Both are pinned here: the collision must panic,
// and (see TestPanicOnDuplicateSortKeyScansSortedNodes) it must panic from the sorted
// order alone.
func TestFinalizePanicsOnEverySurvivingKeyCollision(t *testing.T) {
	for run := 0; run < 5; run++ {
		roots := buildCallGraphRoots(t, swappedLocalTypeArgsSrc)
		raw := rta.Analyze(roots, true).CallGraph

		var colliding []*ssa.Function
		for fn := range raw.Nodes {
			if fn != nil && strings.Contains(fn.RelString(nil), ".pair[") {
				colliding = append(colliding, fn)
			}
		}
		if len(colliding) != 2 {
			t.Fatalf("run %d: raw graph has %d pair instances; want 2", run, len(colliding))
		}
		if colliding[0].RelString(nil) != colliding[1].RelString(nil) {
			t.Fatalf("run %d: fixture did not reproduce the display FQN collision: %q and %q",
				run, colliding[0].RelString(nil), colliding[1].RelString(nil))
		}
		left, right := features.InstanceDiscriminator(colliding[0]), features.InstanceDiscriminator(colliding[1])
		if left != right {
			t.Fatalf("run %d: fixture did not reproduce the discriminator collision: %q and %q", run, left, right)
		}

		assertFinalizeCollisionPanic(t, run, raw)
	}
}

// TestPanicOnDuplicateSortKeyScansSortedNodes pins the guard's independence from the
// sort: it feeds an ALREADY-sorted node slice straight to the scan, so the panic is
// derived from sort.Slice's documented postcondition (equal keys land adjacent) rather
// than from the undocumented question of which pairs the comparator happens to visit.
func TestPanicOnDuplicateSortKeyScansSortedNodes(t *testing.T) {
	roots := buildCallGraphRoots(t, swappedLocalTypeArgsSrc)
	raw := rta.Analyze(roots, true).CallGraph

	var colliding []*ssa.Function
	for fn := range raw.Nodes {
		if fn != nil && strings.Contains(fn.RelString(nil), ".pair[") {
			colliding = append(colliding, fn)
		}
	}
	if len(colliding) != 2 {
		t.Fatalf("raw graph has %d pair instances; want 2", len(colliding))
	}
	nodes := []*Node{
		{FQN: colliding[0].RelString(nil), Func: colliding[0]},
		{FQN: colliding[1].RelString(nil), Func: colliding[1]},
	}
	if nodes[0].FQN != nodes[1].FQN ||
		features.InstanceDiscriminator(nodes[0].Func) != features.InstanceDiscriminator(nodes[1].Func) {
		t.Fatalf("fixture did not reproduce the sort-key collision")
	}
	if !sort.SliceIsSorted(nodes, func(i, j int) bool {
		if nodes[i].FQN != nodes[j].FQN {
			return nodes[i].FQN < nodes[j].FQN
		}
		return features.InstanceDiscriminator(nodes[i].Func) < features.InstanceDiscriminator(nodes[j].Func)
	}) {
		t.Fatalf("fixture nodes are not in finalize's sorted order")
	}

	defer func() {
		got := recover()
		if got == nil {
			t.Fatal("panicOnDuplicateSortKey accepted two distinct functions with one sort key")
		}
		if message := fmt.Sprint(got); !strings.Contains(message, "share sort key") {
			t.Fatalf("panic = %q, want the duplicate sort-key diagnostic", message)
		}
	}()
	panicOnDuplicateSortKey(nodes)
}

// TestPanicOnDuplicateSortKeyAcceptsDistinctKeys is the negative path: adjacent nodes
// that differ in FQN, that differ only in discriminator, and one node repeated (the
// same *ssa.Function, which finalize never duplicates but the scan must not reject).
func TestPanicOnDuplicateSortKeyAcceptsDistinctKeys(t *testing.T) {
	roots := buildCallGraphRoots(t, collidingLocalTypeArgsSrc)
	raw := rta.Analyze(roots, true).CallGraph

	var instances []*ssa.Function
	for fn := range raw.Nodes {
		if fn != nil && strings.Contains(fn.RelString(nil), "instantiate[") {
			instances = append(instances, fn)
		}
	}
	if len(instances) != 2 {
		t.Fatalf("raw graph has %d instantiate instances; want 2", len(instances))
	}
	sort.Slice(instances, func(i, j int) bool {
		return features.InstanceDiscriminator(instances[i]) < features.InstanceDiscriminator(instances[j])
	})

	tests := []struct {
		name  string
		nodes []*Node
	}{
		{name: "empty"},
		{name: "single", nodes: []*Node{{FQN: "a", Func: instances[0]}}},
		{
			name: "distinct_fqn",
			nodes: []*Node{
				{FQN: "a", Func: instances[0]},
				{FQN: "b", Func: instances[1]},
			},
		},
		{
			name: "distinct_discriminator_only",
			nodes: []*Node{
				{FQN: instances[0].RelString(nil), Func: instances[0]},
				{FQN: instances[1].RelString(nil), Func: instances[1]},
			},
		},
		{
			name: "same_function_twice",
			nodes: []*Node{
				{FQN: instances[0].RelString(nil), Func: instances[0]},
				{FQN: instances[0].RelString(nil), Func: instances[0]},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if got := recover(); got != nil {
					t.Fatalf("panicOnDuplicateSortKey panicked on a legal node slice: %v", got)
				}
			}()
			panicOnDuplicateSortKey(test.nodes)
		})
	}
}

// assertFinalizeCollisionPanic runs fromX and requires the duplicate-key panic.
func assertFinalizeCollisionPanic(t *testing.T, run int, raw *xcg.Graph) {
	t.Helper()
	defer func() {
		got := recover()
		if got == nil {
			t.Fatalf("run %d: fromX returned without panicking on a duplicate sort key", run)
		}
		if message := fmt.Sprint(got); !strings.Contains(message, "share sort key") {
			t.Fatalf("run %d: panic = %q, want the duplicate sort-key diagnostic", run, message)
		}
	}()
	fromX(raw, AlgoRTA, nil)
}

func TestFromXKeepsCollidingDisplayGenericInstances(t *testing.T) {
	var wantKeys []string
	for construction := 0; construction < 20; construction++ {
		roots := buildLocalTypeCallGraph(t)
		raw := rta.Analyze(roots, true).CallGraph

		var rawInstances []*ssa.Function
		for fn := range raw.Nodes {
			if fn != nil && strings.Contains(fn.RelString(nil), "instantiate[") {
				rawInstances = append(rawInstances, fn)
			}
		}
		sort.Slice(rawInstances, func(i, j int) bool {
			left, right := features.InstanceDiscriminator(rawInstances[i]), features.InstanceDiscriminator(rawInstances[j])
			if left != right {
				return left < right
			}
			return rawInstances[i].RelString(nil) < rawInstances[j].RelString(nil)
		})
		if len(rawInstances) != 2 {
			t.Fatalf("construction %d raw graph has %d instances; want 2", construction, len(rawInstances))
		}
		if rawInstances[0].RelString(nil) != rawInstances[1].RelString(nil) {
			t.Fatalf("construction %d fixture did not reproduce display collision", construction)
		}

		got := fromX(raw, AlgoRTA, nil)
		var keys []string
		for _, n := range got.Nodes {
			if strings.Contains(n.FQN, "instantiate[") {
				keys = append(keys, n.FQN+"\x00"+features.InstanceDiscriminator(n.Func))
			}
		}
		if len(keys) != 2 {
			t.Fatalf("construction %d converted graph kept %d instances; want 2", construction, len(keys))
		}
		if !sort.StringsAreSorted(keys) {
			t.Fatalf("construction %d instance keys are not sorted: %q", construction, keys)
		}
		if keys[0] == keys[1] {
			t.Fatalf("construction %d duplicate canonical key %q", construction, keys[0])
		}
		if construction == 0 {
			wantKeys = append([]string(nil), keys...)
			continue
		}
		if strings.Join(keys, "\n") != strings.Join(wantKeys, "\n") {
			t.Fatalf("construction %d instance order = %q, want %q", construction, keys, wantKeys)
		}
	}
}
