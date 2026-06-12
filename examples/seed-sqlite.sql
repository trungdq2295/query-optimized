-- Seed dataset for the query-optimizer demo (SQLite / modernc.org/sqlite).
-- Two tables, no helpful indexes on the hot columns — that is deliberate: the
-- whole point is that the optimizer's proposals produce a MEASURABLE speedup.
--
--   customers : 5,000 rows
--   orders    : 300,000 rows, orders.customer_id is UNINDEXED
--
-- Build a demo DB:
--   sqlite3 /tmp/qopt-demo.db < examples/seed-sqlite.sql
-- (any sqlite3 works; the app itself uses the pure-Go modernc driver)
--
-- Then see examples/demo-queries.sql for the slow query + the two cases the
-- tool proves: a self-serve REWRITE and an escalated INDEX (verify-after).

PRAGMA journal_mode = MEMORY;
PRAGMA synchronous = OFF;

DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS customers;

CREATE TABLE customers (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    country    TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE orders (
    id          INTEGER PRIMARY KEY,
    customer_id INTEGER NOT NULL,  -- intentionally UNINDEXED
    status      TEXT NOT NULL,
    amount      REAL NOT NULL,
    created_at  TEXT NOT NULL
);

-- 5,000 customers across 4 countries.
WITH RECURSIVE seq(n) AS (
    SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < 5000
)
INSERT INTO customers (id, name, country, created_at)
SELECT n,
       'cust_' || n,
       CASE n % 4 WHEN 0 THEN 'US' WHEN 1 THEN 'CA' WHEN 2 THEN 'UK' ELSE 'DE' END,
       date('2020-01-01', '+' || (n % 1500) || ' days')
FROM seq;

-- 300,000 orders, spread over the 5,000 customers and 3 statuses.
WITH RECURSIVE seq(n) AS (
    SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < 300000
)
INSERT INTO orders (id, customer_id, status, amount, created_at)
SELECT n,
       (n % 5000) + 1,
       CASE n % 3 WHEN 0 THEN 'shipped' WHEN 1 THEN 'pending' ELSE 'cancelled' END,
       ((n % 500) + 1) + 0.99,
       date('2024-01-01', '+' || (n % 365) || ' days')
FROM seq;

ANALYZE;
