# Provenance-bound `groundwork assert` implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn `groundwork assert` into a versioned, provenance-bound machine interface, then add rich graph predicates that share typed, fail-closed fact evaluators with standing fitness policy.

**Architecture:** Ship two release-safe milestones. Milestone 1 adds canonical JSON, stamp enforcement, machine IDs, structured reasons, bindings, and witnesses without changing text behavior. Milestone 2 adds strict scalar-or-list selectors and extracts reach, pass-through, concurrent, and obligation facts into a presentation-free package consumed by both fitness and claims.

**Tech Stack:** Go 1.26, existing `groundwork/graph` index, `groundwork/policy.MatchPrefix`, `canonjson`, standard `encoding/json`, and the repository's existing CLI/test infrastructure.

## Global Constraints

- Treat `docs/superpowers/specs/2026-07-26-provenance-bound-groundwork-assert-design.md` as normative.
- Preserve current text output bytes and exit precedence: FAIL wins with exit 1; otherwise ERROR gives exit 2; all PASS gives exit 0.
- Print a complete computed text or JSON report before returning a verdict or per-claim evaluation error.
- Print no partial report for usage, graph/claims decode, stamp, or machine-ID errors.
- Never infer a machine state by parsing human `Detail` prose.
- Results stay in claims-file order. Independently sort and deduplicate every binding, witness list, candidate list, and caveat list.
- Rich absence claims over a blind frontier must ERROR, never PASS.
- Unbound rich selectors must ERROR, never become a vacuous proof.
- Existing structural selectors retain normalized-suffix/regex semantics.
- Rich selectors use only the existing boundary-aware `policy.MatchPrefix` semantics.
- The new facts package may import `groundwork/graph` and `groundwork/policy`; it must not import `claims`, construct presentation prose, load policy files, or assign policy severity.
- Do not synthesize a temporary policy and call `fitness.Check` from claims.
- Before moving each fitness evaluator, byte-pin its current findings. Any output correction is a separate reviewed change, not hidden inside extraction.
- No new dependency, schema migration, policy mutation, or graph mutation.

---

# Milestone 1: Machine-safe, provenance-bound structural assertions

Milestone 1 is independently releasable after Tasks 1–4. It does not accept the
four rich claim kinds yet.

## Task 1: Add structured result metadata and canonical JSON DTOs

**Files:**

- Modify: `internal/groundwork/claims/claims.go`
- Create: `internal/groundwork/claims/machine.go`
- Create: `internal/groundwork/claims/machine_test.go`

**Interfaces produced:**

```go
type Reason string
type Bindings struct { /* canonical graph identities */ }
type Witness struct { /* deterministic graph evidence */ }
func MarshalMachine(g *graph.Graph, report Report) ([]byte, error)
func ValidateMachineIDs(file *File) error
```

### Step 1: Pin the JSON contract with a failing test

- [ ] Add a test that constructs a stamped graph and a report containing one
PASS, one FAIL, and one ERROR. Assert the exact canonical JSON object, including
all claims and empty fixture caveats:

```go
func TestMarshalMachineContract(t *testing.T) {
	g := &graph.Graph{
		Stamp: "789abc",
		Tool:  "v0.0.0-test",
		Algo:  "vta",
		Nodes: []graph.Node{
			{FQN: "example.com/p.A", Sig: "func()"},
			{FQN: "example.com/p.B", Sig: "func()"},
		},
		Edges: []graph.Edge{
			{From: "example.com/p.A", To: "example.com/p.B"},
			{From: "example.com/p.A", To: "example.com/p.B"},
		},
	}
	report := Report{
		Results: []Result{
			{
				ID: "present", Kind: "edge", Outcome: Pass,
				Bindings: Bindings{
					From: []string{"example.com/p.A"},
					To:   []string{"example.com/p.B"},
				},
			},
			{ID: "absent", Kind: "node", Outcome: Fail, Detail: "tier mismatch"},
			{
				ID: "unknown", Kind: "edge", Outcome: Errored,
				Reason: ReasonUnresolved, Detail: "does not resolve",
			},
		},
		NumNodes: 2, NumUniquePairs: 1,
	}
	got, err := MarshalMachine(g, report)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"schema_version":"groundwork.assert/v1","fixture":{"stamp":"789abc","producer_tool":"v0.0.0-test","algo":"vta","caveats":[]},"results":[{"id":"present","kind":"edge","outcome":"PASS","bindings":{"from":["example.com/p.A"],"to":["example.com/p.B"]}},{"id":"absent","kind":"node","outcome":"FAIL","detail":"tier mismatch"},{"id":"unknown","kind":"edge","outcome":"ERROR","reason":"UNRESOLVED","detail":"does not resolve"}],"summary":{"passed":1,"failed":1,"errored":1,"nodes":2,"unique_edges":1}}`
	if string(got) != want {
		t.Fatalf("machine JSON mismatch\nwant: %s\ngot:  %s", want, got)
	}
}
```

Use the actual `graph.Edge` required fields on this checkout. Do not weaken the
test to semantic JSON equality; field order and bytes are part of the contract.

- [ ] Add `TestMarshalMachineCanonicalizesSets`. Permute duplicate values in:

- `Graph.Caveats`;
- `Bindings.From`, `To`, `Through`, `FQN`, `Of`, `Fn`, `Entrypoint`,
  `Obligation`;
- `Witnesses`.

Require identical bytes.

- [ ] Add table tests for `ValidateMachineIDs`:

- missing ID;
- `""`;
- whitespace only;
- exact duplicate;
- byte-distinct case variant;
- valid unique IDs.

The error must identify the first invalid claim index in file order.

- [ ] Run:

```console
go test ./internal/groundwork/claims -run 'TestMarshalMachine|TestValidateMachineIDs' -count=1
```

Expected: compile failure because the machine types and functions do not exist.

### Step 2: Add domain metadata without changing text rendering

- [ ] Extend `Result`:

```go
type Result struct {
	ID        string
	Kind      string
	Label     string
	Outcome   Outcome
	Reason    Reason
	Detail    string
	Bindings  Bindings
	Witnesses []Witness
}
```

Keep `Label`; text mode still needs its current fallback label.

- [ ] Define the closed reason vocabulary:

```go
type Reason string

const (
	ReasonUnresolved       Reason = "UNRESOLVED"
	ReasonAmbiguous        Reason = "AMBIGUOUS"
	ReasonUnboundSelector  Reason = "UNBOUND_SELECTOR"
	ReasonBlindFrontier    Reason = "BLIND_FRONTIER"
	ReasonMalformedClaim   Reason = "MALFORMED_CLAIM"
	ReasonUnknownStatus    Reason = "UNKNOWN_STATUS"
	ReasonMissingGraphData Reason = "MISSING_GRAPH_DATA"
	ReasonCantProve        Reason = "CANT_PROVE"
	ReasonUnmatched        Reason = "UNMATCHED"
)
```

- [ ] Define the internal structured values. They intentionally mirror, but do
not carry JSON tags from, the public machine DTO:

```go
type Bindings struct {
	From       []string
	To         []string
	Through    []string
	FQN        []string
	Of         []string
	Fn         []string
	Entrypoint []string
	Obligation []string
}

type Witness struct {
	From      string
	To        string
	Path      []string
	BlindSite string
	Rule      string
	Fn        string
	Site      string
	Status    string
	Detail    string
}
```

`Report.String` must continue to read only `Kind`, `Label`, `Outcome`, and
`Detail`.

### Step 3: Implement a dedicated machine DTO

- [ ] In `machine.go`, define the exact v1 DTO:

```go
const MachineSchemaVersion = "groundwork.assert/v1"

type JSONReport struct {
	SchemaVersion string        `json:"schema_version"`
	Fixture       JSONFixture   `json:"fixture"`
	Results       []JSONResult  `json:"results"`
	Summary       JSONSummary   `json:"summary"`
}

type JSONFixture struct {
	Stamp        string   `json:"stamp"`
	ProducerTool string   `json:"producer_tool"`
	Algo         string   `json:"algo"`
	Caveats      []string `json:"caveats"`
}

type JSONResult struct {
	ID        string        `json:"id"`
	Kind      string        `json:"kind"`
	Outcome   string        `json:"outcome"`
	Reason    string        `json:"reason,omitempty"`
	Detail    string        `json:"detail,omitempty"`
	Bindings  *JSONBindings `json:"bindings,omitempty"`
	Witnesses []JSONWitness `json:"witnesses,omitempty"`
}

type JSONBindings struct {
	From       []string `json:"from,omitempty"`
	To         []string `json:"to,omitempty"`
	Through    []string `json:"through,omitempty"`
	FQN        []string `json:"fqn,omitempty"`
	Of         []string `json:"of,omitempty"`
	Fn         []string `json:"fn,omitempty"`
	Entrypoint []string `json:"entrypoint,omitempty"`
	Obligation []string `json:"obligation,omitempty"`
}

type JSONWitness struct {
	From      string   `json:"from,omitempty"`
	To        string   `json:"to,omitempty"`
	Path      []string `json:"path,omitempty"`
	BlindSite string   `json:"blind_site,omitempty"`
	Rule      string   `json:"rule,omitempty"`
	Fn        string   `json:"fn,omitempty"`
	Site      string   `json:"site,omitempty"`
	Status    string   `json:"status,omitempty"`
	Detail    string   `json:"detail,omitempty"`
}

type JSONSummary struct {
	Passed      int `json:"passed"`
	Failed      int `json:"failed"`
	Errored     int `json:"errored"`
	Nodes       int `json:"nodes"`
	UniqueEdges int `json:"unique_edges"`
}
```

- [ ] Implement one canonical string-set helper that copies, sorts, and
deduplicates. Never mutate the `graph.Graph`, `Report`, or a caller-owned slice.
Use the helper for fixture caveats and every binding slice. A witness path is an
ordered BFS sequence: copy it without reordering its elements.

- [ ] Sort witnesses by the complete tuple:

```text
From, To, Path elements, BlindSite, Rule, Fn, Site, Status, Detail
```

Use an explicit comparator, comparing `Path` lexicographically without
reordering it. Remove exact duplicate witnesses after sorting. Do not sort on
formatted prose or JSON bytes.

- [ ] Validate internal result state before serialization:

- `Pass`, `Fail`, and `Errored` are the only accepted `Outcome` values;
- an errored result must carry one of the nine known reasons;
- a PASS or FAIL must not carry a reason;
- an unknown outcome or reason returns an error and emits no bytes.

This keeps an internal adapter bug from silently widening or weakening the v1
machine contract.

- [ ] Build fixture provenance from:

```go
ix := graph.NewIndex(g)
JSONFixture{
	Stamp:        g.Stamp,
	ProducerTool: g.Tool,
	Algo:         g.Algo,
	Caveats:      canonicalStrings(ix.GateCaveats("")),
}
```

- [ ] Serialize only with:

```go
return canonjson.Marshal(dto)
```

### Step 4: Implement machine ID validation

- [ ] Validate in file order:

```go
func ValidateMachineIDs(file *File) error {
	seen := make(map[string]int, len(file.Claims))
	for i, claim := range file.Claims {
		if strings.TrimSpace(claim.ID) == "" {
			return fmt.Errorf("claim %d has no non-whitespace id", i+1)
		}
		if first, ok := seen[claim.ID]; ok {
			return fmt.Errorf(
				"claim %d duplicates id %q first used by claim %d",
				i+1, claim.ID, first,
			)
		}
		seen[claim.ID] = i + 1
	}
	return nil
}
```

Exact UTF-8 bytes define equality; do not normalize case or whitespace.

- [ ] Format and run:

```console
gofmt -w internal/groundwork/claims/claims.go internal/groundwork/claims/machine.go internal/groundwork/claims/machine_test.go
go test ./internal/groundwork/claims -run 'TestMarshalMachine|TestValidateMachineIDs|TestReportFormat' -count=1
```

Expected: PASS, with existing text format unchanged.

- [ ] Commit:

```console
git add internal/groundwork/claims
git commit -m "feat: define groundwork assert machine report"
```

---

## Task 2: Wire `--json`, `--expect`, and stamp enforcement into the CLI

**Files:**

- Modify: `cmd/groundwork/main.go`
- Modify: `cmd/groundwork/assert_test.go`
- Modify: `cmd/groundwork/gatestamp_test.go`
- Modify: `cmd/groundwork/main_test.go`

**Command contract produced:**

```console
groundwork assert <graph.json> <claims.json> [--expect <stamp>] [--json]
```

### Step 1: Add failing command tests

- [ ] Add table-driven command tests covering:

| Case | Report written? | Error class |
|---|---:|---|
| JSON all pass | yes | nil |
| JSON with FAIL | yes | `verdictError` |
| JSON with ERROR only | yes | operational |
| JSON with both FAIL and ERROR | yes | `verdictError` |
| JSON missing ID | no | operational |
| JSON duplicate ID | no | operational |
| matching `--expect` | normal report | normal result |
| mismatched `--expect` | no | operational |
| missing graph stamp plus `--expect` | no | operational |
| `GROUNDWORK_REQUIRE_STAMP=1`, no `--expect` | no | operational |

Use `t.Setenv` and the existing `captureStdout` helper. Assert JSON by exact
bytes or decode only when the test is specifically about exit classification.

- [ ] Add `"assert"` to the gate-stamp parity table in
`gatestamp_test.go`. Create a stamped graph and valid claims file with IDs for
this table; do not reuse an unstamped structural fixture.

- [ ] Add a compatibility test that runs the current text invocation with no
new flags and compares the existing golden bytes unchanged.

- [ ] Run:

```console
go test ./cmd/groundwork -run 'TestAssert(JSON|Stamp|MachineID|Exit|Text)|TestGateStamp' -count=1
```

Expected: FAIL because `cmdAssert` does not recognize or enforce the new flags.

### Step 2: Parse flags before positional validation

- [ ] Follow existing movable flag helpers:

```go
func cmdAssert(args []string) error {
	expect, hasExpect, args := takeValueFlag(args, "--expect", "-expect")
	asJSON, args := takeFlag(args, "--json", "-json")
	if len(args) != 2 {
		return fmt.Errorf(
			"usage: groundwork assert <graph.json> <claims.json> [--expect <stamp>] [--json]",
		)
	}
	g, err := graph.LoadFile(args[0])
	if err != nil {
		return err
	}
	if err := verifyGateStamp(g, expect, hasExpect); err != nil {
		return err
	}
	cf, err := claims.LoadFile(args[1])
	if err != nil {
		return err
	}
	if asJSON {
		if err := claims.ValidateMachineIDs(cf); err != nil {
			return fmt.Errorf("assert: invalid machine ids: %w", err)
		}
	}

	rep := claims.Evaluate(g, cf)
	if asJSON {
		out, err := claims.MarshalMachine(g, rep)
		if err != nil {
			return err
		}
		fmt.Println(string(out))
	} else {
		fmt.Print(rep.String())
	}
	if rep.Failed() > 0 {
		return verdictf("assert: %d claim(s) failed", rep.Failed())
	}
	if rep.Errored() > 0 {
		return fmt.Errorf("assert: %d claim(s) could not be evaluated", rep.Errored())
	}
	return nil
}
```

Loading claims after stamp verification is acceptable because neither path
prints output. Do not evaluate before the complete machine ID set is valid.

- [ ] Update the `verifyGateStamp` comment's command list to include `assert`.
No helper behavior changes are needed.

### Step 3: Preserve output-before-exit precedence

- [ ] Run:

```console
gofmt -w cmd/groundwork/main.go cmd/groundwork/assert_test.go cmd/groundwork/gatestamp_test.go cmd/groundwork/main_test.go
go test ./cmd/groundwork -run 'TestAssert|TestGateStamp|TestVerdictVsOperationalErrors' -count=1
```

Expected: PASS.

- [ ] Commit:

```console
git add cmd/groundwork
git commit -m "feat: add provenance-bound assert JSON mode"
```

---

## Task 3: Populate reasons and bindings for existing structural claims

**Files:**

- Modify: `internal/groundwork/claims/claims.go`
- Modify: `internal/groundwork/claims/claims_test.go`
- Modify: `internal/groundwork/claims/machine_test.go`

**Behavior produced:** Every existing claim retains its text result while
machine mode exposes structured graph identities and a closed reason code.

### Step 1: Add reason and binding matrix tests

- [ ] For each existing kind, add a PASS test with expected bindings:

| Kind | Binding fields |
|---|---|
| `edge`, `no_edge`, `edge_count` | `from`, `to` |
| `node`, `no_node` | `fqn` |
| `in_degree`, `out_degree` | `of` |
| `entrypoint` | `fn`, `entrypoint` |

- [ ] Add ERROR tests for:

| Existing condition | Required reason |
|---|---|
| missing/wrong-kind field | `MALFORMED_CLAIM` |
| unknown kind | `MALFORMED_CLAIM` |
| malformed regex | `MALFORMED_CLAIM` |
| zero required matches | `UNRESOLVED` |
| non-unique plain or one-required match | `AMBIGUOUS` |
| absent entrypoint join section | `MISSING_GRAPH_DATA` |

Keep `no_node` zero-match as PASS and assert its `fqn` binding is absent rather
than fabricated.

- [ ] Assert each result's `ID` equals the input `Claim.ID`, even in text mode
where the ID remains optional.

- [ ] Run:

```console
go test ./internal/groundwork/claims -run 'TestStructural(MachineBindings|ReasonCodes|NoNodePolarity)' -count=1
```

Expected: FAIL because evaluators currently return only prose.

### Step 2: Make resolution failures typed at their source

- [ ] Add:

```go
type evaluationError struct {
	Reason Reason
	Detail string
}
```

Change internal resolution helpers to return `*evaluationError` rather than a
bare detail string. Set `ReasonUnresolved` when match count is zero,
`ReasonAmbiguous` when uniqueness is required and match count exceeds one, and
`ReasonMalformedClaim` on regex compilation failure.

Do not infer reason from `strings.Contains(detail, ...)`.

- [ ] Replace the existing generic errored-result constructor with:

```go
func errored(c Claim, reason Reason, detail string) Result {
	return Result{
		ID: c.ID, Kind: c.Kind, Label: c.label(),
		Outcome: Errored, Reason: reason, Detail: detail,
	}
}
```

Use `ReasonMalformedClaim` for field validation and unknown kinds.

### Step 3: Carry established bindings through each evaluator

- [ ] Set the resolved canonical identities immediately after successful
resolution, before outcome polarity is computed. This ensures a FAIL exposes
the same bindings as a PASS.

- [ ] For entrypoints:

- `Fn` contains the resolved handler FQN set;
- `Entrypoint` contains every name-matched route/topic/symbol record considered;
- an empty `graph.Entrypoints` slice returns
  `ReasonMissingGraphData`;
- disagreeing joins use `ReasonAmbiguous`.

- [ ] Canonicalization stays in `MarshalMachine`; evaluators may append in their
natural deterministic order.

- [ ] Run the complete claims package and the text command golden:

```console
gofmt -w internal/groundwork/claims/claims.go internal/groundwork/claims/claims_test.go internal/groundwork/claims/machine_test.go
go test ./internal/groundwork/claims -count=1
go test ./cmd/groundwork -run TestAssert -count=1
```

Expected: PASS with no text byte change.

- [ ] Commit:

```console
git add internal/groundwork/claims cmd/groundwork/assert_test.go
git commit -m "feat: structure assert resolution evidence"
```

---

## Task 4: Document and verify the independently releasable machine milestone

**Files:**

- Modify: `cmd/groundwork/main.go`
- Modify: `docs/groundwork/usage.md`
- Modify any existing gate-stamp environment documentation located by:
  `rg -n 'GROUNDWORK_REQUIRE_STAMP|groundwork assert' docs README.md`

### Step 1: Update help and usage

- [ ] Update `usageBody` and command-specific usage to show:

```text
groundwork assert <graph.json> <claims.json> [--expect <stamp>] [--json]
```

- [ ] Remove any statement that the stamp-enforced gate set excludes `assert`.

- [ ] Document:

- text mode compatibility;
- complete-report-before-computed-exit behavior;
- no-report behavior for decode/stamp/ID errors;
- exact machine ID rules;
- fixture provenance;
- the `groundwork.assert/v1` fields;
- the nine reason strings;
- authority distinction: `assert` is caller-supplied point-in-time evidence,
  while `fitness` is standing CODEOWNERS-gated policy.

Do not document rich kinds yet; this milestone cannot execute them.

### Step 2: Add a command-level canonicalization test

- [ ] Extend the assert shuffle test to independently permute graph:

- nodes;
- edges;
- blind spots;
- obligations;
- entrypoints;
- caveats.

Use the same claims order for every run and require byte-identical JSON. Include
duplicate caveats and duplicate edge records so dedup behavior is exercised.

- [ ] Run:

```console
gofmt -w cmd/groundwork/main.go
go test ./cmd/groundwork ./internal/groundwork/claims -count=1
make fmt-check
make comment-drift
```

Expected: PASS. Review `comment-drift` output for the changed command and stamp
comments.

- [ ] Commit:

```console
git add cmd/groundwork/main.go docs/groundwork/usage.md
git diff --cached --name-only
git commit -m "docs: specify groundwork assert machine contract"
```

If the documentation search identified another current user-facing stamp
document that actually needed an edit, add that exact path explicitly after
inspecting its diff. Do not use a broad `git add docs` or stage user-owned
files.

### Milestone 1 release checkpoint

- [ ] Run:

```console
make verify
```

Expected: PASS.

- [ ] Confirm an old claims file without IDs still works in text mode, and the
same file is rejected with no output in JSON mode.

---

# Milestone 2: Shared typed facts and rich claims

Milestone 2 begins only after the Milestone 1 checkpoint is green.

## Task 5: Add strict scalar-or-list selector input

**Files:**

- Create: `internal/groundwork/claims/selectors.go`
- Create: `internal/groundwork/claims/selectors_test.go`
- Modify: `internal/groundwork/claims/claims.go`
- Modify: `internal/groundwork/claims/claims_test.go`

**Interface produced:**

```go
type Selectors struct {
	values  []string
	wasList bool
}

func (s *Selectors) UnmarshalJSON(data []byte) error
func (s Selectors) Values() []string
func (s Selectors) Scalar() (string, bool)
func (s Selectors) Present() bool
func (s Selectors) WasList() bool
```

`Values` returns a copy. `Scalar` returns true only for an originally scalar
JSON string, not for a one-element list.

### Step 1: Add strict decoder tests

- [ ] Test accepted forms:

```json
"example.com/service.Handler"
["example.com/service.Handler"]
["example.com/service.Handler", "example.com/service.Worker"]
```

- [ ] Test rejection of `null`, `""`, whitespace-only strings, `[]`, any
whitespace-only list member, non-string members, objects, and trailing JSON
data.

- [ ] Test that a one-element list remains distinguishable from a scalar.

- [ ] Test structural claims with an array in `from` or `to` produce a per-claim
`MALFORMED_CLAIM` ERROR rather than widening or aborting the file.

- [ ] Run:

```console
go test ./internal/groundwork/claims -run 'TestSelectors|TestStructuralSelectorLists' -count=1
```

Expected: compile failure because `Selectors` does not exist.

### Step 2: Implement the one selector decoder

- [ ] Decode one complete JSON value with a local `json.Decoder`, reject trailing
tokens, and distinguish the first byte:

- `"`: decode one string, validate `strings.TrimSpace(v) != ""`;
- `[`: decode `[]string`, require non-empty, validate every member;
- everything else: return a deterministic type error.

Use `bytes.NewReader(data)` and the same strict single-value EOF discipline as
`LoadFile`.

- [ ] Change only these `Claim` fields:

```go
From    Selectors `json:"from,omitempty"`
To      Selectors `json:"to,omitempty"`
Through Selectors `json:"through,omitempty"`
```

Add `Expect string` now because all rich evaluators use it or validate its
absence. Keep `FQN`, `Of`, `Fn`, and `Name` scalar strings.

- [ ] Add this test helper in `claims_test.go` and mechanically convert existing
Go-literal construction:

```go
func sel(v string) Selectors {
	return Selectors{values: []string{v}}
}
```

Do not expose a production constructor solely for tests.

- [ ] Structural evaluators call `Scalar()`. If `WasList()` is true, return
`ReasonMalformedClaim` even when the list has one member. Existing suffix and
regex resolution then receives the scalar string unchanged.

- [ ] Update the strict wrong-kind reflection test to include `through` and
`expect` and to keep metadata exclusions explicit.

- [ ] Run:

```console
gofmt -w internal/groundwork/claims/selectors.go internal/groundwork/claims/selectors_test.go internal/groundwork/claims/claims.go internal/groundwork/claims/claims_test.go
go test ./internal/groundwork/claims ./cmd/groundwork -count=1
```

Expected: PASS with existing scalar JSON and text behavior unchanged.

- [ ] Commit:

```console
git add internal/groundwork/claims cmd/groundwork
git commit -m "feat: accept strict assert selector lists"
```

---

## Task 6: Extract reach facts and add `reach` claims

**Files:**

- Create: `internal/groundwork/facts/types.go`
- Create: `internal/groundwork/facts/match.go`
- Create: `internal/groundwork/facts/reach.go`
- Create: `internal/groundwork/facts/reach_test.go`
- Modify: `internal/groundwork/fitness/reach.go`
- Modify: `internal/groundwork/fitness/fqn.go`
- Modify: `internal/groundwork/fitness/fitness_test.go`
- Modify: `internal/groundwork/claims/claims.go`
- Create: `internal/groundwork/claims/reach_test.go`

**Interface produced:**

```go
type ReachState uint8

const (
	ReachUnbound ReachState = iota
	ReachFound
	ReachAbsent
	ReachBlind
)

type PathWitness struct {
	From string
	To   string
	Path []string
}

type BlindWitness struct {
	From string
	Site string
}

type ReachResult struct {
	State       ReachState
	From        []string
	To          []string
	Paths       []PathWitness
	Blind       *BlindWitness
	UnboundFrom []string
	UnboundTo   []string
}

func EvaluateReach(ix *graph.Index, from, to []string) ReachResult
```

### Step 1: Pin current fitness behavior before extraction

- [ ] Add explicit `[]fitness.Finding` expectations for:

- reachable violation;
- proven absence with no finding;
- blind frontier caution;
- `require_proof` blind frontier violation;
- unbound source;
- unbound target;
- `entrypoint:*`.

Compare every `Finding` field, including existing summary and detail prose.
Run the tests against the pre-extraction code and commit no implementation until
they pass.

```console
go test ./internal/groundwork/fitness -run TestReachCharacterization -count=1
```

Expected: PASS.

### Step 2: Write direct facts tests

- [ ] Test the four `ReachState` values, reachable-dominates-blind precedence,
source order, function-before-boundary target selection, shortest path contents,
and unbound source/target lists.

- [ ] Shuffle graph nodes, edges, and blind spots; require equal `ReachResult`
values.

- [ ] Run:

```console
go test ./internal/groundwork/facts -run TestEvaluateReach -count=1
```

Expected: compile failure.

### Step 3: Move binding, traversal, and blindness into `facts`

- [ ] Implement matching through one helper:

```go
func matchesAny(value string, selectors []string) bool {
	for _, selector := range selectors {
		if policy.MatchPrefix(value, selector) {
			return true
		}
	}
	return false
}
```

- [ ] Move the behavior currently owned by `bindFroms`, `expandFroms`,
`bindsAnyTarget`, `evalReach`, `frontierBlindSiteWith`, and
`firstReachBlinding` into the facts package. Keep complete binding facts and
deterministic BFS parent maps; do not render `ShortName` or prose there.

- [ ] `EvaluateReach` must implement:

1. bind all sources, including `entrypoint:*`;
2. bind all candidate node/boundary targets;
3. return `ReachUnbound` with exact selector strings in `UnboundFrom` and/or
   `UnboundTo` when either family binds nothing;
4. search sorted sources;
5. consider reachable functions before boundary effects;
6. return the first deterministic shortest path on a hit;
7. if no hit, return `ReachBlind` when any reachable frontier is blind;
8. otherwise return `ReachAbsent`.

### Step 4: Adapt fitness without output drift

- [ ] Replace private reach verdict evaluation with `facts.EvaluateReach`.
Keep the existing fitness presentation functions and `require_proof` severity
mapping.

- [ ] Run the characterization test before adding claims:

```console
go test ./internal/groundwork/fitness -run TestReachCharacterization -count=1
```

Expected: PASS byte-for-byte.

### Step 5: Add the `reach` claim adapter

- [ ] Add allowed fields `id`, `kind`, `from`, `to`, `expect`.
Require non-empty selector lists and `expect` exactly `present` or `absent`.
When text mode has no ID, use the deterministic fallback label
`<from selectors joined by ", "> -> <to selectors joined by ", ">`.

- [ ] Map facts:

| Fact | `present` | `absent` |
|---|---|---|
| `ReachFound` | PASS | FAIL |
| `ReachAbsent` | FAIL | PASS |
| `ReachBlind` | ERROR `BLIND_FRONTIER` | ERROR `BLIND_FRONTIER` |
| `ReachUnbound` | ERROR `UNBOUND_SELECTOR` | ERROR `UNBOUND_SELECTOR` |

Carry `From`/`To` bindings, path witnesses, and blind site. Reachable always
dominates blindness.

Use stable human details without parsing them downstream:

- found: `path found`;
- proven absent: `no path found`;
- blind: `no path found, but the frontier is blind at <site>`;
- unbound: `<field> selector(s) bind nothing: <sorted selectors>`.

- [ ] Test every table cell plus malformed `expect`, missing fields, multiple
selectors, and machine JSON evidence.

- [ ] Run:

```console
gofmt -w internal/groundwork/facts internal/groundwork/fitness internal/groundwork/claims
go test ./internal/groundwork/facts ./internal/groundwork/fitness ./internal/groundwork/claims -run 'Reach' -count=1
```

Expected: PASS.

- [ ] Commit:

```console
git add internal/groundwork/facts internal/groundwork/fitness internal/groundwork/claims
git commit -m "feat: share typed reach facts with assert"
```

---

## Task 7: Extract pass-through facts and add `pass_through`

**Files:**

- Create: `internal/groundwork/facts/passthrough.go`
- Create: `internal/groundwork/facts/passthrough_test.go`
- Modify: `internal/groundwork/fitness/passthrough.go`
- Modify: `internal/groundwork/fitness/passthrough_test.go`
- Create: `internal/groundwork/claims/passthrough_test.go`
- Modify: `internal/groundwork/claims/claims.go`

**Interface produced:**

```go
type AllowPair struct {
	From string
	To   string
}

type PassThroughInput struct {
	From    []string
	To      []string
	Through []string
	Allow   []AllowPair
}

type PassThroughState uint8

const (
	PassThroughUnbound PassThroughState = iota
	PassThroughBypassed
	PassThroughGuarded
	PassThroughBlind
)

type PassThroughResult struct {
	State          PassThroughState
	From           []string
	To             []string
	Through        []string
	Bypasses       []PathWitness
	Blind          *BlindWitness
	UnboundFrom    []string
	UnboundTo      []string
	UnboundThrough []string
}

func EvaluatePassThrough(ix *graph.Index, in PassThroughInput) PassThroughResult
```

### Step 1: Pin fitness findings

- [ ] Before extraction, assert exact findings for function bypass, boundary
bypass, multiple bypass pairs, allow-list suppression, guarded path, blind
frontier, `require_proof`, dead source, dead target, and dead waypoint.

```console
go test ./internal/groundwork/fitness -run TestPassThroughCharacterization -count=1
```

Expected: PASS before refactoring.

### Step 2: Test and implement presentation-free facts

- [ ] Move `guardedWalk` and path reconstruction into facts. Returned paths use
full FQNs/effect labels, not arrows or shortened names.

- [ ] Convert `policy.Exception` at the fitness boundary:

```go
allow := make([]facts.AllowPair, len(rule.Allow))
for i, exception := range rule.Allow {
	allow[i] = facts.AllowPair{From: exception.From, To: exception.To}
}
```

`facts.AllowPair` uses `policy.MatchPrefix` on each non-empty side, matching
`PassRule.Allowed`. Do not import the policy rule shape into facts.

- [ ] Required fact behavior:

1. an unbound source or target family → `PassThroughUnbound`;
2. an unbound waypoint is recorded in `UnboundThrough`, but evaluation
   continues as a walk with no removable waypoint;
3. one or more unallowed bypasses → `PassThroughBypassed`;
4. no bypass plus blind frontier → `PassThroughBlind`;
5. otherwise → `PassThroughGuarded`.

The facts evaluator reports every sorted, deduplicated bypass pair with a
shortest path. This separation is required for compatibility: the claims
adapter rejects any unbound waypoint, while fitness preserves its existing
dead-waypoint presentation from the same binding and traversal facts.

- [ ] Adapt fitness and rerun characterization:

```console
go test ./internal/groundwork/fitness -run TestPassThroughCharacterization -count=1
```

Expected: PASS with identical findings.

### Step 3: Add the claim adapter

- [ ] `pass_through` allows only `id`, `kind`, `from`, `to`, `through`.
Claims pass no allow list.
When text mode has no ID, use
`<from> -> <through> -> <to>` with each selector family joined by `", "`.

- [ ] Map:

- any non-empty `UnboundFrom`, `UnboundTo`, or `UnboundThrough` → ERROR
  `UNBOUND_SELECTOR`, before state mapping;
- `PassThroughBypassed` → FAIL;
- `PassThroughGuarded` → PASS, including vacuous no-source-to-target-path truth;
- `PassThroughBlind` → ERROR `BLIND_FRONTIER`;
- `PassThroughUnbound` → ERROR `UNBOUND_SELECTOR`.

All three binding families must be present when bound. Do not silently accept a
dead waypoint. Document that path existence needs a companion positive `reach`.
Use `bypass path found`, `all visible paths pass through the waypoint`,
`no bypass found, but the frontier is blind at <site>`, and the same
field-specific unbound form as the stable human details.

- [ ] Test multiple selectors, every state, vacuous truth, and sorted witness
paths.

- [ ] Run:

```console
gofmt -w internal/groundwork/facts/passthrough.go internal/groundwork/facts/passthrough_test.go internal/groundwork/fitness/passthrough.go internal/groundwork/fitness/passthrough_test.go internal/groundwork/claims
go test ./internal/groundwork/facts ./internal/groundwork/fitness ./internal/groundwork/claims -run 'PassThrough|pass_through' -count=1
```

Expected: PASS.

- [ ] Commit:

```console
git add internal/groundwork/facts internal/groundwork/fitness internal/groundwork/claims
git commit -m "feat: share pass-through facts with assert"
```

---

## Task 8: Extract the concurrent surface and add `no_concurrent_reach`

**Files:**

- Create: `internal/groundwork/facts/concurrent.go`
- Create: `internal/groundwork/facts/concurrent_test.go`
- Modify: `internal/groundwork/fitness/concurrent.go`
- Modify: `internal/groundwork/fitness/concurrent_test.go`
- Create: `internal/groundwork/claims/concurrent_test.go`
- Modify: `internal/groundwork/claims/claims.go`

**Interface produced:**

```go
type ConcurrentSurface struct {
	// unexported canonical seeds, cone, effects, direct edges, and blindness
}

type ConcurrentState uint8

const (
	ConcurrentUnbound ConcurrentState = iota
	ConcurrentHit
	ConcurrentClean
	ConcurrentBlind
)

type ConcurrentWitness struct {
	From string
	To   string
}

type ConcurrentResult struct {
	State     ConcurrentState
	To        []string
	Hits      []ConcurrentWitness
	BlindSite string
	UnboundTo []string
}

func BuildConcurrentSurface(ix *graph.Index) ConcurrentSurface
func (s ConcurrentSurface) Evaluate(to []string) ConcurrentResult
```

### Step 1: Pin fitness findings

- [ ] Characterize direct concurrent boundary hits, spawned-function hits,
effect hits, duplicate-hit collapse, clean surface, blind cone, dynamic direct
edge, graph-wide `ConcurrentDispatch`, dead target, and `require_proof`.

```console
go test ./internal/groundwork/fitness -run TestConcurrentCharacterization -count=1
```

Expected: PASS before extraction.

### Step 2: Build the surface once and test all states

- [ ] Move the rule-independent seed/cone/effect/direct-edge computation and
`concurrentBlindProbe` into facts. Preserve the graph-wide
`ConcurrentDispatch` check; this is load-bearing against a silent clean pass.

- [ ] Sort/deduplicate hit witnesses by `(From, To)`.

- [ ] Change `fitness.Check` to build one surface only when at least one
concurrent rule exists, then evaluate each rule against it. Claims evaluation
also builds at most one surface per file and reuses it for all
`no_concurrent_reach` claims.

If the simplest claims integration initially builds one surface lazily on the
first concurrent claim, cache it on the claims evaluation model; do not use
package-global state.

- [ ] Adapt fitness and rerun characterization. Expected: identical findings.

### Step 3: Add the claim adapter

- [ ] Allow only `id`, `kind`, `to`; reject `from`, `through`, and `expect`.
When text mode has no ID, use `concurrent -> <to selectors joined by ", ">`.

- [ ] Map:

- `ConcurrentHit` → FAIL;
- `ConcurrentClean` → PASS;
- `ConcurrentBlind` → ERROR `BLIND_FRONTIER`;
- `ConcurrentUnbound` → ERROR `UNBOUND_SELECTOR`.

Carry canonical `To` bindings, hit witnesses, and blind site.
Use `target reachable on a concurrent path`, `no concurrent path found`,
`no concurrent path found, but the frontier is blind at <site>`, and the
field-specific unbound form as stable human details.

- [ ] Run:

```console
gofmt -w internal/groundwork/facts/concurrent.go internal/groundwork/facts/concurrent_test.go internal/groundwork/fitness/concurrent.go internal/groundwork/fitness/concurrent_test.go internal/groundwork/claims
go test ./internal/groundwork/facts ./internal/groundwork/fitness ./internal/groundwork/claims -run 'Concurrent|no_concurrent_reach' -count=1
```

Expected: PASS.

- [ ] Commit:

```console
git add internal/groundwork/facts internal/groundwork/fitness internal/groundwork/claims
git commit -m "feat: share concurrent reach facts with assert"
```

---

## Task 9: Extract obligation interpretation and add `obligation`

**Files:**

- Create: `internal/groundwork/facts/obligation.go`
- Create: `internal/groundwork/facts/obligation_test.go`
- Modify: `internal/groundwork/fitness/obligations.go`
- Modify: `internal/groundwork/fitness/obligations_test.go`
- Create: `internal/groundwork/claims/obligation_test.go`
- Modify: `internal/groundwork/claims/claims.go`

**Interface produced:**

```go
type ObligationState uint8

const (
	ObligationMissingData ObligationState = iota
	ObligationUnresolved
	ObligationViolated
	ObligationUnknown
	ObligationCantProve
	ObligationUnmatched
	ObligationSatisfied
)

type ObligationResult struct {
	State   ObligationState
	Name    string
	Records []graph.Obligation
}

func EvaluateObligation(ix *graph.Index, name string) ObligationResult
```

### Step 1: Pin fitness findings

- [ ] Characterize `VIOLATED`, `CANT-PROVE`, `UNMATCHED`, `SATISFIED`, and an
unknown producer status with exact `Finding` values.

```console
go test ./internal/groundwork/fitness -run TestObligationCharacterization -count=1
```

Expected: PASS before extraction.

### Step 2: Implement exact-name aggregation

- [ ] Move status constants into facts as the single source of truth.
`EvaluateObligation` matches `Rule == name` exactly and sorts records by:

```text
Rule, Kind, Fn, Site, Status, Detail
```

- [ ] Apply this dominance:

1. no obligations section → `ObligationMissingData`;
2. non-empty section, no exact name → `ObligationUnresolved`;
3. any `VIOLATED` → `ObligationViolated`;
4. otherwise any unknown status → `ObligationUnknown`;
5. otherwise any `CANT-PROVE` → `ObligationCantProve`;
6. otherwise any `UNMATCHED` → `ObligationUnmatched`;
7. all matched records `SATISFIED` → `ObligationSatisfied`.

Distinguish an omitted/nil obligations section from a present empty section
only if the existing graph decoder preserves that distinction. If it does not,
both are `MISSING_GRAPH_DATA`; never guess that an empty slice proves absence.

- [ ] Adapt fitness by evaluating each distinct obligation rule present in the
graph and mapping records back to the same per-record findings. Rerun the
characterization test; expected findings are unchanged.

### Step 3: Add the claim adapter

- [ ] Allow only `id`, `kind`, `name`, `expect`. Require
`expect == "satisfied"`.
When text mode has no ID, use the exact obligation `name` as the label.

- [ ] Map:

- `ObligationViolated` → FAIL;
- `ObligationSatisfied` → PASS;
- `ObligationMissingData` → ERROR `MISSING_GRAPH_DATA`;
- `ObligationUnresolved` → ERROR `UNRESOLVED`;
- `ObligationUnknown` → ERROR `UNKNOWN_STATUS`;
- `ObligationCantProve` → ERROR `CANT_PROVE`;
- `ObligationUnmatched` → ERROR `UNMATCHED`.

Every matched record becomes a witness with `rule`, `fn`, `site`, `status`, and
`detail`; set `Bindings.Obligation` to the exact matched rule name.
For human details, name the dominant status and matched record count; keep the
record-specific producer detail in witnesses so prose is not a machine
discriminator.

- [ ] Test mixed-status dominance, including `VIOLATED` plus `CANT-PROVE`.

- [ ] Run:

```console
gofmt -w internal/groundwork/facts/obligation.go internal/groundwork/facts/obligation_test.go internal/groundwork/fitness/obligations.go internal/groundwork/fitness/obligations_test.go internal/groundwork/claims
go test ./internal/groundwork/facts ./internal/groundwork/fitness ./internal/groundwork/claims -run 'Obligation|obligation' -count=1
```

Expected: PASS.

- [ ] Commit:

```console
git add internal/groundwork/facts internal/groundwork/fitness internal/groundwork/claims
git commit -m "feat: share obligation facts with assert"
```

---

## Task 10: Complete rich-claim integration, canonicalization, and docs

**Files:**

- Modify: `internal/groundwork/claims/claims.go`
- Modify: `internal/groundwork/claims/machine_test.go`
- Modify: `cmd/groundwork/assert_test.go`
- Create: `testdata/groundwork/claims/assert-rich.claims.json`
- Modify: `docs/groundwork/usage.md`
- Modify: `cmd/groundwork/main.go`

### Step 1: Add one mixed command-level fixture

- [ ] Create a claims file containing, in this order:

1. passing positive `reach`;
2. passing negative `reach`;
3. failing negative `reach`;
4. passing `pass_through`;
5. failing `pass_through`;
6. passing `no_concurrent_reach`;
7. failing `no_concurrent_reach`;
8. passing `obligation`;
9. errored/blind or cant-prove claim.

Every claim has a stable ID.

- [ ] Run the file through `cmdAssert --json` against a purpose-built graph in
the test. Assert:

- result order equals claim order;
- all PASS results are present;
- FAIL beats ERROR for exit classification;
- reasons are present only on ERROR;
- no machine logic relies on `Detail`;
- all bindings and witnesses are canonical.

### Step 2: Extend whole-graph permutation testing

- [ ] Independently shuffle:

- nodes;
- edges;
- blind spots;
- obligations;
- entrypoints;
- caveats.

For each shuffled graph, evaluate the same mixed file and require byte-identical
machine JSON. Also permute duplicate records within each category.

- [ ] Add focused tests that assert BFS chooses the same shortest path when edge
input order changes.

### Step 3: Finish help and user documentation

- [ ] Document examples for `reach`, `pass_through`,
`no_concurrent_reach`, and `obligation`.

- [ ] Put these adjacent to the examples:

- structural selectors are scalar-only and keep suffix/regex grammar;
- rich selectors accept scalar or list and use boundary-aware
  exact-or-prefix matching;
- `entrypoint:*` is supported only where a from-bearing facts evaluator
  supports it;
- `pass_through` is vacuously true when no source-to-target path exists;
- use a companion positive `reach` to assert path existence;
- a claims PASS is not policy approval;
- `fitness` remains the standing CODEOWNERS-gated authority.

- [ ] Document the complete JSON DTO and exact reason vocabulary. State that
arrays are sorted/deduplicated and result order follows input claims.

### Step 4: Run package and repository verification

- [ ] Run:

```console
gofmt -w internal/groundwork/facts internal/groundwork/claims internal/groundwork/fitness cmd/groundwork
go test ./internal/groundwork/facts ./internal/groundwork/claims ./internal/groundwork/fitness ./cmd/groundwork -count=1
make fmt-check
make comment-drift
make verify
```

Expected: all pass. Review comment-drift output for every moved invariant
comment; update stale ownership comments in the same edit.

- [ ] Run uncached determinism repetitions:

```console
go test ./internal/groundwork/claims ./internal/groundwork/facts ./cmd/groundwork -count=10
```

Expected: PASS.

### Step 5: Audit architecture boundaries

- [ ] Confirm no prohibited coupling:

```console
rg -n 'groundwork/(claims|fitness)' internal/groundwork/facts
rg -n 'fitness\\.Check|policy\\.Load|LoadFile' internal/groundwork/claims
```

Expected:

- no `claims` or `fitness` import from facts;
- no `fitness.Check` or policy-loading shortcut in claims;
- claims' own `LoadFile` references are expected, but facts imports none.

- [ ] Confirm all machine serialization routes through `canonjson`:

```console
rg -n 'json\\.Marshal|canonjson\\.Marshal' internal/groundwork/claims cmd/groundwork
```

Expected: the machine report uses `canonjson.Marshal`; standard JSON is used
only for strict input decoding.

- [ ] Confirm `policy.MatchPrefix` is the single rich-selector matcher:

```console
rg -n 'HasPrefix|MatchPrefix' internal/groundwork/facts
```

Expected: no bare `strings.HasPrefix` selector semantics.

- [ ] Commit:

```console
git add internal/groundwork/facts internal/groundwork/claims internal/groundwork/fitness cmd/groundwork testdata/groundwork/claims/assert-rich.claims.json docs/groundwork/usage.md
git commit -m "feat: add rich provenance-bound graph claims"
```

---

## Final acceptance review

- [ ] Machine milestone:

- schema is exactly `groundwork.assert/v1`;
- every claim appears in JSON;
- every machine result has a unique non-whitespace ID;
- ERROR uses exactly one closed reason value;
- fixture carries stamp, producer, algorithm, and canonical caveats;
- stamp enforcement includes `assert`;
- structural text bytes and exit precedence are unchanged.

- [ ] Rich milestone:

- positive and negative reach obey the full three-valued table;
- pass-through returns all deterministic bypass witnesses;
- concurrent evaluation reuses one rule-independent surface;
- obligations aggregate exact-name statuses with violation dominance;
- unbound and blind facts cannot PASS;
- fitness and claims consume the same typed evaluators;
- characterization tests pin unchanged fitness presentation.

- [ ] Determinism and safety:

- no timestamp, random ID, pointer identity, absolute path, map order, or
  arrival order reaches machine output;
- no partial report is printed for file-level errors;
- graph, policy, and claims inputs are never mutated;
- no new dependency or write-capable interface was added;
- `make verify` passes.

- [ ] Inspect final scope:

```console
git diff --check HEAD~10..HEAD
git status --short
```

Expected: clean planned changes plus any pre-existing user-owned untracked
files.
