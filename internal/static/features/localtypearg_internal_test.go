package features

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strconv"
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

func TestInstanceDiscriminatorSeparatesSameLineLocalTypes(t *testing.T) {
	const src = `package localtypes
func instantiate[T any]() {}
func use() { { type result struct{ A int }; instantiate[result]() }; { type result struct{ A int }; instantiate[result]() } }
`
	instances := localInstances(t, buildLocalTypeProgram(t, "/checkout/root/local.go", src), "example.com/localtypes.instantiate[")
	if len(instances) != 2 {
		t.Fatalf("instantiate instances = %d, want 2", len(instances))
	}
	if got, want := instances[0].RelString(nil), instances[1].RelString(nil); got != want {
		t.Fatalf("display FQNs differ: %q and %q", got, want)
	}
	left, right := InstanceDiscriminator(instances[0]), InstanceDiscriminator(instances[1])
	if left == right {
		t.Fatalf("same-line local declarations have equal discriminators %q", left)
	}
	for _, key := range []string{left, right} {
		if got := strings.Count(key, "\x00local-sites/v1\x00"); got != 1 {
			t.Errorf("discriminator %q has %d local-site suffixes, want 1", key, got)
		}
	}
}

func identityTestFunction(t *testing.T) *ssa.Function {
	t.Helper()
	const src = `package localtypes
func instantiate[T any]() {}
func use() { instantiate[int]() }
`
	instances := localInstances(t, buildLocalTypeProgram(t, "/checkout/root/local.go", src), "example.com/localtypes.instantiate[")
	if len(instances) != 1 {
		t.Fatalf("instantiate instances = %d, want 1", len(instances))
	}
	return instances[0]
}

func newLocalNamedIdentityType(t *testing.T, fn *ssa.Function, name string, offset int, underlying types.Type) (*types.Named, string) {
	t.Helper()
	file := fn.Prog.Fset.File(fn.Origin().Pos())
	if file == nil {
		t.Fatal("fixture origin has no physical token file")
	}
	if offset < 0 || offset >= file.Size() {
		t.Fatalf("fixture offset %d outside physical file size %d", offset, file.Size())
	}
	pos := file.Pos(offset)
	pkg := effectivePkg(fn)
	scope := types.NewScope(pkg.Scope(), pos, pos+1, "identity test local scope")
	obj := types.NewTypeName(pos, pkg, name, nil)
	if alt := scope.Insert(obj); alt != nil {
		t.Fatalf("insert local type %q collided with %v", name, alt)
	}
	return types.NewNamed(obj, underlying, nil), filepath.Base(file.Name()) + ":" + strconv.Itoa(offset)
}

func packageTypeName(fn *ssa.Function, name string) *types.TypeName {
	return types.NewTypeName(token.NoPos, effectivePkg(fn), name, nil)
}

func emptyIdentityInterface() *types.Interface {
	return types.NewInterfaceType(nil, nil).Complete()
}

func TestInstanceDiscriminatorWalksNestedLocalTypes(t *testing.T) {
	fn := identityTestFunction(t)
	pkg := effectivePkg(fn)
	targetInterface := func() types.Type { return emptyIdentityInterface() }
	targetInt := func() types.Type { return types.Typ[types.Int] }

	tests := []struct {
		name             string
		targetUnderlying func() types.Type
		root             func(*testing.T, *types.Named) types.Type
	}{
		{
			name:             "alias_RHS",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				return types.NewAlias(packageTypeName(fn, "aliasRoot"), target)
			},
		},
		{
			name:             "array_element",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				return types.NewArray(target, 2)
			},
		},
		{
			name:             "channel_element",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				return types.NewChan(types.SendRecv, target)
			},
		},
		{
			name:             "interface_explicit_method",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				params := types.NewTuple(types.NewVar(token.NoPos, pkg, "value", target))
				method := types.NewFunc(token.NoPos, pkg, "Use", types.NewSignature(nil, params, nil, false))
				return types.NewInterfaceType([]*types.Func{method}, nil).Complete()
			},
		},
		{
			name:             "interface_embedded_type",
			targetUnderlying: targetInterface,
			root: func(_ *testing.T, target *types.Named) types.Type {
				return types.NewInterfaceType(nil, []types.Type{target}).Complete()
			},
		},
		{
			name:             "map_key",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				return types.NewMap(target, types.Typ[types.String])
			},
		},
		{
			name:             "map_element",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				return types.NewMap(types.Typ[types.String], target)
			},
		},
		{
			name:             "named_underlying",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				field := types.NewVar(token.NoPos, pkg, "Value", target)
				return types.NewNamed(packageTypeName(fn, "namedUnderlyingRoot"), types.NewStruct([]*types.Var{field}, nil), nil)
			},
		},
		{
			name:             "named_type_argument",
			targetUnderlying: targetInt,
			root: func(t *testing.T, target *types.Named) types.Type {
				t.Helper()
				param := types.NewTypeParam(packageTypeName(fn, "NamedArg"), emptyIdentityInterface())
				origin := types.NewNamed(packageTypeName(fn, "namedArgRoot"), types.NewStruct(nil, nil), nil)
				origin.SetTypeParams([]*types.TypeParam{param})
				instance, err := types.Instantiate(nil, origin, []types.Type{target}, false)
				if err != nil {
					t.Fatalf("instantiate named root: %v", err)
				}
				return instance
			},
		},
		{
			name:             "pointer_element",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				return types.NewPointer(target)
			},
		},
		{
			name:             "signature_receiver",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				recv := types.NewVar(token.NoPos, pkg, "recv", target)
				return types.NewSignature(recv, nil, nil, false)
			},
		},
		{
			name:             "signature_receiver_type_parameter",
			targetUnderlying: targetInterface,
			root: func(_ *testing.T, target *types.Named) types.Type {
				recv := types.NewVar(token.NoPos, pkg, "recv", types.Typ[types.Int])
				param := types.NewTypeParam(packageTypeName(fn, "RecvParam"), target)
				return types.NewSignatureType(recv, []*types.TypeParam{param}, nil, nil, nil, false)
			},
		},
		{
			name:             "signature_type_parameter",
			targetUnderlying: targetInterface,
			root: func(_ *testing.T, target *types.Named) types.Type {
				param := types.NewTypeParam(packageTypeName(fn, "SigParam"), target)
				return types.NewSignatureType(nil, nil, []*types.TypeParam{param}, nil, nil, false)
			},
		},
		{
			name:             "signature_parameter",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				params := types.NewTuple(types.NewVar(token.NoPos, pkg, "value", target))
				return types.NewSignature(nil, params, nil, false)
			},
		},
		{
			name:             "signature_result",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				results := types.NewTuple(types.NewVar(token.NoPos, pkg, "", target))
				return types.NewSignature(nil, nil, results, false)
			},
		},
		{
			name:             "slice_element",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				return types.NewSlice(target)
			},
		},
		{
			name:             "struct_field",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				field := types.NewVar(token.NoPos, pkg, "Value", target)
				return types.NewStruct([]*types.Var{field}, nil)
			},
		},
		{
			name:             "tuple_element",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				return types.NewTuple(types.NewVar(token.NoPos, pkg, "value", target))
			},
		},
		{
			name:             "type_parameter_constraint",
			targetUnderlying: targetInterface,
			root: func(_ *testing.T, target *types.Named) types.Type {
				return types.NewTypeParam(packageTypeName(fn, "ConstraintParam"), target)
			},
		},
		{
			name:             "union_term",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				return types.NewUnion([]*types.Term{types.NewTerm(false, target)})
			},
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, want := newLocalNamedIdentityType(t, fn, "target"+strconv.Itoa(i), 10+i, test.targetUnderlying())
			got := localDeclaredTypeSites(fn, []types.Type{test.root(t, target)})
			if len(got) != 1 || got[0] != want {
				t.Fatalf("localDeclaredTypeSites() = %v, want [%q]", got, want)
			}
		})
	}
}

type unsupportedIdentityType struct{}

func (unsupportedIdentityType) Underlying() types.Type { return unsupportedIdentityType{} }
func (unsupportedIdentityType) String() string         { return "unsupportedIdentityType" }

func assertIdentityPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		got := recover()
		if got == nil {
			t.Fatalf("did not panic, want %q", want)
		}
		if message := fmt.Sprint(got); message != want {
			t.Fatalf("panic = %q, want %q", message, want)
		}
	}()
	f()
}

func TestInstanceDiscriminatorFailsClosedOnUntrustworthyLocalIdentity(t *testing.T) {
	fn := identityTestFunction(t)
	// The invalid-position check after File returns is a defensive invariant:
	// token.FileSet.File returns a file only when the position is already within
	// that file's [Base, Base+Size] range. No public token API can construct a
	// non-nil file that then fails the same bounds check, so the missing-file
	// case below pins the constructible invalid-position failure.
	t.Run("missing_physical_file", func(t *testing.T) {
		const name = "missingFile"
		pkg := effectivePkg(fn)
		pos := token.Pos(1 << 29)
		scope := types.NewScope(pkg.Scope(), pos, pos+1, "missing physical file")
		obj := types.NewTypeName(pos, pkg, name, nil)
		if alt := scope.Insert(obj); alt != nil {
			t.Fatalf("insert local type %q collided with %v", name, alt)
		}
		root := types.NewNamed(obj, types.Typ[types.Int], nil)
		assertIdentityPanic(t, `features: no physical token file for local type "missingFile"`, func() {
			localDeclaredTypeSites(fn, []types.Type{root})
		})
	})

	t.Run("unsupported_type", func(t *testing.T) {
		assertIdentityPanic(t, "features: unsupported go/types implementation features.unsupportedIdentityType in local type identity", func() {
			localDeclaredTypeSites(fn, []types.Type{unsupportedIdentityType{}})
		})
	})
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
