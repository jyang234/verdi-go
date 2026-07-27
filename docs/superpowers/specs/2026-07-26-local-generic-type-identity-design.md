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
   followed by the receiver type when `fn.Signature != nil &&
   fn.Signature.Recv() != nil` (see "Wrapper discrimination"). The roots section
   lists one framed `R<id>` token per root, in that order — including repeats, so
   a root appearing twice is recorded twice.
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

The root set is extended: when `fn.Signature != nil && fn.Signature.Recv() != nil`,
the receiver type is appended to the roots, **after** the type arguments.
`mergeKey` is **not** widened — merging a receiver-carrying wrapper remains
forbidden, because go/ssa caches those wrappers and two distinct ones are not
interchangeable.

Goal 2 is preserved without exception: a method whose receiver reaches no
function-local declaration produces no suffix, so its discriminator stays `""`
exactly as before. Only wrappers over local receivers change, and only from `""`
to something. Both the value-receiver and pointer-receiver wrapper forms must be
covered.

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
analyzed instances of that one generic body.** Every instance shares one
syntactic declaration, hence one site. When the local's structure varies with the
type parameter (`type L struct{ A X }`, witness `n1c`), the structural component
separates them. When it does not (`type L struct{ A int }`, witness `n1b`),
position and structure are both identical and no refinement of either can help.

Two analyzed instances arise **either** from instantiating the generic more than
once (witness `n1b`) **or** from analyzing the generic's own uninstantiated body
alongside a single instantiation (witnesses `n1` under `cha`, `n1lib` under all
three algorithms) — see the sub-case below. Nothing available at the refusal
distinguishes them, so nothing in the diagnostic may assert which occurred.

### The uninstantiated-body sub-case — same class, one instantiation

**Blast radius, plainly: under `--algo cha`, ANY program containing
`func f[X any]() { type L struct{ …no X… }; g(L{}) }` is refused — even when `f`
is instantiated exactly once, and even when it is never instantiated at all.**
That is an ordinary Go idiom, not a pathological one, so the refusal is not a
corner case; it is the price of failing closed, and it is paid by whole-program
analysis of any unit written that way.

The mechanism: under `cha` the analyzed set is the whole program, which includes
the **uninstantiated** body of every generic function alongside its
instantiations. `go/ssa` builds `sink[L]` from the uninstantiated body of `gen[X]`
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

Exact text (`%s` placeholders in order: FQN, local type name, site, FQN,
discriminator):

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

The local type name and site are read from the first `@<site>`-bearing node in
the shared discriminator, in id order, so the choice is deterministic.

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
  non-zero with the specified diagnostic — **and except `n1` under `cha`, and
  `n1lib` under all three**, which fall into the same declared residual class
  through the uninstantiated generic body (see "The uninstantiated-body
  sub-case"). `n1` under `rta` and `vta` must exit 0. These exceptions are
  disclosed unmet criteria, not a relaxation: the shape is undecidable by the
  ratified vocabulary, and it fails closed.
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
