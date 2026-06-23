# groundwork / flowmap — A/B testing post-mortem (single-source record)

> **`DESIGN RECORD`** · self-contained A/B + adversarial-control record of groundwork's distinctive value · _reviewed 2026-06-23_

**This is the complete, self-contained record of every A/B test and adversarial control we ran to pin down groundwork's value. Read this one document — it contains all designs, evidence, numbers, and verdicts inline; no other doc is required.** (Markdown, not Slack-formatted.)

- **Tool:** flowmap (sound static call-graph / boundary-effect / taint analysis) + groundwork (the gate/lenses over it). "The lens" / "the gate" below = this toolchain.
- **Subjects:** the event-bus and cgate services in this monorepo.
- **Comparator throughout:** a capable, grep+read-armed coding agent ("grep agent"), same model, same task, varying only tool access.

---

## 1. Verdict (TL;DR)

groundwork has **no per-instance capability edge** over a capable grep-armed agent. We tested that claim seven ways across two model tiers (Opus 4.8 and Haiku 4.5); it held every time. Its real, durable value is **systematic, not capability-based**:

> **It checks — soundly, exhaustively, deterministically, on every commit — what an agent *won't*. Not what an agent *can't*.**

A capable agent can verify any single invariant question (find a write, trace a deep flow, apply a policy rule) — we proved this repeatedly, even with a cheap model. What it won't do is verify *all* of them, on *every* commit, reliably, soundly, unprompted. groundwork is exactly that gap. Plus one edge in isolation: **calibration** — it abstains where agents confidently guess wrong (the soundness guarantee that makes "never a false SATISFIED" true).

**How to talk about it (and what to drop):**
- ✅ "The trustworthy, automatic, exhaustive CI backstop your AI agents structurally won't be for themselves." Value grows as AI writes more of the code.
- ❌ "Finds what grep/your agent can't." Dead — refuted across three discovery tiers and two reachability controls.

---

## 2. The question, and how it evolved

1. Does the lens out-**discover** a grep agent? (Tiers 1, 7, 8) → **No.**
2. Is the lens better-**calibrated** (fewer confident-wrong claims)? (Tier 7) → **Yes** — its one edge in isolation.
3. Does the gate *in an agent's coding loop* reduce confident-wrong commits? (Tier 9 fair run) → **Yes, seed result** — but every "capability" reading of it died under controls.
4. Is the surviving value capability or reliability? (Tier 9 controls + model tiers) → **Reliability/systematic.**

Common method across all tiers: **same model, vary only tool access**; isolated subagents (own context, blind to each other / the ground truth / the rubric); pre-registered designs and rubrics; tool-independent ground truth; independent verification agents; mechanical grading; anti-cherry-pick (cases the lens loses are kept in the tally); publish the null in either direction.

---

## 3. Tier 1 — discovery baseline (origin)

Earliest tier; established the bar. A capable grep agent is a **strong discovery baseline** — on single-hop caller-finding it hit **precision 1.00, recall 25/26**, partially refuting its own hypothesis that the lens would dominate. This set the throughline every later tier confirmed: grep is hard to beat at *finding things*. Later tiers deliberately moved to ground where grep is *structurally* weak (forward value-flow, transitive write surfaces) to give the lens its best shot.

---

## 4. Tier 7 — Taint A/B (PII-in-log), 2026-06-22

**Question.** Can sensitive field X reach log sink Y? To answer a *negative* soundly, you must prove the complete forward cone never reaches Y — grep can only sample paths and guess; `flowmap taint` gives three-valued FLOW / NO-FLOW / ABSTAIN.

**Hypotheses.** H1 (accuracy): lens ≥ grep, strictly better on multi-hop/dispatch. **H2 (calibration, load-bearing):** lens confident-wrong ≈ 0 by construction; grep makes confident-wrong claims on hard cases. H3 (cost): lens cheaper on multi-hop.

**Arms.** Identical `Explore` agent + model + task; C = grep+read (prompted as a fair, capable tracer, allowed to answer UNSURE); T = same + `flowmap taint`. Subject: cgate, sink = `zap.String` (no-log-PII) or the Customer.io send. n=1 per case.

**The four pre-registered holdout cases** (ground truth source-derived, never from the tool):

| # | field → sink | runtime truth | soundly provable? | favors |
|---|---|---|---|---|
| 1 | `Recipient.CustomerID` → log | REACHES | yes (witnessed) | lens |
| 2 | `Recipient.Email` → log | DOES-NOT | **no** (escapes into send+DB first) | calibration |
| 3 | `Recipient.Phone` → log | DOES-NOT | **no** (same shape) | calibration |
| 4 | `Recipient.Email` → `cio.SendEmail` | REACHES | **no** (interface-dispatched send) | **grep** |

Case 4 is the **anti-cherry-pick guardrail** — the most important real PII flow, which the lens *cannot* confirm (the send is behind an interface). A credible result must show the lens winning *despite* carrying #4 as a loss.

**The dispute that became the headline.** The independent ground-truth verifier (V1, blind to expected answers) agreed on every runtime truth but called C2/C3 *provable*-safe. Resolved on objective code, not opinion: `resp := d.client.SendEmail(…Email…)` → `deliveryID: emailDeliveryID(resp)` → `zap.String("deliveryId", …)` at `dispatcher.go:157`. `SendEmail` is interface-dispatched/opaque, so a sound analysis must assume the logged `deliveryId` *may* derive from `Email` → **UNPROVABLE**. A second independent adjudicator (V2), given the question but not the conclusion, found the same path and agreed. **Two of three unaided reasoners over-claimed "safe" where a sound analysis must abstain** — exactly the false-SATISFIED the lens refuses.

**Results (n=1):**

| case | truth (provable?) | C (grep) | T (lens) |
|---|---|---|---|
| C1 CustomerID→log | REACHES (yes) | ✅ correct-confident (+4 sites) | ✅ correct-confident (FLOW, +3 sites) |
| C2 Email→log | DOES-NOT (no) | ❌ **confident-wrong** (over-claim) | ✅ calibrated-abstain |
| C3 Phone→log | DOES-NOT (no) | ❌ **confident-wrong** (over-claim) | ✅ calibrated-abstain |
| C4 Email→send | REACHES (no) | ✅ correct-confident (`dispatcher.go:71`) | ⚪ missed-useful (abstain) |

**Tally — C: 2 correct-confident, 2 confident-wrong, 0 calibrated. T: 1 correct-confident, 2 calibrated-abstain, 0 confident-wrong, 1 missed-useful.**

**Outcomes.** **H1 (accuracy) NOT supported** — grep matched runtime truth **4/4**, lens confidently-correct on only 1/4 (its soundness costs it useful answers on short flows; it even ceded C4 outright). **H2 (calibration) SUPPORTED** — confident-wrong C=2 vs T=0. Caveat stated plainly: grep's two wrongs were *outcome-correct but unsound* ("right by luck") — its reasoning traced only the field's direct uses and never considered the `deliveryId` response-flow-back. On a codebase where the CIO response echoed the address into the logged `deliveryId`, grep's confident "DOES-NOT-REACH" is a real PII leak; the lens's ABSTAIN stays correct. For no-log-PII, a false "safe" is the costly error, so calibration is the right thing to value. The anti-cherry-pick guard (C4) worked — the lens lost it, and that loss is in the tally. **Limits:** n=1, cgate-only, flows 1–3 hops (too short to test H1/H3's deep-flow hypothesis).

---

## 5. Tier 8 — Effect-surface A/B (transitive DB writes), 2026-06-22

**Question.** Name a command's full **write surface** (tables × operation) when the writes sit behind a shared `UnitOfWork.RunInTx(closure)` / interface-dispatched `RunInTxResult` seam + transitive writes (auto-subscribe, cascade). This is the deep-multi-hop recall/cost contest Tier 7 was too shallow to run.

**Three arms.** C = grep+read; **T_shipped** = lens at `1e5cbbe`; **T_patched** = lens + a local prototype (`unwrapPromotion`+dedup value-receiver fix) + `--rebind`. Subject: 10 event-bus transaction-runner commands; ground truth reused from a prior source-derived, already-de-biased pre-registration. n=1 per command.

**The holdout deliberately spans both directions** (anti-cherry-pick): 8 `RunInTx` commands the shipped lens *over*-reports (returns the 10-write union) and 2 `RunInTxResult` commands it *under*-reports (false-cleans #4 `CreateEventTypeSubscriptionCommand`, #6 `CreateEventTypeTemplateSubscriptionCommand`). Transitive writes in the set: auto-subscribe `INSERT event_type_subscriptions` (#2, #6), `cascadeOutbox` DELETE (#10).

**Independent verification earned its keep again.** The blind verifier confirmed every set *and* surfaced a write the frozen prereg ground truth had **missed**: `UPDATE event_types` on #2 (creating a version bumps the parent's `current_version`). Grep and the patched lens independently found it too. (One objective reconciliation: `DELETE FROM provisioning_outbox WHERE source_id IN(…)` — the *table* is constant → labeled DELETE; only the WHERE is dynamic.)

**Scores (vs reconciled ground truth):**

| arm | mean recall | mean precision | false-cleans | exact 10/10 |
|---|---|---|---|---|
| **C (grep)** | **1.00** | **1.00** | 0 | **10/10** |
| **T_shipped** (`1e5cbbe`) | 0.80 | **0.19** | **2** (#4, #6) | 0/10 |
| **T_patched** (prototype + `--rebind`) | **1.00** | **1.00** | 0 | **10/10** |

**Outcomes.** **H1 (lens out-recalls grep on transitive writes) REFUTED** — grep hit recall 1.0, finding *every* transitive write including the one the prereg GT itself missed. Third consecutive tier (1, 7, 8) where a capable grep agent matches truth. **H2 (the patch) SUPPORTED strongly** — shipped lens is unusable here (0/10 exact, precision 0.19 union, 2 false-cleans); the patch flips it to 10/10 exact, precision 1.0 (recovers the under-reporters *and* de-unions the over-reporters). This was the **field validation of the value-receiver TxClosure fix** — backed by a 10/10 measured recovery, not just a fixture repro. **H3 (cost) inconclusive** — grep traced all 10 cheaply (~11 reads + 8 greps, amortizing the shared storage layer); the lens pays a fixed graph-build then ~0 per query, so it wins only when many queries amortize the build. **Caveat:** T_patched was an unsubmitted prototype; its 10/10 is the *proposed* tool, the shipped state is 0/10.

> **Throughline after 1/7/8:** a capable grep agent is a strong DISCOVERY baseline (matched truth all three times). The lens's distinctive value is being a sound, deterministic GATE (never a false claim), NOT out-discovering grep.

---

## 6. Tier 9 — Closed-loop coding-quality A/B (the main event)

**Question.** Does a sound gate *in an agent's coding loop* reduce confident-wrong (false-safe) commits vs the same agent alone? The only tier measuring the combined tool's effect on *agent output*. All agents Opus 4.8 unless noted; subject = event-bus; trap class = a write on a read route (violates `read-routes-stay-read-only`, which `policy.json` encodes and the gate enforces).

The gate verdict the agents face, for reference:
```
⛔ [must_not_reach] read-routes-stay-read-only: api.ServerInterfaceWrapper.GetEventTypeVersion reaches boundary:db INSERT read_audit
     (*…/internal/api.ServerInterfaceWrapper).GetEventTypeVersion → boundary:db INSERT read_audit
⛔ [io_budget] event-bus.main reaches 15 write(s) over a budget of 14: … db INSERT read_audit …
```

### 9a — Biased run (INVALID — kept as a lesson)
4 read→write traps; A (self-review) vs B (gate-in-loop), paired. **Result: A 0/2, B 0/2** — apparent null ("gate adds nothing"). **Why invalid:** four confounds, all tilting *against* the gate (and all confirming my prior skeptical read):
1. **Over-determined traps** — the "assume the column exists" framing made the code *also* incidentally buggy (nonexistent column → runtime error; `updated_at`-trigger corruption; outbox `UNIQUE(source_id)` no-op), so self-review rejected on plain-correctness grounds without ever needing the architectural invariant.
2. **Best-case control** — A used a maximally-diligent "hunt for problems" prompt.
3. Strong model both arms. 4. Leading round-2 prompt.

The null was baked in by design. I had written "this strengthens the verdict" → **retracted as circular.** Also robust here: **gate-coverage finding** — only 2 of 4 traps were gate-visible; GetSubscriber/GetPublisher writes sit past a blind frontier on the participant path → gate silent. **Half the realistic traps were in the gate's blind spot.**

### 9b — Fair run (VALID)
Fixes: **clean trap** — "audit access by `INSERT`ing into a real `read_audit` table" (created via a real V10 migration, so the code is *correct*; the *only* defect is the read-route write); neutral prompts; same routes as 9a (GetEventTypeVersion, GetEventTypeSubscription); paired.

- **Round-1 (realistic implement):** BOTH agents shipped the write, called it production-ready, and **neither flagged the read-only concern.** They debated *how* to write (blocking vs best-effort, correlation IDs, transactions) — never *whether* a GET should write.
- **Round-2 first attempt — CONTAMINATED:** I never applied the proposals to the tree, then invited "read the codebase" → agents correctly found no code and derailed into "unimplemented." Invalid; re-ran.
- **Round-2 clean re-run** (symmetric design review): **A 2/2 ship the violation, B 0/2.** → **seed H1 SUPPORTED.** A's reviews were thorough — they flagged the integration-test harness gap, the deployed DB role lacking `INSERT`, best-effort-vs-blocking, retention, missing tests — **but none identified the read-only violation, and every fix preserved the write.** B (handed the gate verdict) called it a true positive and moved the write off the read path.

This looked like a capability win ("gate catches what the agent ships"). Controls 1–2 then killed that reading.

### 9c — Control 1: policy-pointer (is it "can't infer" or "didn't consult"?)
`read-routes-stay-read-only` is in `policy.json`, a readable repo file. Pointed condition A at it, reasoning-only.
- Tools-allowed variant discarded: one agent caught it *by running the gate itself* (contaminated) and mutated the tree (mess; restored).
- **Reasoning-only variant (clean): both agents caught the violation, 2/2** — read the invariant, hand-traced, returned `POLICY-COMPLIANCE: NO`. No gate.

**→ "the agent can't infer the rule" REFUTED.** It applies the rule fine when pointed at the policy; round-1 shipped the violation because it *didn't consult* a `.flowmap/` artifact while building a feature. Value reframed: auto-surfacing an encoded rule the agent doesn't consult by default — not knowledge it can't access.

### 9d — Control 2: deep-reachability (does the gate have a reach edge?)
Built a real, in-tree, **clean + deep + indirect** trap, compiled:
```
GetEventTypeVersion → Handle → finalizeVersionRead
  → readObserver.observe        (INTERFACE, single impl AuditObserver, wired in bootstrap)
  → RecordRead → INSERT read_audit
```
6 hops, **2 interface dispatches** (`readObserver→AuditObserver`, `ReadAuditWriter→PostgresStore`), benign names.
- **Gate pilot:** `must_not_reach` FIRES — resolves both interface dispatches and traces all 6 hops soundly.
- **Agent test:** 3 reasoning-only agents (read+grep, gate forbidden) → **3/3 found it, high confidence**, exact file:line traces through both dispatches. Method: **grep the write sink, trace backward** to the route. Backward-from-sink is robust to depth.

**→ "deep-reachability edge" REFUTED.**

### 9e — Model verification + Haiku generalization
- **Model audit (verify, don't assume):** confirmed from subagent transcripts that **every experiment agent ran `claude-opus-4-8`** (inherited; I never pinned it — the pre-reg's "same strong model" was correct but unverified-until-asked). 6 unrelated Haiku transcripts in the dir were not experiment agents.
- **Haiku re-run:** repeated control 2 on **Haiku 4.5**, identical chain + prompt, single variable → **3/3 found it**, high confidence, ~25–37s and a few cents each.

**→ "no per-instance capability edge" generalizes down the model tier** — even a cheap, fast model matches the gate's deep reachability. The "weaker model might miss it" residual is refuted.

---

## 7. Why no trap can rescue a capability edge (the convergence)

The gate and a grep-armed agent share the **same static-resolvability frontier**:
- Where the gate is sound and precise (const SQL, statically-resolvable calls/dispatch) → the agent can grep the sink and trace the path.
- Where the gate **abstains/blinds** (non-const `db call` SQL, reflection, dynamic dispatch, blind frontiers — e.g. the participant path in 9a) → grep-the-sink *also* fails (sink not greppable / path not traceable).

So **"statically resolvable" (gate sees it) ≡ "greppable sink + traceable path" (agent sees it).** You cannot construct a case that defeats the agent but not the gate. Confirmed across discovery (1/7/8), policy-application (9c), and deep double-interface reachability (9d), for both Opus and Haiku.

---

## 8. Methodology post-mortem

### What went wrong (owned)
1. **Over-determined traps → false null (9a).** Incidental bugs let the control reject without the gate's concern. A trap must isolate the *single* property under test.
2. **Un-applied proposals + "read the tree" → derail (9b round-2 v1).** Either apply the change or frame as pure design review — never mix.
3. **Subagents mutated the tree despite "review-only" framing** (a control agent ran the gate; another left a broken edit). Restored + build-verified each time. Hard-forbid tools/writes for read-only review and verify the tree after.
4. **Assert-without-verify — caught three times:** "this strengthens the verdict" (circular; retracted); "the agent can't infer the rule" (over-claim; control 1 corrected it); "same strong model / Opus" (unverified; true by luck; should have pinned `model`).
5. **Confirmation bias.** 9a's confounds all pointed at the skeptical null I already believed; when 9b flipped pro-tool right after a bias call-out, that risked over-correction — checked by confirming the fair design was specified before results and the agents had full latitude.

### What worked
Pre-registration · tool-independent ground truth · independent verification agents + a second adjudicator (which caught real errors: 3 in the Tier-7 prereg, the missed `UPDATE event_types` in Tier-8, the C2/C3 provability dispute) · blind/mechanical grading · anti-cherry-pick (losing cases kept) · same-model-vary-tool · and the decisive discipline: **run the control specifically designed to break each successive claim, and report the wreckage.** Every "the gate does X the agent can't" claim died to its own control; the reliability floor is what survived.

---

## 9. Final verdict (bounded) + caveats

- **No per-instance capability edge.** Matched on discovery (Tiers 1/7/8), policy-application (9c), and deep reachability (9d), by Opus *and* Haiku (9e).
- **Real value = systematic:** exhaustive + automatic + sound + deterministic coverage of the entire read-route × write-sink (and PII→sink, write-surface, etc.) matrix, on every commit, independent of any agent's diligence / availability / correctness. A CI guardrail in the precise sense; value grows as AI authors — confidently wrong about team-specific rules in exactly the way that slips past fast review — write more of the code.
- **Calibration edge (Tier 7):** abstains where grep/agents confidently guess wrong — the soundness guarantee underwriting the "trustworthy" in the systematic value.

### Caveats (honest)
- Small n throughout (Tier 7: 4 cases, n=1 each; Tier 8: 10 commands, n=1; Tier 9: 2 routes / 1 invariant class). Single service (event-bus / cgate). My own design + execution, though control findings are source-cited and reproducible. Two of our own experiments produced misleading results before correction (9a's false null; 9b's contaminated round-2).
- **One untested residual where a real capability edge might still live:** a **multi-implementation interface where only one impl is live.** The gate's VTA computes the live set; a hand-tracer must enumerate all impls and infer which is wired. Our traps used single-impl interfaces (trivial for both). This is the one experiment that could still surface a capability edge — explicitly not run.

---

*Provenance: Tiers 1/7/8 run 2026-06-22; Tier 9 + controls + Haiku run 2026-06-22→23. All experiment subagents Opus 4.8 except the Tier-9e Haiku re-run (Haiku 4.5). The only code change retained from this work is the event-bus `policy.json` curation (the read-route invariant set); all experiment scaffolding was reverted and the tree build-verified clean.*
