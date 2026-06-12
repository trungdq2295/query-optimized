---
name: query-optimizing
description: Self-service SQL query optimizer for data analysts. Diagnoses a slow query (runs EXPLAIN), proposes a rewrite and an index/partition recommendation, and PROVES the rewrite is both faster and behavior-preserving against a live database via the deterministic `qopt` verify engine. Use when someone says "this query is slow", "optimize this SQL", "why is this query timing out", or asks how to speed up a SELECT.
version: 1.0.0
allowed-tools: [Read, Bash, Grep, Glob]
---

# Query Optimizing

Self-service SQL optimizer. A data analyst (DA) writes good SQL but does **not**
know physical design (indexes, partitioning, sharding). This skill fills that gap
and — critically — **proves** its rewrites instead of guessing.

This is a **connect-diagnose-verify** tool. You (the AI) are the *proposer*: you
read the EXPLAIN plan + the rule catalog and propose an optimization. The
deterministic Go binary **`qopt`** is the *verifier*: it runs the old vs new
query against a live DB and decides `verified` / `behavior_changed` / `not_faster`.
A faster query that returns different rows is a bug — `qopt` catches that. "Generate
fuzzy, verify hard."

> **Two files, two jobs.**
> - **[`REFERENCE.md`](REFERENCE.md)** — the rule catalog (rewrite patterns, index
>   heuristics, partition/shard signals). Read it before advising.
> - **`qopt`** — the verify engine (Go binary). You drive it; you never reinvent it.
>   Build once: `cd backend && go build -o qopt ./cmd/qopt`.

## The contract you produce: a `Proposal` (JSON)

`qopt verify` reads a JSON `Proposal` on **stdin** and writes a JSON `Verdict` to
**stdout**. You construct the Proposal:

```json
{
  "kind": "rewrite",            // rewrite | index | partition | shard
  "original_sql": "SELECT ... ORDER BY id",
  "rewritten_sql": "SELECT ... ORDER BY id",   // kind=rewrite only
  "ddl": "CREATE INDEX ...",                    // kind=index/partition; TEXT, never executed
  "rationale": "correlated subquery -> join",   // plain-language why
  "self_serve": true            // rewrite=true; index/partition/shard=false
}
```

`Verdict` statuses:
- `verified` — faster **and** same results. Safe to present.
- `behavior_changed` — different rows. Reject the rewrite.
- `not_faster` — same rows, no speedup.
- `unverifiable` — index/partition/shard (system change) or no DB. Not proven here.

## The escalation ladder (what you may self-serve vs escalate)

| Kind | What you do |
|---|---|
| `rewrite` | **Self-serve.** Propose + `qopt verify` proves it live. Present only if `verified`. |
| `index` | **Escalate.** Output the `CREATE INDEX` DDL, tell the DA to have an engineer/DBA apply it. The tool **never runs DDL**. Then **verify after applied** (below). |
| `partition` | **Escalate.** Propose the partition key; engineer applies; verify after. |
| `shard` | **Detect and hand off.** Sharding is an architecture decision — flag it for data engineering, never design it here. |

**Never predict a speedup for a system change.** For index/partition, do not invent
a cost number. Recommend the DDL, escalate it to an engineer, and prove the effect
*after* it is applied (see step 5).

## Workflow

### 1. Intake — ask up front (don't guess)
Need before advising; ask in one message for whatever is missing:
1. **The query** — raw SQL or a path to a `.sql` file.
2. **Engine + connection** — engine name (`mysql`/`postgres`/`sqlite`/...) and a
   **read-only** DSN, passed to `qopt` as `-engine` + `-dsn` (or env `QOPT_DSN`).
   If the DA can't give one → offline fallback (paste schema + EXPLAIN).
3. **How slow + target** — optional, e.g. "5 min, need < 30s".

**Connection-string handling (non-negotiable):**
- Insist on a **read-only** account. `qopt` refuses non-SELECT regardless (defense
  in depth).
- Never echo it back, never write it to disk, never commit it. Refuse admin/superuser.

### 2. Diagnose (run EXPLAIN via qopt)
```bash
# cheap: plan only, no execution
qopt explain -engine mysql -dsn "$QOPT_DSN" -sql "<slow SQL>"
# real timing (EXECUTES — warn first on a slow query):
qopt explain -engine mysql -dsn "$QOPT_DSN" -sql "<slow SQL>" -analyze
```
Read the plan against [`REFERENCE.md`](REFERENCE.md) §A: full scan, nested loop,
sort spill, stale-stats row-estimate skew. Match each smell to a rule; note the
rule per finding. The filter/join/sort columns are your index candidates.

### 3. Stop-and-ask gate (non-negotiable)
A rewrite must be **behavior-preserving**. If a change could alter the result set
— NULL handling, duplicate rows, ordering, `DISTINCT`, `LEFT` vs `INNER` — **stop,
show the DA, ask** before presenting it. Speeding up by quietly changing results is
worse than a slow query.

### 4. Propose + verify the rewrite (prove it)
Build a `kind:"rewrite"` Proposal and pipe it to `qopt verify`:
```bash
echo '{"kind":"rewrite","original_sql":"<old ORDER BY id>","rewritten_sql":"<new ORDER BY id>","rationale":"subquery->join","self_serve":true}' \
  | qopt verify -engine mysql -dsn "$QOPT_DSN"
```
- `qopt` runs both, compares **row count + sample hash** (behavior) and **timing**.
- Present the rewrite **only if** the Verdict is `verified`. On `behavior_changed`,
  fix or drop it. On `not_faster`, say so honestly.
- Give compared queries a deterministic `ORDER BY` (only the first 200 rows are
  fingerprinted — without an order, large result sets can falsely mismatch).

### 5. Index / partition → escalate, then verify-after-applied
- Emit the `CREATE INDEX ...` / partition DDL as **text**. Tell the DA: *"qopt never
  changes your schema — have your engineer/DBA apply this."*
- **Capture a baseline now**: run `qopt explain` on the original query and record the
  cost (the "before").
- **After** the engineer applies the change, re-run `qopt explain` on the same query
  and compare to the baseline → report the **measured** before/after (e.g. cost
  30171 → 7, scan → index seek). This is a real verification, not a prediction.

### 6. Output — single structured report
```
## Why it's slow
<plain-language root cause, 1-3 bullets; cite the EXPLAIN op>

## Rewritten query              [VERIFIED | withheld]
<optimized SQL, or "no rewrite needed">
- old <Xs> -> new <Ys>  (<N>x faster), rows match   <-- from qopt Verdict
<if behavior_changed: rewrite withheld, say why>

## Index / partition recommendation  (NEEDS ENGINEER)
<CREATE INDEX ... DDL, or "no new index needed">
-- qopt never runs DDL. Have your engineer/DBA apply it, then re-run to verify.
-- baseline cost captured: <N>

## Shard (only if relevant)
<"single-node limits — escalate to data engineering", or omit>

## Assumptions
<anything assumed>
```

## Offline fallback (no DB)
If the DA can't give a DSN, work from text: ask for the query, schema, existing
indexes, and an EXPLAIN dump. Diagnose + advise as normal, but the **Verified**
section becomes "not verified — no DB access; advice is from query shape." Be
explicit about the downgrade.

## Guardrails
- **Read-only, SELECT-only.** `qopt`'s safety guard refuses any non-SELECT /
  multi-statement input; the session is set read-only. Never try to bypass it.
- **Never run DDL.** Index/partition DDL is text output only — the engineer/DBA
  applies it. `qopt` has no write path by design.
- **Behavior-preserving only.** Present a rewrite only on a `verified` Verdict, or
  (offline) after an explicit stop-and-ask.
- **Never predict a system-change speedup.** Escalate, then measure after apply.
- **Connection string is a secret.** Never echo, log, write, or commit it. Refuse
  admin/superuser strings.
- **`-analyze` / `verify` execute the query.** Warn before running on a slow query.
- **No invented columns/tables.** Only what's in the query or schema.
- **Engine-honest.** No B-tree index advice on Snowflake/BigQuery — pivot to
  clustering / partition pruning / rewrite (REFERENCE.md §G).
- **Sharding is not self-serve.** Detect and escalate; never design it here.
