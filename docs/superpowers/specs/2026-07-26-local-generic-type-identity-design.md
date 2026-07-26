# Function-local generic type identity

> **Status:** approved design · authored 2026-07-26

## Summary

`flowmap graph` must analyze valid Go programs that instantiate one generic with
distinct function-local declared types that render to the same
`types.TypeString`. The call-graph determinism guard remains unchanged: two
distinct functions may never survive with an equal canonical sort key.

The fix extends `features.InstanceDiscriminator` with physical declaration
identities for function-local declared types reachable from a generic
instance's type arguments. It does not merge instances, use pointer identity,
or weaken the guard. Package-level type arguments retain their current
discriminator bytes.

## Problem

Go permits two functions in one package to declare local types with the same
name. These declarations create distinct types, but `types.TypeString` omits
their lexical scopes:

```go
func first() {
	type result struct{ A int }
	instantiate[result]()
}

func second() {
	type result struct{ B string }
	instantiate[result]()
}
```

Both generic instances can render as:

```text
example.com/service.instantiate[example.com/service.result]
```

The current discriminator is the generic's effective package plus each
argument's `types.TypeString`. The two instances therefore share both their
display FQN and discriminator. `callgraph.finalize` correctly panics rather
than allow map iteration to choose their relative order.

The defect is the incomplete discriminator, not the panic.

## Evidence

The submitted report contains:

- A self-contained valid-Go reproduction.
- The exact panic and exit status.
- The introducing commit and last known version without the guard.
- A real-service reproduction.
- Red/green regression-test validation.
- Three byte-identical patched runs over the real service.
- No-churn comparisons over unaffected services.
- A clean `make verify`.

The failure was independently reproduced on the evaluated checkout.

A temporary feasibility spike then established:

- Two declarations on the same physical line receive distinct physical byte
  offsets.
- A local alias and a recursive named type nested inside its RHS are both
  reachable through a complete type walk.
- Changing `/checkout/one/spike.go` to `/another/root/spike.go` does not change
  the keys.
- A `//line misleading-generated-name.go:900` directive does not affect the
  physical filename or offsets.
- A package-level argument retains the exact existing discriminator.
- Twenty repeated SSA builds produce identical keys.

The spike was removed after recording these conclusions.

## Goals

1. Give distinct generic instances a deterministic total sort key when their
   type arguments contain colliding function-local declarations.
2. Preserve existing discriminator bytes for package-level type arguments.
3. Keep keys portable across checkout roots and immune to `//line` rewriting.
4. Traverse the complete current `go/types` type vocabulary.
5. Preserve the fail-closed collision guard.
6. Ensure the downstream serialized node order is total over emitted data.

## Non-goals

- Merging generic instances.
- Changing display FQNs.
- Exposing the discriminator in graph JSON.
- Structurally hashing type definitions.
- Making keys stable across source edits that move a local declaration.
- Explaining the reported 59-node difference between widely separated
  revisions.

## Alternatives

### Structural fingerprint

Rejected. Distinct local named types may have identical underlying structure.
A structural key can merge or collide behaviorally distinct instances.

### Enclosing-scope identity

Deferred. Recovering a named enclosing function still requires source-position
mapping for nested or same-named scopes and adds AST ownership machinery solely
to avoid position churn in a rare collision class.

### Declaration position

Selected. A physical source location is intrinsic to the declaration, already
part of the producer's source model, and distinguishes structurally identical
local types.

## Canonical discriminator

`InstanceDiscriminator` retains its existing prefix:

```text
<effective-package>\x00<type-string-1>\x00<type-string-2>...
```

If no reachable function-local declaration exists, the function returns that
prefix byte-for-byte.

If local declarations exist, append one framed suffix:

```text
\x00local-sites/v1\x00<len>:<site>\x00<len>:<site>...
```

where:

- `len` is the decimal byte length of `site`;
- `site` is `<physical-file-basename>:<physical-byte-offset>`;
- sites are sorted lexicographically and deduplicated across all type
  arguments.

Length framing prevents a comma, colon, or other legal filename byte from
creating an ambiguous concatenation. The `local-sites/v1` marker distinguishes
the extension from the existing type-string components.

### Physical position

For a local `*types.TypeName`:

1. Read the physical token file with
   `fn.Prog.Fset.File(obj.Pos())`.
2. Use `filepath.Base(file.Name())`.
3. Use `file.Offset(obj.Pos())`.

Do not use:

- `FileSet.Position`, which honors `//line` adjustments;
- an absolute filename;
- raw `token.Pos`, whose file-set base depends on load construction;
- line alone, because valid Go may contain two declarations on one line.

Within one Go package, source file basenames are unique. The existing type
string already carries the declaring package path, so the basename and offset
only need to distinguish declarations inside that package.

If a function-local declaration lacks a valid physical token file or offset,
`InstanceDiscriminator` must fail loudly with a deterministic diagnostic. It
must not fabricate an identity and continue.

## Function-local declarations

A declared type object is function-local when:

```go
obj.Pkg() != nil &&
obj.Parent() != nil &&
obj.Parent() != obj.Pkg().Scope()
```

The collector examines both:

- `*types.Named.Obj()`;
- `*types.Alias.Obj()`.

An alias does not necessarily introduce distinct Go type identity, but its RHS
may hide a function-local named declaration. Collecting the alias site as well
keeps the discriminator total when the rendered alias name itself is
scope-lossy.

Package-scope objects add no site suffix.

## Complete type walk

The walker uses a `map[types.Type]bool` seen set and handles every current
concrete implementation:

| Type | Children |
|---|---|
| `Basic` | none |
| `Alias` | object, type arguments, RHS |
| `Array` | element |
| `Chan` | element |
| `Interface` | explicit method types, embedded types |
| `Map` | key, element |
| `Named` | object, type arguments, underlying type |
| `Pointer` | element |
| `Signature` | receiver, receiver type parameters, type parameters, parameters, results |
| `Slice` | element |
| `Struct` | field types |
| `Tuple` | variable types |
| `TypeParam` | constraint |
| `Union` | term types |

An unrecognized future `go/types` implementation must fail loudly in tests and
production rather than silently skip a possible local declaration.

## Call-graph guard

`callgraph.finalize` continues to:

1. Compare FQN.
2. Compare `InstanceDiscriminator`.
3. Panic when distinct functions still share both.

The comment above `finalize` must state both known resolved collision classes:

- byte-identical `$bound`/`$thunk` wrappers, merged by `mergeKey`;
- generic instances with scope-lossy local type strings, separated by physical
  declaration sites.

Any surviving class remains an error.

## Serialized graph ordering

`graphio.sortGraph` must retain its existing comparison prefix and then compare
every remaining serialized `Node` field:

1. `FQN`
2. `Sig`
3. `Package`
4. `File`
5. `Line`
6. `EndLine`
7. `Tier`
8. `Fallible`

Two nodes equal on all fields are byte-identical records; their relative order
cannot change serialized bytes. The comparator comment must describe this
actual invariant instead of claiming `Sig` necessarily distinguishes every
instance.

## Tests

### Feature-level identity

A permanent inline SSA fixture must include:

- Two same-named local types in different functions.
- Both declarations on one physical line.
- Structurally different and structurally identical variants.
- A recursive local named type.
- A local alias whose RHS nests the local type through a signature, map, slice,
  pointer, and channel.
- A package-level control type.
- A `//line` directive.

Tests assert:

- Equal display FQNs plus distinct discriminators for distinct local
  instances.
- Repeated discriminator calls return identical bytes.
- Repeated SSA builds return the same sorted key multiset.
- Different checkout roots return the same keys.
- The package-level control equals the pre-extension key.
- The walker visits every supported `go/types` implementation through focused
  constructed-type cases.

### Guard regression

An internal call-graph test must prove the raw graph contains the colliding
instances, run `fromX`, and assert:

- no panic;
- both distinct nodes survive;
- their order is stable across repeated constructions.

The test must fail when the local-site suffix is disabled.

### End-to-end flowmap

A fixture equivalent to the submitted reproduction must prove:

- `flowmap graph --algo vta` succeeds;
- the graph contains two node records for the colliding display FQN and retains
  both instances' outgoing behavior;
- repeated graph bytes are identical.

### Graph serializer

A unit test shuffles nodes equal on the old comparator prefix but different in
`EndLine`, `Tier`, or `Fallible`, then proves `sortGraph` emits one canonical
order.

## Compatibility and rollout

Only graphs containing function-local declarations in generic type arguments
may change. Package-level generic instances and non-generic functions retain
their discriminator behavior and graph bytes.

The change needs no flag or migration. A rollback is the inverse code change,
but it reintroduces the total-analysis panic for affected programs; the
determinism guard must never be removed as a rollback mechanism.

## Acceptance criteria

- The submitted minimal reproduction graphs successfully.
- Same-line local declarations are distinguished.
- Local aliases and nested local named types are discovered.
- Package-level discriminator bytes do not change.
- No absolute path or adjusted source position reaches a key.
- Repeated SSA builds and graph runs are byte-identical.
- `callgraph.finalize` retains its collision panic.
- `graphio.sortGraph` is canonical over all serialized node fields.
- `make fmt-check` passes.
- `make verify` passes.
