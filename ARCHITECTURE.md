# Architecture

`query-optimizer` (`qopt`) is a **connect-diagnose-verify** SQL optimizer built
on **clean architecture** (same layering as the `warden` service). The AI
proposes optimizations; this Go binary **proves** them against a live database.
"Generate fuzzy, verify hard."

## The one idea

> The AI is good at *proposing* (reads EXPLAIN + rules, suggests a rewrite). It
> is bad at *being trusted*. So a deterministic layer **runs old vs new and
> compares** before any advice is shown. A faster query that returns different
> rows is a bug, not a fix — the verify layer catches that.

## Clean-architecture layers

Dependencies point **inward**. `domain` is the center and imports nothing.
Outer layers depend on the `port` interfaces, never on each other's concretions.

```
        ┌──────────────────────────────────────────────┐
        │  cmd/qopt        (entry: imports DB drivers)   │
        ├──────────────────────────────────────────────┤
        │  internal/deps   (composition root / wiring)   │
        ├──────────────────────────────────────────────┤
        │  internal/handler/cli   (delivery: CLI)        │   ← Slack/web would sit here
        ├──────────────────────────────────────────────┤
        │  internal/usecase/optimize  (orchestration)    │
        ├───────────────┬────────────────────────────────┤
        │ internal/service │  internal/repo               │
        │  safety          │   database (QueryRunner)      │
        │  verification    │   engine adapters (dialect)   │
        ├───────────────┴────────────────────────────────┤
        │  internal/port   (interfaces — the seams)      │
        ├──────────────────────────────────────────────┤
        │  internal/domain (entities: Proposal, Verdict) │  ← imports nothing
        └──────────────────────────────────────────────┘
```

| Layer | Package | Imports | Responsibility |
|---|---|---|---|
| Domain | `internal/domain` | nothing | Entities: `Proposal`, `Verdict`, `OptKind`, `RunResult`, sentinel errors. |
| Port | `internal/port` | domain | The interfaces (seams): `SafetyValidator`, `QueryRunner`, `Verifier`, `VerificationService`. |
| Service | `internal/service/safety` | domain | `AssertSafeSelect` — the security boundary. |
| Service | `internal/service/verification` | domain, port | Per-kind verifier strategies + dispatcher. **The growth axis.** |
| Repo | `internal/repo/database` | domain, port | `QueryRunner` over `database/sql`; engine dialect hidden here. |
| Usecase | `internal/usecase/optimize` | domain, port | Orchestration: `Explain`, `Verify`. No SQL, no rules. |
| Handler | `internal/handler/cli` | domain, deps | Delivery: CLI args → use case → printed result. |
| Wiring | `internal/deps` | all impls | Composition root: builds concretes, injects into ports. |
| Entry | `cmd/qopt` | deps, handler, drivers | `main`. The only place DB drivers are imported. |

## The dependency rule in practice

- `domain` knows nobody. `port` knows only `domain`.
- `usecase` depends on `port` interfaces — it never imports `repo` or `service`.
- `service` and `repo` implement `port` interfaces; the rewrite verifier uses
  `port.QueryRunner` (an abstraction), not the concrete repo.
- `deps` is the single place that knows the concretes and wires them together.
- Swap any implementation by changing one line in `deps`. Tests inject fakes
  for the ports — no DB needed for usecase/service unit tests.

## Data flow (verify)

```
AI writes Proposal JSON ─stdin─▶ handler/cli ─▶ usecase.Verify
                                                    │ (port.VerificationService)
                                       verification.Service dispatch by Kind
                                                    │ (port.Verifier)
                              rewrite: runner.Run(old) + runner.Run(new)
                                                    │ (port.QueryRunner)
                                   repo/database: safety.AssertSafeSelect
                                                    │  + engine guards + EXPLAIN
                                       compare rows + sampleHash + timing
                                                    │
                                            Verdict JSON ─stdout─▶ AI/DA
```

## The escalation ladder (why 4 verifier kinds)

`v2-migration` had one transform. This tool has many, unequal in how far
self-service goes:

| Kind | Self-serve? | Verifier behavior |
|---|---|---|
| `rewrite` | yes | **run old vs new, compare.** `verified` / `behavior_changed` / `not_faster`. |
| `index` | propose-only | `unverifiable` — prediction; HypoPG noted on Postgres. |
| `partition` | propose-only | `unverifiable` — DBA territory. |
| `shard` | escalate | `unverifiable` — detect, hand to data eng. |

This is why the **verifier kind** (`internal/service/verification`) is the
primary extension axis — see `EXTENDING.md`.

## What "verified" means

`repo/database` fingerprints each run: row count + sha256 of the **sorted**
first 200 rows. The rewrite verifier compares both + timing. Outcomes: see the
ladder table above.

> Sampling caveat: only the first 200 rows are fingerprinted. Result sets > 200
> rows with no `ORDER BY` can falsely mismatch — give compared queries a
> deterministic `ORDER BY`. Inherited from the Python design.
