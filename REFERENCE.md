# Query Optimizing — Reference

The optimization rule catalog. Each rule: the smell, the fix, why it helps, and
when **not** to apply it. Match query findings to these before advising.

Engine-specific notes are tagged `[PG]` Postgres, `[MY]` MySQL, `[MS]` MSSQL,
`[SF]` Snowflake, `[BQ]` BigQuery. Untagged = applies broadly.

---

## A. Reading the EXPLAIN first

If an `EXPLAIN` / `EXPLAIN ANALYZE` is available, start there — it tells you the
real cost, not the guessed cost. Look for, in priority order:

1. **Full / sequential scan on a big table** → missing or unused index (Rule B1).
2. **Nested loop over many rows** → bad join, missing index on join key (B2).
3. **Sort / hash spilling to disk** → memory too small or avoidable sort (C3).
4. **Rows estimate wildly off actual** → stale stats; suggest `ANALYZE` / update
   statistics before anything else.
5. **Index present but not used** → non-sargable predicate (C1) or type mismatch
   (C2) defeating the index.

How to read which op: `[PG]` `EXPLAIN (ANALYZE, BUFFERS)`; `[MY]` `EXPLAIN ANALYZE`
or `EXPLAIN FORMAT=JSON`; `[MS]` actual execution plan / `SET STATISTICS IO ON`;
`[SF]` query profile in UI / `SYSTEM$EXPLAIN_PLAN_JSON`; `[BQ]` query plan in
job stats.

---

## B. Index rules (the main self-serve win)

### B1. Full scan on a filtered table → index the filter columns
Smell: `WHERE col = ?` (or range) on a large table, EXPLAIN shows full scan.
Fix: index on the filter column(s). Order matters — **equality columns first,
then range, then sort** (the "ESR" rule).
Example: `WHERE status = 'x' AND created_at > ?` → `(status, created_at)`.
Why: index seek replaces scanning every row.
Don't: index a tiny table, or a column with ~2 distinct values (low cardinality
— scan is cheaper). `[SF][BQ]` no manual indexes — use clustering key instead.

### B2. Join on an unindexed key → index the join column
Smell: join `a.id = b.a_id`, EXPLAIN shows nested loop / scan on `b`.
Fix: index the foreign-key side (`b.a_id`). PKs are usually already indexed; FKs
often are not.
Why: turns O(n·m) nested loop into index lookups.

### B3. Composite index column order
Rule: **Equality → Sort → Range** (ESR). Put `=` predicates leftmost, then
`ORDER BY` columns, then range (`>`, `<`, `BETWEEN`) last.
Why: a range column "stops" the index from being usable for columns after it.

### B4. Covering index (index-only scan)
Smell: query selects a few columns, all from one table, on a hot path.
Fix: include the selected columns in the index so the engine never touches the
table. `[PG]` `INCLUDE (...)`; `[MS]` `INCLUDE (...)`; `[MY]` add cols to the
composite (InnoDB clusters on PK).
Don't: over-include — wide indexes cost write throughput and storage.

### B5. Redundant / duplicate index
Smell: existing index `(a)` when `(a, b)` already exists.
Fix: drop the redundant one (a prefix of another). Note it, let DBA decide.
Why: every index slows writes; duplicates give no read benefit.

---

## C. Query rewrite rules (behavior-preserving — mind the gate)

### C1. Non-sargable predicate → make it sargable
Smell: function or math on the indexed column kills the index.
- `WHERE YEAR(created_at) = 2024` → `WHERE created_at >= '2024-01-01' AND created_at < '2025-01-01'`
- `WHERE UPPER(name) = 'X'` → store/compare consistently, or use an expression index `[PG]`.
- `WHERE col + 0 = 5` → `WHERE col = 5`.
Why: the optimizer can only use an index on the bare column.

### C2. Implicit type cast on join/filter → fix the type
Smell: `WHERE varchar_col = 123` or join `int_col = varchar_col`.
Fix: compare same types (quote the literal, or cast the constant not the column).
Why: a cast on the column side defeats the index, same as C1.

### C3. Avoidable sort
Smell: `ORDER BY` that matches no index; `DISTINCT` or `GROUP BY` forcing a sort.
Fix: index to provide the order (B3), or drop the sort if the caller doesn't need
it. `SELECT DISTINCT` is often masking a join that fans out — fix the join
instead (gate: changing this changes row count → ask).

### C4. SELECT * → explicit columns
Smell: `SELECT *` when few columns are used.
Fix: list only needed columns.
Why: less I/O, enables covering indexes (B4), avoids breaking on schema change.
Note: behavior-preserving only if downstream truly used a subset — confirm.

### C5. OR across columns → UNION ALL or IN
Smell: `WHERE a = 1 OR b = 2` — often can't use either index well.
Fix: `... WHERE a = 1 UNION ALL ... WHERE b = 2 AND a <> 1` (mind dedup), or `IN`
for same-column ORs (`a = 1 OR a = 2` → `a IN (1,2)`).
Gate: `UNION` vs `UNION ALL` changes dedup behavior — ask.

### C6. Correlated subquery → join or window
Smell: subquery in SELECT/WHERE that runs per row.
Fix: rewrite as a JOIN, or a window function (`ROW_NUMBER() OVER (...)`).
Why: one set-based pass instead of N executions.

### C7. Leading-wildcard LIKE
Smell: `WHERE name LIKE '%foo%'`.
Fix: can't use a normal B-tree index. Options: trailing-only `'foo%'` if intent
allows; full-text index `[PG]` `pg_trgm` / `[MY]` FULLTEXT / `[MS]` full-text.
Gate: changing match semantics → ask.

### C8. Pagination with large OFFSET
Smell: `LIMIT 20 OFFSET 100000`.
Fix: keyset pagination — `WHERE id > <last_seen> ORDER BY id LIMIT 20`.
Why: OFFSET still scans+discards all skipped rows.

### C9. Over-broad date / range scan
Smell: querying years of data when only recent needed; no date bound.
Fix: add the tightest correct filter. Pairs with partition pruning (D1).

### C10. Implicit cross join / missing join predicate
Smell: tables in FROM with no/weak join condition → row explosion.
Fix: add the correct join key. (This is correctness as much as speed.)

---

## D. Partition rules (propose-only — DBA approves)

### D1. Range/date partition for time-series
Signal: huge table, queries almost always filter on a date/time column, table
grows forever.
Propose: RANGE partition on the date column → engine prunes to relevant
partitions. `[PG]` declarative partitioning; `[MY]` `PARTITION BY RANGE`;
`[MS]` partition function/scheme; `[SF]` auto micro-partitions + clustering key
on the date col; `[BQ]` partition by date/ingestion-time column.
Caveat: partitioning is a big, often one-time DDL with rewrite cost — **always**
propose, never claim it's free. DBA decides.

### D2. List partition for low-cardinality category
Signal: queries filter on a small fixed set (region, tenant) and data is large.
Propose: LIST partition on that column. Same DBA-approval caveat.

### D3. Don't partition small tables
If the table is small or queries don't filter on the partition key, partitioning
adds overhead with no pruning benefit. Say no.

---

## E. Sharding — DETECT AND ESCALATE (never solve here)

Sharding is an architecture decision, not a self-serve fix. This skill only
**flags** it and hands off to a data engineer / DBA.

Signals that sharding *might* be needed (any of these = escalate, don't design):
- Single table/DB beyond what one node handles even with indexes + partitions.
- Write throughput saturating one primary.
- Hot-key access pattern that partitioning can't relieve.

Output when detected:
> This looks like it's hitting single-node limits — indexing/partitioning won't
> be enough. Sharding is an architecture change; escalate to the data
> engineering team rather than handling it as a query fix.

Do **not** propose a shard key, topology, or routing here.

---

## F. Stats & maintenance (cheap wins, check early)

- **Stale statistics** → optimizer picks bad plans. `[PG]` `ANALYZE`; `[MY]`
  `ANALYZE TABLE`; `[MS]` `UPDATE STATISTICS`. Suggest when EXPLAIN estimates are
  far from actual rows.
- **Index bloat / fragmentation** → `[PG]` `REINDEX`; `[MS]` rebuild/reorganize.
  Note for DBA; not a query change.

---

## G. Engine quick-reference

| Engine | Manual indexes? | Partition mechanism | Speedup lever when no index |
|---|---|---|---|
| Postgres `[PG]` | Yes (B-tree, GIN, GiST, expression, partial, INCLUDE) | Declarative range/list/hash | partial/expression index, `ANALYZE` |
| MySQL/InnoDB `[MY]` | Yes (clustered PK, secondary, FULLTEXT) | `PARTITION BY RANGE/LIST/HASH` | composite index, covering via PK |
| MSSQL `[MS]` | Yes (clustered, nonclustered, INCLUDE, filtered) | partition function + scheme | filtered index, `UPDATE STATISTICS` |
| Snowflake `[SF]` | **No** | auto micro-partitions | **clustering key**, prune via filters, rewrite |
| BigQuery `[BQ]` | **No** | partition + cluster columns | partition pruning, cluster cols, less scanned data |

`[SF][BQ]`: never recommend `CREATE INDEX`. Redirect to clustering keys and
reducing bytes scanned (column pruning C4, date bounds C9, partition pruning).
