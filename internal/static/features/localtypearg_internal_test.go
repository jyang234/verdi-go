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
	"time"

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
		if got := strings.Count(key, LocalTypeGraphMarker); got != 1 {
			t.Errorf("discriminator %q has %d local-type-graph suffixes, want 1", key, got)
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

// multiSiteLocalTypes reaches TWO distinct local declarations from one type
// argument: the local alias `alias`, and the recursive local named type `node`
// nested inside its RHS. Byte offsets in the pinned key below are offsets into
// this source, so the two must be edited together.
//
//	offset 66 -> `node` in `type node struct{ Next *node }`
//	offset 98 -> `alias` in `type alias = []node`
const multiSiteLocalTypes = `package localtypes
func instantiate[T any]() {}
func use() { type node struct{ Next *node }; type alias = []node; instantiate[alias]() }
`

// readFramed splits one `<decimal len>:<value>` token off the front of s,
// returning the value and the remainder. Length framing is what keeps a colon,
// comma, '>' or '@' inside a filename, package path, field name or struct tag
// from producing an ambiguous concatenation, so every framing is asserted here
// rather than assumed.
func readFramed(t *testing.T, s string) (value, rest string) {
	t.Helper()
	colon := strings.Index(s, ":")
	if colon < 0 {
		t.Fatalf("framed token %q has no length delimiter", s)
	}
	length, err := strconv.Atoi(s[:colon])
	if err != nil {
		t.Fatalf("framed token %q has a non-decimal length: %v", s, err)
	}
	if colon+1+length > len(s) {
		t.Fatalf("framed token %q declares length %d, but only %d bytes follow", s, length, len(s)-colon-1)
	}
	return s[colon+1 : colon+1+length], s[colon+1+length:]
}

// parseLocalTypeGraph unframes key's local-type-graph suffix into its root
// tokens (`R<id>`) and its definitions, indexed by node id. It asserts the
// framing, that ids are dense and ascending, and that every root names an
// existing node.
func parseLocalTypeGraph(t *testing.T, key string) (roots []string, defs []string) {
	t.Helper()
	index := strings.Index(key, LocalTypeGraphMarker)
	if index < 0 {
		t.Fatalf("discriminator %q has no local-type-graph suffix", key)
	}
	if strings.Count(key, LocalTypeGraphMarker) != 1 {
		t.Fatalf("discriminator %q has more than one local-type-graph suffix", key)
	}
	for _, framed := range strings.Split(key[index+len(LocalTypeGraphMarker):], "\x00") {
		token, rest := readFramed(t, framed)
		if rest != "" {
			t.Fatalf("framed token %q has %d trailing bytes", framed, len(rest))
		}
		switch {
		case strings.HasPrefix(token, "R"):
			if len(defs) != 0 {
				t.Fatalf("root token %q follows a definition; roots must come first", token)
			}
			roots = append(roots, token)
		case strings.HasPrefix(token, "#"):
			eq := strings.Index(token, "=")
			if eq < 0 {
				t.Fatalf("definition token %q has no '=' separator", token)
			}
			id, err := strconv.Atoi(token[1:eq])
			if err != nil {
				t.Fatalf("definition token %q has a non-decimal id: %v", token, err)
			}
			if id != len(defs) {
				t.Fatalf("definition token %q is out of order; want id %d", token, len(defs))
			}
			defs = append(defs, token[eq+1:])
		default:
			t.Fatalf("suffix token %q is neither a root nor a definition", token)
		}
	}
	for _, root := range roots {
		id, err := strconv.Atoi(root[1:])
		if err != nil || id < 0 || id >= len(defs) {
			t.Fatalf("root %q does not name an existing node (%d definitions)", root, len(defs))
		}
	}
	return roots, defs
}

// localDeclarationSites returns the @<site> labels the definitions carry, in
// ASCENDING NODE-ID ORDER — never sorted and never deduplicated. That order is
// the whole discrimination mechanism: it is what tells pair[outer, inner] from
// pair[inner, outer], so a test that sorted this would be testing the defect.
func localDeclarationSites(t *testing.T, defs []string) []string {
	t.Helper()
	var sites []string
	for _, def := range defs {
		body := ""
		switch {
		case strings.HasPrefix(def, "named:"):
			body = strings.TrimPrefix(def, "named:")
		case strings.HasPrefix(def, "alias:"):
			body = strings.TrimPrefix(def, "alias:")
		default:
			continue
		}
		_, rest := readFramed(t, body)
		if !strings.HasPrefix(rest, "@") {
			continue
		}
		site, _ := readFramed(t, rest[1:])
		sites = append(sites, site)
	}
	return sites
}

// declarationSitesOf is the whole-key shorthand the site assertions use.
func declarationSitesOf(t *testing.T, key string) []string {
	t.Helper()
	_, defs := parseLocalTypeGraph(t, key)
	return localDeclarationSites(t, defs)
}

// framed mirrors the encoder's length framing, derived here from the grammar in
// the design rather than from the production helper, so a change to that helper
// cannot make this test agree with itself.
func framed(s string) string { return strconv.Itoa(len(s)) + ":" + s }

// TestInstanceDiscriminatorEncodesTheReachableTypeGraph pins the suffix BYTE FOR
// BYTE on a fixture that reaches two distinct local declarations, a recursive
// named type, an alias, a slice, a struct and a pointer through one type
// argument. The expected definitions are written out from the design's grammar,
// not copied from a run: they are the specification, and the concatenation below
// is the only place the framing, the root section, the ascending id order and the
// dense id numbering are all pinned at once.
//
// Node 2 (the recursive `node`) is referenced from node 1 (the slice) and again
// from node 4 (the pointer inside its own underlying struct). That is the
// sharing: one definition, two references, and a back edge that terminates
// because node 2's id was reserved before its underlying type was visited.
func TestInstanceDiscriminatorEncodesTheReachableTypeGraph(t *testing.T) {
	instances := localInstances(t, buildLocalTypeProgram(t, "/checkout/root/local.go", multiSiteLocalTypes), "example.com/localtypes.instantiate[")
	if len(instances) != 1 {
		t.Fatalf("instantiate instances = %d, want 1", len(instances))
	}

	wantRoots := []string{"R0"}
	wantDefs := []string{
		"alias:" + framed("example.com/localtypes.alias") + "@" + framed("local.go:98") + ":::>1",
		"slice:>2",
		"named:" + framed("example.com/localtypes.node") + "@" + framed("local.go:66") + ":::>3",
		"struct:" + framed("Next") + ":0:" + framed("") + ">4",
		"ptr:>2",
	}

	want := "example.com/localtypes\x00example.com/localtypes.alias" + LocalTypeGraphMarker
	for i, root := range wantRoots {
		if i > 0 {
			want += "\x00"
		}
		want += framed(root)
	}
	for id, def := range wantDefs {
		want += "\x00" + framed("#"+strconv.Itoa(id)+"="+def)
	}

	got := InstanceDiscriminator(instances[0])
	if got != want {
		t.Fatalf("InstanceDiscriminator() =\n %q\nwant\n %q", got, want)
	}

	roots, defs := parseLocalTypeGraph(t, got)
	if strings.Join(roots, "|") != strings.Join(wantRoots, "|") {
		t.Errorf("roots = %v, want %v", roots, wantRoots)
	}
	if strings.Join(defs, "|") != strings.Join(wantDefs, "|") {
		t.Errorf("definitions = %v, want %v", defs, wantDefs)
	}

	sites := localDeclarationSites(t, defs)
	if strings.Join(sites, "|") != "local.go:98|local.go:66" {
		t.Fatalf("declaration sites = %v, want the alias (id 0) before the named type (id 2) in ID order, NOT sorted", sites)
	}

	for call := 0; call < 20; call++ {
		if repeat := InstanceDiscriminator(instances[0]); repeat != want {
			t.Fatalf("call %d discriminator = %q, want %q", call, repeat, want)
		}
	}
}

// TestInstanceDiscriminatorRecordsOneDeclarationAtEveryPosition pins the absence
// of site deduplication against a genuine duplicate: box[int] and box[string] are
// separate *types.Named values sharing one *types.TypeName, so both are reached
// through distinct types and the id map cannot mask one. Reaching one local type
// twice through the identical types.Type (map[inner]inner) would resolve to the
// same id and prove nothing.
//
// Under the retired sorted, deduplicated site set this fixture yielded ONE site.
// Collapsing it is exactly the defect: a set records which declarations exist and
// never which one sits in which role.
func TestInstanceDiscriminatorRecordsOneDeclarationAtEveryPosition(t *testing.T) {
	const src = `package localtypes
func instantiate[T any]() {}
func use() { type box[T any] struct{ V T }; instantiate[struct{ A box[int]; B box[string] }]() }
`
	instances := localInstances(t, buildLocalTypeProgram(t, "/checkout/root/local.go", src), "example.com/localtypes.instantiate[")
	if len(instances) != 1 {
		t.Fatalf("instantiate instances = %d, want 1", len(instances))
	}

	args := instances[0].TypeArgs()
	if len(args) != 1 {
		t.Fatalf("type arguments = %d, want 1", len(args))
	}
	fields, ok := args[0].Underlying().(*types.Struct)
	if !ok || fields.NumFields() != 2 {
		t.Fatalf("type argument underlying = %v, want a two-field struct", args[0].Underlying())
	}
	left, leftOK := fields.Field(0).Type().(*types.Named)
	right, rightOK := fields.Field(1).Type().(*types.Named)
	if !leftOK || !rightOK {
		t.Fatalf("struct fields = %v and %v, want two *types.Named instantiations", fields.Field(0).Type(), fields.Field(1).Type())
	}
	if left == right {
		t.Fatal("fixture reached one declaration through a single types.Type; the cycle guard, not the site set, would deduplicate")
	}
	if left.Obj() != right.Obj() {
		t.Fatalf("instantiations resolve to different TypeNames %v and %v, want one shared declaration", left.Obj(), right.Obj())
	}

	sites := declarationSitesOf(t, InstanceDiscriminator(instances[0]))
	if len(sites) != 2 {
		t.Fatalf("declaration sites = %v, want box's one declaration recorded at BOTH instantiation positions", sites)
	}
	if sites[0] != sites[1] {
		t.Errorf("declaration sites = %v, want two references to one declaration", sites)
	}
	if !strings.HasPrefix(sites[0], "local.go:") {
		t.Errorf("declaration site = %q, want the physical declaration of box", sites[0])
	}
}

// genericDeclaredLocalTypes is the `n1` witness: the local `L` is declared INSIDE
// a generic function and is reached only after instantiation, which is when
// go/types re-creates its TypeName with a nil Parent(). Under the retired
// `obj.Parent() != nil` conjunct both sink[L] instances were misclassified as
// package-scope, contributed no site at all, and collided on a bare prefix.
const genericDeclaredLocalTypes = `package localtypes
func sink[T any](v T) {}
func genA[X any](x X) { type L struct{ A int }; sink(L{}) }
func genB[X any](x X) { type L struct{ A int }; sink(L{}) }
func main() { genA(1); genB("s") }
`

// Note on the instance count: ssautil.AllFunctions reaches sink[L] twice per
// generic — once from the UNINSTANTIATED body of genA/genB (where L is the
// unsubstituted local) and once from the instantiated body. Both forms share one
// declaration, hence one key. Only the instantiated form is reachable from main,
// so a call graph holds two nodes, not four; this test therefore asserts over the
// distinct KEY set rather than over the raw function count.
func TestInstanceDiscriminatorCollectsLocalDeclaredInGenericFunction(t *testing.T) {
	instances := localInstances(t, buildLocalTypeProgram(t, "/checkout/root/local.go", genericDeclaredLocalTypes), "example.com/localtypes.sink[")
	if len(instances) < 2 {
		t.Fatalf("sink instances = %d, want at least 2", len(instances))
	}

	fqn := instances[0].RelString(nil)
	keys := make(map[string]bool)
	for _, fn := range instances {
		if got := fn.RelString(nil); got != fqn {
			t.Fatalf("display FQNs differ: %q and %q", got, fqn)
		}
		key := InstanceDiscriminator(fn)
		if !strings.Contains(key, LocalTypeGraphMarker) {
			t.Errorf("discriminator %q has no local-declaration suffix; the nil-Parent local was misclassified as package scope", key)
		}
		keys[key] = true
	}
	if len(keys) != 2 {
		t.Fatalf("locals declared in two different generic functions produced %d distinct discriminators, want 2: %v", len(keys), keys)
	}
}

// TestLocalNessPredicateAcceptsNilParent pins the predicate arm itself: a
// TypeName with a nil Parent() (what go/types hands back for an instantiated
// local) must be treated as function-local, not as package scope.
func TestLocalNessPredicateAcceptsNilParent(t *testing.T) {
	fn := identityTestFunction(t)
	pkg := effectivePkg(fn)
	file := fn.Prog.Fset.File(fn.Origin().Pos())
	if file == nil {
		t.Fatal("fixture origin has no physical token file")
	}
	pos := file.Pos(7)
	obj := types.NewTypeName(pos, pkg, "nilParentLocal", nil)
	if obj.Parent() != nil {
		t.Fatalf("fixture object has Parent() = %v, want nil", obj.Parent())
	}
	root := types.NewNamed(obj, types.Typ[types.Int], nil)

	sites := declarationSitesOf(t, localTypeGraph(fn, []types.Type{root}))
	if len(sites) != 1 || sites[0] != filepath.Base(file.Name())+":7" {
		t.Fatalf("declaration sites = %v, want one site for the nil-Parent local", sites)
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
		if got := strings.Count(key, LocalTypeGraphMarker); got != 1 {
			t.Errorf("discriminator %q has %d local-type-graph suffixes, want 1", key, got)
		}
	}
}

func TestInstanceDiscriminatorCollectsLocalAliasObject(t *testing.T) {
	const src = `package localtypes
func instantiate[T any]() {}
func use() { type result = int; instantiate[result]() }
`
	instances := localInstances(t, buildLocalTypeProgram(t, "/checkout/root/local.go", src), "example.com/localtypes.instantiate[")
	if len(instances) != 1 {
		t.Fatalf("instantiate instances = %d, want 1", len(instances))
	}
	args := instances[0].TypeArgs()
	if len(args) != 1 {
		t.Fatalf("type arguments = %d, want 1", len(args))
	}
	if _, ok := args[0].(*types.Alias); !ok {
		t.Fatalf("type argument = %T, want *types.Alias", args[0])
	}
	sites := declarationSitesOf(t, localTypeGraph(instances[0], args))
	if len(sites) != 1 || !strings.HasPrefix(sites[0], "local.go:") {
		t.Fatalf("declaration sites = %v, want one local alias site", sites)
	}
}

func TestInstanceDiscriminatorCollectsRecursiveLocalNamedType(t *testing.T) {
	const src = `package localtypes
func instantiate[T any]() {}
func use() { type node struct{ Next *node }; instantiate[node]() }
`
	instances := localInstances(t, buildLocalTypeProgram(t, "/checkout/root/local.go", src), "example.com/localtypes.instantiate[")
	if len(instances) != 1 {
		t.Fatalf("instantiate instances = %d, want 1", len(instances))
	}
	args := instances[0].TypeArgs()
	if len(args) != 1 {
		t.Fatalf("type arguments = %d, want 1", len(args))
	}
	named, ok := args[0].(*types.Named)
	if !ok {
		t.Fatalf("type argument = %T, want *types.Named", args[0])
	}
	underlying, ok := named.Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("recursive type underlying = %T, want *types.Struct", named.Underlying())
	}
	if underlying.NumFields() != 1 {
		t.Fatalf("recursive type fields = %d, want 1", underlying.NumFields())
	}
	next, ok := underlying.Field(0).Type().(*types.Pointer)
	if !ok || next.Elem() != named {
		t.Fatalf("recursive field type = %v, want pointer to the local named type", underlying.Field(0).Type())
	}

	key := InstanceDiscriminator(instances[0])
	if got := strings.Count(key, LocalTypeGraphMarker); got != 1 {
		t.Fatalf("recursive local discriminator %q has %d local-type-graph suffixes, want 1", key, got)
	}
	if !strings.Contains(key, "local.go:") {
		t.Fatalf("recursive local discriminator %q does not contain its physical declaration site", key)
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

// packageTypeName returns a TypeName that is GENUINELY package-scope: it is
// inserted into the package's own scope, so obj.Parent() == obj.Pkg().Scope() and
// the local-ness predicate rejects it. Constructing it without the insert leaves
// Parent() nil, which the corrected predicate reads as "not package scope" —
// correctly, since that is exactly the shape go/types gives an instantiated local
// (witness n1). The insert is what makes these table roots the package-scope
// controls they are meant to be.
func packageTypeName(fn *ssa.Function, name string) *types.TypeName {
	pkg := effectivePkg(fn)
	obj := types.NewTypeName(token.NoPos, pkg, name, nil)
	if alt := pkg.Scope().Insert(obj); alt != nil {
		panic(fmt.Sprintf("test package scope already declares %q as %v", name, alt))
	}
	return obj
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
			name:             "alias_type_parameter",
			targetUnderlying: targetInterface,
			root: func(_ *testing.T, target *types.Named) types.Type {
				param := types.NewTypeParam(packageTypeName(fn, "AliasParam"), target)
				origin := types.NewAlias(packageTypeName(fn, "aliasTypeParamRoot"), types.Typ[types.Int])
				origin.SetTypeParams([]*types.TypeParam{param})
				return origin
			},
		},
		{
			name:             "alias_type_argument",
			targetUnderlying: targetInt,
			root: func(t *testing.T, target *types.Named) types.Type {
				t.Helper()
				param := types.NewTypeParam(packageTypeName(fn, "AliasArg"), emptyIdentityInterface())
				origin := types.NewAlias(packageTypeName(fn, "aliasTypeArgRoot"), types.Typ[types.Int])
				origin.SetTypeParams([]*types.TypeParam{param})
				instance, err := types.Instantiate(nil, origin, []types.Type{target}, false)
				if err != nil {
					t.Fatalf("instantiate alias root: %v", err)
				}
				return instance
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
				method := types.NewFunc(token.NoPos, pkg, "Use", types.NewSignatureType(nil, nil, nil, params, nil, false))
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
			name:             "named_type_parameter",
			targetUnderlying: targetInterface,
			root: func(_ *testing.T, target *types.Named) types.Type {
				param := types.NewTypeParam(packageTypeName(fn, "NamedParam"), target)
				origin := types.NewNamed(packageTypeName(fn, "namedTypeParamRoot"), types.NewStruct(nil, nil), nil)
				origin.SetTypeParams([]*types.TypeParam{param})
				return origin
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
				return types.NewSignatureType(recv, nil, nil, nil, nil, false)
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
				return types.NewSignatureType(nil, nil, nil, params, nil, false)
			},
		},
		{
			name:             "signature_result",
			targetUnderlying: targetInt,
			root: func(_ *testing.T, target *types.Named) types.Type {
				results := types.NewTuple(types.NewVar(token.NoPos, pkg, "", target))
				return types.NewSignatureType(nil, nil, nil, nil, results, false)
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
			got := declarationSitesOf(t, localTypeGraph(fn, []types.Type{test.root(t, target)}))
			if len(got) != 1 || got[0] != want {
				t.Fatalf("declaration sites = %v, want [%q]", got, want)
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
	const wantGuardPanic = "features: local type identity requires an SSA file set"
	t.Run("nil_function", func(t *testing.T) {
		assertIdentityPanic(t, wantGuardPanic, func() {
			localTypeGraph(nil, nil)
		})
	})
	t.Run("nil_program", func(t *testing.T) {
		assertIdentityPanic(t, wantGuardPanic, func() {
			localTypeGraph(&ssa.Function{}, nil)
		})
	})
	t.Run("nil_file_set", func(t *testing.T) {
		assertIdentityPanic(t, wantGuardPanic, func() {
			localTypeGraph(&ssa.Function{Prog: ssa.NewProgram(nil, 0)}, nil)
		})
	})

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
			localTypeGraph(fn, []types.Type{root})
		})
	})

	t.Run("unsupported_type", func(t *testing.T) {
		assertIdentityPanic(t, "features: unsupported go/types implementation features.unsupportedIdentityType in local type identity", func() {
			localTypeGraph(fn, []types.Type{unsupportedIdentityType{}})
		})
	})
}

// TestInstanceDiscriminatorRepeatable covers both the colliding single-site
// source and the multi-site source, so the rebuild loop also exercises the
// suffix's intra-key ordering and not just its per-instance stability.
func TestInstanceDiscriminatorRepeatable(t *testing.T) {
	for _, src := range []struct {
		name   string
		source string
	}{
		{name: "single_site_per_instance", source: collidingLocalTypes},
		{name: "multiple_sites_per_instance", source: multiSiteLocalTypes},
	} {
		t.Run(src.name, func(t *testing.T) {
			instances := localInstances(t, buildLocalTypeProgram(t, "/checkout/root/local.go", src.source), "example.com/localtypes.instantiate[")
			want := make([]string, len(instances))
			for i, fn := range instances {
				want[i] = InstanceDiscriminator(fn)
				for call := 0; call < 20; call++ {
					if got := InstanceDiscriminator(fn); got != want[i] {
						t.Fatalf("same-instance call %d discriminator = %q, want %q", call, got, want[i])
					}
				}
			}

			for run := 0; run < 20; run++ {
				rebuilt := localInstances(t, buildLocalTypeProgram(t, "/checkout/root/local.go", src.source), "example.com/localtypes.instantiate[")
				got := make([]string, len(rebuilt))
				for i, fn := range rebuilt {
					got[i] = InstanceDiscriminator(fn)
				}
				if strings.Join(got, "\n") != strings.Join(want, "\n") {
					t.Errorf("run %d discriminators = %q, want %q", run, got, want)
				}
			}
		})
	}
}

// promotedLocalReceivers is the `n2` witness at unit scale: two same-named
// function-local `result` types embedding DIFFERENT named types, reached through
// an interface so go/ssa builds a promotion wrapper for each. The wrappers carry
// no type arguments — so the discriminator used to be "" for both — and they
// carry a receiver, which callgraph.mergeKey deliberately refuses to merge. Both
// the pointer-receiver and the value-receiver wrapper form are exercised.
//
// pkgRecv is the Goal-2 control: a method whose receiver reaches no function-local
// declaration must keep the empty discriminator it had before the receiver joined
// the root set.
const promotedLocalReceivers = `package localtypes
type ifc interface{ QueryContext() string }
type embA struct{}
func (embA) QueryContext() string { return "A" }
type embB struct{}
func (embB) QueryContext() string { return "B" }
type pkgRecv struct{ embA }
func firstPointer() ifc { type result struct{ embA }; return &result{} }
func secondPointer() ifc { type result struct{ embB }; return &result{} }
func firstValue() ifc { type result struct{ embA }; return result{} }
func secondValue() ifc { type result struct{ embB }; return result{} }
func usePkg() ifc { return pkgRecv{} }
func main() { _ = firstPointer(); _ = secondPointer(); _ = firstValue(); _ = secondValue(); _ = usePkg() }
`

// functionsWithFQN returns every SSA function whose display FQN is exactly fqn.
func functionsWithFQN(prog *ssa.Program, fqn string) []*ssa.Function {
	var got []*ssa.Function
	for fn := range ssautil.AllFunctions(prog) {
		if fn != nil && fn.RelString(nil) == fqn {
			got = append(got, fn)
		}
	}
	sort.Slice(got, func(i, j int) bool {
		return InstanceDiscriminator(got[i]) < InstanceDiscriminator(got[j])
	})
	return got
}

func TestInstanceDiscriminatorSeparatesPromotedLocalReceiverWrappers(t *testing.T) {
	prog := buildLocalTypeProgram(t, "/checkout/root/local.go", promotedLocalReceivers)
	for _, form := range []struct {
		name string
		fqn  string
	}{
		{name: "pointer_receiver", fqn: "(*example.com/localtypes.result).QueryContext"},
		{name: "value_receiver", fqn: "(example.com/localtypes.result).QueryContext"},
	} {
		t.Run(form.name, func(t *testing.T) {
			// Four, not two: each of the fixture's four functions declares its
			// own local `result`, and go/ssa builds both wrapper forms for each.
			// All four are distinct receivers behind ONE display FQN.
			wrappers := functionsWithFQN(prog, form.fqn)
			if len(wrappers) != 4 {
				t.Fatalf("wrappers for %q = %d, want 4", form.fqn, len(wrappers))
			}
			keys := make(map[string]bool, len(wrappers))
			for _, fn := range wrappers {
				if len(fn.TypeArgs()) != 0 {
					t.Fatalf("wrapper %v carries type arguments; the fixture no longer exercises the empty-discriminator path", fn)
				}
				key := InstanceDiscriminator(fn)
				if key == "" {
					t.Fatalf("wrapper %v has an empty discriminator; the receiver did not reach its local declaration", fn)
				}
				if !HasLocalTypeGraph(key) {
					t.Errorf("wrapper discriminator %q carries no local-type-graph suffix", key)
				}
				keys[key] = true
			}
			if len(keys) != len(wrappers) {
				t.Fatalf("%d distinct promotion wrappers produced %d discriminators: %v", len(wrappers), len(keys), keys)
			}
		})
	}
}

// TestInstanceDiscriminatorLeavesNonLocalReceiverEmpty is the Goal-2 guard for
// wrapper discrimination: adding the receiver to the root set must change the key
// of wrappers over LOCAL receivers only, and only from "" to something. Every
// other method keeps the empty discriminator it had before.
func TestInstanceDiscriminatorLeavesNonLocalReceiverEmpty(t *testing.T) {
	prog := buildLocalTypeProgram(t, "/checkout/root/local.go", promotedLocalReceivers)
	for _, fqn := range []string{
		"(example.com/localtypes.pkgRecv).QueryContext",
		"(example.com/localtypes.embA).QueryContext",
	} {
		methods := functionsWithFQN(prog, fqn)
		if len(methods) != 1 {
			t.Fatalf("functions for %q = %d, want 1", fqn, len(methods))
		}
		if got := InstanceDiscriminator(methods[0]); got != "" {
			t.Errorf("package-scope receiver %q discriminator = %q, want \"\"", fqn, got)
		}
	}
}

// genericVaryingLocalTypes is the `n1c` witness: ONE declaration of L inside ONE
// generic function, whose structure varies with the enclosing type parameter. The
// two sink[L] instances share a source position, so only structure can separate
// them — and only if a Basic node carries its NAME. A definition of "kind plus
// children by id" encodes basic:int and basic:string identically, and this test is
// what fails in that case.
const genericVaryingLocalTypes = `package localtypes
func sink[T any](v T) {}
func gen[X any](x X) { type L struct{ A X }; sink(L{A: x}) }
func main() { gen(1); gen("s") }
`

func TestInstanceDiscriminatorSeparatesStructurallyVaryingLocalType(t *testing.T) {
	instances := localInstances(t, buildLocalTypeProgram(t, "/checkout/root/local.go", genericVaryingLocalTypes), "example.com/localtypes.sink[")
	keys := make(map[string]bool)
	sites := make(map[string]bool)
	basics := make(map[string]bool)
	for _, fn := range instances {
		key := InstanceDiscriminator(fn)
		keys[key] = true
		_, defs := parseLocalTypeGraph(t, key)
		for _, site := range localDeclarationSites(t, defs) {
			sites[site] = true
		}
		for _, def := range defs {
			if strings.HasPrefix(def, "basic:") {
				basics[def] = true
			}
		}
	}
	if len(sites) != 1 {
		t.Fatalf("declaration sites = %v, want ONE declaration shared by every instantiation", sites)
	}
	if len(keys) < 2 {
		t.Fatalf("structurally varying instantiations produced %d distinct discriminators, want at least 2: %v", len(keys), keys)
	}
	if !basics["basic:"+framed("int")] || !basics["basic:"+framed("string")] {
		t.Fatalf("basic definitions = %v, want both the int and the string name", basics)
	}
}

// sharedDAGRoot builds Tk = struct{A T(k+1); B T(k+1)} down to the given depth,
// with BOTH fields referencing the identical *types.Named so the graph is a shared
// DAG rather than a tree. A fully-expanded encoding of this shape is exponential
// in OUTPUT SIZE — tens of megabytes at depth 20, which is a denial of service,
// not a sort key — so the size bound below is the real assertion.
func sharedDAGRoot(t *testing.T, fn *ssa.Function, depth int) types.Type {
	t.Helper()
	var child types.Type = types.Typ[types.Int]
	for k := depth; k >= 0; k-- {
		fields := []*types.Var{
			types.NewVar(token.NoPos, effectivePkg(fn), "A", child),
			types.NewVar(token.NoPos, effectivePkg(fn), "B", child),
		}
		named, _ := newLocalNamedIdentityType(t, fn, "dag"+strconv.Itoa(k), 10+k, types.NewStruct(fields, nil))
		child = named
	}
	return child
}

func TestLocalTypeGraphEncodesSharedDAGInBoundedSize(t *testing.T) {
	fn := identityTestFunction(t)
	const depth = 20
	root := sharedDAGRoot(t, fn, depth)

	start := time.Now()
	got := localTypeGraph(fn, []types.Type{root})
	elapsed := time.Since(start)

	if len(got) >= 4<<10 {
		t.Fatalf("depth-%d shared DAG encoded to %d bytes, want under 4 KiB; the encoding is not sharing nodes", depth, len(got))
	}
	// The size bound above is what proves the encoding is linear in DISTINCT
	// nodes. This wall-clock bound is deliberately loose — it is a smoke guard
	// against a pathological regression on a loaded machine, not a benchmark. A
	// fully-expanded walk of this shape takes hundreds of milliseconds AND blows
	// the size bound, so the size assertion is the one with teeth.
	if elapsed > 250*time.Millisecond {
		t.Errorf("depth-%d shared DAG took %v to encode", depth, elapsed)
	}
	if repeat := localTypeGraph(fn, []types.Type{root}); repeat != got {
		t.Fatal("repeated encodings of one shared DAG differ")
	}
}

// TestLocalTypeGraphFailsClosedOverBudget pins the byte budget. Exceeding it must
// panic with a deterministic diagnostic and never truncate: a truncated key would
// silently merge two distinct functions, which is the one outcome worse than a
// refused analysis.
func TestLocalTypeGraphFailsClosedOverBudget(t *testing.T) {
	fn := identityTestFunction(t)
	fields := make([]*types.Var, 0, 3000)
	name := strings.Repeat("f", 400)
	for i := 0; i < 3000; i++ {
		fields = append(fields, types.NewVar(token.NoPos, effectivePkg(fn), name+strconv.Itoa(i), types.Typ[types.Int]))
	}
	root := types.NewStruct(fields, nil)

	want := fmt.Sprintf(
		"features: local type graph for %q exceeds the %d-byte budget",
		fn.RelString(nil), localTypeGraphBudget,
	)
	assertIdentityPanic(t, want, func() {
		localTypeGraph(fn, []types.Type{root})
	})
}
