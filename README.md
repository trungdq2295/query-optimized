# query-optimizer (`qopt`)

A **connect-diagnose-verify** SQL optimizer. The AI proposes an optimization
(rewrite / index / partition / shard); this Go binary **proves** a rewrite is
both faster and behavior-preserving against a live database before any advice is
trusted. "Generate fuzzy, verify hard."

> New here? Read in this order: `ARCHITECTURE.md` (how it's shaped) →
> `DECISIONS.md` (why) → `EXTENDING.md` (how to add to it safely) →
> `STATE.md` (what's done vs stub).

## Layout (clean architecture — dependencies point inward)

```
internal/domain                  Proposal, Verdict, OptKind — entities; import nothing
internal/port                    interfaces (the seams): SafetyValidator, QueryRunner, Verifier, VerificationService
internal/service/safety          AssertSafeSelect — SELECT-only security boundary
internal/service/verification    per-kind verifier strategies + dispatcher (the growth axis)
internal/repo/database           QueryRunner over database/sql + per-engine adapters
internal/usecase/optimize        orchestration: Explain, Verify (no SQL, no rules)
internal/handler/cli             delivery: CLI args -> use case -> printed result
internal/deps                    composition root: builds concretes, injects into ports
cmd/qopt                         main — the ONLY place DB drivers are imported
```

See `ARCHITECTURE.md` for the layer table + dependency rule.

## Build & test

```bash
go vet ./...
go build -o qopt ./cmd/qopt
go test ./...        # offline — pure-Go SQLite, no network, no DB server
```

Inner layers use only the standard library (`database/sql`); concrete DB drivers
are blank-imported in `cmd/qopt` only.

## Use

`explain` — diagnose (cheap; `-analyze` executes for real timing):
```bash
qopt explain -engine mysql -dsn "$QOPT_DSN" -sql "SELECT ..."
```

`verify` — prove a Proposal (reads JSON on stdin, writes a Verdict):
```bash
echo '{
  "kind": "rewrite",
  "original_sql": "SELECT ... ORDER BY id",
  "rewritten_sql": "SELECT ... ORDER BY id",
  "rationale": "subquery -> join",
  "self_serve": true
}' | qopt verify -engine mysql -dsn "$QOPT_DSN"
```

The connection string (`-dsn` or `QOPT_DSN`) is a **secret** — never logged,
never written, never committed. Use a **read-only** account (defense in depth;
the guard refuses non-SELECT regardless).

### Verdict statuses
- `verified` — faster **and** same results. Safe to present.
- `behavior_changed` — different rows. Rewrite rejected.
- `not_faster` — same rows, no speedup.
- `unverifiable` — can't prove without DDL (index/partition/shard) or without a DB.

## Live demo (Docker MySQL)

Reproduces the proof in `STATE.md`.

```bash
# 1. throwaway MySQL
docker run -d --name qopt-mysql -e MYSQL_ROOT_PASSWORD=root \
  -e MYSQL_DATABASE=shop -p 13306:3306 mysql:8
# wait until ready:
until docker exec qopt-mysql mysqladmin ping -uroot -proot --silent 2>/dev/null | grep -q alive; do sleep 1; done

# 2. seed 5k customers + 300k orders (unindexed join column) — see the seed SQL
#    in the build transcript / ARCHITECTURE notes.

export QOPT_DSN='root:root@tcp(127.0.0.1:13306)/shop'

# 3. diagnose a correlated-subquery query -> tool shows the table scan
qopt explain -engine mysql -sql "SELECT c.id, c.name, (SELECT COUNT(*) FROM orders o WHERE o.cust_id=c.id AND o.status='open') open_cnt FROM customers c WHERE c.region='us' ORDER BY c.id"

# 4. verify the subquery->join rewrite (got: 503x faster, behavior_preserved)
echo '{"kind":"rewrite","original_sql":"...ORDER BY c.id","rewritten_sql":"...LEFT JOIN...GROUP BY...ORDER BY c.id","self_serve":true}' | qopt verify -engine mysql

# 5. index advice is `unverifiable` (tool never runs DDL); apply it by hand in
#    the sandbox and re-EXPLAIN to confirm the cost drop.

# cleanup
docker rm -f qopt-mysql
```

## Relationship to the skill

This is the Go re-implementation of the verify engine described in
`skills/query-optimizing/` (Python `tool.py`, `REFERENCE.md`). The **rules** the
AI reads to propose optimizations still live in that skill's `REFERENCE.md`;
this binary is the deterministic **verify** half.
