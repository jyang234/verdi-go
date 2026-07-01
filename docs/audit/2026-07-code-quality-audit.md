# Code-quality audit — 2026-07-01

> **`ACTIVE`** · repo-wide code-quality and soundness audit · _baseline commit `9450868`, `make verify` green_

This document is a comprehensive audit of the codebase's quality, measured
against the repo's own prime directive (CLAUDE.md): determinism and trust
before everything else. It is written to be **handed to a coding agent for
implementation** — every finding carries an ID, file:line anchors, a concrete
failure scenario, a suggested fix, and the regression test the fix must ship
with. Findings marked **[reproduced]** were empirically confirmed during the
audit with throwaway fixtures/tests driven through the real pipeline (all
since removed); findings marked **[code-verified]** were confirmed by direct
code reading, in most cases at two independent audit passes.

---

## 1. Audit scope and method

- **Baseline:** commit `9450868` (merge of PR #83), ~76,100 lines of Go across
  385 files. `make verify` (build + vet + golangci-lint + test + fixture gates
  + gofmt) is **fully green** at baseline; toolchain go1.25.0,
  golangci-lint v2.5.0. Everything below is therefore invisible to the
  existing gates — that is what makes it audit material.
- **Coverage:** seven parallel deep-read passes covering (1) the canon
  determinism core (`internal/canon`, `internal/canonjson`), (2) the static
  extraction pipeline (`internal/static/{loader,ssabuild,callgraph,analyze,
  roots,rebind,soundness,features,signatures,graphio,statictest}`), (3) the
  per-analysis passes (`obligations`, `taint`, `blindspots`, `frontier`,
  `reclaim`, `sqlfold`, `schemadrift`, `boundary` + shared enums), (4) the
  judge core (`internal/groundwork/{fitness,graph,setutil}`, `internal/glob`,
  `internal/config`), (5) the judge surfaces (`contract`, `chains`, `ground`,
  `impact`, `policy`, `review`, `reviewtriage`, `transcript`,
  `internal/render`, `internal/diff`), (6) both CLIs + the MCP server + SARIF
  (`cmd/`), (7) the behavioral capture side (`harness`, `capture`, `flow`,
  `ir`, `internal/{ingest,otlpjson,impeach,coverage,golden,await,syscontext}`),
  plus a whole-repo cross-cutting sweep (determinism greps, duplicated
  predicates, enumeration soundness, error handling, docs drift, CI hygiene).
- **Severity taxonomy** (derived from the repo's tenets — this is not a
  generic lint scale):
  - **CRITICAL** — a wrong verdict is possible: a false PROVEN / SATISFIED /
    NO-FLOW / clean-pass, a silently erased effect in a gated artifact, or a
    non-deterministic "canonical" output. The repo's own worst-outcome class.
  - **HIGH** — a fail-open path (bad input silently tolerated on a trust
    boundary, disclosure silently dropped, gate exits 0 on a non-proof), a
    reproducible crash under a documented configuration, or a hidden
    disclosure (data that should be redacted leaking into gated output).
  - **MEDIUM** — drift risk (duplicated predicates without parity tests),
    narrow determinism residue, silent-degradation footguns, robustness gaps.
  - **LOW** — polish: stale comments, minor API sharp edges, performance.

**Headline count:** 8 CRITICAL, 14 HIGH, ~30 MEDIUM, ~30 LOW. The counts need
the context of §2: the baseline discipline here is exceptional, and the
criticals cluster in exactly two systemic seams, not spread randomly.

---

## 2. What the project is, and the overall assessment

Verdi-Go is a two-tool chain — **flowmap** (producer: static call graph with
typed boundary effects, path obligations, frontier disclosure, opt-in sound
reclaimers, behavioral golden snapshots from OTel traces) and **groundwork**
(judge: fitness gates, review artifacts with recomputable digests, triage
cards, MCP server) — whose entire product is the *trustworthiness of its
verdicts*: byte-deterministic, fail-closed, three-valued
(proven / violated-with-witness / abstained-with-reason), consumed by CI and
agents without re-derivation.

**Overall assessment: this is one of the most determinism-disciplined
codebases any of the audit passes had seen.** The tenets are practiced, not
aspirational: nearly every ordering resolves on intrinsic keys with an
explicit, commented tie-break; map iteration essentially never reaches output;
one-source-of-truth helpers exist with named parity tests (`sqlverb`,
`effectkind`, the `opkey` prefix anti-retype test that scans the whole repo);
strict `DisallowUnknownFields` decoding guards every trust boundary; the
digest/verify chain resists tamper, staleness, and re-signed forgery (all
tested); the enumeration-soundness rule ("collect functions completely") is
honored at every checked site, with both previously-recorded regressions
(`taint.firstPartyFuncs`, `roots` library exports) genuinely fixed and
regression-locked. The adversarial test culture (fuzzers with annotated
regression seeds, property tests over generated graph corpora, soundness
probes, effectiveness drills as committed assertions) is exactly right.

The defects that survive that discipline cluster in **two systemic seams**,
and both are instances of failure classes the project itself has named:

1. **The under-collection class, one layer up.** CLAUDE.md records that
   incomplete function enumeration under an absence claim "bit us twice." It
   is present a third time — not in a pass's own walk, but in the projection
   layer's *first-party predicate*: `ssa.Function.Pkg` is nil for the shared
   synthetic functions modern go/ssa produces (generic instances,
   `$bound`/`$thunk` wrappers), so reachable first-party behavior is silently
   severed from the emitted graph with no blind spot (C-1). The same
   "the absence machinery doesn't see this shape" pattern recurs in
   obligations' failure-branch pruning (C-2), taint's invoke-blindness
   (C-3/C-4/C-5), the concurrent-rule blind probe (C-6), promote's unordered
   groups (C-7), and capture's same-goroutine race claim (C-8).
2. **Drift between parallel surfaces.** Where one rule has two hand-rolled
   homes, they have already drifted, exactly as the one-source-of-truth tenet
   predicts: the MCP fitness lens sheds the provenance caveats the CLI fought
   to keep (H-9), fitness's caveat assembly fell behind review's (H-10), the
   broker-conflict merge was determinism-fixed on the MCP twin only (M-1),
   the ground card's io_budget predicate diverged from the enforcer (H-8),
   `bindsAnyTarget` uses a different boundary-edge predicate than every other
   consumer (H-5), and CI no longer mirrors `make verify` (M-2).

Nothing found permits a *forged* artifact, and no wall clock, randomness, or
goroutine race outcome reaches a committed golden or verdict today. The two
seams above are fixable with localized changes plus the regression tests the
repo's own standards demand; §5 orders that work.

### Risks to the stated capabilities (README / scorecard claims)

| Stated capability | Risk found | Findings |
|---|---|---|
| `must_not_reach` "no path" proofs / write-surface budgets | Reachable first-party effects can be absent from the emitted graph entirely (no node, no edge, no blind spot) — false PASS reproduced | **C-1** |
| Path obligations: "a SATISFIED verdict is a universal proof" | False SATISFIED reproduced on a common Go idiom (named error result + annotating defer + reassigned `err`) | **C-2** |
| `flowmap taint` NO-FLOW | Blind to the entire interface-dispatch surface and to struct-carried fields while still emitting the proven pole (three reproduced holes); `--gate` also exits 0 on ABSTAIN | **C-3, C-4, C-5, M-13** |
| `no_concurrent_reach` (with `require_proof`) | Vacuous clean pass over a disclosed `ConcurrentDispatch` blind spot, reproduced | **C-6** |
| Behavioral goldens / impeachment corpus fidelity | Tier-1 effects can be silently erased from freshly-minted post-hoc goldens (C-7); a snake_case OTLP export empties the corpus with exit 0 (H-13); a false `Concurrent: true` structural claim for the single-worker idiom (C-8); parent-cycle spans silently dropped (H-2) | **C-7, C-8, H-2, H-13** |
| Redaction: literals never reach goldens | SQL normalizer leaks literal fragments via backslash escapes and comments; allowlisting `db.statement` re-injects the raw statement | **H-1, H-3, M-24** |
| "Opt-in, sound" reclaimers | **Held up under scrutiny** — all four reclaimers verified add-only/fail-closed; frontier bins every kind | (none) |
| Review artifact digest / verify-artifact | **Held up** (tamper/stale/forgery tested); gap is a missing field-coverage self-check | M-6 |
| "Silence is never a silent pass" | The pre-flight gate drops newly-introduced cautions; MCP fitness drops all caveats; several funnel edges discard without disclosure | **H-7, H-9, H-10, M-16..M-19** |
| Byte-determinism across machines | Holds everywhere gated today; three narrow residues (span-ID tie-break, broker-conflict error text, Mermaid fold merge) + one latent (callgraph FQN ties) | M-3, M-4, M-5, M-20 |
| CI is the trust boundary ("graphs MUST come from trusted CI") | CI does not mirror `make verify` (impeachsvc fixture missing, `-race` asymmetry); nightly fuzz covers 3 of 12 targets | **M-2**, M-21 |

---

## 3. Findings — CRITICAL and HIGH (detailed, implementation-ready)

> Implementation notes for the fixing agent: work on a branch, run
> `make verify` after every finding, and honor CLAUDE.md — every fix to an
> ordering/canonicalization path ships a determinism test; every soundness fix
> ships a regression test that exercises the exact reproduced shape; when a
> comment's claim changes, fix the comment in the same edit. Where a fix says
> "abstain," that means the existing CANT-PROVE/blind-spot/caveat channel of
> that component, never a new ad-hoc string.

### CRITICAL

#### C-1 · Paths through nil-`Pkg` synthetic functions (generic instances, `$bound`/`$thunk` wrappers) are silently severed from the emitted graph — false "no path", hidden writes, zero disclosure **[reproduced]**
- **Where:** `internal/static/graphio/scope.go:24-35` (`firstPartyScope`),
  `internal/static/graphio/scope.go:138-160` (`reachableFirstParty`),
  `internal/static/graphio/graphio.go:1148-1151` (`edgeOf` default drop);
  root predicate `internal/static/ssabuild/ssabuild.go:61-67` (`IsFirstParty`
  returns false for `pkg == nil`). Same filter in
  `internal/static/blindspots/blindspots.go:309` (so no blind spot is emitted
  either) and `internal/static/taint/taint.go:693-703`.
- **Defect:** go/ssa gives `Pkg: nil` to shared synthetic functions
  (x/tools@v0.42.0 `go/ssa/instantiate.go` `createInstance`; `ssa.go:351`).
  Under RTA + `InstantiateGenerics` the *reachable* node for a first-party
  generic is the instance (nil Pkg), and method values route through `$bound`
  wrappers (nil Pkg). `firstPartyScope` keys on `fn.Pkg`, so these are
  excluded from the rendered graph, and `edgeOf` drops any edge whose callee
  is out of scope as "unhinted third-party" — with no blind spot.
- **Reproduced failure scenarios:**
  1. Wrapper severance: call graph contains `Handle → Do → (*Store).Purge$bound
     → (*Store).Purge → ExecContext`; emitted graph has `Handle → Do` and
     `Purge → boundary:db DELETE ledger` but **no `Do → Purge` edge and no
     blind spot**. `must_not_reach{from: Handle, to: "boundary:db DELETE"}`
     reads noPathFound → **false PASS** while the route demonstrably executes
     the DELETE.
  2. Generic severance: `Handle2 → GenericSave[int] → ExecContext`
     (`INSERT INTO audit`) — the instance has **no node, the caller edge is
     dropped, and the DB write is entirely absent** from the write-surface
     budget, effect order, obligations scope, and frontier. Zero blind spots.
  3. On the committed fixture: `codec.Decode[origination.Application]` is
     reachable in the call graph (pinned by
     `callgraph_test.go:TestGenericInstantiationReachable`) yet absent — node
     and edge — from loansvc's `graphio.Build` output.
- **Fix:** introduce ONE function-level first-party predicate (in `ssabuild`
  or `features`): `fn.Pkg`, else `fn.Origin().Pkg` (generic instances), else
  `fn.Object().Pkg()` (bound/thunk wrappers wrap a real method object). Use it
  in `firstPartyScope`, `blindspots`, and `taint.firstPartyFuncs`. Either
  render the instance/wrapper node, or splice edges through wrappers
  (caller → wrappee). Extend `Node.Package`/`File` derivation for nil-Pkg
  functions, and fix the node-sort tie-break at the same time (M-20 — instance
  FQNs are documented non-unique). The stale comment at `graphio.go:262-267`
  ("Empty only for a synthetic node…", currently unreachable) documents the
  intended behavior — restore it to truth.
- **Tests to ship:** fixtures putting a boundary effect *behind a method-value
  callback through a `*T` receiver* and *inside a first-party generic helper*;
  assert node+edge presence in `graphio.Build` output and that a
  `must_not_reach` over the seam finds the path. These are the two shapes the
  current suite never crosses.

#### C-2 · obligations: false SATISFIED — a reassigned `err` prunes a genuinely-leaking branch **[reproduced]**
- **Where:** `internal/static/obligations/obligations.go:658-707`
  (`errorValuesOf` + `failureBranch`).
- **Defect:** when the acquire's error result is a named result (or otherwise
  stored into an `Alloc` — which the supported "annotating defer" idiom
  forces), `errorValuesOf` expands the error web through the alloc's loads, so
  **every** later `if err != nil` reading that alloc — including tests of a
  *reassigned* `err` — is classified as the acquire's failure branch and its
  leaking arm is pruned.
- **Reproduced:** verdict `SATISFIED` for:
  ```go
  func LeakAfterReassign(s *Store) (err error) {
      defer func() { err = annotate(err) }()
      tx, err := s.BeginTx()
      if err != nil { return err }
      err = doWork()                 // REASSIGN
      if err != nil { return err }   // LEAKS tx — arm pruned
      return tx.Commit()
  }
  ```
  A false universal proof on a common Go shape (named error result +
  annotating defer + intermediate fallible call).
- **Fix:** restrict failure-branch pruning to the acquire's error result
  before any redefinition — credit only an `if` whose tested value is the
  direct acquire `Extract` (or Phi/Convert of it). Where the acquire error
  flows into an alloc that has **more than one store**, *abstain*
  (CANT-PROVE) instead of pruning. Regression test mirroring the PoC; note
  the interaction with the documented `usesRecover` residual (M-14) — both
  guards protect SATISFIED, so this one must be airtight.

#### C-3 · taint: false NO-FLOW — taint returned through an interface (invoke) method silently dies **[reproduced]**
- **Where:** `internal/static/taint/taint.go:507-524` (`handleReturn`);
  `taint.go:145,193-197` (`callsTo` built from `StaticCallee()` only).
- **Defect:** `handleReturn` propagates a tainted return only to callers in
  `callsTo`, which indexes static calls only; an invoke-mode call (`x.Get()`
  through an interface) is not indexed, so the tainted return taints nothing
  downstream **and never sets `escaped`** — NO-FLOW while the value reaches
  the sink at runtime. Reproduced: `var x Getter = g{}; sinkA(x.Get())` where
  `g.Get` returns the declared source → `NO-FLOW`, escaped=false. The "PII
  getter behind an interface" is the dominant Go idiom.

#### C-4 · taint: false NO-FLOW — a declared source function invoked via an interface is never seeded **[reproduced]**
- **Where:** `internal/static/taint/taint.go:403-423` (`seed`).
- **Defect:** `seed` matches sources only at `StaticCallee()` sites; an
  invoke-mode call to a declared source method matches nothing → `Sources==0`
  → NO-FLOW (reproduced). With C-3, the taint gate is blind to the entire
  interface-dispatch surface while still emitting the proven pole.

#### C-5 · taint: false NO-FLOW — a struct value carrying a tainted field, passed whole to non-first-party code, escapes nothing **[reproduced]**
- **Where:** `internal/static/taint/taint.go:445-503`
  (`propagate`/`handleStore`).
- **Defect:** storing a tainted value into a field taints the `(type, field)`
  key without tainting the enclosing struct value; handing the struct to an
  unmodeled callee (`fmt.Println(r)`) is never an escape, so the cone reads
  complete → NO-FLOW. Reproduced on `r.Secret = secretC(); fmt.Println(r)` —
  exactly the "this PII field cannot reach this log" gate the package exists
  for.
- **Fix for C-3/C-4/C-5 (one change-set):** the package doc promises "taint
  passed into non-first-party code … is an escape" and "any value that escapes
  a modeled construct sets escaped"; the invoke and field-carrier cases
  violate both. Minimal sound repair: (a) in `handleCall`, when taint flows
  into an invoke-mode call whose concrete callees cannot be enumerated,
  `escape` (→ ABSTAIN); (b) index `callsTo` and source seeding over the call
  graph's resolved callees for invoke sites (the RTA/VTA result is already in
  hand), or escape the invoke result when taint reaches it; (c) when a tainted
  value is stored into a struct field, propagate taint to the struct alloc so
  whole-struct uses escape. Conservative over-escaping only costs precision
  (ABSTAIN), which is the permitted direction. Temper the by-source/by-sink
  decomposition doc comments (`taint.go:262-344`) in the same edit (M-13
  discusses the `--gate` exit-code posture). Regression tests: the three PoC
  shapes above.

#### C-6 · fitness: `no_concurrent_reach` blind probe surveys only the *resolved* concurrent cone — an unresolved `go` yields a silent clean pass, even under `require_proof` **[reproduced]**
- **Where:** `internal/groundwork/fitness/concurrent.go:32-51`.
- **Defect:** the check seeds its universe from edges with `Concurrent: true`
  and probes blindness via `frontierBlindSiteWith(ix, cone, effects)` over the
  spawned functions' forward closure only. A `go someFuncValue()` whose target
  could not be resolved produces **no edge**, only a `ConcurrentDispatch`
  blind spot at the *spawning* function — which is not in the cone, so the
  probe never sees it. Reproduced: graph with a bindable `boundary:db DELETE`
  target, zero concurrent edges, and a `ConcurrentDispatch` blind spot →
  **zero findings** for a `require_proof: true` rule. `must_not_reach` handles
  the analogous case correctly; only this check has the hole. Secondary: the
  `direct` concurrent boundary edges are never probed for `IsDynamic()`.
- **Fix:** extend the blind probe to (a) any `ConcurrentDispatch` blind spot
  anywhere in the graph (an unresolved spawn is a concurrent entry the cone
  cannot represent) and (b) `direct` edges with `e.IsDynamic()`. Consider
  probing all blinding kinds graph-wide for this rule (a reflect-invoked body
  can spawn). Regression test: ConcurrentDispatch spot + no concurrent edges +
  `require_proof: true` must yield a caution/violation, not silence.

#### C-7 · canon: `promote.Filter` silently deletes members of `Unordered` groups — a tier-1 effect can vanish from a post-hoc golden **[reproduced]**
- **Where:** `internal/canon/promote/promote.go:76-84` (with the
  now-false comment "Sequential group: exactly one member" at :76);
  unordered groups minted at `internal/canon/canon.go:372`.
- **Defect:** `Filter`'s non-concurrent branch assumes exactly one member, but
  post-hoc canonicalization produces multi-member `Unordered` groups; `Filter`
  inspects only `g.Members[0]` and ignores the rest.
- **Reproduced:** an unordered pair [tier-3 `Auth.check` (sorts first),
  tier-1 `DB postgres INSERT ledger`] canonicalized to a snapshot whose only
  surviving op was the HTTP root — **the tier-1 DB write silently erased**
  from a freshly minted golden (and from everything impeach/severance
  consumes). Mirror case: a kept-first member retains the whole group, so
  sub-threshold internal compute **leaks into** the golden. Which failure you
  get depends on the members' op-key sort order. No test covers unordered
  groups in `promote_test.go` (only `seq`/`conc` helpers exist; the posthoc
  tests all use tier-1 publishes so `Filter` is a no-op there).
- **Fix:** add an explicit `Unordered` branch symmetric with the `Concurrent`
  one (per-member keep/promote with the same lossless-promotion rule, re-sort,
  `Unordered: len>1`); assert the sequential branch's single-member invariant
  (fail closed on violation); fix the comment. Tests: promote unit tests for
  unordered keep/drop/promote, plus an end-to-end post-hoc regression with a
  droppable member ordered before a tier-1 member.

#### C-8 · capture: `Concurrent` classifies two sequential spans on the *same* worker goroutine as a race — a confidently wrong structural claim in goldens **[reproduced]**
- **Where:** `capture/capture.go:277-286`.
- **Defect:** the structural branch tests only
  `a.Goroutine != parentGoroutine && b.Goroutine != parentGoroutine`; it never
  compares `a.Goroutine` to `b.Goroutine`. Reproduced: `a{G:7}`, `b{G:7}`
  sequential, parent G1 → `Concurrent == true`. Two spans on one goroutine
  are serialized by construction; the golden emits
  `ChildGroup.Concurrent: true` (documented as *asserting parallelism*) and
  canon then reorders the members by canonical key, erasing their real
  happens-before order. Hits the extremely common single-worker /
  dispatch-queue / pipeline idiom; also perturbs loop-collapse shape (a
  sequential `1..*` repetition becomes a concurrent group). Deterministic,
  but *confidently wrong* — the prime directive's named worst outcome.
- **Fix:** require `a.Goroutine != b.Goroutine` in the structural branch
  (same-goroutine siblings fall through to `overlaps`, which is correctly
  false for serialized spans). Update the doc comment ("dispatched onto worker
  goroutines" → "onto *distinct* worker goroutines"). Regression test:
  `a.Goroutine == b.Goroutine != parent` must not be concurrent.

### HIGH

#### H-1 · canon/sql: tokenizer leaks literal fragments through backslash-escaped quotes — redaction promise broken **[reproduced]**
- **Where:** `internal/canon/sql/sql.go:54-69`.
- **Defect:** the string scanner honors only `''` doubling, not `\'`
  (MySQL/MariaDB default); an escaped quote terminates the literal early and
  the remainder is tokenized as identifiers. Reproduced:
  `INSERT INTO t (a,b) VALUES ('O\'Brien', 'secret-value-42')` →
  `…VALUES ( ? brien ? secret - value - ? ?` — raw captured user data reaching
  the canonical `db.statement` projected into goldens (`canon.go:478`).
  Simultaneously a hidden disclosure and per-run golden churn; also corrupts
  `Operation`/`Table` extraction. The idempotence fuzzer cannot see it.
- **Fix:** treat `\` inside a quoted literal as consuming the next byte
  (conservative over-consumption only widens the `?`). Ship a leak-oriented
  property test: *no byte sequence from inside a quoted literal may survive
  into `Normalized.Statement`* — this is the property the module actually
  promises, and it is stronger than idempotence.

#### H-2 · canon: unreachable spans (parent cycles / self-parents) are silently dropped from a `Complete=true` capture **[reproduced]**
- **Where:** `internal/canon/canon.go:59-81` (assembly), `:516-538`
  (`orphans`).
- **Defect:** orphan surfacing only catches spans whose parent ID is *absent*
  from the set. Spans in a parent cycle have present parents, are never
  orphans, are unreachable from the root, and are silently omitted.
  Reproduced: root + two spans in a 2-cycle each carrying `DELETE FROM …`
  canonicalized without error to a snapshot with zero children. Post-hoc OTLP
  from arbitrary collectors is untrusted input; this is fail-open in the first
  line of defense against a false golden.
- **Fix:** after assembly, compare the count of spans reachable from the root
  plus orphan heads against `len(cf.Spans)`; on mismatch (cycle, duplicate-ID
  shadowing — see also the L-list) refuse with an `ErrIncomplete`-class error.

#### H-3 · canon: allowlisting `db.statement` replaces the normalized statement with the raw one **[reproduced]**
- **Where:** `internal/canon/canon.go:472-497` (`projectAttrs`).
- **Defect:** `out["db.statement"]` is first set to the SQL-normalized form,
  then the allowlist loop unconditionally overwrites `out[k]` with
  `c.redact(k, raw)`; raw SQL matches no placeholder shape, so the natural
  config `attributeAllowlist: ["db.statement"]` silently defeats both the
  redaction and byte-identity (every run's literals differ). Same family:
  allowlisting `db.query.text` projects the raw statement alongside.
- **Fix:** in the allow loop, skip keys already explicitly projected
  (`db.statement`, `db.query.text`, `capture.FQNTagKey`) or route them
  through the SQL normalizer. Test pinning "allowlisted db.statement is still
  normalized".

#### H-4 · graphio: `Build` panics (nil deref) whenever a graph node's own function matches a `classify.db` hint **[reproduced]**
- **Where:** `internal/static/graphio/graphio.go:1350` (`nodeTier` computes
  the compute-floor via `ext.Edge(fn, fn, nil)`);
  `internal/static/features/features.go:75,153-182` (`Edge` → `dbEffect` →
  `constSQLOp`); `internal/static/features/hints.go:236-244` (`StringArgs`
  derefs `site.Common()` with no nil guard).
- **Defect:** a `.flowmap.yaml` with `classify.db:` naming a first-party
  DB-wrapper package — the exact hint pattern the config docs bless
  (`internal/config/config.go:362-366`) — crashes `Build` with a nil-pointer
  panic for every reachable node in the hinted package. Latent siblings:
  `edgeOf` passes `e.Site` into `busEdges`/`httpLabel`/`dbLabel`
  (graphio.go:1119-1140) which all deref it, though `callgraph.Edge.Site` is
  documented "nil for synthetic edges".
- **Fix:** nil-site guards in `StringArgs`/`constSQLOp`/`sinkMethodName`
  (fail closed to the dynamic/method-name label) and/or skip the hint switch
  for `nodeTier`'s synthetic self-edge. Regression test: a fixture with a
  first-party package under `classify.db`.

#### H-5 · fitness: `bindsAnyTarget` uses a different "is boundary edge" predicate (`e.Boundary != ""`) than every other consumer (`e.IsBoundary()` = `To`-prefix) — can mask a real reachable violation as an "unbindable target" caution **[reproduced]**
- **Where:** `internal/groundwork/fitness/reach.go:112-124` vs
  `internal/groundwork/graph/graph.go:499`; the skip guards in
  `checkMustNotReach`, `checkMustPassThrough` (`passthrough.go:33`),
  `checkNoConcurrentReach` (`concurrent.go:60`).
- **Defect:** a decoded-valid graph with
  `{"from":"svc.From","to":"boundary:db DELETE ledger"}` and an empty
  `Boundary` *field* yields **no violation** under
  `must_not_reach {to:["boundary:db DELETE"]}` — only the "binds nothing"
  caution — because the rule short-circuits before `evalReach`, whose own walk
  keys on the prefix and would have found the path.
- **Fix:** `bindsAnyTarget` uses `e.IsBoundary()`; additionally have
  `graph.Load` reject edges where `IsBoundary() != (Boundary != "")`
  (fail-closed decoder invariant). Parity test.

#### H-6 · fitness: layering `exempted` lacks the empty-side wildcard that one-sided allow entries require — half-working exceptions split by receiver shape **[reproduced]**
- **Where:** `internal/groundwork/fitness/layering.go:114-121` vs
  `internal/groundwork/policy/policy.go:175-185` (`PassRule.Allowed`, the
  sibling that implements `pat == "" || MatchPrefix(...)`).
- **Defect:** `policy.Validate` explicitly permits one-sided layering
  exceptions; `exempted` calls `MatchPrefix(from, ex.From)` bare, and
  `MatchPrefix(s, "")` is `len(s)>0 && !isIdentByte(s[0])`. Confirmed: an
  entry `{To: "…store.Find"}` fails to exempt a violating edge whose From is a
  free function but *does* exempt every method edge (`'('` is a non-ident
  byte). Persistent false violations on one side, an undocumented accidental
  wildcard on the other, and the `Exceptions` audit mis-reports through the
  same matcher.
- **Fix:** hoist ONE shared exception matcher with the `pat == "" ||` clause
  (tenet 5), used by `exempted` and `Allowed`. Test with a one-sided entry
  over a free-function edge and a method edge.

#### H-7 · review: the pre-flight gate silently discards newly-introduced cautions — the disclosure channel fails open exactly where agents converge **[code-verified]**
- **Where:** `internal/groundwork/review/gate.go:113` —
  `newViolations, _, standingCautions, _, _ := newFindings(...)`; `GateResult`
  has no `NewCautions` field; `Render` never mentions them.
- **Defect:** a branch that makes an `io_budget` newly unenforceable or
  introduces a new blind-frontier `must_not_reach` caution gets
  "PASS — No new violations, no scope escapes, no breaking contract changes."
  with zero disclosure. The gate's own R1 rationale (gate.go:27-33) argues new
  cautions are *more* attention-worthy than standing ones — only standing ones
  were plumbed through, and no comment or test pins the drop as deliberate.
- **Fix:** add non-blocking `NewCautions []Violation` to `GateResult`
  (digest-covered like every other field), render on PASS and BLOCK, pin with
  a test mirroring `TestGateSurfacesStandingCautionOnPass`.

#### H-8 · ground: the card's `io_budget` binding predicate is a hand-rolled duplicate that has already drifted from the enforcer **[code-verified]**
- **Where:** `internal/groundwork/ground/ground.go:164` vs
  `internal/groundwork/fitness/budget.go:135-141` (`RouteWrites`).
- **Defect:** the card claims the budget binds any caller-less function;
  the enforcer defines a route as a **non-root** entrypoint
  (`ix.Sources()` minus `p.RootPackages()`). The card tells an agent editing
  `main` (or any layering-root function) that the budget binds it when
  `checkIOBudget` will never charge it — a false "guardrail binds here" from
  the package whose doc promises "the same matchers the checks use"
  (ground.go:9-12). `ground_test.go` has no io_budget coverage at all.
- **Fix:** a shared `fitness.IsRoute(p, ix, fqn)` helper used by both;
  regression test with a root-package source. Coordinate with M-9 (the
  io_budget root-predicate triple-implementation).

#### H-9 · MCP: the `fitness` tool drops every substrate/provenance disclosure the CLI prints — a "clean green" laundering channel for the agent loop **[reproduced]**
- **Where:** `cmd/groundwork/mcp.go:818-833` vs `cmd/groundwork/main.go:571-596`.
- **Defect:** `cmdFitness` computes `g.Caveats` + `SubstrateMismatchCaveat` +
  `ReclaimCaveat` and prints `graph.ProvenanceLine` precisely so "an
  unsound-substrate pass cannot annotate a PR as a clean green run" (its own
  comment; enforced for SARIF by `sarif_test.go`). The MCP `fitness` case
  prints only rule+summary — no provenance line, no caveats — and drops the
  per-finding `From→To`/`Detail` witness lines `printFinding` treats as
  load-bearing (main.go:626-636). Reproduced: vta-substrate policy against an
  rta graph — CLI prints the full mismatch caveat; the MCP tool answers
  exactly `"all invariants hold; no cautions\n"`. The ground→edit→verify agent
  loop reads an unqualified green.
- **Fix:** extract `cmdFitness`'s caveat assembly into a shared helper;
  prefix the MCP fitness text with `graph.ProvenanceLine(ix.Algo(), caveats)`;
  include witness lines per finding. MCP-parity test mirroring
  `TestSARIFCarriesCaveats`.

#### H-10 · fitness CLI + SARIF omit `SQLFoldCaveat` — a fold-informed verdict is disclosed by review/verify but not by fitness **[reproduced]**
- **Where:** `cmd/groundwork/main.go:577-583` vs
  `internal/groundwork/review/provenance.go:43`;
  `internal/groundwork/graph/graph.go:420-429` ("a verdict that leaned on one
  **must** disclose it").
- **Defect:** two hand-rolled caveat assemblies; fitness's fell behind.
  Reproduced: a graph with a `via:"sql-constfold"` boundary edge —
  `groundwork review` discloses "sql-fold-informed…"; `groundwork fitness`
  prints a clean substrate line and `fitness OK`; SARIF inherits the gap.
- **Fix:** one shared caveat-assembly helper for the single-graph gate
  surface (also feeds H-9's helper); extend `TestSARIFCarriesCaveats` with a
  folded fixture. Note the adjacent wording bug: `SQLFoldCaveat` counts every
  via-tagged boundary edge, so a `topic-constfold` fold is described as "DB
  effect verb(s) recovered from constant-fragment SQL" (`graph.go:430-436` +
  `graphio/labels.go:23`) — split the caveat by via kind while in there.

#### H-11 · sqlfold: false READ — a user accumulator method with an invisible dynamic append is folded to a complete constant SELECT **[reproduced]**
- **Where:** `internal/static/sqlfold/summary.go:66-118` (`writerTemplate`).
- **Defect:** `writerTemplate` scans only `strings.Builder` method calls and
  `continue`s past every other call, so a method mixing a visible
  `WriteString` with an invisible `fmt.Fprintf(&q.sb, "%s", userInput)`
  produces a template omitting the dynamic splice; `assembleBuilder` reports
  `complete=true` and `Recover` classifies a runtime-spliced statement as a
  wholly-constant read — the SQL-injection-smuggling case the package doc says
  read-classification must prevent. Reproduced (`op="SELECT" ok=true`).
- **Fix:** fail closed on any call inside the method that the pass cannot
  classify and that can touch the receiver's builder (builder value or
  receiver flowing into any non-modeled call = a hole → abstain). Regression
  test with an `Fprintf`-splicing accumulator.

#### H-12 · schemadrift: DDL inside PostgreSQL dollar-quoted bodies (`$$…$$`) is scanned as real schema DDL, masking drift **[reproduced]**
- **Where:** `internal/static/schemadrift/migrations.go:202-241` (`stripSQL`),
  `:262-283` (`splitStatements`).
- **Defect:** `stripSQL` strips `--`, `/* */`, and single-quoted literals but
  not dollar-quoted strings; a `CREATE TABLE audit_x` inside a plpgsql
  function body becomes a phantom defined table that masks real drift for a
  code write to that table, with no `ParseCaveat`. Reproduced (`drift=[]`,
  `caveats=[]`). The package's own soundness note names this as the unsound
  direction.
- **Fix:** teach `stripSQL`/`splitStatements` dollar-quoting
  (`$tag$…$tag$`) — strip such bodies like literals, or emit a fail-closed
  `ParseCaveat` whenever a `$…$` delimiter is seen. Tests both ways.

#### H-13 · otlpjson: snake_case OTLP support is partial — a fully snake_case (proto-JSON) export decodes to zero spans with no error, and the behavioral gate goes vacuously green **[reproduced]**
- **Where:** `internal/otlpjson/otlpjson.go:272-302` (envelope tolerates
  `resource_spans`; `scopeSpans`/`spanJSON` accept only camelCase);
  downstream `cmd/flowmap/main.go:788-796`.
- **Defect:** a full snake_case document decodes to `spans=0, err=nil`
  (`sawEnvelope` is true, so the `errNotOTLP` fail-closed guard at line 148 is
  bypassed); ingest prints "0 span(s), nothing to map" and exits 0 **before
  `gateEffectGoldens` runs** — committed effect goldens silently not asserted;
  impeach corpus silently empty. The half-tolerance is exactly the fail-open
  the `resourceSpans()` doc comment warns about, one level down. Only the
  *empty* snake envelope is tested today (`otlpjson_test.go:233`).
- **Fix:** either extend snake_case tolerance uniformly (`scope_spans`,
  `span_id`, `trace_id`, `parent_span_id`, `start_time_unix_nano`, …) or drop
  the envelope tolerance so such a file fails as `errNotOTLP`. Independently:
  make the ingest gate path fail loudly when a flows dir contains committed
  goldens but zero flows decoded. Test with a full snake_case document.

#### H-14 · flow (public API): `Flow.Tier()` accepts any string and silently degrades to the `warn` default **[code-verified]**
- **Where:** `flow/flow.go:124` + `internal/config/config.go:664-669`
  (`SalienceThreshold` falls back to 2 for unknown names; validation happens
  only in `config.Load`, which `Tier` bypasses).
- **Defect:** `Tier("Info")`, `Tier("trace")`, `Tier("al")` silently produce a
  warn-threshold golden — a public-API fail-open that contradicts tenet 2; the
  config-file path hard-errors on the same typo.
- **Fix:** validate against the `warn|info|debug|all` vocabulary in `Run`
  (`t.Fatalf` on unknown), sourcing the set from one shared place with
  config. Test both paths.

---

## 4. Findings — MEDIUM

Grouped by theme. Each is a self-contained work item.

### Determinism residues (tenet 1)

- **M-3 · canon: equal-start sibling order falls back to run-varying span IDs
  (in-process profile).** [reproduced] `internal/canon/canon.go:189-194`,
  `:528-533`; same pattern `capture/capture.go:255-262`. When two
  non-concurrent siblings share a start timestamp and no goroutine signal
  disambiguates, group order — and golden bytes — follow the per-run random
  span IDs, despite the manifest's `Discards.IDs: "dropped"` claim. Narrow
  trigger (zero-duration spans, coarse clocks). Fix: break start-time ties on
  intrinsic data (op key, then subtree signature; an exact tie becomes an
  `Unordered` group, matching the post-hoc philosophy); extend the canon
  fuzz generator to same-goroutine/zero-goroutine siblings.
- **M-4 · cmdChains: broker-conflict error text is map-iteration-dependent;
  the MCP twin was already fixed for exactly this.** [code-verified]
  `cmd/groundwork/main.go:512-521` vs `cmd/groundwork/mcp.go:777-790` (whose
  comment names the bug). Fix: one shared conflict-collecting merge helper
  (e.g. `policy.MergeBrokers`), sorted conflict names, used by both; test with
  two conflicting brokers.
- **M-5 · render: Mermaid service-name fold merge is map-iteration-dependent.**
  [code-verified] `internal/render/render.go:496-498` — `byFold[foldSeparators(s)] = s`
  over a map; two services differing only in `_` vs `-` merge onto whichever
  iteration visits last. The loop just above (:485-492) was deliberately
  sorted first-wins for the same reason. Fix: iterate sorted, pinned rule.
- **M-20 · callgraph: FQN sort has no tie-break, and instance names are
  documented non-unique.** [code-verified]
  `internal/static/callgraph/callgraph.go:171-177` (unstable `sort.Slice`
  by FQN over map-iteration input), `:192-199` (`Lookup` returns first of
  possibly several), `graphio.go:1360`. Currently masked because C-1 excludes
  instances; **becomes live the moment C-1 is fixed** — fix together. Total
  tie-break on intrinsic data (origin package path + full `types.TypeString`
  of targs) or fail loudly on collision; determinism test.
- **M-22 · statictest.FindFunc returns the first match of a map iteration** —
  ambiguous substrings make the test suite itself flaky.
  `internal/static/statictest/statictest.go:50-57`. Collect all matches;
  panic on >1 (force disambiguation).
- **M-23 · reclaim: unstable sort keyed on `RelString(nil)` alone over
  map-ordered input** (`internal/static/reclaim/middleware_resolve.go:138-140,
  365-367`); `obligations/summaries.go:162-167` adds a `Pos()` tie-break for
  exactly this generic-instantiation tie — mirror it.

### One source of truth / drift (tenet 5)

- **M-7 · The `"boundary:db "` / `"boundary:bus "` label grammar is re-typed
  across ~10 packages with no shared constant and no guard test**
  (graphio producer, `fitness/propose.go` ×8, `impact/resolve.go`,
  `impeach/{effectkey,severance}.go`, schemadrift's private
  `dbBoundaryPrefix`, …). The op-key prefixes got the exemplary
  `TestNoHardcodedOpKeyPrefix` repo-scan treatment; these did not. Fix: hoist
  into a shared package (natural home: alongside `effectkind`/`opkey`) and
  extend the prefix-guard test pattern to them.
- **M-8 · Obligation verdict vocabulary duplicated without a direct parity
  test** — `internal/static/obligations/obligations.go:56-61` (producer
  constants) vs `internal/groundwork/fitness/obligations.go:13-18` (re-typed
  consumer strings). Deliberate and commented ("the graph JSON is the
  interface"), consumer fails closed on unknowns — but per CLAUDE.md's own bar
  the parity needs a named test (like schemadrift's `TestVerbParity`), not
  just an indirect fixture golden.
- **M-9 · io_budget composition-root predicate exists in three divergent
  forms.** [reproduced] `internal/groundwork/fitness/budget.go:14-22,135-140`
  (enforcer: `PkgOf(src) ∈ p.RootPackages()`, which is empty without
  layering — so a policy with io_budget but no layering **charges `main` as a
  route**, contradicting the function's own doc comment) vs
  `fitness/propose.go:439-453,469-476,645-653` (proposers:
  `strings.HasSuffix(s, ".main")`, which also matches any function named
  `main` anywhere). When `proposeLayers` withdraws (package cycle, <2
  packages), init measures the budget over routes-without-main while `Check`
  enforces over routes-including-main: either the budget silently inflates to
  main's write count (weakened gate for every real route) or `init` emits a
  policy that fails its own gate, breaking the documented self-clean
  invariant. Fix: ONE root predicate — `RouteWrites` falls back to the graph's
  `CompositionRoots()` when `RootPackages()` is empty; proposers call the
  enforcer's predicate; update the comment; extend the property-test generator
  with `.main` roots. (Coordinates with H-8.)
- **M-10 · canon: DB operation-derivation precedence duplicated**
  (`opkey.go:259-269` vs `canon.go:595-609`) — export one
  `opkey.DBOperation(attrs)` used by both, parity comment + test.
- **M-11 · CLI↔MCP triage/reach duplication with no shared helper or parity
  test** (`cmd/groundwork/main.go:202-274` vs `mcp.go:859-896`; CLI `reach`
  and MCP `reach` are two different cards under one name). Extract a shared
  `triageCard(ix, symptom)`; converge or explicitly rename the reach renders.
- **M-12 · graphio: two resolvers for `--entry` disagree on ambiguity**
  (`scope.go:127-134` picks the first match arbitrarily;
  `mermaid_rooted.go:153-168` fails closed on >1 distinct handler). Share one
  resolver; `Build` errors on ambiguity.

### Fail-open / disclosure gaps (tenets 2–3)

- **M-13 · `flowmap taint --gate` exits 0 on ABSTAIN** — the must-not-flow
  gate passes when no-flow could not be proven (`cmd/flowmap/main.go:550-555`;
  the comment defers "strict fail-closed mode" as a follow-up). With C-3..C-5
  fixed, escapes become the *common* outcome, making this the operative
  soundness posture of the gate. Add `--strict` (fail on ABSTAIN) and a loud
  stderr warning in gate mode; document the default.
- **M-14 · obligations: `usesRecover` accepts an unresolvable dynamic deferred
  func as non-recovering** (`obligations.go:260-289`) — documented, accepted
  residual; recorded here because it is the second guard protecting SATISFIED
  alongside C-2. No action beyond C-2 + keeping the disclosure accurate.
- **M-15 · graph.Load silently ignores trailing data after the first JSON
  value** [reproduced] (`internal/groundwork/graph/graph.go:509-520`) — a
  truncated/concatenated graph file should be refused: check `dec.More()`/EOF.
- **M-16 · graph.NewIndex tolerates edges whose `From` is not a declared
  node** (`internal/groundwork/graph/index.go:48-66,79-84`) — such an edge
  silently revokes the callee's source status (drops it out of the
  `entrypoint:*` universe of io_budget/read-only/must_pass_through) and
  injects non-node FQNs into `Reaching`. Reject at Load/NewIndex (the mirror
  "edge to unknown target" case is already handled), or document + test.
- **M-17 · io_budget with zero bound routes passes with no vacuity
  disclosure** (`budget.go:22-61`) — every other rule kind discloses inert
  binding (`inertRuleFinding`, `unbindableTargetFinding`, `UNMATCHED`). Emit a
  Caution when `len(routes)==0 && p.IOBudget != nil`.
- **M-18 · capture: spans that lose the baggage context are silently excluded
  with no disclosure** (`capture/capture.go:219-245`) — a SUT span opened from
  a fresh `context.Background()` (the classic lost-ctx instrumentation bug)
  vanishes from the golden while `Complete=true`. Sound direction for
  impeachment, but tenet 3 says disclose: report the count of in-window
  correlation-less spans on `CapturedFlow` (deterministic marker) or via
  `t.Logf`.
- **M-19 · await/flow: quiet-only completion can silently drop a late
  fire-and-forget effect** (`internal/await/await.go:69-86`) — without
  markers, an async effect landing after Quiet (2s) is dropped with
  `Complete=true`; straddling the boundary makes the golden itself flake.
  Document loudly on `CaptureOptions`/`Flow.Expect` that fire-and-forget flows
  must declare markers; optionally disclose when completion was decided by
  quiet alone while spans were still arriving near the boundary.
- **M-24 · canon/sql: comments and quoted identifiers are not tokenized**
  [reproduced] (`sql.go:45-107`, `:179-189`). `--`/`/* */` payloads (where
  per-request volatile data lives, e.g. sqlcommenter) churn goldens and leak
  identifier-shaped fragments; double-quoted/backtick identifiers become `?`
  so `Table` extraction returns `""` and structurally distinct statements
  merge under one op key. Strip comments; treat `"`/backtick as identifier
  quoting (lower-cased). Extend the fuzz corpus. (Pairs with H-1.)
- **M-25 · canon/opkey: table casing diverges between attribute-supplied and
  statement-derived tables** [reproduced] (`opkey.go:258-278`) —
  `db.sql.table: "Applicants"` vs statement-derived `applicants` produce two
  op keys for one table, splitting the impeachment join and churning goldens
  on instrumentation upgrades. Lower-case the attribute-sourced table; test.
- **M-26 · canon/promote: a `1..*` loop marker is silently discarded when its
  wrapper is contracted** [reproduced] (`promote.go:83-84`) — the IR then
  asserts "once" where the truth is "1..\*". Propagate `g.Multiplicity` onto
  spliced child groups. (Fold into the C-7 change-set.)
- **M-27 · contract.Compare never checks the two contracts belong to the same
  service** (`internal/groundwork/contract/contract.go:111-117`) — a
  copy-paste artifact mix-up produces a plausible mostly-breaking diff and a
  spurious BLOCK. Refuse on `base.Service != branch.Service`; test.
- **M-28 · reviewtriage: `changedFns` misses functions that only *lost* an
  edge/effect, and the package doc overstates the changed set**
  (`reviewtriage.go:426-450`, doc at :36-37) — a body change that removed the
  auth-check call appears in no triage zone. Mark `e.From` changed for base
  edges absent from the branch; correct the doc.
- **M-29 · review: io_effects delta is keyed per source-edge but rendered per
  label** (`review.go:370-385`, `delta.go:169-181`) — duplicate `+` rows and
  phantom `-`/`+` pairs on a pure emitter move; the contract surface fixed
  this class (R10), io_effects didn't inherit. Dedupe on `(Op, Effect, Write)`
  dropping cancelling pairs, or carry `From` honestly in the row.
- **M-30 · impeach: `canonicalDigest` swallows the marshal error and returns
  `""`** (`internal/impeach/audit.go:596-606`) — every failing trace would
  dedup onto one `""` corpus entry; the one fail-open cell in an otherwise
  rigorously fail-closed package. Propagate the error or emit a non-colliding
  sentinel.

### Process / CI (trust boundary)

- **M-2 · CI does not mirror `make verify`.** `.github/workflows/gates.yml:43`
  gates only the loansvc fixture while `Makefile:32-33` gates loansvc **and**
  impeachsvc (the behavioral-impeachment golden can regress without CI
  noticing); CI runs `go test -race` while `make test` does not (local green ≠
  CI green, inverting the CLAUDE.md claim that CI mirrors it exactly). Fix:
  add impeachsvc to CI; add `-race` to `make test` (or a `test-race` target
  that `verify` includes); also align the Makefile gofmt exclusion (covers
  loansvc but not impeachsvc) and pin/verify the golangci-lint version used
  locally vs CI (unpinned local vs v2.5.0 pinned in CI), and reconcile the Go
  version split (go.mod 1.24.0 / go.work 1.25.0 / CI 1.25.11).
- **M-21 · Nightly fuzz covers 3 of 12 fuzz targets.** `fuzz.yml` runs
  FuzzGraphLoad, FuzzContractLoad, FuzzCanonConcurrentOrderInvariant; absent:
  FuzzCanonFQNSymmetry/Total, FuzzWitnessSortStrictWeakOrder (impeach),
  FuzzMarshalDeterministic (canonjson), FuzzNormalizeIdempotent (canon/sql —
  the one that already found a real bug), FuzzTemplateInvariants (canon/url),
  FuzzRebindConfinement, otlpjson targets. Enumerate targets dynamically or
  list them all; add the new leak-property fuzzer from H-1 when it exists.
- **M-6 · No digest field-coverage self-check on the trust boundary**
  (`review_test.go:510` flips only `Verdict`). A future `json:"-"` field
  (the pattern already exists: `impeach.Resolution.Origin`) would silently
  escape the digest. Reflection-based test perturbing each exported field of
  `Artifact`/`GateResult`, asserting the digest moves.
- **M-1 · harness (public) goroutine-id self-check.** `harness/goid.go:16-19`
  claims a parsing regression "surfaces as a determinism self-test failure —
  never a silently wrong golden," which is only true inside this repo; in a
  consumer repo a Go-runtime stack-header change silently degrades every span
  to `Goroutine: 0`, flipping `Concurrent` to timing-dependent overlap. Have
  `install()`/`NewInProcess` self-check `goid() != 0` once and fail/warn
  loudly through `TB`; soften the comment.

### Public API / behavioral fidelity

- **M-31 · harness: `fromOTel` discards span Links and TraceID**
  (`harness/harness.go:454-489`) — an in-process SUT emulating a broker
  hand-off (new root + FOLLOWS_FROM link, the pattern `ingest.stitch` recovers
  post-hoc) silently loses the continuation; the `Span.Links` doc claim is
  false for that case. Map links + trace id, or state the single-trace
  restriction as a hard documented contract.
- **M-32 · harness: `statusRecorder` hides `http.Flusher`/`Hijacker`/
  `io.ReaderFrom`** (`harness.go:444-452`) — streaming/SSE handlers take a
  different code path under the harness than in production. Add `Flush()`
  passthrough and `Unwrap() http.ResponseWriter`.
- **M-33 · flowmap CLI ergonomics with correctness edges:**
  `behavior ingest --update` without `--flows-dir` is a silent no-op
  (`cmd/flowmap/main.go:843-857` — error out); `flowmap <cmd> -h` exits 1
  with `flag: help requested` (groundwork fixed this centrally,
  `main.go:74-82` + test; port it); help text omits real flags on both CLIs
  (`groundwork fitness -h` omits `--sarif`, `verify -h` omits
  `--corpus`/`--capture`, `init -h` omits `--out`; flowmap `usage()` omits
  `--stamp/--diff/--root/--max-nodes/--show-plumbing/--all-blind-spots/
  --rollup-bands` and taint's `--algo`) — reconcile and add a usage-parity
  test; the usageBody command list is interrupted mid-list by the `--expect`
  prose (`main.go:179-183`).
- **M-34 · MCP transcript session ids collide across server restarts**
  (`cmd/groundwork/mcp.go:218,437-446`) — O_APPEND log + per-process ids
  restarting at 1 silently merge unrelated sessions in the E4 evidence. Derive
  a run epoch from existing log content (deterministic) or require
  `--log-append` opt-in.
- **M-35 · roots: a registration with a resolved handler but a non-constant
  route is silently dropped from the entrypoint surface, and the comment
  claims otherwise** (`internal/static/roots/roots.go:435-459`, false comment
  at :479-481, drop at `graphio.go:604`) — the route vanishes from
  `Entrypoints`, the frontier's route universe, and
  `RouteEntrypointCount`'s denominator with no disclosure. Record a blind spot
  (or a `<dynamic>` entrypoint sentinel); fix the comment in the same edit.

---

## 5. Findings — LOW (aggregated work list)

Grouped; each line is `file:line — defect → fix`.

**Stale / false comments (tenet 6 — fix in the same PR as the code they
describe where applicable):**
- `internal/canon/promote/promote.go:76` — "exactly one member" false for
  Unordered groups (part of C-7).
- `internal/canonjson/canonjson.go:11-12` — "survive verbatim instead of
  becoming `"<uuid>"`" says X-instead-of-X; second literal meant the escaped
  form.
- `internal/canon/canon.go:553-564` — `signature` doc has two
  stitched-together intro paragraphs.
- `internal/canon/promote/promote.go:89-97` — `sortByOp` overclaims that
  promoted children keep bySig order; state the weaker invariant.
- `internal/static/graphio/graphio.go:262-267` — `Node.Package` "empty only
  for a synthetic node" unreachable today (part of C-1).
- `internal/static/graphio/mermaid.go:43-49` — `MaxNodes` field doc says
  "first-party nodes"; implementation caps the full drawn set (safe
  direction; doc stale).
- `internal/static/graphio/graphio.go:1108-1116` — edgeOf init-callee branch
  reads as live behavior; it is a backstop (scope excludes inits).
- `internal/groundwork/graph/graph.go:453` — "Shared by fitness (as a
  Caution)" contradicts `fitness_test.go:46-59`.
- `internal/config/config.go:600-607` — justification comment claims an empty
  prefix would match every package; the matcher makes it match nothing.
  Fix comment; add the missing parallel non-empty check for
  `ExternalBoundaryTrivial`.
- `ir/ir.go:36-37` — equality headline ignores `Stamp`/`Provenance` too, not
  just Discards.
- `cmd/flowmap/main.go:1296-1298` — stacked stale duplicate doc comment on
  `dirArg`.
- `internal/groundwork/transcript/transcript.go:42-55` — `Load` claims strict
  fail-loud but `Tool()` swallows decode errors into `"(unnamed)"`; validate
  `Call` decodes to a named object at Load time.

**Robustness / fail-closed polish:**
- `internal/canon/canon.go:61-64` — duplicate span IDs tolerated
  (last-writer-wins + subtree duplication); fold into H-2's completeness
  check.
- `internal/canon/canon.go:56` — zero-value `CaptureMode` silently selects the
  in-process (less conservative) profile; refuse `""` or default to post-hoc.
- `internal/canon/opkey/opkey.go:310-321` — degenerate `"RPC "` op key when
  system set but service/method absent; fall back to raw span name.
- `internal/canon/redact.go:9-10` vs `internal/canon/url/url.go:15-16` —
  byte-identical id-shape regexes duplicated; `url.IsID` doc claims it is the
  shared rule. Fold or add parity test.
- `internal/groundwork/policy/policy.go:373-414` — duplicate-name check only
  for `must_pass_through`; extend to `must_not_reach`/`no_concurrent_reach`
  (findings carry rule names as identity).
- `internal/groundwork/fitness/exceptions.go:73-89` — must_pass_through
  suppression attribution by summary prefix `name + ": "` is not prefix-free
  across rule names; carry the rule name on the Finding or forbid `": "` in
  names.
- `internal/groundwork/graph/index.go:255-272` — `BusEffects` drops malformed
  labels with no tally (`DBEffects` counts its unreadables); add parity tally.
- `internal/groundwork/chains/chains.go:119-129` — consumer entrypoint with
  empty Name seeds an event-`""` card; duplicate fleet service names last-win;
  skip empties, error on duplicates.
- `internal/render/render.go:58-62` — malformed Mermaid for an edge endpoint
  not in `g.Nodes` (empty alias); lazy alias or skip-with-comment.
- `internal/diff/diff.go:79-84` — `Diff(nil, x) == no changes` fail-open
  footgun (current callers safe); sentinel Changed entry or non-nil contract.
- `internal/groundwork/ground/ground.go:91` — hand-rolled blind-spot dedup
  key; call the shared `graph.BlindSpot.DedupKey()`.
- `internal/config/config.go:448-451,515-535` — empty `Pin.Identity` is a
  silently disabled pin; reject at load. Also `config.Version` never
  validated (accepts 0/negative/future); mirror `policy.Validate`.
- `internal/golden/golden.go:151-167` — `Slug` collisions between distinct
  flow names silently last-write; port the collision refusal the cmd-side
  effect-golden writer already has (`cmd/flowmap/main.go:876-878`).
- `internal/coverage/coverage.go:135-149` — empty `d.Peer` emits an
  unmatchable double-space key (forever-unexercised noise row); guard like
  the `ok=false` branch.
- `harness/harness.go:390-419` — `crypto/rand` errors ignored for
  run/trace/span ids; a zero runID would cross-contaminate scoping; check the
  error (panic is fine — fail loudly).
- `internal/static/blindspots/blindspots.go:632-643` — `SortBlindSpots`
  ("the ONE comparator") not total over the struct (Package/Severity not
  compared); make it total.
- `internal/static/blindspots/blindspots.go:452-468` — nil `*types.Named`
  passed to `NamedTypeIs` for bare `func()` values; add a one-line guard.
- `internal/static/sqlfold/summary.go:141-169` — `returnsBuilderString`'s
  existential match is safe only because `assemble` treats a terminal Phi as
  a hole; require every return to yield the builder string (mirror
  `everyReturnThreaded`) or document the dependency.
- `internal/static/roots/roots.go:465-474` — a registrar hint whose handler
  param is interface-typed silently no-ops; add a "hint matched calls but
  never a func-typed handler" disclosure.
- `internal/static/signatures/signatures.go:32` — bare-name package qualifier
  can render identical signatures for same-named packages; comment or
  disambiguate.

**CLI / server polish:**
- `cmd/groundwork/mcp_http.go:109` — token compare leaks length via the
  ConstantTimeCompare short-circuit (compare SHA-256 digests); `TrimPrefix`
  accepts a raw token without the `Bearer` scheme; prefer documenting
  `$GROUNDWORK_MCP_TOKEN` over `--token` on argv.
- `cmd/groundwork/mcp.go:419` — stdio loop silently drops malformed JSON that
  carried an id (client hangs); HTTP answers 400 — transports disagree; send
  `-32700`.
- `cmd/groundwork/mcp_http.go:43-47` — add Read/Write/Idle timeouts; comment
  the session-id threat model (label-only).
- `cmd/flowmap/main.go:224` — `--stamp` silently ignored with
  `--mermaid`/`--rollup`; warn.
- `cmd/flowmap/main.go:983-987` — "no capture this run — skipped" also covers
  canonicalization failures; empty flows dir gates 0 goldens silently; add a
  "gated N golden(s)" disclosure.
- `cmd/flowmap/main.go:883` — corrupt existing golden silently overwritten on
  `--update` (LoadEffectGolden error ignored); comment the intent or refuse.
- `cmd/flowmap/main.go:415-418` — `--library-owned` cannot override config to
  empty.
- flowmap lacks groundwork's verdict-vs-operational exit-code split
  (everything exits 1; CI can't tell "stale contract" from "tool crashed");
  bare `flowmap`/`groundwork` print usage and exit 0.
- `cmd/flowmap/main.go:1147` `splitList` ≡ `cmd/groundwork/main.go:903`
  `splitComma`; trivial consolidation.
- `internal/otlpjson` `DecodePath` globs only `*.json` — `.json.gz` silently
  invisible; doc note at the CLI.
- README.md layout line ~149 omits `groundwork review-triage` and flowmap's
  `--rollup`, `--rollup-bands`, `--reclaim-sql`, `--reclaim-topic`.
- `harness/harness.go:329-341` — O(N²) capture polling across a large suite
  (documented trade-off); index-by-runID or high-water mark when it starts to
  hurt.
- `internal/static/callgraph/callgraph.go:192-205` — `Lookup`/`Reachable`
  linear scans over a sorted slice; `sort.Search` when convenient.
- FQN short-name/package parsing re-implemented ad hoc in ~6 places
  (`fitness/fqn.go:24`, `impeach/canonfqn.go:72`, `chains/chains.go:268`,
  `review/artifact.go:334`, `frontier/frontier.go:522`,
  `cmd/groundwork/main.go:1227-1234`); consider one shared helper if the FQN
  grammar ever changes.

---

## 6. Verified strengths — do NOT "fix" these

Recorded so the implementing agent does not mistake deliberate design for
defects, and because several look suspicious at first glance:

- **Enumeration soundness holds everywhere checked.** The only `pkg.Members`
  walk (`roots.go:338-366`) correctly walks both `MethodSet(T)` and
  `MethodSet(types.NewPointer(T))` with the rationale inline and the
  regression test present. `reclaim` uses `prog.RuntimeTypes()` deliberately.
  Both CLAUDE.md-cited historical bugs are fixed and locked.
- **The reclaimers are genuinely sound-or-abstain.** All four (including the
  newest middleware pair): add-only true edges, all-quantified
  implementations, fail-closed field-escape/ambiguous-store guards, anchored
  seam-clearing that cannot clear a same-site different-type blind spot,
  cycle guards. The frontier bins every blind-spot kind with a
  fails-on-new-kind test.
- **`fitness.IsWrite` vs `graphio.mutatingSQLOp`:** unified via
  `internal/sqlverb` with the parity test — the drift risk named in CLAUDE.md
  is closed. Likewise `effectkind` and the `opkey` prefix-parity repo scan.
- **The digest/verify chain:** covers all exported fields via canonical JSON
  with only `Digest` zeroed; `LoadArtifact` rejects unknown fields; tamper,
  stale, and re-signed forgery are each tested. The "digest is not the anchor,
  recomputation is" framing is correctly implemented.
- **`must_pass_through` core logic** (waypoint-removal reachability),
  **`evalReach`'s three-valued dominance**, and the ExternalBoundaryCall
  disclosure-only carve-out are sound and pinned.
- **Determinism pillars:** canonical JSON marshaler (fuzz-pinned), the
  concurrent-ordering tie-break fuzzers, `token.Pos` never crossing files in
  comparisons, `features.RelFile` path normalization (no absolute paths in
  JSON), `g.Tool` provenance injected at the CLI boundary keeping
  `graphio.Build` pure, `reclaim.implementationsOver`'s documented
  order-insensitivity contract, review/gate/ratchet/impact/ground render
  determinism tests.
- **Strict decoding everywhere:** graph/policy/config/artifact/transcript
  loaders all `DisallowUnknownFields`/`KnownFields(true)`; unknown obligation
  statuses fail closed to caution; goldens double-ratcheted (section-count
  manifest + content digests).
- **CLI trust plumbing:** verdict-vs-operational exit split (groundwork),
  `--expect`/`GROUNDWORK_REQUIRE_STAMP` stamp binding, fail-closed
  corpus/capture validation shared between `verify` and `mcp`, constant-time
  bearer auth failing closed off-loopback, `-race`-tested concurrent MCP
  sessions, deterministic timestamp-free transcripts.
- **Deliberate panics** (`canon.go:568`, `summaries.go:220`,
  `artifact.go:161`) are fail-loud invariant checks on "impossible" states —
  consistent with the prime directive; leave them.
- **Accepted residuals** documented in `docs/groundwork/scorecard.md`
  ("Standing residuals") are decisions, not oversights — do not relitigate
  them in fixes (e.g. version-skew decode failures as lockstep design,
  dynamic deferred values in recover detection).

---

## 7. Recommended remediation plan

Ordered so each phase leaves `make verify` green and independently shippable.
Findings within a phase are independent unless noted.

**Phase 0 — stop the bleeding (small, high leverage):**
M-2 (CI↔Makefile parity — the trust boundary), H-4 (reproducible panic),
H-14 (`Flow.Tier` validation), M-33's `--update` no-op, M-15 (trailing JSON).

**Phase 1 — soundness criticals (each = fix + PoC-shaped regression test):**
C-1 with M-20 (one first-party predicate + node tie-break — the largest single
work item; touches graphio/blindspots/taint scope), C-2, C-3+C-4+C-5 (one
taint change-set) with M-13's `--strict`, C-6, C-7 with M-26, C-8 with M-1.

**Phase 2 — fail-open HIGHs (funnel must disclose or refuse):**
H-1 with M-24/M-25 (SQL normalizer hardening + leak property fuzzer), H-2,
H-3, H-5, H-6, H-7, H-11, H-12, H-13.

**Phase 3 — drift closure (the one-source-of-truth debt):**
H-8 with M-9 (one root predicate), H-9+H-10 (one caveat-assembly helper),
M-4 (broker merge helper), M-7 (boundary-label prefix constants + repo-scan
guard test), M-8, M-10, M-11, M-12, M-27, M-28, M-29.

**Phase 4 — hardening and disclosure parity:**
remaining MEDIUMs (M-3, M-5, M-6, M-16..M-19, M-21..M-23, M-30..M-32,
M-34, M-35), then the LOW list (batch the comment fixes; batch the CLI
polish).

Two structural suggestions beyond individual fixes:

1. **Make the "parallel surface" invariant self-checking.** The repo already
   knows how (the `opkey` prefix repo-scan test, `TestVerbParity`,
   `dbwritetargets_test.go`). Extend that pattern to the newly-identified
   pairs as part of Phase 3, so this audit's largest class cannot recur
   silently: CLI↔MCP disclosure parity, caveat-assembly parity, boundary-label
   prefixes, obligation-status strings, usage-text↔flag-set parity.
2. **Add "shape fixtures" for the projection layer.** The two shapes that hid
   C-1 (an effect behind a method-value wrapper; an effect inside a generic
   instance) should become permanent fixture services in `go.work`, the same
   way `mwchainsvc`/`reflectsvc` pin the reclaimer poles — the suite's current
   generic test stops at the callgraph and never crosses `graphio.Build`.

---

## 8. Closing statement

The architecture is doing its job: nothing found here forges an artifact,
destabilizes a committed golden, or slips randomness into a verdict, and the
worst findings are all instances of the two failure classes the project itself
documented as its nemeses — incomplete collection under an absence claim, and
duplicated predicates drifting. The audit's criticals are concentrated where
new capability layers (post-hoc unordered groups, generics-era SSA synthetics,
the concurrent rule, interface-heavy taint targets) outran the older
assumptions beneath them, and every one has a localized fix with a clear
regression-test shape. Fixing Phases 0–2 closes every wrong-verdict path this
audit identified; Phase 3 makes the drift class structurally recurrence-proof
rather than individually patched.
