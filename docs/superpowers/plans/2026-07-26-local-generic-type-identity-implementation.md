# Function-local generic type identity implementation plan

> **Superseded in part on 2026-07-27.** The design this plan implements was
> revised — see "Revision 2026-07-27" in
> `docs/superpowers/specs/2026-07-26-local-generic-type-identity-design.md`. The
> `local-sites/v1` suffix this plan builds (a sorted, deduplicated site set) is
> retired and replaced by the positional `local-type-graph/v1` serialization; the
> local-ness predicate loses its `obj.Parent() != nil` conjunct; the root set
> gains the receiver. Where this plan and the revised spec disagree, **the spec
> wins**. The plan is retained as decision history, not as instructions.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make valid Go programs with distinct, same-rendering function-local generic type arguments produce deterministic call graphs without weakening the collision guard or changing package-level instance keys.

**Architecture:** Extend `features.InstanceDiscriminator` with a framed, portable suffix derived from every reachable function-local declared type's physical declaration site. Keep `callgraph.finalize` fail-closed on any surviving collision, and make `graphio.sortGraph` total over every serialized node field.

**Tech Stack:** Go 1.26, `go/types`, `go/token`, `golang.org/x/tools/go/ssa`, the existing static analyzer and canonical JSON encoder.

## Global Constraints

- Treat the approved design at `docs/superpowers/specs/2026-07-26-local-generic-type-identity-design.md` as normative.
- Preserve the current discriminator bytes exactly when no local declaration is reachable.
- Never put absolute checkout paths, adjusted `//line` positions, raw `token.Pos` values, pointer identities, map order, or arrival order into canonical output.
- A missing physical token file, invalid offset, or unknown future `go/types` implementation must panic with a deterministic diagnostic.
- Do not remove, catch, downgrade, or weaken the collision panic in `callgraph.finalize`.
- Keep display FQNs and the graph JSON schema unchanged.
- New ordering logic must compare intrinsic fields through a deterministic final tie.
- Use test-first changes and keep each commit independently buildable.

---

## Task 1: Add the local-declaration discriminator suffix

**Files:**

- Modify: `internal/static/features/features.go`
- Create: `internal/static/features/localtypearg_internal_test.go`

**Interfaces consumed:**

- `(*ssa.Function).TypeArgs() []types.Type`
- `(*ssa.Function).Prog.Fset`
- `(*token.FileSet).File(token.Pos) *token.File`
- `(*token.File).Offset(token.Pos) int`

**Interfaces produced:**

```go
func InstanceDiscriminator(fn *ssa.Function) string
func localDeclaredTypeSites(fn *ssa.Function, roots []types.Type) []string
```

The public behavior remains one discriminator string. The helper returns sorted,
deduplicated `<basename>:<physical-byte-offset>` strings and panics when it
cannot prove a trustworthy identity.

### Step 1: Write the failing feature tests

- [ ] Add an internal-package test fixture that builds SSA from an explicit physical filename. Do not reuse `buildInline` from `effect_internal_test.go`, because that helper fixes the filename and cannot exercise checkout-root or `//line` behavior.

```go
package features

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/ssa"
)

func buildLocalTypeProgram(t *testing.T, filename, src string) *ssa.Program {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, src, parser.AllErrors)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	conf := types.Config{Importer: importer.Default()}
	pkg, err := conf.Check("example.com/localtypes", fset, []*ast.File{f}, nil)
	if err != nil {
		t.Fatalf("type-check: %v", err)
	}
	prog := ssa.NewProgram(fset, ssa.InstantiateGenerics)
	ssapkg := prog.CreatePackage(pkg, []*ast.File{f}, nil, true)
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
```

Add the missing `sort` and `golang.org/x/tools/go/ssa/ssautil` imports. Use this
source in `TestInstanceDiscriminatorSeparatesFunctionLocalTypes`:

```go
const collidingLocalTypes = `package localtypes
func instantiate[T any]() {}
func first() { type result struct{ A int }; instantiate[result]() }
func second() { type result struct{ A int }; instantiate[result]() }
func third() { type result struct{ B string }; instantiate[result]() }
func main() { first(); second(); third() }
`
```

Assert that there are three `instantiate[...]` instances, all display FQNs are
equal, and all discriminators are distinct. Assert every discriminator contains
exactly one `"\x00local-sites/v1\x00"` suffix and that the suffix contains
`local.go:` but not the checkout directory.

Also add:

- `TestInstanceDiscriminatorPackageTypeBytesUnchanged`: compare the result for a
  package-level argument to the literal current-format key
  `"example.com/localtypes\x00example.com/localtypes.result"`.
- `TestInstanceDiscriminatorIgnoresCheckoutRoot`: build the same source as
  `/checkout/one/local.go` and `/another/root/local.go`; compare sorted keys.
- `TestInstanceDiscriminatorIgnoresLineDirective`: add
  `//line misleading-generated-name.go:900` before the local declarations and
  assert the keys still contain `local.go`, not the adjusted name.
- `TestInstanceDiscriminatorWalksNestedLocalTypes`: place a local alias and a
  recursive local named type under signature, map, slice, pointer, channel,
  array, struct, tuple, interface, type parameter, and union shapes; assert both
  sites appear once.
- `TestInstanceDiscriminatorRepeatable`: rebuild the source 20 times and compare
  the sorted keys byte-for-byte.

- [ ] Run the focused test and confirm it fails because the three current
discriminators collide:

```console
go test ./internal/static/features -run 'TestInstanceDiscriminator(Separates|Package|Ignores|Walks|Repeatable)' -count=1
```

Expected failure includes equal discriminator values for the local `result`
instances. The package-level compatibility test should already pass.

### Step 2: Implement the complete type walk and framing

- [ ] Add `fmt`, `sort`, and `strconv` imports if they are not already present.
Keep the existing `filepath`, `strings`, and `go/types` imports.

- [ ] Implement physical site collection. Use a `map[types.Type]bool` cycle
guard and a `map[string]bool` site set. This is the required traversal shape:

```go
func localDeclaredTypeSites(fn *ssa.Function, roots []types.Type) []string {
	if fn == nil || fn.Prog == nil || fn.Prog.Fset == nil {
		panic("features: local type identity requires an SSA file set")
	}
	seenTypes := make(map[types.Type]bool)
	seenSites := make(map[string]bool)

	addObject := func(obj *types.TypeName) {
		if obj == nil || obj.Pkg() == nil || obj.Parent() == nil ||
			obj.Parent() == obj.Pkg().Scope() {
			return
		}
		file := fn.Prog.Fset.File(obj.Pos())
		if file == nil {
			panic(fmt.Sprintf(
				"features: no physical token file for local type %q",
				obj.Name(),
			))
		}
		pos := int(obj.Pos())
		if pos < file.Base() || pos > file.Base()+file.Size() {
			panic(fmt.Sprintf(
				"features: invalid physical position for local type %q",
				obj.Name(),
			))
		}
		offset := file.Offset(obj.Pos())
		seenSites[filepath.Base(file.Name())+":"+strconv.Itoa(offset)] = true
	}

	var walk func(types.Type)
	walkTuple := func(tuple *types.Tuple) {
		if tuple == nil {
			return
		}
		for i := 0; i < tuple.Len(); i++ {
			walk(tuple.At(i).Type())
		}
	}
	walkTypeParams := func(list *types.TypeParamList) {
		if list == nil {
			return
		}
		for i := 0; i < list.Len(); i++ {
			walk(list.At(i))
		}
	}
	walk = func(t types.Type) {
		if t == nil || seenTypes[t] {
			return
		}
		seenTypes[t] = true
		switch x := t.(type) {
		case *types.Basic:
		case *types.Alias:
			addObject(x.Obj())
			walkTypeParams(x.TypeParams())
			if args := x.TypeArgs(); args != nil {
				for i := 0; i < args.Len(); i++ {
					walk(args.At(i))
				}
			}
			walk(x.Rhs())
		case *types.Array:
			walk(x.Elem())
		case *types.Chan:
			walk(x.Elem())
		case *types.Interface:
			for i := 0; i < x.NumExplicitMethods(); i++ {
				walk(x.ExplicitMethod(i).Type())
			}
			for i := 0; i < x.NumEmbeddeds(); i++ {
				walk(x.EmbeddedType(i))
			}
		case *types.Map:
			walk(x.Key())
			walk(x.Elem())
		case *types.Named:
			addObject(x.Obj())
			walkTypeParams(x.TypeParams())
			if args := x.TypeArgs(); args != nil {
				for i := 0; i < args.Len(); i++ {
					walk(args.At(i))
				}
			}
			walk(x.Underlying())
		case *types.Pointer:
			walk(x.Elem())
		case *types.Signature:
			if x.Recv() != nil {
				walk(x.Recv().Type())
			}
			walkTypeParams(x.RecvTypeParams())
			walkTypeParams(x.TypeParams())
			walkTuple(x.Params())
			walkTuple(x.Results())
		case *types.Slice:
			walk(x.Elem())
		case *types.Struct:
			for i := 0; i < x.NumFields(); i++ {
				walk(x.Field(i).Type())
			}
		case *types.Tuple:
			walkTuple(x)
		case *types.TypeParam:
			walk(x.Constraint())
		case *types.Union:
			for i := 0; i < x.Len(); i++ {
				walk(x.Term(i).Type())
			}
		default:
			panic(fmt.Sprintf(
				"features: unsupported go/types implementation %T in local type identity",
				t,
			))
		}
	}

	for _, root := range roots {
		walk(root)
	}
	sites := make([]string, 0, len(seenSites))
	for site := range seenSites {
		sites = append(sites, site)
	}
	sort.Strings(sites)
	return sites
}
```

The two local closures refer to `walk` before it is assigned; declare `walk`
before declaring either helper, as shown.

- [ ] Extend `InstanceDiscriminator` only after writing the existing prefix:

```go
	sites := localDeclaredTypeSites(fn, targs)
	if len(sites) == 0 {
		return b.String()
	}
	b.WriteString("\x00local-sites/v1")
	for _, site := range sites {
		b.WriteByte('\x00')
		b.WriteString(strconv.Itoa(len(site)))
		b.WriteByte(':')
		b.WriteString(site)
	}
	return b.String()
```

Do not call the collector for non-instances. This preserves the current empty
result and avoids requiring an SSA file set when no type arguments exist.

- [ ] Rewrite the `InstanceDiscriminator` doc comment to state:

  1. the existing package/type-string prefix is compatibility-sensitive;
  2. local declarations add a sorted, framed physical-site suffix;
  3. package-only arguments remain byte-identical;
  4. invalid or unknown identity inputs fail closed.

### Step 3: Verify feature identity

- [ ] Format and run the focused tests:

```console
gofmt -w internal/static/features/features.go internal/static/features/localtypearg_internal_test.go
go test ./internal/static/features -run 'TestInstanceDiscriminator' -count=1
```

Expected: PASS.

- [ ] Run the whole feature package:

```console
go test ./internal/static/features -count=1
```

Expected: PASS.

- [ ] Commit:

```console
git add internal/static/features/features.go internal/static/features/localtypearg_internal_test.go
git commit -m "fix: distinguish function-local generic types"
```

---

## Task 2: Lock the call-graph collision guard around the reported case

**Files:**

- Modify: `internal/static/callgraph/callgraph.go`
- Create: `internal/static/callgraph/localtypearg_internal_test.go`

**Interfaces consumed:**

- `rta.Analyze`
- `fromX`
- `features.InstanceDiscriminator`

**Behavior produced:** The raw Go call graph may contain same-display-FQN
instances, but conversion succeeds because their canonical secondary keys are
distinct. A remaining equal `(FQN, discriminator)` pair still panics.

### Step 1: Add an integration regression

- [ ] Build the same-line local-type program with `ssa.InstantiateGenerics`.
Collect `main` and `init` as RTA roots, run `rta.Analyze(roots, true)`, and first
assert the raw graph contains at least two distinct functions with the same
`RelString(nil)`.

- [ ] Call `fromX` and assert:

```go
func TestFromXKeepsCollidingDisplayGenericInstances(t *testing.T) {
	roots := buildLocalTypeCallGraph(t)
	raw := rta.Analyze(roots, true).CallGraph

	var rawInstances []*ssa.Function
	for fn := range raw.Nodes {
		if fn != nil && strings.Contains(fn.RelString(nil), "instantiate[") {
			rawInstances = append(rawInstances, fn)
		}
	}
	if len(rawInstances) < 2 {
		t.Fatalf("raw graph has %d instances; want at least 2", len(rawInstances))
	}
	if rawInstances[0].RelString(nil) != rawInstances[1].RelString(nil) {
		t.Fatalf("fixture did not reproduce display collision")
	}

	got := fromX(raw, AlgoRTA, nil)
	var keys []string
	for _, n := range got.Nodes {
		if strings.Contains(n.FQN, "instantiate[") {
			keys = append(keys, n.FQN+"\x00"+features.InstanceDiscriminator(n.Func))
		}
	}
	if len(keys) < 2 {
		t.Fatalf("converted graph kept %d instances; want at least 2", len(keys))
	}
	if !sort.StringsAreSorted(keys) {
		t.Fatalf("instance keys are not sorted: %q", keys)
	}
	for i := 1; i < len(keys); i++ {
		if keys[i] == keys[i-1] {
			t.Fatalf("duplicate canonical key %q", keys[i])
		}
	}
}
```

Make `buildLocalTypeCallGraph` return the `main` and `init` RTA roots, matching
`buildBoundGraph` in `boundwrapper_test.go`. Do not inspect map iteration order
in assertions: sort test-side raw functions before any pairwise comparison.

- [ ] Run:

```console
go test ./internal/static/callgraph -run TestFromXKeepsCollidingDisplayGenericInstances -count=1
```

Expected after Task 1: PASS. To prove the test is causally sensitive, temporarily
replace the local-sites suffix with `return b.String()`, rerun, observe the
existing deterministic collision panic, then restore the suffix before
continuing. Do not commit the temporary edit.

### Step 2: Keep the guard and make its comment honest

- [ ] Update only the invariant comment above `finalize`. It must name both
resolved collision classes:

- byte-identical `$bound`/`$thunk` wrappers are merged by `mergeKey`;
- generic instances whose type strings lose local lexical scope are separated
  by physical declaration sites.

The final sentence must continue to say that any surviving duplicate is a
producer bug and must panic. Do not alter the comparison or panic branch.

- [ ] Run:

```console
gofmt -w internal/static/callgraph/localtypearg_internal_test.go
go test ./internal/static/callgraph -count=1
```

Expected: PASS.

- [ ] Commit:

```console
git add internal/static/callgraph/callgraph.go internal/static/callgraph/localtypearg_internal_test.go
git commit -m "test: cover local generic call graph identity"
```

---

## Task 3: Make serialized node ordering total

**Files:**

- Modify: `internal/static/graphio/graphio.go`
- Create: `internal/static/graphio/sortgraph_internal_test.go`

**Interface produced:**

```go
func nodeLess(a, b Node) bool
```

`sortGraph` uses this one comparator. The existing comparison prefix remains:
`FQN`, `Sig`, `Package`, `File`, `Line`; then it compares `EndLine`, `Tier`,
and `Fallible`.

### Step 1: Write the failing permutation test

- [ ] Construct records that tie on the current comparator but differ in exactly
one remaining serialized field. Feed every permutation into `sortGraph` and
compare `Graph.Marshal()` bytes:

```go
func TestSortGraphNodesUsesEverySerializedField(t *testing.T) {
	base := Node{
		FQN: "example.com/p.F",
		Sig: "func()",
		Package: "example.com/p",
		File: "p.go",
		Line: 10,
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
```

Add a small recursive permutation helper in the test file. Keep the fixture to
four records so the test remains cheap.

- [ ] Run:

```console
go test ./internal/static/graphio -run TestSortGraphNodesUsesEverySerializedField -count=1
```

Expected: FAIL because the current comparator leaves at least two input
permutations in arrival order.

### Step 2: Add the total comparator

- [ ] Implement:

```go
func nodeLess(a, b Node) bool {
	switch {
	case a.FQN != b.FQN:
		return a.FQN < b.FQN
	case a.Sig != b.Sig:
		return a.Sig < b.Sig
	case a.Package != b.Package:
		return a.Package < b.Package
	case a.File != b.File:
		return a.File < b.File
	case a.Line != b.Line:
		return a.Line < b.Line
	case a.EndLine != b.EndLine:
		return a.EndLine < b.EndLine
	case a.Tier != b.Tier:
		return a.Tier < b.Tier
	default:
		return !a.Fallible && b.Fallible
	}
}
```

Replace the inline node comparator with:

```go
sort.Slice(g.Nodes, func(i, j int) bool {
	return nodeLess(g.Nodes[i], g.Nodes[j])
})
```

The comment must say equal comparator keys mean byte-identical serialized node
records. Remove the stale claim that `Sig` necessarily disambiguates instances.

- [ ] Run:

```console
gofmt -w internal/static/graphio/graphio.go internal/static/graphio/sortgraph_internal_test.go
go test ./internal/static/graphio -run 'TestSortGraphNodes|TestGraphDeterministic' -count=1
```

Expected: PASS.

- [ ] Commit:

```console
git add internal/static/graphio/graphio.go internal/static/graphio/sortgraph_internal_test.go
git commit -m "fix: totally order serialized graph nodes"
```

---

## Task 4: Add a command-level regression fixture

**Files:**

- Create: `testdata/fixtures/localtypeargsvc/go.mod`
- Create: `testdata/fixtures/localtypeargsvc/main.go`
- Modify: `cmd/flowmap/main_test.go`

**Behavior produced:** `flowmap graph` analyzes the original failure shape,
retains both generic instances, and emits byte-identical JSON over repeated
fresh analyses.

### Step 1: Add the failing command test before the fixture

- [ ] Add a fixture-path helper beside the existing `fixtureDir` helper and a
test that calls the real command dispatcher:

```go
func localTypeArgFixtureDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(
		filepath.Dir(file),
		"..", "..", "testdata", "fixtures", "localtypeargsvc",
	)
}

func TestGraphFunctionLocalGenericTypesDeterministic(t *testing.T) {
	dir := localTypeArgFixtureDir()
	var want string
	for i := 0; i < 20; i++ {
		got := captureStdout(t, func() {
			if err := run([]string{"graph", dir}); err != nil {
				t.Fatalf("run %d: %v", i, err)
			}
		})
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("run %d changed graph bytes", i)
		}
	}

	var g graphio.Graph
	if err := json.Unmarshal([]byte(want), &g); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	var instances int
	for _, n := range g.Nodes {
		if strings.Contains(n.FQN, "report[") {
			instances++
		}
	}
	if instances < 2 {
		t.Fatalf("graph kept %d report instances; want at least 2", instances)
	}
}
```

Use the command's actual default output contract. If `graph` requires an
explicit JSON flag on this checkout, pass that documented flag rather than
capturing a non-JSON renderer.

- [ ] Run:

```console
go test ./cmd/flowmap -run TestGraphFunctionLocalGenericTypesDeterministic -count=1
```

Expected: FAIL because the fixture directory does not yet exist.

### Step 2: Add the minimal valid-Go fixture

- [ ] Create `go.mod`:

```go
module example.com/localtypeargsvc

go 1.26
```

- [ ] Create `main.go`. Keep same-named declarations on one physical line and
make both instantiated functions reachable:

```go
package main

import "fmt"

func report[T any](v T) string {
	return fmt.Sprintf("%v", v)
}

func alpha() string { type result struct{ Value int }; return report(result{Value: 1}) }
func beta() string { type result struct{ Value int }; return report(result{Value: 2}) }
func gamma() string { type result struct{ Text string }; return report(result{Text: "three"}) }

func main() {
	fmt.Println(alpha(), beta(), gamma())
}
```

- [ ] Run:

```console
gofmt -w testdata/fixtures/localtypeargsvc/main.go cmd/flowmap/main_test.go
go test ./cmd/flowmap -run TestGraphFunctionLocalGenericTypesDeterministic -count=1
```

Expected: PASS with all runs byte-identical.

- [ ] Run the command package and static package set:

```console
go test ./cmd/flowmap ./internal/static/features ./internal/static/callgraph ./internal/static/graphio -count=1
```

Expected: PASS.

- [ ] Commit:

```console
git add testdata/fixtures/localtypeargsvc cmd/flowmap/main_test.go
git commit -m "test: reproduce local generic type graph collision"
```

---

## Task 5: Final trust and determinism verification

**Files:**

- Review only; no planned source edits.

### Step 1: Check compatibility and forbidden identity inputs

- [ ] Confirm the compatibility test pins the exact package-level discriminator
bytes.
- [ ] Search the new identity path and confirm it contains no
`FileSet.Position`, no raw integer conversion of `obj.Pos()`, no absolute
`file.Name()` use without `filepath.Base`, and no pointer formatting:

```console
rg -n 'Position\\(|obj\\.Pos\\(\\).*Itoa|fmt\\..*%p|file\\.Name\\(\\)' internal/static/features/features.go
```

Expected: the only `file.Name()` match is wrapped by `filepath.Base`; none of
the other forbidden forms appear.

- [ ] Confirm `callgraph.finalize` still panics on a surviving duplicate:

```console
rg -n 'deterministic collision|panic' internal/static/callgraph/callgraph.go
```

Expected: the collision panic remains.

### Step 2: Run repository gates

- [ ] Run:

```console
make fmt-check
make comment-drift
make verify
```

Expected: all commands succeed; `comment-drift` reports no newly stale
invariant comment.

- [ ] Run one final uncached determinism check:

```console
go test ./cmd/flowmap -run TestGraphFunctionLocalGenericTypesDeterministic -count=5
```

Expected: PASS.

### Step 3: Review the diff against the approved boundaries

- [ ] Verify:

  - package-level discriminator bytes are unchanged;
  - local-site suffixes are framed, sorted, and deduplicated;
  - physical basename and physical byte offset are the only new site inputs;
  - every current concrete `go/types` implementation is handled;
  - unknown types and invalid positions fail loudly;
  - the collision guard remains;
  - node sorting covers all eight serialized fields;
  - no schema or display-FQN change was introduced;
  - no new dependency was added.

- [ ] Inspect:

```console
git diff --check HEAD~4..HEAD
git status --short
```

Expected: no whitespace errors and only the intentional implementation commits
plus any pre-existing user-owned untracked files.
