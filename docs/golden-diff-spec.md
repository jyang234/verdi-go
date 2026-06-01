# Golden Files & Behavioral Diff — Specification

This closes the behavioral loop. The golden is the committed canonical IR (the snapshot); the diff engine compares observed-vs-golden and produces the change set a reviewer reads. This is where the product's "verify PRs faster" payoff is realized — so the diff has to be *semantic and prioritized*, not a raw text delta.

---

## 1. The golden file

The golden **is** the serialized canonical IR from canonicalization — sorted, position-insensitive, deterministic. No new format; it's the canonical tree on disk.

Layout, one test = one flow = one golden:

```
testdata/flows/<flow-id>.golden.json   # the canonical IR (the assertion target)
testdata/flows/<flow-id>.flow.md       # the rendered Mermaid view (committed, for review)
```

---

## 2. Lifecycle

- **Normal run:** harness captures → canonicalize → load golden → **structural diff** → empty ⇒ pass; non-empty ⇒ fail with the rendered change set.
- **`-update`** (the Go golden convention): re-run, canonicalize, rewrite the golden IR and re-render the view. The deliberate re-baseline.
- **Determinism self-test first** (from the harness/canonicalization): stabilize before comparing, so flakiness surfaces as a config bug rather than golden churn.
- **Two gate outcomes** (established): diff with no `-update` in the PR ⇒ CI fail (unintended behavioral change caught); PR updates the golden ⇒ CODEOWNERS routes the view diff to a human, who judges "is this the new behavior I expect?"
- **One of two gate mechanisms.** This pipeline's gate is *snapshot-assertion* — run the flow, compare observed-canonical to the committed golden, `-update` to re-baseline. The static pipeline's gate is a distinct *currency* check — regenerate the artifact from code and `git diff --exit-code`. Both are unified by CODEOWNERS routing plus fail-on-unexpected-change plus human-as-oracle, but the GitLab wiring is **two jobs, not one**.
- **Cardinality assertions are enforced by the test runner.** A per-flow prescriptive cardinality (e.g. `ExpectExactlyOnce`) is checked at test time against the IR's observed `Multiplicity`, independently of the golden diff — a violation fails the test even if the golden otherwise matches.
- **The golden reflects *tested* behavior, so test changes move the baseline.** Because the snapshot is a faithful function of the flow test (its doubles, its trigger), restructuring a test — swapping a mock, changing what a stub returns — can change the golden and route to review even with no production-code change. That's correct (the test defines the contract-as-tested), but reviewers and CODEOWNERS should expect a test-only diff to be a legitimate baseline shift, not assume every golden change implies a code change. See the scope & guarantees doc.

---

## 3. The diff engine — structural, not textual

The IR is a tree, so a text diff of the JSON is noise: a moved subtree shows as a large delete-plus-add. Diff the **structure**, exploiting the stable identity canonicalization already gives every node.

- **Keyed top-down match:** root ↔ root, then children matched by canonical `Op`, with duplicates disambiguated by order among same-`Op` siblings. Robust to insertion/deletion — no index-shift cascade.
- **Per matched pair:** compare attributes (`status`, `errorType`, `tier`, `peer`, `kind`, and salient `attrs`) → *Changed*; compare the group tags → *ConcurrencyChanged* / *CardinalityChanged*; then recurse. A changed secondary `attrs` value (e.g. normalized SQL where the operation and table are unchanged) is a real change, but it is tier-prioritized low (§4) so it never outranks a contract or tier-1 change.
- **Order:** among matched siblings, detect reorders via longest-increasing-subsequence — order is behavioral, so a reordered call is a real change, not noise.
- **Unmatched:** new-only → *Added* (subtree); old-only → *Removed*.

Taxonomy: Added, Removed, Changed(attr), Reordered, ConcurrencyChanged, CardinalityChanged. Tractable — keyed match plus LIS, not general tree-edit-distance, because `Op` supplies stable identity.

---

## 4. Prioritization — the third consumer of the tier-map

Not all changes carry equal weight, and a flat change list buries the important ones. Prioritize:

1. **Contract changes** — a published/consumed event added or removed, an external dependency added or removed. The service's interface with the bus/world changed; this is the headline.
2. **Tier-1 changes** — status (ok→error), mutations.
3. **Structural** — reorders, concurrency, cardinality.
4. **Lower tiers.**

This reuses the tier-map a third time (publish/consume = the bus contract; tier = consequence), so the reviewer sees "this PR now publishes `disbursement.failed` and the payment span can now error" *before* "auditLog moved." Noise is inherited from canonicalization — volatile bits stripped, multiplicity collapsed, sorted — so every reported change is a real behavioral change, and diff granularity equals snapshot granularity (the tier threshold).

---

## 5. Presentation

- **Structured change set (primary, authoritative).** The typed, prioritized list — what fails the test and what the reviewer reads. Precise and copy-pasteable: `[CONTRACT] + publishes disbursement.initiated`, `[T1] payment-gw charge: status ok→error`, `[REORDER] auditLog before publish`.
- **Rendered view (orientation).** The committed `.flow.md` Mermaid — GitLab renders it, so the reviewer can see the whole new flow.
- **Visual delta (optional).** Before/after diagrams, or a single annotated diagram (added in green, removed noted). A reviewer convenience only — **the gate depends on the IR structural diff, never on visual diffing**, which keeps it precise and immune to Mermaid-renderer drift.

---

## 6. Gate configuration (the flexibility lever)

- **Default:** any change in the snapshot fails and is reviewed.
- **Tier-aware gate (opt-in):** fail only on changes at tier ≤ N (e.g., contract + tier-1); treat lower-tier changes as informational. For teams that want the gate to fire only on consequential deltas — part of catering to all repo styles.

---

## 7. Open decisions

- **Same-`Op` duplicate matching:** order-based (simple, predictable) vs. attribute-assisted (match the most-similar pair, fewer false add/remove when duplicates' attributes differ). Order-based is the safe default.
- **Visual delta:** invest in annotated diff diagrams, or ship structured-change-set plus before/after only.
- **Gate default:** any-change vs. tier-aware (default any-change; tier-aware opt-in).
- **Rendered view location:** committed (GitLab renders, the diagram diff is visible in the MR) vs. generated-on-demand (cleaner repo, but no MR-visible diagram diff). Committed gives the CODEOWNERS diagram-diff signal.
