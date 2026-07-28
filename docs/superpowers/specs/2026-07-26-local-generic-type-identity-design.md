# Function-local generic type identity

> **Status:** approved design · authored 2026-07-26 · **revised 2026-07-27
> (ratified by the repository owner)**

## Revision 2026-07-27 — what changed and why

The 2026-07-26 design did not achieve its own Goal 1. Five defects were witnessed
against the implementation of the accepted text; all five are addressed here, and
the goal is restated as **bounded**.

| Ref | Defect | Resolution |
|---|---|---|
| F3 | The `local-sites/v1` suffix was a sorted, deduplicated *set* pooled across all type arguments, so it recorded *which sites exist* and never *which local type sits in which role*. Three independent valid-Go programs (`f3a`, `f3b`, `f3c`) got equal keys for distinct instances. | The suffix becomes a canonical, **positional**, shared numbered-DAG serialization of the reachable type graph, under the new marker `local-type-graph/v1`. Sorting and deduplicating sites is now forbidden. |
| N1 | The local-ness predicate rejected `obj.Parent() == nil`. `go/types` re-creates a local `TypeName` with a nil `Parent()` during generic instantiation, so every such object was misclassified as package scope and contributed no site at all (witness `n1`). | The `obj.Parent() != nil` conjunct is deleted. A nil `Parent()` means "not package scope", never "package scope". |
| F26 | Sites were basename-and-offset only. Two packages whose files share a basename and whose local declarations are offset-aligned pooled into one equal set (witness `f26`, a live cross-package key collision that N1 masked). | Every `Named`/`Alias` node carries `obj.Pkg().Path()`, so a site is only ever read as the pair (declaring package path, basename:offset). |
| N2 | A promoted-method wrapper over a function-local receiver carries no type arguments, so `InstanceDiscriminator` returned `""`, while `mergeKey` deliberately excludes receiver-carrying wrappers. The input fell between the two mechanisms (witness `n2`). | The root set is extended with the receiver type when the function has one. |
| N3 | Declaration position is not sufficient: all instantiations of one generic function share one syntactic declaration, hence one site (witnesses `n1b`, `n1c`). | `n1c` (structure varies with the type parameter) is separated by the structural component. `n1b` (structure identical) **is not separable** and **fails closed**, with the diagnostic in "Residual undecided classes". |

**Goal 1 is bounded, and the bound is this: the discriminator separates every
collision class *except* structurally-identical function-local types declared
inside a generic function of which the analysis holds two instances.** That
class is undecidable by the chosen identity vocabulary — one declaration means one
position, and equal structure means equal structure — and it is refused loudly
rather than merged or ordered by map iteration. This is a deliberate, disclosed
limit, not an oversight and not a deferral. Two instances need not mean two
instantiations: an uninstantiated generic body analyzed alongside one
instantiation is enough (see "Residual undecided classes").

## Summary

`flowmap graph` must analyze valid Go programs that instantiate one generic with
distinct function-local declared types that render to the same
`types.TypeString`. The call-graph determinism guard remains unchanged: two
distinct functions may never survive with an equal canonical sort key.

The fix extends `features.InstanceDiscriminator` with a positional serialization
of the reachable type graph, carrying physical declaration identities for every
function-local declared type reachable from a generic instance's type arguments
or from a method wrapper's receiver. It does not merge instances and does not
weaken the guard. Package-level type arguments retain their current discriminator
bytes.

It *does* use `types.Type` pointer identity, as the node-sharing key of that
serialization. The dependency is on precision only: more sharing coarsens the key
and less sharing refines it, and a key that is too coarse produces the
duplicate-key panic — a loud abstain — never a merge. The 2026-07-26 text claimed
pointer identity was unused; that claim is retired here rather than left to
mislead.

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

1. Give distinct generic instances, and distinct method wrappers over
   function-local receivers, a deterministic sort key that separates every
   collision class **except one, named below**, whenever a function-local
   declaration is reachable.

   **This goal is bounded, deliberately and by disclosure, not by oversight.**
   The discriminator separates every class the identity vocabulary of
   *declaration position* and *type structure* can decide. It cannot separate
   **structurally identical function-local types declared inside a generic
   function of which the analysis holds two instances**: one syntactic
   declaration means one position, and equal structure means equal structure.
   Two instances arise from two instantiations, or from one instantiation
   analyzed alongside the generic's uninstantiated body. That class
   continues to **fail closed** at `callgraph.finalize` with the diagnostic
   specified in "Residual undecided classes". No claim of totality may be made
   in this document, in code comments, or in commit messages while that class
   exists.
2. Preserve existing discriminator bytes for package-level type arguments.
3. Keep keys portable across checkout roots and immune to `//line` rewriting.
4. Traverse the complete current `go/types` type vocabulary.
5. Preserve the fail-closed collision guard.
6. Ensure the downstream serialized node order is total over emitted data.

## Non-goals

- Merging generic instances.
- Changing display FQNs.
- Exposing the discriminator in graph JSON.
- Using a structural fingerprint **as a substitute for** declaration identity
  (see Alternatives), and hashing the serialization into a fixed-width digest.
  Structure appears in the key only as an *additional* discriminator composed
  after declaration sites, where it can refine but never merge; and it appears
  in full, not as a digest, so the diagnostic stays readable.
- Making keys stable across source edits that move a local declaration.
- Explaining the reported 59-node difference between widely separated
  revisions.

## Alternatives

### Structural fingerprint

Rejected **as a replacement**, adopted **as an additive component**. The
original objection stands unchanged: distinct local named types may have
identical underlying structure, so a structural key *alone* can conflate
behaviorally distinct instances. But structure composed *after* declaration
identity is monotone — it can only split an equivalence class, never merge one —
and it is the only thing that separates the `n1c` witness, where one syntactic
local declaration inside a generic function yields `struct{A int}` in one
instantiation and `struct{A string}` in another at the same position.
Composition order is therefore normative: declaration identity first, structure
second.

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

If a function-local declaration is reachable, append one framed suffix:

```text
\x00local-type-graph/v1\x00<len>:R<id>...\x00<len>:#<id>=<definition>...
```

The suffix is a **canonical, shared, positional serialization of the reachable
type graph**, not a set of sites. Sorting and deduplicating sites is
**forbidden**: it is exactly what discards role information and leaves the
`f3a`/`f3b`/`f3c` witnesses indistinguishable, and exactly what let two
packages' offset-aligned locals pool into one equal set in witness `f26`.

Encoding rules, all normative:

1. **Roots**, in order: the function's type arguments in declaration order,
   followed by the type the function dispatches on, when it has one — that is
   `features.receiverType`, which is `Signature.Recv()` for everything except a
   `$thunk` (first **parameter**) and a `$bound` (sole **free variable**); see
   "Wrapper discrimination". The roots section lists one framed `R<id>` token per
   root, in that order — including repeats, so a root appearing twice is recorded
   twice.
2. **Node ids** are assigned on *first visit* of a fixed depth-first traversal
   that follows the roots in order and each node's children in the order given
   by the definition grammar below. An id is **reserved before recursing**, so a
   back edge in a recursive type resolves to an already-assigned id. There is no
   on-path stack and no cycle-depth counter.
3. **Definitions** are emitted in ascending id order, one framed
   `#<id>=<definition>` token each. A definition carries the node's kind, **every
   intrinsic label of that kind** (below), and its children *by id, in positional
   order*. Positional order is the whole point: `struct,X>1,Y>2,Z>1` and
   `struct,X>2,Y>1,Z>2` must differ.
4. **Definition grammar per kind.** Children are written `<name>><id>` or
   `><id>`. This list is normative and exhaustive over the current `go/types`
   vocabulary:

   | Kind | Definition |
   |---|---|
   | `Basic` | `basic:<Basic.Name()>` — **the name, not merely the kind tag**; `basic:int` and `basic:string` must differ, or witness `n1c` is not separated |
   | `Named` | `named:<pkgpath>.<name>[@<site>]:<type-param ids in order>:<targ ids in order>:><underlying id>` |
   | `Alias` | `alias:<pkgpath>.<name>[@<site>]:<type-param ids in order>:<targ ids in order>:><rhs id>` |
   | `Array` | `array:<Len()>:><elem id>` |
   | `Chan` | `chan:<int(Dir())>:><elem id>` |
   | `Interface` | `iface:` then, for each explicit method, `<pkgpath>.<name>><sig id>`; then, for each embedded, `e><id>` |
   | `Map` | `map:><key id>:><elem id>` |
   | `Pointer` | `ptr:><elem id>` |
   | `Signature` | `sig:<0\|1 variadic>:<recv id or `-`>:<recv type-param ids>:<type-param ids>:><params tuple id>:><results tuple id>` |
   | `Slice` | `slice:><elem id>` |
   | `Struct` | `struct:` then, for each field in index order, `<name>:<0\|1 embedded>:<Tag(i)>><type id>` |
   | `Tuple` | `tuple:` then `><id>` per variable in index order |
   | `TypeParam` | `typeparam:<Index()>:><constraint id>` |
   | `Union` | `union:` then, per term in index order, `<0\|1 tilde>><type id>` |

5. **The `@<site>` label appears exactly when the node's object satisfies the
   function-local predicate**, and `site` is defined in "Physical position". The
   `<pkgpath>.<name>` component is present on every `Named` and `Alias`, local or
   not.

   **Type parameters are encoded too**, as a further id list on `Named` and
   `Alias`, written **before** the type arguments — carried into the rule-4 table
   above, which is normative and must not be read without them.
   They are reachable neither through a node's underlying type nor through its
   type arguments, and a function-local type can hide inside a constraint — which
   the shipped walker already reached and a permanent test already pins. Encoding
   them is a deliberate superset of the minimum the witnesses need: it can only
   split an equivalence class, never merge one, and omitting it would
   under-collect, which costs a refused program.
6. Every variable-length token is length-framed exactly as `local-sites/v1`
   framed its sites, so no legal filename, package path, field name, struct tag,
   or method name byte can create an ambiguous concatenation. This applies to the
   variable-length *lexemes inside* a definition as well as to the definition
   token itself: the guarantee is what is normative, and a schematic
   `<pkgpath>.<name>` in the table above is realized as `<len>:<pkgpath>.<name>`.
7. **If no function-local declaration is reachable, no suffix is emitted and the
   prefix is returned byte-for-byte.** This is what preserves Goal 2.

Because the encoding shares repeated nodes by id, its size is linear in the
number of *distinct* reachable type nodes. A fully-expanded (unshared) encoding
is exponential in **output size**, not merely in time — see "Complexity", which
is normative, not advisory.

### Complexity

The encoding must be *shared* — linear in the number of distinct reachable type
nodes — not merely memoized. On a shared DAG `Tk = struct{A T(k+1); B T(k+1)}` of
function-local types, a naive on-path walk produces 63 MB in 453 ms at depth 20,
memoizing the *computation* alone leaves the same 63 MB, and the numbered-DAG form
produces 814 bytes in 46 µs. A memoized walk that still materializes the expansion
does **not** satisfy this requirement: the explosion is in output size, and a
63 MB sort key is a denial of service, not a key.

`go/types` is itself exponential on that shape (804 ms at depth 20, 63 s at depth
26), so the reachable window is roughly depth ≤ 22. The DAG form is chosen because
it makes the encoding negligible across the entire window where the analyzed
program is loadable at all — not because arbitrarily deep graphs must be
supported. Budgeted expansion was considered and rejected: at depth 16 the
type-checker finishes in 45 ms while the naive encoding is already 3.9 MB, so a
budget alone would refuse programs the loader handles comfortably.

### Version identifier

The marker is `local-type-graph/v1`, replacing the retired `local-sites/v1`. The
suffix *grammar* changed wholesale — a sorted set of sites became a positional
graph serialization — so a reader who guesses the old grammar mis-parses the new
bytes, and a name containing "sites" invites exactly that guess. The rename also
makes a diagnostic emitted by a stale binary unmistakable. The retired marker must
not appear in code, tests, or comments after this change except in prose
describing what was replaced.

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

`site` remains `<physical-file-basename>:<physical-byte-offset>`. It is **never
used alone**: it appears only inside a `Named`/`Alias` definition that already
carries `obj.Pkg().Path()`, so the effective site identity is the pair (declaring
package path, basename:offset).

#### Display convention — deliberately not the site bytes

The four prohibitions above are all statements about what may become a **key**.
None of them governs what a **human** is shown, and the two must not be conflated:

- **Key**: `<basename>:<offset>`, exactly as specified above, unchanged.
- **Display** (the residual diagnostic's third placeholder):
  `<basename>, byte offset <offset> (line <line>)`.

`main.go:98` is read by every reader as line 98 — the form every compiler uses —
and 98 is a byte offset, into a file that may have twelve lines. The offset is
kept in the prose because it is the value the key is actually built from, so a
reader can correlate the message with the key; the line is added because it is
what an editor jumps to.

The line is **display-only** and is resolved with `FileSet.Position`, i.e. it
**honours `//line`** — which is exactly why it may not reach a key, and exactly
what a reader of a generated file wants to see. Two normative consequences:

1. The producer of `site` must expose the file and offset as **structured data**,
   not as a joined string. The key formatter and the prose formatter are then
   independent, and neither re-parses the other's output.
2. `FileSet.Position` must be unreachable from the encoding path. It is called
   once, in the accessor the diagnostic uses, after the key bytes already exist.

A single test pins both halves at once: two sources differing only in a `//line`
directive's target line, at equal byte length, must produce a **byte-identical
discriminator** and **different displayed lines**. Asserting only the key leaves
the display untested; asserting only the display leaves open that the key moved
with it.

The `local-sites/v1` uniqueness argument — that intra-package basename uniqueness
suffices because the type string already carries the package path — **is
invalid**. Once sites are pooled into one alphabet, two packages whose files share
a basename and whose local declarations are offset-aligned collide: witness `f26`
builds two distinct `*ssa.Function` with one key from exactly that, and renaming
one file to break the basename equality makes it graph cleanly.

**Separate normative tripwire.** Package-path qualification does **not**
distinguish a package from its `go/packages` in-package test variant: both report
the same `Path()`, over the same files at the same offsets. `Tests: false` in
`internal/static/loader/loader.go` is what keeps that variant out of the loaded
program. **A change to `loader.Tests` must not be made without revisiting this
section**; under `Tests: true` that class would be a genuine collision the key
cannot decide, and would correctly fail closed.

The tripwire must fire on its **own** assertion. It reads a fixture that holds an
**in-package** test file — the only kind that yields the same-`PkgPath` variant —
and that declares no dependencies, so flipping the field fails on "this path was
loaded twice", not on a module-resolution error from a fixture whose test
dependencies `go list -test` cannot satisfy. A tripwire that fires by accident
misdirects whoever trips it.

If a function-local declaration lacks a valid physical token file or offset,
`InstanceDiscriminator` must fail loudly with a deterministic diagnostic. It
must not fabricate an identity and continue.

## Function-local declarations

A declared type object is function-local when:

```go
obj.Pkg() != nil &&
obj.Parent() != obj.Pkg().Scope()
```

**A nil `Parent()` means "not package scope", never "package scope".**
`go/types` re-creates a local `TypeName` during generic instantiation with a nil
`Parent()`; the earlier `obj.Parent() != nil` conjunct silently reclassified
every such object as package-scope, dropping its site entirely and leaving the
discriminator with no suffix (witness `n1`). Because the predicate gates a
*disclosure*, its failure mode must be over-collection, not under-collection: an
over-collected package-scope object costs a longer key; an under-collected local
object costs a refused program, and — before this revision — masked the
cross-package site collision of witness `f26`.

An implementation must carry a regression test whose local type is declared
inside a **generic** function and is reached only after instantiation.

The collector examines both:

- `*types.Named.Obj()`;
- `*types.Alias.Obj()`.

An alias does not necessarily introduce distinct Go type identity, but its RHS
may hide a function-local named declaration. Collecting the alias site as well
keeps the discriminator total when the rendered alias name itself is
scope-lossy.

Package-scope objects add no site suffix.

## Wrapper discrimination

`InstanceDiscriminator` previously returned `""` for any function with no type
arguments, and `callgraph.mergeKey` deliberately excludes any wrapper carrying a
receiver. A promoted-method wrapper over a function-local receiver therefore fell
between the two and got an empty discriminator (witness `n2`).

The root set is extended: the type the function dispatches on is appended to the
roots, **after** the type arguments. That type is `features.receiverType`, and it
is deliberately **not** `Signature.Recv()`, because two `go/ssa` kinds carry their
receiver somewhere else (`wrappers.go`):

| Kind | `Synthetic` prefix | Receiver lives in |
|---|---|---|
| declared method, promotion wrapper, interface wrapper | — / `wrapper for ` | `Signature.Recv()` |
| method expression | `thunk for ` | `Signature.Params()[0]` |
| method value | `bound method wrapper for ` | `FreeVars[0]` |

Reading only `Signature.Recv()` gave the last two an empty root set, hence an
empty discriminator, hence admission to `mergeKey` — the silent merge recorded
under "Residual undecided classes" (witnesses `thunkmerge`, `n1thunk`). The three
carriers are exhaustive over `go/ssa` v0.40.0 and are pinned against the toolchain
by `TestReceiverTypeReadsThunkParameterAndBoundFreeVar`; `range-over-func yield`
and `package initializer` have no receiver at all.

`mergeKey` is widened in NO direction. Merging a receiver-carrying wrapper remains
forbidden, because go/ssa caches those wrappers and two distinct ones are not
interchangeable; and `mergeKey` gains a third exclusion — any function whose
discriminator carries the `local-type-graph/v1` suffix — so nothing `mergeKey`
admits can ever be a member of the residual class.

That admitted subset is defined by `mergeKey`'s conjuncts, **not** by an
enumeration of kinds, and it is wider than the two forwarders the merge exists
for. A bodiless declared package-level function — `Synthetic == "from type
information"`, which `go/ssa` mints for every dependency the loader resolves from
export data rather than syntax (`fmt.Errorf`, `net/http.HandleFunc`,
`context.Background`) — has a non-empty `Synthetic`, a non-nil `Object()`, no type
arguments and no `Signature.Recv()`, so it is admitted too — in the thousands under
`--algo cha`, and a clear majority of the candidates (15 of 18 on loansvc) under the
default `rta`/`vta`, whose node set is far smaller — on
any real run. This is harmless rather than overlooked, and no merge has ever fired
for one: it genuinely has no receiver, so `receiverType` returning nil is correct
and its empty discriminator is earned rather than missed; and one `*types.Func`
yields one `*ssa.Function` per package, so two of them cannot share the
`(RelString, Object)` key a merge would require. Admitted, never merged.

Goal 2 is preserved without exception: a method, thunk or bound whose receiver
reaches no function-local declaration produces no suffix, so its discriminator
stays `""` exactly as before, and it stays mergeable. Only wrappers and forwarders
over local receivers change, and only from `""` to something. Both the
value-receiver and pointer-receiver wrapper forms must be covered, and both
forwarder kinds.

## Complete type walk

The walker assigns ids through a `map[types.Type]int`, which serves as the memo,
the cycle guard, and the sharing mechanism at once. It is **queried by key only
and never ranged over**, so map iteration order cannot reach the output. It
handles every current concrete implementation (the children column below is
superseded by the definition grammar in "Canonical discriminator", which fixes
both the child set and its order):

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

Four additional normative rules:

- **A boolean `seen` set is forbidden.** Under a *global* boolean seen set a
  type's second occurrence is skipped, so `struct{X A; Y B; Z A}` records `A`
  only at position `X` and role information is lost again (witness `f3c`). Under
  an *on-path-only* set the walk is exponential in output size (see Complexity).
  The id map is the only shape that is simultaneously positional, cycle-safe, and
  linear.
- **Node identity is `types.Type` pointer identity.** This is a *precision*
  dependency, never a soundness one: less sharing yields more ids and a finer
  key, more sharing yields a coarser key, and a key that is too coarse produces a
  duplicate-key panic — a loud abstain — never a merge.
- **Interface explicit methods are encoded in ascending `(pkgpath, name)`
  order.** `go/types` already returns them in a canonical order for a completed
  interface; sorting makes the encoder independent of that guarantee. Method
  names are unique within an interface once qualified by package, so the order is
  total and the tie-break question does not arise. This is the **only** sort in
  the encoder.
- **Byte budget.** The serialization carries a fixed byte budget checked
  incrementally during construction. Exceeding it is an operational failure with
  a deterministic diagnostic — never a truncated key, which would silently merge
  two distinct functions. This is belt-and-braces against a pathological type
  graph the id-sharing analysis did not anticipate.

  **The budget is a refusal surface wider than the suffix it guards, and that is
  disclosed, not accidental.** It is checked as each definition is completed —
  during the traversal, before the encoder knows whether any function-local
  declaration was reachable at all. A function whose reachable type graph exceeds
  the budget is therefore refused even when it would have emitted **no suffix**
  and returned the prefix byte-for-byte, which is a refusal the pre-budget code
  did not make. This does not breach the fail-closed rule — it is a loud abstain
  on an input the encoder cannot represent, never a merge — but it is a behavior
  change on programs unrelated to function-local types, and a reader of the
  budget's rationale must not have to infer it. Narrowing the check to
  suffix-emitting functions would mean completing a >1 MiB traversal before
  deciding to discard it, so the wider surface is deliberate.

An unrecognized future `go/types` implementation must fail loudly in tests and
production rather than silently skip a possible local declaration.

## Call-graph guard

`callgraph.finalize` continues to:

1. Compare FQN.
2. Compare `InstanceDiscriminator`.
3. Panic when distinct functions still share both.

The comment above `finalize` must state the resolved classes and, separately, the
residual one:

Resolved:

- byte-identical `$bound`/`$thunk` wrappers, merged by `mergeKey`;
- generic instances with scope-lossy local type strings, separated by the
  positional `local-type-graph/v1` serialization;
- local declarations inside generic functions, whose `TypeName` has a nil
  `Parent()` after instantiation;
- offset-aligned local declarations in same-basename files of **different**
  packages, separated by the package path carried in each named node;
- promoted-method wrappers over function-local receivers, separated by the same
  serialization applied to the receiver.

Residual (see "Residual undecided classes"): two analyzed instances of one
generic function whose function-local declaration is structurally identical in
both.

`panicOnDuplicateSortKey`'s doc comment must enumerate exactly that single
residual class; the three deferred classes `(a)`, `(b)`, `(c)` it previously
listed are all resolved by this revision and must be replaced in the same change.
**The panic itself must not be widened, narrowed, downgraded, or caught.** A
surviving duplicate remains a loud abstain.

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

The permanent fixture set must additionally contain, each asserting *distinct*
keys and *no panic* unless stated:

- swapped two-parameter roles over two local types reachable through a func
  literal's parameter (`f3a`), with the two locals **structurally identical**;
- the same swap collapsed into one type argument through a generic container
  (`f3b`);
- repeated roles in one struct, `struct{X A; Y B; Z A}` vs `struct{X B; Y A; Z B}`
  (`f3c`) — the case that fails under a global seen-set;
- a local type declared inside a **generic** function, reached only after
  instantiation, in two different generic functions (`n1`);
- two promoted-method wrappers over same-named function-local receivers
  embedding different named types, asserted for **both** the value and the
  pointer receiver form (`n2`);
- **two packages with equal file basenames and offset-aligned local
  declarations, reached into one instantiation through a generic intermediary
  (`f26`), asserting no panic and distinct keys** — this is a full end-to-end
  graph test, not a site-bytes assertion, because the collision is live once the
  predicate of "Function-local declarations" is corrected;
- a local type whose structure varies with the enclosing type parameter (`n1c`),
  asserting the two instances are separated — this is the test that fails if a
  `Basic` node is encoded without its name;
- a shared-DAG type of depth 20 (`Tk = struct{A T(k+1); B T(k+1)}`), asserting
  the encoding is produced in bounded time and bounded size;
- a recursive local named type (`type R struct{ next *R }`), asserting the walk
  terminates and the encoding is finite;
- the `n1b` residual, asserted to **still panic**, with the diagnostic text of
  "Residual undecided classes"; the test must fail if it ever silently stops
  panicking;
- `n1lib`, a **library** unit whose exported generic is instantiated exactly
  once, asserted to still panic under `rta`, `vta` **and** `cha` with that same
  text. It is what keeps the diagnostic honest: any message asserting an
  instantiation count, or offering "instantiate it only once" as a remedy, is
  false here under every algorithm, and the test asserts those phrasings are
  absent.
- three **positive** witnesses that reach the residual class WITHOUT a call to a
  generic function, each asserted to exit 0 under `rta` and `vta` and to be
  refused under `cha` with the residual diagnostic: `n1method` (the local is a
  type argument of a generic **type**, and the refused pair is that type's
  method), `n1methodnocall` (the same with the method **never called**), and
  `n1recv` (the local reaches a promotion wrapper's **receiver**, so the refused
  function carries no type arguments at all). They are what make the blast radius
  a mechanism rather than a shape list; a phrasing quantified over calls to
  generic functions, or over type arguments alone, contains none of them.
- three **negative** witnesses that bound the disclosed blast radius, all holding
  `n1b`'s shape yet asserted to exit 0 under `rta`, `vta` **and** `cha`:
  `n1zeroinst`, whose generic is never instantiated; `n1nongencallee`, whose
  callee is not generic; and `n1typeonly`, whose local IS a type argument of a
  generic type the analyzed set instantiates twice, but that type has no methods,
  so no SSA function is built at the local. Each also asserts the node counts
  that keep it armed — `n1zeroinst` must show **exactly one** `sink[L]` under
  `cha` (the uninstantiated body's, so the door is demonstrably open), and
  `n1nongencallee` and `n1typeonly` must each show both `gen` and `gen[int]`
  under `cha`. Without those counts a fixture whose subject was simply never
  analyzed would pass, and the witnesses would prove nothing. They exist because
  the blast-radius sentence in "The uninstantiated-body sub-case" was once
  written as an unbounded "ANY program containing this shape", which execution
  falsified twice. `n1typeonly` and `n1methodnocall` are a controlled pair: they
  differ by exactly one never-called method declaration, and that one line is the
  difference between a clean graph and a refusal under `cha`.
- an assertion that `mergeKey` and the local-type-graph suffix are **disjoint** —
  no function `mergeKey` accepts may carry a non-empty discriminator — over a
  program exercising both mechanisms, with non-vacuity asserted in both
  directions. It is the one step of the blast radius's "exactly when" that no
  end-to-end fixture can exhibit.
- a `//line` **display/key separation** test: two sources differing only in a
  `//line` directive's target line, at equal byte length, asserted to produce a
  byte-identical discriminator and different displayed lines (see "Physical
  position → Display convention").

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

## Residual undecided classes

One class survives this revision. It is not deferred for convenience — it is
undecidable by the chosen identity vocabulary, and it is the reason Goal 1 is
stated as bounded.

**A generic function that declares a function-local type, reached through two
analyzed instances of that one generic body** — where "generic function" is
shorthand for any body the analyzer builds more than once, including a closure
nested in one and a method of a generic type; see the sub-case below. Every
instance shares one syntactic declaration, hence one site. When the local's
structure varies with the
type parameter (`type L struct{ A X }`, witness `n1c`), the structural component
separates them. When it does not (`type L struct{ A int }`, witness `n1b`),
position and structure are both identical and no refinement of either can help.

Two analyzed instances arise **either** from instantiating the generic more than
once (witness `n1b`) **or** from analyzing the generic's own uninstantiated body
alongside a single instantiation (witnesses `n1` under `cha`, `n1lib` under all
three algorithms) — see the sub-case below. Nothing available at the refusal
distinguishes them, so nothing in the diagnostic may assert which occurred.

### The uninstantiated-body sub-case — same class, one instantiation

**Blast radius, stated as the mechanism.** The refusal fires exactly when all
five of the following hold. Each is individually **necessary**, and the five are
jointly **sufficient**:

1. a function-local type `L` is declared inside a body the analyzer builds **more
   than once** — a generic function (`n1b`), a **closure nested in** one
   (`n1closure`), or a **method of a generic type** (`n1typemethod`, where the
   declaring body is `func (Box[T]) Show()`, which declares no type parameters of
   its own; the program's one generic function, `sink`, is the CALLEE and declares
   nothing);
2. `L` is **reachable in the type graph** from the **discriminator roots** of some
   SSA function `F` — `F`'s type arguments in order, then **the type `F`
   dispatches on**, which is `features.receiverType` and NOT `Signature.Recv()`
   (see "Wrapper discrimination", and the reading note below). `L` need not *be* a
   root: the encoding walks the whole graph below each root, so `[]L`
   (`n1nestedroot`), `*L`, `chan L`, `map[string]L`, `func(L)` and `Wrap[L]` all
   reach it;
3. the analyzed set holds **two** analyzed instances of that one enclosing body —
   two instantiations, or one instantiation alongside the body's own
   uninstantiated form — and **builds** `F` from each, so the two are distinct
   `*ssa.Function` produced from the one declaration of `L`;
4. `L`'s structure does not depend on the enclosing type parameter, so the two
   copies of `L` encode identically (`n1c` fails here);
5. and **nothing else reaching `F`'s roots separates the two either**. Every
   *other* function-local type reachable from those roots is likewise produced
   from a single declaration with a structure that does not vary, and the
   enclosing type parameter's own instantiation does not reach them
   (`n1fqndiffers` and `n1twodecls` fail here).

**Conjunct 2 is read two ways, and the gap between them is where a defect
lived.** Under the STRICT reading it names the function `discriminatorRoots`, and
the five conjuncts are then sufficient by construction — the criterion is whatever
that function returns. Under the SOURCE-LEVEL reading a reader actually applies —
"its type arguments, then its receiver" — the two readings must be argued equal,
and until 2026-07-27 they were not. `discriminatorRoots` read `Signature.Recv()`,
which is empty for the two `go/ssa` forwarder kinds that carry their receiver
elsewhere: a `$thunk`'s is its first **parameter** and a `$bound`'s its sole
**free variable**. `n1thunk` satisfied all five conjuncts on the source reading
and was **not** refused; `mergeKey` absorbed the pair instead. `n1recv` — the same
program one wrapper kind away — was refused. One shape, two answers.

`features.receiverType` closes that instance: the strict reading now includes the
thunk parameter and the bound free variable, so both fixtures behave alike. **The
tension itself is not closed and is not claimed to be.** "Receiver" is a
source-level word and `discriminatorRoots` is a function; they agree today because
`go/ssa` v0.40.0 has exactly three receiver carriers (`Signature.Recv()`, a
thunk's `Params()[0]`, a bound's `FreeVars[0]`) and `receiverType` enumerates all
three, pinned against the toolchain by
`TestReceiverTypeReadsThunkParameterAndBoundFreeVar`. A fourth carrier — a new
synthetic kind, or an existing one changing where it puts its receiver — would
reopen the gap in exactly the same shape and with exactly the same symptom: a
silent merge where the source reading expects a refusal. When reading this
section as a criterion, read conjunct 2 strictly; when reading it as a claim about
programs, treat the equality of the two readings as a property that must be
re-checked whenever `go/ssa` is upgraded, not as a definition.

Conjuncts 4 and 5 are one property split by role — `L` cannot separate the pair,
and neither can anything else — and together they say exactly this: **the two
instances agree on everything the encoding reads from `F`'s roots**: every kind,
every basic and field label, and every function-local object's declaring package
path, file basename and byte offset. Equal encodings render equally, so conjunct
5 also delivers the equal **display FQN** that `panicOnDuplicateSortKey` tests
first — it short-circuits on `prev.FQN != cur.FQN` and never consults the
discriminator, so a pair the FQN already separates is not a refusal however well
conjuncts 1–4 fit it.

Drop conjunct 2, 3, 4 or 5 and the program graphs cleanly — the table below
carries a committed negative witness for each. Conjunct 1 has none of its own,
and needs none: it is *implied* by conjunct 3, since a body the analyzer builds
once yields one `F` and no pair (that program is `n1zeroinst`). It is stated
separately because it is the conjunct a reader checks by looking at the source.
Nothing in the five is exotic, so the refusal is not a corner case; it is the
price of failing closed, and it is paid by any analysis whose set holds that
pair.

**"Generic function" is shorthand elsewhere in this document**, including in the
frozen diagnostic text ("inside a generic function"), for what conjunct 1
actually needs: a body the analyzer builds more than once. `n1typemethod` is
refused with that wording though no generic function declares its local. The
shorthand is
disclosed here rather than corrected there, because the diagnostic's exact text
is ratified and byte-identical to the block under "Diagnostic"; widening it is a
separate spec change.

**There is no shorter equivalent, and this section no longer offers one.** "Two
distinct `*ssa.Function` that differ only in which instantiation of the enclosing
generic body produced them" was carried here as an equivalence and reads well,
but "differ" has no stable meaning that makes it one. Read as type identity it
also describes `n1c`, whose two `sink[L]` differ in exactly that and graph
cleanly; read as "differ in anything the key can see" it is
`panicOnDuplicateSortKey` restated. Keep it as a mnemonic; the five conjuncts are
the criterion.

The syntactic forms below are **illustrations, not an enumeration.** This
sentence has now been written three times and falsified three times by execution
— first by over-claiming refusals that do not happen, then by being read as
exhaustive, then by omitting conjunct 5 — so every shape is a committed fixture,
indexed by the conjunct it exercises, and the table's exit codes are asserted.

| Fixture | Shape | `rta` | `vta` | `cha` |
|---|---|---|---|---|
| `n1b` | `f[X]` declares `L`, passes it to a generic `g`; `f` instantiated twice | refused | refused | refused |
| `n1` | two *different* generic functions declare `L`, so conjunct 3 is met only via the uninstantiated body | 0 | 0 | refused |
| `n1lib` | an exported generic in a **library** unit, instantiated once; root discovery supplies its uninstantiated body | refused | refused | refused |
| `n1closure` | conjunct 1 — `L` is declared in a **closure** nested in the generic function, which has no type parameters of its own | refused | refused | refused |
| `n1typemethod` | conjunct 1 — `L` is declared in a **method of a generic type**; no generic *function* declares it (the program's `sink[T]` is the callee) | refused | refused | refused |
| `n1method` | `L` is a type argument of a generic **type**; the refused pair is that type's method, `(Box[L]).Show[L]` | 0 | 0 | refused |
| `n1methodnocall` | the same with the method **never called** | 0 | 0 | refused |
| `n1recv` | `L` reaches a promotion wrapper's **receiver**; the refused function carries no type arguments at all | 0 | 0 | refused |
| `n1thunk` | conjunct 2 — the same through a `$thunk`, whose receiver is its first **parameter**, so it has no `Signature.Recv()` either | 0 | 0 | refused |
| `n1nestedroot` | conjunct 2 — the type argument is `[]L`, so `L` is **not a root**; the walk reaches it one edge below | refused | refused | refused |
| `n1onedecl` | conjunct 5, the refusing half of `n1twodecls`' minimal pair: the *second* root is two instances of ONE declaration of `M` | refused | refused | refused |
| `n1c` | conjunct 4 fails — `L`'s structure mentions `X` | 0 | 0 | 0 |
| `n1zeroinst` | conjunct 3 fails — zero instantiations, so `cha` builds exactly one `g[L]` | 0 | 0 | 0 |
| `n1nongencallee` | conjunct 2 fails — the callee is not generic, so nothing carries `L` in its roots | 0 | 0 | 0 |
| `n1typeonly` | conjunct 2 fails differently — `Box[L]` *is* instantiated twice, but `Box` has no methods, so no SSA function is built at `L` | 0 | 0 | 0 |
| `n1fqndiffers` | conjunct 5 fails — `x` carries `X`'s instantiation into the roots, so the FQNs **differ** | 0 | 0 | 0 |
| `n1twodecls` | conjunct 5 fails with the FQNs **equal** — the second root's two locals render alike but come from two declarations | 0 | 0 | 0 |

Six of those rows exist because they falsify a reading an earlier wording
invited, and each was confirmed by execution:

- **"a generic function declares `L`."** Too narrow, twice over. `n1closure`
  declares it in a closure whose own signature has no type parameters, and
  `n1typemethod` declares it in `func (Box[T]) Show()`, which declares none
  either — and in `n1typemethod` no `func f[X]` encloses the declaration at any
  depth, since `Box`'s parameter, not a function's, is what makes the analyzer
  build `Show` twice. Both fixtures do contain a generic function, `sink[T]`, but
  it is the CALLEE at `L`, never the declaring body; a reading of conjunct 1 that
  looks for `func f[X]` syntax finds the wrong function in `n1closure` and none at
  all in `n1typemethod`. What conjunct 1 needs is a body the analyzer **builds
  more than once**; "generic function" names only the commonest way to get one.
- **"`L` IS one of the type arguments, or the receiver."** Too narrow.
  `n1nestedroot` passes `[]L`; `L` is one edge below the root and the refusal
  fires all the same, because `localTypeGraph` walks the graph under each root
  rather than reading the roots themselves.
- **"the four conjuncts are sufficient."** False, and this is the falsification
  that added conjunct 5. `n1fqndiffers` satisfies all four — `gen` declares `L`,
  `L` reaches `sink`'s roots, `cha` builds `sink` from the uninstantiated body
  and from each of two instantiations, and `L`'s structure does not mention `X` —
  and graphs cleanly under all three algorithms, because the second argument
  `x` gives the three instances three different FQNs (`sink[L X]`, `sink[L int]`,
  `sink[L string]`). Replacing `x` with `0` refuses it.
- **"then require the two to share a display FQN."** The obvious repair, and also
  false. In `n1twodecls` they *do* share one — both are
  `sink[…n1twodecls.L …n1twodecls.L]`, two node records under one FQN — and the
  program still graphs cleanly, because `mkA`'s `L` and `mkB`'s `L` render alike
  but sit at different declaration sites. Sharing an FQN is necessary, not
  sufficient; conjunct 5 has to be about what the *encoding* reads.
- **"the pair must differ in nothing but the instantiation."** `n1onedecl` is
  `n1twodecls` with `mkA`/`mkB` collapsed into one generic instantiated twice —
  one declaration of `M` behind the differing root instead of two — and it is
  refused. The two fixtures are a minimal pair: what conjunct 5 counts is
  declarations, not type arguments and not rendering.

Four further rows exist because they falsify a reading the original shape-list
wording invited, and each was confirmed by execution:

- **"the callee must be a generic function."** False. `n1method` and `n1recv`
  call no generic function at `L`. Conjunct 2 requires only that `L` be reachable
  from a *discriminator root* — the type arguments **and** the receiver. The
  receiver is a root because a promoted-method wrapper carries no type arguments
  whatsoever (see "Wrapper discrimination"), so a blast radius stated over type
  arguments alone omits `n1recv` entirely.
- **"the receiver is `Signature.Recv()`."** False, and this is the falsification
  that cost a silent merge rather than a mis-worded sentence. `n1thunk` is
  `n1recv` with the promotion wrapper replaced by a method expression; the
  resulting `$thunk` has no `Signature.Recv()` and no type arguments, so its root
  set was empty, its discriminator `""`, and `mergeKey` absorbed the pair — exit 0
  where `n1recv` exits 2, for one program shape. See the conjunct 2 reading note
  above and "Wrapper discrimination".
- **"the function at `L` must be called."** False. `n1methodnocall` never calls
  `Show`; converting `Box[L]` to an interface puts its method set into the
  analyzed set, and `cha` refuses. Conjunct 3 says *built*, not *called*.
- **"`L` becoming a type argument of something the analyzed set instantiates
  twice is enough."** False. In `n1typeonly` the generic *type* `Box[L]` is
  instantiated twice and nothing is refused, because a type instantiation is not
  an `*ssa.Function`. The type argument must land on a function the analyzer
  builds.

The earlier wording — "ANY program containing the shape is refused, even when `f`
is never instantiated at all" — was false on both of its counts, which is what
`n1zeroinst` (conjunct 3) and `n1nongencallee` (conjunct 2) exist to keep from
being written a third time. Every negative witness also asserts the node counts
that keep it armed, so a fixture whose subject was simply never analyzed could
not pass for one whose guard did not fire.

**Nothing intercepts a member of the class before the guard sees it.** `mergeKey`
merges byte-identical wrappers; it refuses any function whose
`InstanceDiscriminator` carries the `local-type-graph/v1` suffix, so no member of
the class can be absorbed before `panicOnDuplicateSortKey` ever runs. That is the
one step of "exactly when" no end-to-end fixture can exhibit, and the two
definitions live in two packages, so it is asserted by
`TestMergeKeyNeverAbsorbsASuffixCarryingFunction` rather than argued.

**The earlier argument for this paragraph was true and useless, and it licensed a
real defect.** It ran: `mergeKey` accepts only functions with no type arguments
**and** no receiver, which is an empty discriminator root set, hence an empty
discriminator, hence never a suffix-carrying key. Every clause of that is literally
true. What it establishes is only that `mergeKey` cannot hold a function *carrying*
the suffix — and `mergeKey` routinely held functions that **should have** carried
one. The `$thunk` and `$bound` forwarders it exists to merge are precisely the two
`go/ssa` kinds whose receiver is **not** `Signature.Recv()`: a thunk's is its first
**parameter** and a bound's its sole **free variable**. `discriminatorRoots` read
`Signature.Recv()` alone, returned an empty root set for both, and the empty key
that followed was an artifact of the omission, not evidence of an empty class. Two
thunks over two distinct same-rendering function-local types therefore merged
into one node — silently, with no refusal at any algorithm. Witnesses `thunkmerge`
(the merged node emits the **union** of two out-edge sets `vta` had kept disjoint,
so edges are fabricated on the positive pole; a union deletes no callee, so no
absence proof can flip) and `n1thunk` (the same program shape as `n1recv`, exit 0
through a thunk where `n1recv` exits 2 through a promotion wrapper — one shape,
two answers, one of them silent).

Both halves of the repair are load-bearing and neither works alone.
`features.receiverType` is what makes the suffix appear for the two forwarder
kinds; `mergeKey`'s `HasLocalTypeGraph` conjunct is what then keeps it out of the
merge subset. With only the first, `mergeKey` admits a suffix-carrying thunk and
`TestMergeKeyNeverAbsorbsASuffixCarryingFunction` fails on its `key != ""`
assertion; with only the second, the conjunct is unreachable because the key is
still empty.

The exclusion now has **three** conjuncts and the test pins each separately,
because a fixture that exercises one is silent about the others. Relaxing the
*receiver* conjunct is a genuine silent merge: `n1recv` under `cha` goes from
refused to exit 0, two distinct promotion wrappers collapsing into one node with
no refusal — and while `mergeKeyDisjointSrc` held only a type-argument-rooted
local, that mutation left the whole `internal/static/callgraph` suite green.
Dropping the *`HasLocalTypeGraph`* conjunct is the same failure through the
forwarder door. The fixture now carries a receiver-rooted function-local type and
a thunk-rooted one, and the suffix non-vacuity counter is split one per conjunct,
so hoisting any local back to package scope fails the test instead of quietly
disarming a third of it.

The mechanism behind conjunct 3: under `cha` the analyzed set is the whole
program, which includes the **uninstantiated** body of every generic function
alongside its instantiations. `go/ssa` builds `sink[L]` from the uninstantiated body of `gen[X]`
*and* from each instantiation, and those are distinct `*ssa.Function` produced
from the same one declaration of `L` with the same structure. So the class is
reached with a generic instantiated only **once** — witness `n1`, which graphs
cleanly under `rta` and `vta` and is refused under `cha`.

**`cha` is not the only door to it.** Root discovery falls back to a unit's
exported surface when it finds no primary entry point, and an exported *generic*
function's **uninstantiated** body is one of those roots. A library unit with an
exported generic is therefore refused under `rta` and `vta` too, with the generic
instantiated once — witness `n1lib`, refused under all three algorithms. The
algorithm the caller chose does **not** decide which door was taken.

This is the same undecidable class reached through a different door, and it is
refused with the same diagnostic. The diagnostic therefore states **no
instantiation count**: it names both doors, says plainly that flowmap does not
report which one was taken, and offers no remedy that a one-instantiation program
already satisfies. Its earlier wording ("…that this program instantiates more than
once", and the remedy "instantiate the enclosing generic function only once") was
written for the two-instantiation door alone, was false for this one, and is
retired.

### Decision: fail closed

The class **panics**, deliberately and permanently until a different decision is
ratified. A valid Go program is refused loudly rather than ordered by map
iteration.

### Merging was considered and is rejected

The obvious alternative is to merge the two instances into one node, on the
theory that if nothing can tell them apart they are interchangeable — the same
argument `mergeKey` already makes for byte-identical `$bound`/`$thunk` wrappers.
It is rejected. The two instances are **two distinct `*ssa.Function` with two
distinct bodies**, reached from two distinct instantiations; "the key cannot tell
them apart" is a statement about the key, not about the functions. Collapsing
them would delete one node and its out-edges from the graph, and every absence
proof the toolchain emits — PROVEN, NO-FLOW, NEVER, "no path", "covered" — would
then cover a function the analysis never examined. That is a false PROVEN, the
worst outcome under CLAUDE.md tenet 4, and it would be *silent*. A panic is a
refused analysis; a merge is a wrong answer that looks right. Merging must not be
adopted without a mechanical interchangeability proof, and the panic must not be
softened into a merge as a convenience.

A second future, also not chosen: qualify the site by the enclosing
instantiation's type arguments. Correct in principle; it requires recovering the
enclosing generic instance's type arguments from an instantiated local
`TypeName`, for which no `go/types` API was found. Unexplored, not rejected.

### Diagnostic

A refusal in this class is a user-facing failure on a **valid** Go program. The
diagnostic must therefore say what the analyzer refused, *why* it refused, that
this is a disclosed limit rather than a crash, and what the user can change. It
must be deterministic: no pointer values, no map-ordered content, no wall clock.

The message is emitted whenever a surviving duplicate's discriminator **carries a
`local-type-graph/v1` suffix** — the signature of this class, since the two
functions then agree on every site and every structural label. A surviving
duplicate with **no** suffix is a different, unknown class and keeps the generic
duplicate-sort-key wording; the message below says so explicitly rather than
asserting a diagnosis it cannot prove.

It must also state **no instantiation count**, because the refusal establishes
none: it sees two `*ssa.Function` with one key, not how they were produced. Both
doors of the class (above) must be named, and no remedy may be offered that a
one-instantiation program already satisfies.

Exact text (`%s` placeholders in order: FQN, local type name, **display
location**, FQN, discriminator). The third placeholder is the display rendering
of "Physical position → Display convention" — `main.go, byte offset 98 (line 7)`
— **not** the key's `site` bytes:

```text
callgraph: refusing to order two distinct instances of
    %s

WHY: both instances were produced from ONE declaration of the function-local
type %q at %s, inside a generic function, and in both the type has identical
structure. One declaration means one source position, and equal structure means
equal structure — so neither position nor structure can tell the instances
apart. flowmap will not invent an order it cannot derive from the source: a
run-varying "canonical" order would silently change every downstream verdict,
snapshot, and gate.

HOW THE ANALYSIS GOT TWO OF THEM: either the enclosing generic function is
instantiated more than once, or the analyzed set holds its UNINSTANTIATED body
alongside a single instantiation — which is what --algo cha does for the whole
program, and what root discovery does when an exported generic is a library
unit's entry point. flowmap does not report which: it refused on the key, and
the key does not record it.

This is a DISCLOSED LIMIT of the function-local type discriminator, not a crash
and not a defect in your program. See "Residual undecided classes" in
docs/superpowers/specs/2026-07-26-local-generic-type-identity-design.md.

WHAT YOU CAN DO — any one of:
  * give the local type a structure that depends on the enclosing type
    parameter, so the instances differ:
        type L struct{ A int }        // indistinguishable
        type L struct{ A int; _ X }   // distinguishable — mentions X
  * move the type declaration out of the generic function (package scope, or a
    non-generic helper called from it);
  * shrink the analyzed set to ONE instance of the enclosing generic body:
    instantiate the generic once AND keep its uninstantiated body out of the
    analysis (see HOW above).

If your program does not match that shape, this is an UNKNOWN collision class,
not the disclosed one — please report it with this message; the correct fix is a
further refinement of the key, never a merge.

sort key:      %s
discriminator: %q
```

The local type name and position are read from the first `@<site>`-bearing node
in the shared discriminator, in id order, so the choice is deterministic. The
accessor returns them as structured data (name, file, offset, display line); the
display line is resolved there and nowhere else.

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
- Every witness of the 2026-07-27 revision graphs successfully under `rta`,
  `vta`, and `cha`, except the declared residual (`n1b`), which must still exit
  non-zero with the specified diagnostic — **and except `n1`, `n1method`,
  `n1methodnocall`, `n1recv` and `n1thunk` under `cha`, and `n1lib` under all
  three**, which fall into the same declared residual class through the
  uninstantiated generic body (see "The uninstantiated-body sub-case"). Those five
  must exit 0 under `rta` and `vta`. These exceptions are disclosed unmet
  criteria, not a relaxation: the shape is undecidable by the ratified vocabulary,
  and it fails closed.
- The `thunkmerge` witness graphs successfully under all three algorithms and
  emits **no fabricated edge**: under `vta` exactly `one -> (A).QueryContext` and
  `two -> (B).QueryContext`, never the union of the two. `rta` and `cha`
  over-approximate the embedded-interface invoke to both implementers, which is
  sound and unchanged; asserting all three is what distinguishes "the fabricated
  edges are gone" from "the graph got smaller".
- The residual diagnostic renders the declaration's position as
  `<basename>, byte offset <offset> (line <line>)`, never as `<basename>:<n>`,
  while the discriminator still frames `<basename>:<offset>`.
- The pooled-sorted-deduplicated site set no longer appears anywhere, and the
  string `local-sites/v1` no longer appears in code, tests, or comments.
- `loader.Tests` is still `false`, or "Physical position" has been revisited.
- The `localtypeargsvc` fixture's `flowmap graph --algo vta` output is unchanged
  by this design, compared **with `provenance.tool` normalized** — or, equivalently,
  between two binaries built the same way.

  The criterion must not be stated as a literal `sha256` of the whole document.
  `provenance.tool` is a build stamp: `go run` yields `"dev"`, a VCS-visible
  `go build` yields a version string, and two otherwise byte-identical graphs then
  differ by exactly that one line and by every hash taken over it. Stated as a
  digest, this criterion has already raised one false alarm.

  **No committed test pins this equality, and none can**: it is a comparison
  between two *binaries*, and only one of them exists in the tree at a time. The
  committed evidence is adjacent, not equivalent —
  `TestGraphFunctionLocalGenericTypesDeterministic` pins that one binary's output
  is stable over 20 runs and still carries three `report[...]` records with their
  three callers, and `TestLocalGenericIdentityDeterministicAcrossProcesses` pins
  the same across processes plus the discriminator key multiset. Cross-binary
  equality stays a release-time check, run by hand, with the tool line normalized.
