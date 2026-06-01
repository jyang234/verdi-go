# Salience Tier-Map — Specification

The single classifier both pipelines call to assign `Tier`. The static extractor tiers call-graph edges; canonicalization (§3.6) tiers spans to decide what survives a threshold and what the diagram foregrounds. Same policy, same code, two sources — so a logging call is tier 4 whether seen statically or as a span, and a publish is tier 1 either way. It is deterministic: a fixed config plus a given operation yields exactly one tier, with no model anywhere in the path.

Tiers, lowest number = most consequential (log-level analogy): **1 = error, 2 = warn, 3 = info, 4 = debug.** The default snapshot/view threshold is `warn` (tiers 1–2).

---

## 1. Normalized features

Both extractors reduce an operation to a small feature set. Rules match on **features**, never on raw source or raw spans — that decoupling is what lets one ruleset serve both pipelines.

| Feature | Values | Populated by |
|---|---|---|
| `Boundary` | inbound, internal, cross-package, outbound-sync, outbound-async | both |
| `Effect` | mutate, read, io, telemetry, compute, unknown | both |
| `Origin` | same-package, first-party, third-party, stdlib, unknown | static-strong |
| `Fallible` | bool (returns/propagates error) | both |
| `Concurrent` | bool (spawned in goroutine / async dispatch) | static-strong |
| `Identity` | fully-qualified symbol (static) or canonical op/destination (runtime) | both |

**Population asymmetry, stated honestly.** `Boundary`, `Effect`, and `Identity` populate well on both sides. `Origin` and `Concurrent` are static-strong (import paths, SSA call context); runtime often leaves them `unknown`. Rules keyed on static-only features simply don't fire on runtime data — acceptable, because the snapshot's tiering hinges on `Boundary` + `Effect` (the I/O), which both sides have.

### Derivation
- **Static:** `Boundary` from caller-vs-callee package plus the classification hints below; `Effect` from hints + statically-visible `db.operation`/HTTP method; `Origin` from import path; `Fallible` from the signature; `Concurrent` from `go`/`defer` context; `Identity = pkg.Func`.
- **Runtime:** from OTel semconv — span kind → `Boundary` (server/consumer = inbound, client = outbound-sync, producer = outbound-async, internal = internal); `messaging.operation` / `db.operation` / HTTP method → `Effect`; span status → `Fallible`; `Identity` = canonical op.

---

## 2. Classification model

**Ordered rules, first-match wins.** Precedence, highest to lowest:

1. **In-code annotation** (`//tier:N`) — optional; "this symbol is critical/trivial regardless."
2. **Config pins** (identity glob → tier) — explicit per-symbol/per-event overrides.
3. **Tier rules** — an ordered list; user rules are checked before the built-in defaults.
4. **Catch-all tier** — configurable; default 3.

**Determinism.** Rules and pins are ordered lists, never maps (map iteration would be non-deterministic). First match wins; given a fixed config, every operation gets exactly one tier.

**Shadowing.** Because it is first-match, a broad rule placed before a narrow one hides it — author specific → general. A lint that flags unreachable rules is a worthwhile later addition.

Globs use `*` = "any run of characters, separators included" (so `*ledger#Post` matches `internal/ledger#Post`).

---

## 3. Configuration — two layers

### 3a. Classification hints (feed feature extraction)

Tell the extractor which packages/symbols are logging, the bus client, the DB layer — the things the tool cannot infer. Stdlib and OTel semconv are built in; teams add their internal libs. This is the layer most teams actually touch.

```yaml
classify:
  telemetry:  ["log", "log/slog", "go.uber.org/zap", "github.com/rs/zerolog"]   # + stdlib defaults
  busPublish: ["github.com/acme/eventbus#Publish"]
  busConsume: ["github.com/acme/eventbus#Subscribe"]
  db:         ["database/sql", "github.com/jackc/pgx/v5"]
```

### 3b. Tier rules (features → tier)

```yaml
useDefaults: true     # layer the built-in defaults beneath your rules
catchAll: 3
rules:
  - match: { effect: mutate, identity: "*ledger*" }
    tier: 1
pins:
  - { identity: "*decisioning#Evaluate", tier: 1 }   # escalate a critical internal fn
  - { identity: "*health#Ping",          tier: 4 }   # demote a noisy probe
```

---

## 4. Reasonable defaults (the starting point)

Built-in classification hints cover stdlib loggers (`log`, `log/slog`), `database/sql`, the `net/http` client, plus OTel semconv on the runtime side.

Built-in tier rules, in order (first-match):

| # | match | tier |
|---|---|---|
| 1 | `effect=telemetry` | 4 |
| 2 | `boundary=outbound-async` (publish) | 1 |
| 3 | `boundary=inbound` (entry) | 1 |
| 4 | `effect=mutate` | 1 |
| 5 | `boundary=outbound-sync, effect=read` | 2 |
| 6 | `boundary=outbound-sync` (other external sync) | 1 |
| 7 | `boundary=cross-package, origin=first-party, fallible` | 2 |
| 8 | `origin=first-party` | 3 |
| 9 | `origin=stdlib` | 4 |
| — | catch-all | 3 |

A team using standard logging/DB/HTTP and OTel semconv gets correct tiering with **zero config**; the only common addition is naming their internal bus client and logger under `classify`. Telemetry is ranked first deliberately so a logging call never gets caught by the `first-party` rule.

---

## 5. Consumption by both pipelines

Both call `Classify(features)` during extraction. Canonicalization §3.7 drops spans above the threshold (default `warn`) and promotes survivors; the static extractor annotates each edge with its tier for foregrounding and the same threshold filtering. Because the tier *policy* is shared, the two artifacts agree on what counts as consequential — the behavioral snapshot and the structural map foreground the same boundary I/O.

---

## 6. Open knobs

- **Catch-all default:** `3` ("unknown is ordinary" — visible at info, hidden from the warn snapshot) vs. `4` (hidden entirely). Recommend 3, so an unclassified operation surfaces at info rather than vanishing.
- **Resolution mode:** first-match (default, predictable) vs. escalate-to-most-consequential (the minimum tier across all matching rules). First-match recommended for transparency; escalate is friendlier if you dislike ordering rules but can surprise.
- **In-code annotations:** include them or stay config-only. Annotations put intent next to the code but rot; config centralizes and is reviewable in one place.
