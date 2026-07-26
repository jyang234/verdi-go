package callgraph

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"

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
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "local.go", collidingLocalTypeArgsSrc, 0)
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
