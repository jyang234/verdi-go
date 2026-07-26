package features

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

func buildLocalTypeProgram(t *testing.T, filename, src string) *ssa.Program {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{
		Types:        make(map[ast.Expr]types.TypeAndValue),
		Defs:         make(map[*ast.Ident]types.Object),
		Uses:         make(map[*ast.Ident]types.Object),
		Implicits:    make(map[ast.Node]types.Object),
		Instances:    make(map[*ast.Ident]types.Instance),
		Selections:   make(map[*ast.SelectorExpr]*types.Selection),
		Scopes:       make(map[ast.Node]*types.Scope),
		FileVersions: make(map[*ast.File]string),
	}
	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check("example.com/localtypes", fset, []*ast.File{f}, info)
	if err != nil {
		t.Fatalf("type-check: %v", err)
	}
	prog := ssa.NewProgram(fset, ssa.InstantiateGenerics)
	ssapkg := prog.CreatePackage(pkg, []*ast.File{f}, info, true)
	ssapkg.Build()
	return prog
}

func localInstances(t *testing.T, prog *ssa.Program, prefix string) []*ssa.Function {
	t.Helper()
	var got []*ssa.Function
	for fn := range ssautil.AllFunctions(prog) {
		if fn != nil && fn.Origin() != nil && strings.HasPrefix(fn.RelString(nil), prefix) {
			got = append(got, fn)
		}
	}
	sort.Slice(got, func(i, j int) bool {
		return InstanceDiscriminator(got[i]) < InstanceDiscriminator(got[j])
	})
	return got
}

const collidingLocalTypes = `package localtypes
func instantiate[T any]() {}
func first() { type result struct{ A int }; instantiate[result]() }
func second() { type result struct{ A int }; instantiate[result]() }
func third() { type result struct{ B string }; instantiate[result]() }
func main() { first(); second(); third() }
`

func TestInstanceDiscriminatorSeparatesFunctionLocalTypes(t *testing.T) {
	prog := buildLocalTypeProgram(t, "/checkout/root/local.go", collidingLocalTypes)
	instances := localInstances(t, prog, "example.com/localtypes.instantiate[")
	if len(instances) != 3 {
		t.Fatalf("instantiate instances = %d, want 3", len(instances))
	}

	fqn := instances[0].RelString(nil)
	keys := make(map[string]bool, len(instances))
	for _, fn := range instances {
		if got := fn.RelString(nil); got != fqn {
			t.Errorf("display FQN = %q, want shared %q", got, fqn)
		}
		key := InstanceDiscriminator(fn)
		keys[key] = true
		if got := strings.Count(key, "\x00local-sites/v1\x00"); got != 1 {
			t.Errorf("discriminator %q has %d local-site suffixes, want 1", key, got)
		}
		if !strings.Contains(key, "local.go:") {
			t.Errorf("discriminator %q does not contain physical basename", key)
		}
		if strings.Contains(key, "/checkout/root") {
			t.Errorf("discriminator %q contains checkout directory", key)
		}
	}
	if len(keys) != len(instances) {
		t.Errorf("distinct local result instances have %d discriminators, want %d: %v", len(keys), len(instances), keys)
	}
}

func TestInstanceDiscriminatorPackageTypeBytesUnchanged(t *testing.T) {
	const src = `package localtypes
type result struct{ A int }
func instantiate[T any]() {}
func use() { instantiate[result]() }
`
	prog := buildLocalTypeProgram(t, "/checkout/root/local.go", src)
	instances := localInstances(t, prog, "example.com/localtypes.instantiate[")
	if len(instances) != 1 {
		t.Fatalf("instantiate instances = %d, want 1", len(instances))
	}
	if got, want := InstanceDiscriminator(instances[0]), "example.com/localtypes\x00example.com/localtypes.result"; got != want {
		t.Errorf("InstanceDiscriminator() = %q, want %q", got, want)
	}
}

func TestInstanceDiscriminatorIgnoresCheckoutRoot(t *testing.T) {
	left := localInstances(t, buildLocalTypeProgram(t, "/checkout/one/local.go", collidingLocalTypes), "example.com/localtypes.instantiate[")
	right := localInstances(t, buildLocalTypeProgram(t, "/another/root/local.go", collidingLocalTypes), "example.com/localtypes.instantiate[")
	if len(left) != len(right) {
		t.Fatalf("instance counts = %d and %d, want equal", len(left), len(right))
	}
	for i := range left {
		if got, want := InstanceDiscriminator(left[i]), InstanceDiscriminator(right[i]); got != want {
			t.Errorf("key[%d] differs across checkout roots:\n got  %q\n want %q", i, got, want)
		}
	}
}

func TestInstanceDiscriminatorIgnoresLineDirective(t *testing.T) {
	const src = `package localtypes
func instantiate[T any]() {}
//line misleading-generated-name.go:900
func first() { type result struct{ A int }; instantiate[result]() }
func second() { type result struct{ B string }; instantiate[result]() }
`
	instances := localInstances(t, buildLocalTypeProgram(t, "/checkout/root/local.go", src), "example.com/localtypes.instantiate[")
	if len(instances) != 2 {
		t.Fatalf("instantiate instances = %d, want 2", len(instances))
	}
	for _, fn := range instances {
		key := InstanceDiscriminator(fn)
		if !strings.Contains(key, "local.go:") {
			t.Errorf("discriminator %q does not contain physical basename", key)
		}
		if strings.Contains(key, "misleading-generated-name.go") {
			t.Errorf("discriminator %q contains adjusted //line name", key)
		}
	}
}

func TestInstanceDiscriminatorWalksNestedLocalTypes(t *testing.T) {
	const src = `package localtypes
func instantiate[T any]() {}
func shapes() {
	type localAlias = int
	type localNamed[P interface{ localAlias | ~string }] struct {
		Sig func(P, []localAlias) *localNamed[P]
		Map map[localAlias][]*localNamed[P]
		Chan chan [2]*localNamed[P]
		Struct struct { Alias localAlias; Named *localNamed[P] }
		Interface interface { M(localAlias) *localNamed[P] }
	}
	instantiate[localNamed[localAlias]]()
}
`
	prog := buildLocalTypeProgram(t, "/checkout/root/local.go", src)
	instances := localInstances(t, prog, "example.com/localtypes.instantiate[")
	if len(instances) != 1 {
		t.Fatalf("instantiate instances = %d, want 1", len(instances))
	}
	key := InstanceDiscriminator(instances[0])
	if got := strings.Count(key, "local.go:"); got != 2 {
		t.Errorf("discriminator %q contains %d local declaration sites, want alias and named declaration once each", key, got)
	}
}

func TestInstanceDiscriminatorRepeatable(t *testing.T) {
	var want []string
	for run := 0; run < 20; run++ {
		instances := localInstances(t, buildLocalTypeProgram(t, "/checkout/root/local.go", collidingLocalTypes), "example.com/localtypes.instantiate[")
		got := make([]string, len(instances))
		for i, fn := range instances {
			got[i] = InstanceDiscriminator(fn)
		}
		if run == 0 {
			want = got
			continue
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("run %d discriminators = %q, want %q", run, got, want)
		}
	}
}
