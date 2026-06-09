---
name: query-optimizing
description: Self-service SQL query optimizer for data analysts. Connects to the database (read-only), runs EXPLAIN itself, returns a rewritten query, an index recommendation, and (when relevant) a partition/shard flag — and verifies the rewrite is both faster and behavior-preserving. No support engineer needed. Use when someone says "this query is slow", "optimize this SQL", "why is this query timing out", or asks how to speed up a SELECT.
version: 0.2.0
allowed-tools: [Read, Bash, Grep, Glob]
---

# Query Optimizing

Self-service SQL optimizer. Built so a data analyst (DA) can fix a slow query
themselves instead of pinging a support engineer. The DA writes good SQL but
does **not** know physical design (indexes, partitioning, sharding) — this skill
fills that gap.

This is a **connect-diagnose-verify** tool. The DA hands it a read-only
connection string; the skill connects, runs `EXPLAIN` itself, rewrites the
query, and **proves** the rewrite is faster and returns the same data — then
hands back the result. The DA pastes a connection string and gets an answer; no
manual `EXPLAIN`, no support engineer.

It **never runs DDL** and **never writes** — every statement sent to the DB is a
single read-only `SELECT`, the session is set read-only, and a statement timeout
guards against a runaway query. The connection string is never logged or
committed.

> Optimization rules live in **[`REFERENCE.md`](REFERENCE.md)** — the full
> catalog of rewrite patterns, index heuristics, and partition/shard signals.
> The engine lives in **[`tool.py`](tool.py)** — `connect`, `explain`, `verify`.
> Read REFERENCE.md before giving advice.

## Scope

| In scope | Out of scope |
|---|---|
| Connect read-only + run `EXPLAIN` itself | Running any write / DDL |
| Rewrite the query (sargable predicates, join order, kill `SELECT *`, fix `OR`-chains, subquery→join) | Creating the index / running any DDL |
| **Verify** the rewrite: faster + same results (row count + sample) | Auto-applying anything to the DB |
| Recommend an index (which columns, what order, covering or not) | Designing a sharding topology end-to-end |
| Suggest a partition key (range/date) — **propose, DBA approves** | Guaranteeing index speedup without creating the index |
| Detect when sharding is needed and **flag it** | |
| Explain *why* the query is slow in plain terms | |

The escalation ladder is deliberate: **index = the main self-serve win**,
partition = propose-only, **sharding = detect and hand off to an engineer/DBA**,
never solve it inside this skill.

## Workflow

### 1. Intake — ask up front (don't guess)

Need these before advising. Ask in one message for whatever is missing:

1. **The query** — raw SQL pasted, or a path to a `.sql` file.
2. **Connection string** — a **read-only** DB account. The skill connects, runs
   `EXPLAIN`, and verifies. SQLAlchemy URL form, e.g.
   `postgresql+psycopg2://user:pass@host/db`. The engine name comes from the URL
   — no need to ask separately. If the DA can't give one, fall back to offline
   mode (paste schema + `EXPLAIN` output; see "Offline fallback" below).
3. **How slow + target** — "takes 5 min, need under 30s" (optional, helps decide
   whether a full timed `verify` is worth the wait).

**Connection-string handling (non-negotiable):**
- Insist on a **read-only** account. The tool refuses non-`SELECT` anyway, but
  the account should be read-only as defense in depth.
- Never echo it back, never write it to the session folder, never commit it.
- Refuse any admin / superuser string if the DA offers one — ask for read-only.

### 2. Connect + diagnose (the tool does this, not the DA)

```python
import tool
eng = tool.connect("<read-only connection string>")

# cheap: plan only, no execution
plan = tool.explain(eng, slow_sql)            # full scan? nested loop? sort spill?
```

1. `tool.explain(..., analyze=False)` is **cheap** — it only plans the query, no
   execution. Start here. Read the plan against [`REFERENCE.md`](REFERENCE.md) §A:
   full scan, nested loop on big tables, sort spill, stale-stats row-estimate skew.
2. Match each smell to a rule in `REFERENCE.md`. Note the rule per finding.
3. Identify the filter / join / sort columns — these are the index candidates.
4. `tool.explain(..., analyze=True)` **executes** the query for real timing. On a
   multi-minute query this is slow — only run it after telling the DA, and rely on
   the statement timeout.

### 3. Stop-and-ask gate (non-negotiable)

A rewrite must be **behavior-preserving**. If a proposed change could alter the
result set — NULL handling, duplicate rows, ordering, `DISTINCT` semantics,
`LEFT` vs `INNER` join meaning — **stop, show the DA, ask** before presenting it
as the answer. Speeding up a query by quietly changing its results is worse than
a slow query.

### 4. Verify (prove it, don't claim it)

For **rewrites**, run the verification before presenting the answer:

```python
v = tool.verify(eng, slow_sql, rewritten_sql)   # WARNING: runs both queries
# v.behavior_preserved, v.speedup, v.old/new.elapsed_s, v.notes
```

- `verify` runs old vs new and checks **row count + sampled content match**
  (behavior-preserving) and **timing** (faster). Same quantity/quality idea as
  v2-migration-tool's validation.
- It **executes the slow query in full** — expensive on a multi-minute query.
  Warn the DA, lean on the statement timeout, and consider verifying on a bounded
  subset if the full run is impractical.
- If `v.behavior_preserved` is False → the rewrite is wrong. Do **not** present
  it. Fix or drop it.

For **index** recommendations, you cannot verify without creating the index
(DDL — out of scope). Report the plan-cost reasoning and say it's a prediction.
Where the engine supports hypothetical indexes (`[PG]` HypoPG), note that as the
way to confirm without DDL.

### 5. Output

Return a single structured report:

```
## Why it's slow
<plain-language root cause, 1-3 bullets. Cite the EXPLAIN op if available.>

## Rewritten query
<the optimized SQL, or "no rewrite needed" — only if behavior-preserving>
<for each change: one line on what changed + why>

## Verified
<from tool.verify — only for the rewrite>
- behavior-preserving: <yes/no — row count + sample match>
- old: <Xs>  →  new: <Ys>   (<N>x faster)
<if not behavior-preserving: rewrite withheld, say why>

## Index recommendation
<CREATE INDEX ... statement(s), or "no new index needed">
<why these columns, in this order; covering or not>
-- Prediction from plan cost — not verified (verifying needs the index to exist).
-- DA action: ask your DBA to apply this; do not run DDL yourself.

## Partition / shard (only if relevant)
<"consider RANGE partition on <date col> — discuss with DBA", or
 "this needs sharding — escalate to the data engineering team", or omit>

## Assumptions
<anything assumed — e.g. "index speedup is a prediction, not verified">
```

Keep output copy-pasteable. The DA takes the index/partition lines to their DBA;
they apply the verified rewrite themselves.

## Offline fallback (no connection string)

If the DA can't give a read-only connection string, the skill still works from
text: ask them to paste the query, table schema, existing indexes, and an
`EXPLAIN` output. Diagnosis + advice proceed as normal, but the **Verified**
section becomes "not verified — no DB access; advice is from query shape". Be
explicit about the downgrade.

## Guardrails

- **Read-only, SELECT-only.** `tool.assert_safe_select` refuses any non-`SELECT`
  / multi-statement input; the session is set read-only. Never bypass it.
- **Never run DDL.** `CREATE INDEX` / partition DDL is text output only — the DA
  or DBA applies it. The tool has no write path by design.
- **Behavior-preserving only.** Present a rewrite only if `tool.verify` returns
  `behavior_preserved=True`, or (offline) after an explicit stop-and-ask. A
  faster query that returns different rows is a bug, not a fix.
- **Connection string is a secret.** Never echo, log, write to the session
  folder, or commit it. Refuse admin/superuser strings — ask for read-only.
- **`EXPLAIN ANALYZE` / `verify` execute the query.** Warn before running on a
  slow query; rely on the statement timeout; verify on a subset if needed.
- **No invented columns/tables.** Only reference what's in the query or schema.
- **Engine-honest.** No B-tree index advice on Snowflake/BigQuery — pivot to
  clustering / partition pruning / rewrite (REFERENCE.md §G).
- **Sharding is not self-serve.** Detect and escalate; never design it here.

## Extending later (when requirements firm up)

- **Specific engine** → tighten `REFERENCE.md` to that dialect once known.
- **Hypothetical-index verify** → wire `[PG]` HypoPG so index advice can be
  confirmed without DDL.
- **Slack bot / web / their own AI** → keep this skill + `tool.py` as the core
  engine and put a thin adapter in front; the workflow and rules don't change.
