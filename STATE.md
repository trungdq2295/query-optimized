# Current state

Snapshot of what is built, tested, and stubbed — so you know what to trust and
what to finish before relying on it.

_Last verified: clean-architecture refactor (MySQL live re-run + offline test suite green)._

## Done + proven

| Piece | Package | State | Proof |
|---|---|---|---|
| Security guard (`AssertSafeSelect`) | `service/safety` | done | `safety_test.go` — accepts SELECT/WITH, rejects writes/DDL/multi-statement |
| Entities (`Proposal`/`Verdict`) | `domain` | done | imported by every layer; cross-layer contract |
| Port interfaces (the seams) | `port` | done | implemented by service/repo, consumed by usecase |
| Engine registry + adapters | `repo/database` | mysql, sqlite **proven**; postgres/mssql/snowflake **written, untested live** | mysql via Docker demo; sqlite via test |
| `QueryRunner` (timed run + fingerprint) | `repo/database` | done | exercised by every verify |
| `rewrite` verifier | `service/verification` | done + **proven live** | MySQL re-run: `verified`, behavior_preserved, identical sample hash `e51b91f19b48117e` |
| `index` verifier | `service/verification` | done (returns `unverifiable`) | demo: tool refused DDL; index applied by hand dropped EXPLAIN cost 30171 → 7 |
| `partition` / `shard` verifiers | `service/verification` | done (return `unverifiable`) | unit-level only |
| Orchestration (`Explain`/`Verify`) | `usecase/optimize` | done | unit tests inject fakes — no DB |
| CLI (`explain` / `verify`) | `handler/cli` | done | used for the whole live demo |
| Composition root (wiring) | `deps` | done | `deps_integration_test.go` — full stack on pure-Go SQLite |
| End-to-end test on SQLite | `deps` | done | preserve / change / write-smuggle cases through real wiring |

## Written but NOT exercised live

- **postgres adapter** (`repo/database/engine.go`) — code present, no driver
  imported in `cmd/qopt`, no live run. To enable: import `pgx/v5/stdlib` in
  `cmd/qopt`, add `"postgres":"pgx"` to `engineToDriver` in `deps`, run against a
  real PG.
- **mssql adapter** — `explainSQL` is a placeholder (returns the body; real
  MSSQL plan output needs `SET SHOWPLAN_ALL` toggling). Diagnose will not show a
  plan until that's wired.
- **snowflake adapter** — code present, no driver, no run.

## Known limitations (by design, documented)

- **Sampling fingerprint** = first 200 rows only. Result sets > 200 rows with no
  `ORDER BY` can falsely mismatch. Mitigation: compare queries with a
  deterministic `ORDER BY` (ARCHITECTURE.md). Inherited from the Python design.
- **No code retry loop** — the agent-in-the-loop retries; an autonomous frontend
  must add its own (ADR-0008).
- **Index advice is a prediction** — never DDL-verified in-tool. HypoPG noted as
  the Postgres path to confirm without DDL (ADR-0009).
- **Keyword denylist guard** can over-reject (fail safe) — intentional (ADR-0003).

## Not built (future, only if required)

- HypoPG hypothetical-index verify for Postgres.
- Slack/web frontend (+ retry loop for the autonomous case).
- `partition`/`clustering` verification beyond "propose-only".
- Tolerant (`<1%`) rewrite verdict (ADR-0007 says how to add it).

## How to re-verify this state

```bash
go vet ./... && go build ./... && go test ./...   # offline, must be green
```
For the live MySQL demo, see README.md "Live demo".
