# Evaluating a deterministic tool against a capable agent — a reusable playbook

> **`ACTIVE`** · reusable method for honestly testing whether a dev tool beats or complements a coding agent · _reviewed 2026-06-23_

This is the **method**, distilled from the groundwork A/B engagement so it can be
re-run on the next tool. The specific results and the wreckage that produced these
rules live in the companion record, [`ab-testing-postmortem.md`](ab-testing-postmortem.md);
read that for the evidence, read this to **run your own evaluation without fooling
yourself.**

It exists because the default way teams "evaluate" a dev tool — build it, demo it,
believe it — is exactly how you ship a confidently-wrong value claim. The hardest
adversary is not a competitor; it's your own desire for the tool to be good. Every
rule below is a guard against that.

---

## 0. The one idea

> **"Does it work?" is not one question. It is four, each answered by different
> evidence, and conflating them is the master error.** Name which one you are asking
> before you design anything.

| Axis | The question | What evidence settles it |
|---|---|---|
| **Capability** | Can it find/prove/trace something a capable agent *can't*, per instance? | A holdout case the agent fails and the tool passes, same model, varying only tool access |
| **Calibration** | Does it make fewer *confident-wrong* claims? | Cases where the agent confidently guesses and the tool soundly abstains, scored on confident-wrong count, not accuracy |
| **Systematic / reliability** | Does it check exhaustively, automatically, *every* time, regardless of whether anyone remembers to? | Coverage × frequency × determinism — measured, not argued; the agent's *willingness*, not its ability |
| **Demand** | Does anyone who isn't you want it enough to pay the adoption cost? | An **external** grader, or your own **revealed preference** (do you keep using it). Never an authored reaction |

Most disappointment comes from running a capability test, getting a null, and
concluding the tool is worthless — when its real value was on the *systematic* axis
you never measured. Pick the axis first.

---

## 1. The comparator: pick the opponent that can actually beat you

- **Same model, vary only tool access.** Condition C = a capable, grep+read-armed
  agent, fairly prompted, allowed to answer "unsure." Condition T = the same agent +
  the tool. Any difference is then attributable to the tool, not the model.
- **Choose the *real* comparator, not a soft one.** The honest opponent is the
  strongest thing a real deployment would actually use — today that is often a
  **standingly-prompted agent**, not a bare grep rule. Picking a weak comparator you
  beat is the single easiest way to rig the result in your own favor, and you will be
  tempted to, because the weak comparator makes the tool look good. Name your
  comparator and ask: *could this opponent plausibly win?* If not, you picked wrong.
- **Pin every variable.** Model id, temperature/seed, tool access, prompt. "Same
  strong model" is a *claim to verify from the transcripts*, not a default to assume.
  (We asserted it; it was true only by luck; we should have pinned it.)

---

## 2. Pre-register before you run

Write down, before touching the tool:

1. **Hypotheses** (H1, H2…), each falsifiable and tied to one axis.
2. **Tool-independent ground truth** — derived from source or runtime, **never** from
   the tool under test. If the tool helps define "the right answer," the test is
   circular.
3. **The grading rubric** — how each case scores, mechanically, before you see
   outputs.
4. **The holdout set**, including **at least one case you expect the tool to lose**
   (see §4).

Pre-registration is what makes a later "the result supports the tool" non-circular.
A result baked in by the design is ~zero independent evidence — and you will not
notice it was baked in, because it will agree with what you already believed.

---

## 3. The de-biasing kit (what actually worked)

Run as many of these as you can; the last is non-negotiable.

- **Tool-independent ground truth** (§2).
- **Independent verification agent** — a separate agent, blind to the expected
  answers, re-derives the ground truth. It earns its keep: ours caught real errors
  (a missed write the frozen ground truth itself had wrong; an over-claim two of
  three unaided reasoners made).
- **A second adjudicator** for disputes — given the *question* but not the
  *conclusion*. Resolve disagreements on objective artifacts (the code, the trace),
  never on proposer say-so.
- **Blind / mechanical grading** — score against the rubric without knowing which
  arm produced which output.
- **Anti-cherry-pick** — the cases the tool *loses* stay in the tally. A credible win
  is one that survives carrying its losses.
- **Same-model-vary-tool** (§1).
- **The decisive one: build the control specifically designed to break your *current*
  claim, run it, and report the wreckage.** Every "the tool does X the agent can't"
  claim should have to survive an experiment built to kill it. The ones that survive
  are the ones you can trust. (In our run, *every* capability claim died to its own
  control; only the reliability floor survived. That is the method working, not
  failing.)

---

## 4. Trap design — the rules, each bought with a mistake

A "trap" is a holdout case: a defect the tool should catch. Designing it badly is the
fastest way to a false result. The rules below are the failures we made, inverted.

1. **Isolate exactly one property.** *Mistake:* an "assume the column exists" trap
   made the code *incidentally* buggy (runtime error, trigger corruption), so the
   control rejected it on plain-correctness grounds without ever needing the property
   under test → a false null. *Rule:* the code must be **correct in every respect
   except the one property you are testing.** (Our clean trap: a write to a *real*
   audit table via a *real* migration — the only defect is that it sits on a read
   route.)
2. **Neutral prompts.** *Mistake:* a leading round-two prompt ("re-examine for
   correctness / architecture / side-effects") hands the agent the answer. *Rule:*
   prompt the agent as a fair practitioner doing its normal job, not as one told where
   to look.
3. **Never mix "apply the change" with "review the design."** *Mistake:* we invited
   agents to "read the codebase" but had not applied the proposed change — they
   correctly found nothing and derailed into "unimplemented." *Rule:* either apply the
   change and ask for review, or run a pure design review on the proposal text. Not
   both.
4. **Hard-forbid tools/writes for review-only agents, and verify the tree after.**
   *Mistake:* subagents mutated the working tree despite "review-only" framing, leaving
   broken state. *Rule:* deny write/exec capability for read-only review, and check the
   tree is clean afterward.
5. **Never let the agent use the tool under test.** *Mistake:* a control agent "caught"
   the defect — by running the very tool we were trying to compare against. *Rule:*
   that contaminates the comparison; isolate tool access to the T arm only.

---

## 5. The assert-without-verify rule

> Any statement of the form *"the tool does X," "the agent can't Y," "we held Z
> fixed"* is a **claim to verify**, not a fact to assert.

We were caught three times, and each is a pattern to watch for:

- **"This strengthens the standing verdict."** Circular — the result was baked in by a
  biased design. *Retract claims that merely restate your prior.*
- **"The agent can't infer the rule."** Refuted by the very next control (the agent
  applied the rule fine when pointed at the policy; it had simply not consulted it).
  *Distinguish "can't" from "didn't" — they have opposite product implications.*
- **"Same strong model throughout."** True only by inherited-default luck; never
  pinned. *Verify the variable you claim to have controlled, from the transcripts.*

---

## 6. Confirmation bias runs both ways

- **The skeptical null can be baked in.** If every confound in a "negative" result
  happens to point at the prior you already hold, you have not found evidence — you
  have found your prior. Audit each confound for which way it tilts.
- **Over-correction is also bias.** When a result flips to *favor* the tool right after
  you have publicly called out your own skepticism, suspect an over-correction. Guard:
  *was the design fully specified before results were seen, and did the agents have
  genuine latitude?* If yes, trust the flip; if the design moved after you saw data,
  don't.

---

## 7. Two meta-tests that save you from running infinite traps

**The convergence test — before claiming a capability edge.**
Ask: do the tool and the agent share the same **resolvability frontier**? Where the
tool is sound and precise (constant data, statically-resolvable structure), the agent
can usually grep the sink and trace the path. Where the tool **abstains** (dynamic
dispatch, reflection, non-constant values), grep-the-sink and trace-the-path usually
**also** fail. If "the tool can see it" and "the agent can see it" coincide across the
frontier, **there is no capability edge to find, and no trap will manufacture one.**
Stop building traps and re-aim at the systematic axis. (Confirm this empirically once;
then trust it.)

**The treadmill test — before believing any in-house result.**
Every experiment you **design, run, and grade yourself on fixtures you own** is a
closed loop. It can sharpen understanding; it **cannot** answer the demand axis. The
only exits are (a) an **external grader** — someone who isn't you judging whether the
output was worth it, or (b) **revealed preference** — whether you keep using the tool
when no experiment is running. Beware the seductive substitute: an authored reaction
to a stimulus you designed (a "whoa" from a subagent shown a scary case) is **theater**,
not evidence. Continued voluntary use under real deadline pressure is signal, because
you cannot fool yourself into reaching for a tool that does not help when no one is
grading you.

---

## 8. "Novel," "valuable," and "wanted" are three different claims

A composition can be genuinely **novel** (no one wired exactly these parts this way),
deliver only modest **value** (the use it unlocks is replicable or marginal), and have
no **demand** (no one will pay the adoption cost) — all at once. Each is a separate
claim needing separate evidence. Excitement about novelty is the most common way a
dead value claim gets smuggled back in after the data killed it. When you feel "surely
we're sitting on something," name *which* of the three you are claiming, and demand the
matching evidence.

---

## 9. Grade yourself with three-valued honesty

Apply to your *own* conclusions the same discipline a sound tool applies to code:

| Grade | Meaning |
|---|---|
| **Proven** | Locked by a committed test/golden; a regression fails the suite |
| **Measured** | Quantified on a fixture — real numbers, with the small-n / one-service caveat stated |
| **Designed** | Criteria and a results slot specified, not yet run |
| **Unproven** | No evidence either way — graded as such, never as "probably fine" |

Nothing is graded on intention. A capability that demos well but has no committed lock
is **Unproven**, not Proven. **Publish the null in either direction** — a result that
disappoints you is the most valuable kind, because it is the one your bias was working
hardest to suppress.

---

## 10. Run-sheet (the one-page checklist)

```
BEFORE
[ ] Which axis? capability | calibration | systematic | demand   (pick ONE per run)
[ ] Comparator named, and it could plausibly WIN (not a soft one)
[ ] Variables pinned: model id, temp/seed, tool access, prompt
[ ] Hypotheses written, falsifiable, one per axis
[ ] Ground truth derived tool-independently (source/runtime)
[ ] Rubric fixed; grading is mechanical
[ ] Holdout includes ≥1 case you expect the tool to LOSE

DESIGN THE TRAP
[ ] Code correct in every respect EXCEPT the property under test
[ ] Prompt neutral (not "go look for problems")
[ ] Apply-the-change XOR review-the-design — never both
[ ] Review-only agents: no write/exec; verify tree clean after
[ ] T-arm only gets the tool; agent never runs the tool under test

RUN
[ ] Independent verifier re-derives ground truth, blind
[ ] Second adjudicator for disputes (question, not conclusion)
[ ] Blind/mechanical grading
[ ] Losing cases kept in the tally

BEFORE BELIEVING IT
[ ] Convergence: do tool & agent share the frontier? (if yes, no capability edge)
[ ] Treadmill: is this self-graded on owned fixtures? (if yes, it can't answer demand)
[ ] Every "tool does X / agent can't Y / we held Z" VERIFIED from transcripts
[ ] Confounds audited for which way they tilt (incl. toward your prior)
[ ] Built and ran the control designed to BREAK this run's claim
[ ] Graded with three-valued honesty; null published either way
```

---

## 11. The shortest version

You are not trying to prove the tool is good. You are trying to find out whether it
is, against an opponent that can win, on the axis you actually care about, with
controls built to kill each claim — and then to report what survived, including
nothing. The capability claim almost never survives a capable agent; the systematic
and calibration claims sometimes do; the demand claim is settled only outside the
room. Run it in that order, and stop when the controls stop killing things.
