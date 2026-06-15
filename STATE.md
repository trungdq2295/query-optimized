# Current state

Snapshot of what is built, proven, and stubbed — so you know what to trust and
what to finish before relying on it.

_Last updated: web + packaging arc (HTTP/SSE, proposers, Optimize/Recheck, seeder, launcher + Docker, 3-persona clone-and-run)._

> Correctness here is shown by **real end-to-end runs**, not unit tests
> (ADR-0017). New adapters ship without unit tests on purpose; the offline
> suite (`go test ./...`) still must stay green.

## Done + proven

| Piece | Package | State | Proof |
|---|---|---|---|
| Security guard (`AssertSafeSelect`) | `service/safety` | done | `safety_test.go` — accepts SELECT/WITH, rejects writes/DDL/multi-statement |
| Entities (`Proposal`/`Verdict`/`Progress`) | `domain` | done | imported by every layer; cross-layer contract |
| Port interfaces (the seams) | `port` | done | `QueryRunner`, `Verifier`, `VerificationService`, `Proposer`, `BaselineStore` |
| Engine registry + adapters | `repo/database` | mysql, sqlite **proven**; postgres/mssql/snowflake **written, untested live** | mysql via Docker demo; sqlite via test |
| `QueryRunner` (timed run + fingerprint) | `repo/database` | done | exercised by every verify |
| `rewrite` verifier | `service/verification` | done + **proven live** | sqlite 210×, live mysql 574× — `verified`, behavior_preserved; identical sample hash `acbd81aad5282e4c` across both engines |
| `index` verifier | `service/verification` | done (returns `unverifiable`) | demo: tool refused DDL; index applied by hand → recheck measured 46× |
| `partition` / `shard` verifiers | `service/verification` | done (return `unverifiable`) | unit-level only |
| Orchestration `Explain` / `Verify` | `usecase/optimize` | done | unit tests inject fakes — no DB |
| Orchestration `Optimize` (propose→verify→retry, `maxAttempts=3`) | `usecase/optimize` | done + **proven live** | sqlite local mode (claude): 210×; live mysql (claude): 574× (ADR-0011) |
| Orchestration `Recheck` (vs captured baseline) | `usecase/optimize` | done + **proven live** | index applied by hand → recheck reported 46× measured |
| Baseline store | `repo/baseline` | done | feeds Recheck |
| `cli` proposer (drives user's claude/cursor) | `service/propose/cli` | done + **proven live** | produced verified rewrites in local mode |
| `api` proposer (server-held Anthropic key) | `service/propose/api` | **written, NOT proven** | no full real-key run yet |
| CLI (`explain` / `verify`) | `handler/cli` | done | used for the whole live demo |
| HTTP adapter (`/optimize` SSE, `/recheck`, `/explain`, `/health`) | `handler/http` | done | `/optimize` streamed progress→result live; serves SPA via `QOPT_STATIC_DIR` (ADR-0012/0014) |
| Frontend (React + Vite SPA) | `frontend/` | done | builds; live STEPS, verified-badge metrics, recheck flow |
| Go seeder | `cmd/qopt-seed` | done | seeds sqlite + mysql demo, no client binary (ADR-0015) |
| Composition root (wiring + modes + proposer pick) | `deps` | done | `deps_integration_test.go` full stack on pure-Go SQLite |
| End-to-end test on SQLite | `deps` | done | preserve / change / write-smuggle cases through real wiring |
| 3-persona packaging | `run-local.sh`/`.command`, `Dockerfile`, `docker-compose.yml` | done | launcher built+seeded+served; Docker image built + container served hosted (ADR-0016) |

## Written but NOT exercised live

- **`api` proposer (hosted mode)** — code present; never run with a real
  `QOPT_API_KEY` end-to-end. Only the page-load path was checked. Verify before
  relying on hosted deploys.
- **postgres adapter** — code present, no driver imported in `cmd/`, no live
  run. To enable: import `pgx/v5/stdlib`, add `"postgres":"pgx"` to `deps`, run
  against a real PG.
- **mssql adapter** — `explainSQL` is a placeholder; real plan output needs
  `SET SHOWPLAN_ALL`. Diagnose won't show a plan until wired.
- **snowflake adapter** — code present, no driver, no run.

## Known limitations (by design, documented)

- **Sampling fingerprint** = first 200 rows only. Result sets > 200 rows with no
  `ORDER BY` can falsely mismatch. Mitigation: compare with a deterministic
  `ORDER BY` (ARCHITECTURE.md).
- **Index advice is a prediction** — never DDL-verified in-tool. Recheck proves
  it *after* a human applies it in a sandbox. HypoPG is the Postgres no-DDL path
  (ADR-0009).
- **Verification fidelity** — proof is only as good as the dataset. Demo DB ≠
  prod scale/skew/NULLs/collation; a rewrite verified here can still differ at
  prod scale.
- **Keyword denylist guard** can over-reject (fail safe) — intentional (ADR-0003).

## Repo / process

- Remote: github.com/trungdq2295/query-optimized, default branch `master`.
- **`master` is PR-only** — no direct pushes (`enforce_admins` on, 0 required
  approvals). Branch + PR for every change.

## Not built (future, only if required)

- Hosted-mode end-to-end proof with a real API key.
- HypoPG hypothetical-index verify for Postgres.
- Slack / other autonomous frontends.
- `partition`/`clustering` verification beyond "propose-only".
- Tolerant (`<1%`) rewrite verdict (ADR-0007 says how to add it).
- CI status check wired into branch protection.

## How to re-verify this state

```bash
cd backend && go vet ./... && go build ./... && go test ./...   # offline, must be green
```
For the live runs, see README.md (the three run cases).
