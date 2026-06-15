# Architecture

`query-optimizer` (`qopt`) is a **connect-diagnose-verify** SQL optimizer built
on **clean architecture** (same layering as the `warden` service). The AI
proposes optimizations; this Go binary **proves** them against a live database.
"Generate fuzzy, verify hard."

> Paths below are under `backend/`. The frontend (React + Vite SPA) lives in
> `frontend/` and is a pure client of the HTTP adapter.

## The one idea

> The AI is good at *proposing* (reads EXPLAIN + rules, suggests a rewrite). It
> is bad at *being trusted*. So a deterministic layer **runs old vs new and
> compares** before any advice is shown. A faster query that returns different
> rows is a bug, not a fix — the verify layer catches that.

## Clean-architecture layers

Dependencies point **inward**. `domain` is the center and imports nothing.
Outer layers depend on the `port` interfaces, never on each other's concretions.

```
        ┌────────────────────────────────────────────────────────┐
        │  cmd/{qopt, qopt-server, qopt-seed}  (entry; DB drivers) │
        ├────────────────────────────────────────────────────────┤
        │  internal/deps   (composition root: wiring + run mode)   │
        ├────────────────────────────────────────────────────────┤
        │  internal/handler/{cli, http}   (delivery adapters)      │
        ├────────────────────────────────────────────────────────┤
        │  internal/usecase/optimize                               │
        │    Explain · Verify · Optimize (loop) · Recheck          │
        ├──────────────┬───────────────────┬──────────────────────┤
        │ internal/service                  │  internal/repo        │
        │  safety                           │   database (Runner)   │
        │  verification (per-kind)          │   baseline (store)    │
        │  propose → {cli, api} (Proposer)  │   engine adapters     │
        ├──────────────┴───────────────────┴──────────────────────┤
        │  internal/port   (interfaces — the seams)                │
        ├────────────────────────────────────────────────────────┤
        │  internal/domain (Proposal, Verdict, Progress, Baseline) │  ← imports nothing
        └────────────────────────────────────────────────────────┘
```

| Layer | Package | Imports | Responsibility |
|---|---|---|---|
| Domain | `internal/domain` | nothing | Entities: `Proposal`, `Verdict`, `OptKind`, `RunResult`, `Progress`, `ProposeInput`, `Baseline`, `RecheckResult`, sentinel errors. |
| Port | `internal/port` | domain | Seams: `SafetyValidator`, `QueryRunner`, `Verifier`, `VerificationService`, `Proposer`, `BaselineStore`. |
| Service | `internal/service/safety` | domain | `AssertSafeSelect` — the security boundary. |
| Service | `internal/service/verification` | domain, port | Per-kind verifier strategies + dispatcher. **The growth axis.** |
| Service | `internal/service/propose` | domain, port | The AI half: prompt build + parse, with `cli` (user's claude/cursor) and `api` (server key) `Proposer` implementations. |
| Repo | `internal/repo/database` | domain, port | `QueryRunner` over `database/sql`; engine dialect hidden here. |
| Repo | `internal/repo/baseline` | domain, port | `BaselineStore` — "before" snapshots for verify-after-applied. |
| Usecase | `internal/usecase/optimize` | domain, port | Orchestration: `Explain`, `Verify`, `Optimize` (propose→verify→retry), `Recheck`. No SQL, no rules. |
| Handler | `internal/handler/cli` | domain, deps | Delivery: CLI args → use case → printed result. |
| Handler | `internal/handler/http` | domain, usecase | Delivery: HTTP/SSE → use case → JSON/event stream. Holds engine + DSN; can serve the built SPA. |
| Wiring | `internal/deps` | all impls | Composition root: builds concretes, picks a `Proposer` by run mode, injects into ports. |
| Entry | `cmd/qopt` | deps, handler/cli, drivers | CLI `main`. |
| Entry | `cmd/qopt-server` | deps, handler/http, drivers | Web `main` (env-driven; serves all run modes). |
| Entry | `cmd/qopt-seed` | deps, drivers | Loads demo data through the Go drivers (no client binary). |

## The dependency rule in practice

- `domain` knows nobody. `port` knows only `domain`.
- `usecase` depends on `port` interfaces — it never imports `repo` or `service`.
- `service` and `repo` implement `port` interfaces; the rewrite verifier uses
  `port.QueryRunner` (an abstraction), the loop uses `port.Proposer`.
- `deps` is the single place that knows the concretes and wires them together.
- Swap any implementation by changing one line in `deps`. Tests inject fakes
  for the ports — no DB needed for usecase/service unit tests.

## Run modes + the Proposer seam

One `qopt-server` binary serves three modes, selected by `QOPT_MODE`; `deps`
picks the `Proposer` (ADR-0013):

| Mode | Proposer | Key | Who |
|---|---|---|---|
| `local` | `propose/cli` — drives the user's `claude`/`cursor-agent` headless | none (user's own login) | analyst with an agent |
| `hosted` | `propose/api` — calls the Anthropic API | server-held `QOPT_API_KEY` | public browser user |
| `verify` | none | — | verify-only (CLI-style) |

The DSN is always a **server secret**: request bodies carry only SQL.

## Data flow — verify (CLI, one-shot)

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

## Data flow — optimize (web, autonomous loop + SSE)

```
browser POST {sql} ─▶ handler/http /optimize ─▶ usecase.Optimize
                            ▲  SSE: progress per stage      │
                            │                               │ diagnose (EXPLAIN)
                            │                               ▼
                            │            ┌── proposer.Propose (cli|api) ──┐
                            │            │                                │
                            │   verification.Verify(old vs new)           │ retry ≤3 on a
                            │            │                                │ retryable rewrite
                            │            └── feed failed Verdict back ────┘ (ADR-0011)
                            │                               │
                            └─── final "result" event ◀─────┘
   (non-rewrite kind → baseline.Save; later /recheck diffs the applied change)
```

## The escalation ladder (why 4 verifier kinds)

`v2-migration` had one transform. This tool has many, unequal in how far
self-service goes:

| Kind | Self-serve? | Verifier behavior |
|---|---|---|
| `rewrite` | yes | **run old vs new, compare.** `verified` / `behavior_changed` / `not_faster`. |
| `index` | propose-only | `unverifiable` — prediction; prove via `Recheck` after a human applies it; HypoPG noted on Postgres. |
| `partition` | propose-only | `unverifiable` — DBA territory. |
| `shard` | escalate | `unverifiable` — detect, hand to data eng. |

This is why the **verifier kind** (`internal/service/verification`) is the
primary extension axis — see `EXTENDING.md`.

## What "verified" means

`repo/database` fingerprints each run: row count + sha256 of the **sorted**
first 200 rows. The rewrite verifier compares both + timing. Outcomes: see the
ladder table above. For a system change the tool cannot apply, `repo/baseline`
stores the "before" so `Recheck` can report a **measured** after.

> Sampling caveat: only the first 200 rows are fingerprinted. Result sets > 200
> rows with no `ORDER BY` can falsely mismatch — give compared queries a
> deterministic `ORDER BY`. Inherited from the Python design.
