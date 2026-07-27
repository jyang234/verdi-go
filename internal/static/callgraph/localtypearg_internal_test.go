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

// residualLocalTypeArgsSrc reproduces the ONE collision class the discriminator
// is not claimed to separate: `gen` declares ONE function-local type and is
// instantiated twice, and L's structure does not mention X — so both sink[L]
// instances come from one source position with byte-identical structure. Neither
// position nor structure can decide them, and no refinement of either will.
//
// The swapped-role shape this fixture replaced (pair[A, B] versus pair[B, A] over
// two local types) is now SEPARATED by the positional encoding, so it can no
// longer prove the guard fires. This one exists to prove finalize's duplicate-key
// guard fires on EVERY run, and to pin the disclosed diagnostic.
const residualLocalTypeArgsSrc = `package localtypes
func sink[T any](v T) {}
func gen[X any](x X) { type L struct{ A int }; sink(L{}) }
func main() { gen(1); gen("s") }
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
		roots := buildCallGraphRoots(t, residualLocalTypeArgsSrc)
		raw := rta.Analyze(roots, true).CallGraph

		var colliding []*ssa.Function
		for fn := range raw.Nodes {
			if fn != nil && strings.Contains(fn.RelString(nil), ".sink[") {
				colliding = append(colliding, fn)
			}
		}
		if len(colliding) != 2 {
			t.Fatalf("run %d: raw graph has %d sink instances; want 2", run, len(colliding))
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
// derived from sort.Slice's documented postcondition (equal keys land in one contiguous
// run, so a group of two lands adjacent) rather
// than from the undocumented question of which pairs the comparator happens to visit.
func TestPanicOnDuplicateSortKeyScansSortedNodes(t *testing.T) {
	roots := buildCallGraphRoots(t, residualLocalTypeArgsSrc)
	raw := rta.Analyze(roots, true).CallGraph

	var colliding []*ssa.Function
	for fn := range raw.Nodes {
		if fn != nil && strings.Contains(fn.RelString(nil), ".sink[") {
			colliding = append(colliding, fn)
		}
	}
	if len(colliding) != 2 {
		t.Fatalf("raw graph has %d sink instances; want 2", len(colliding))
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
		assertResidualDiagnostic(t, fmt.Sprint(got))
	}()
	panicOnDuplicateSortKey(nodes)
}

// collidingSortKeyPair returns the two DISTINCT *ssa.Functions of residualLocalTypeArgsSrc
// that share one (FQN, InstanceDiscriminator) sort key, failing if the fixture stops
// reproducing the collision the guard exists for.
func collidingSortKeyPair(t *testing.T) (*ssa.Function, *ssa.Function) {
	t.Helper()
	roots := buildCallGraphRoots(t, residualLocalTypeArgsSrc)
	raw := rta.Analyze(roots, true).CallGraph

	var colliding []*ssa.Function
	for fn := range raw.Nodes {
		if fn != nil && strings.Contains(fn.RelString(nil), ".sink[") {
			colliding = append(colliding, fn)
		}
	}
	if len(colliding) != 2 {
		t.Fatalf("raw graph has %d sink instances; want 2", len(colliding))
	}
	if colliding[0].RelString(nil) != colliding[1].RelString(nil) ||
		features.InstanceDiscriminator(colliding[0]) != features.InstanceDiscriminator(colliding[1]) {
		t.Fatalf("fixture did not reproduce the sort-key collision")
	}
	return colliding[0], colliding[1]
}

// TestPanicOnDuplicateSortKeyScansDuplicateGroups pins the property the scan actually
// rests on for duplicate groups LARGER than a pair. Sortedness makes each group
// contiguous; it does NOT make every duplicate pair adjacent, so in a run of three the
// outer two are never compared. The scan is complete anyway because a run holding two
// distinct functions must hold an adjacent distinct pair. The teeth are the trailing_
// distinct case: its only distinct adjacency is (1,2), so a scan weakened to compare
// just nodes[0] against nodes[1] misses it and the test fails. all_same is the negative
// direction — a legitimately equal run of three must not panic.
func TestPanicOnDuplicateSortKeyScansDuplicateGroups(t *testing.T) {
	left, right := collidingSortKeyPair(t)
	fqn := left.RelString(nil)
	node := func(fn *ssa.Function) *Node { return &Node{FQN: fqn, Func: fn} }

	tests := []struct {
		name      string
		nodes     []*Node
		wantPanic bool
	}{
		{
			name:      "trailing_distinct",
			nodes:     []*Node{node(left), node(left), node(right)},
			wantPanic: true,
		},
		{
			name:      "leading_distinct",
			nodes:     []*Node{node(left), node(right), node(right)},
			wantPanic: true,
		},
		{
			name:      "outer_distinct",
			nodes:     []*Node{node(left), node(right), node(left)},
			wantPanic: true,
		},
		{
			name:      "all_same",
			nodes:     []*Node{node(left), node(left), node(left)},
			wantPanic: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Every node shares one key, so any permutation is a legal sorted slice;
			// assert it rather than assume it, since the scan's contract requires it.
			if !sort.SliceIsSorted(test.nodes, func(i, j int) bool {
				if test.nodes[i].FQN != test.nodes[j].FQN {
					return test.nodes[i].FQN < test.nodes[j].FQN
				}
				return features.InstanceDiscriminator(test.nodes[i].Func) <
					features.InstanceDiscriminator(test.nodes[j].Func)
			}) {
				t.Fatalf("group is not in finalize's sorted order")
			}
			defer func() {
				got := recover()
				if !test.wantPanic {
					if got != nil {
						t.Fatalf("panicOnDuplicateSortKey panicked on a legal group of three: %v", got)
					}
					return
				}
				if got == nil {
					t.Fatal("panicOnDuplicateSortKey accepted a group of three holding two distinct functions")
				}
				assertResidualDiagnostic(t, fmt.Sprint(got))
			}()
			panicOnDuplicateSortKey(test.nodes)
		})
	}
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
		assertResidualDiagnostic(t, fmt.Sprint(got))
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

// assertResidualDiagnostic requires the DISCLOSED residual wording, not merely
// "something panicked". A refusal here lands on a VALID Go program, so the
// message is the only thing standing between a user and the conclusion that the
// tool is broken: it must name the refused FQN, name the single declaration both
// instances were produced from, say plainly that this is a disclosed limit rather
// than a crash, and list what the user can change.
//
// It also requires the generic unknown-class wording to be ABSENT: falling back
// to "share sort key" here would mean the guard stopped recognizing the class it
// documents.
func assertResidualDiagnostic(t *testing.T, message string) {
	t.Helper()
	for _, want := range []string{
		"callgraph: refusing to order two distinct instances of\n    example.com/localtypes.sink[example.com/localtypes.L]",
		"WHY: both instances were produced from ONE declaration of the function-local\ntype \"L\" at local.go:",
		"This is a DISCLOSED LIMIT of the function-local type discriminator, not a crash",
		"WHAT YOU CAN DO — any one of:",
		"give the local type a structure that depends on the enclosing type",
		"move the type declaration out of the generic function",
		"instantiate the enclosing generic function only once",
		"this is an UNKNOWN collision class,\nnot the disclosed one",
		"sort key:      example.com/localtypes.sink[example.com/localtypes.L]",
		"discriminator: ",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("residual diagnostic is missing %q\n--- got ---\n%s", want, message)
		}
	}
	if strings.Contains(message, "share sort key") {
		t.Errorf("residual collision fell back to the unknown-class wording:\n%s", message)
	}
	// The discriminator is rendered with %q, so its NUL frames appear escaped; the
	// marker is matched in that escaped form rather than as raw bytes.
	if !strings.Contains(message, `local-type-graph/v1`) {
		t.Errorf("residual diagnostic does not quote the colliding discriminator:\n%s", message)
	}
}

// TestResidualDiagnosticFillsPlaceholdersInOrder pins the ratified placeholder
// ORDER — FQN, local type name, site, FQN, discriminator — which a substring
// check alone cannot catch: swapping the name and the site, or the two FQN slots,
// still leaves every phrase present.
func TestResidualDiagnosticFillsPlaceholdersInOrder(t *testing.T) {
	left, _ := collidingSortKeyPair(t)
	fqn := left.RelString(nil)
	key := features.InstanceDiscriminator(left)
	name, site, ok := features.FirstLocalDeclaration(left)
	if !ok {
		t.Fatal("fixture instance carries no function-local declaration")
	}
	if name != "L" || !strings.HasPrefix(site, "local.go:") {
		t.Fatalf("first local declaration = %q at %q, want L at local.go:<offset>", name, site)
	}

	want := fmt.Sprintf(residualCollisionDiagnostic, fqn, name, site, fqn, key)
	if got := duplicateSortKeyDiagnostic(fqn, key, left); got != want {
		t.Fatalf("duplicateSortKeyDiagnostic() =\n%s\n\nwant\n%s", got, want)
	}
}

// TestDuplicateSortKeyDiagnosticKeepsUnknownClassWording is the negative path: a
// surviving duplicate whose discriminator does NOT carry the local-type-graph
// suffix is a DIFFERENT, unknown class. Diagnosing it as the disclosed residual
// would assert a diagnosis the key cannot support.
func TestDuplicateSortKeyDiagnosticKeepsUnknownClassWording(t *testing.T) {
	left, _ := collidingSortKeyPair(t)
	got := duplicateSortKeyDiagnostic("example.com/localtypes.opaque", "", left)
	if !strings.Contains(got, "share sort key") {
		t.Errorf("suffix-less duplicate = %q, want the unknown-class wording", got)
	}
	if strings.Contains(got, "DISCLOSED LIMIT") {
		t.Errorf("suffix-less duplicate claims the disclosed residual class: %q", got)
	}
}
