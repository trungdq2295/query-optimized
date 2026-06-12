-- Seed dataset for the query-optimizer demo (MySQL 8 / go-sql-driver).
-- Mirrors examples/seed-sqlite.sql: same shapes, same intentionally-missing
-- index on orders.customer_id, so proposals produce a MEASURABLE speedup.
--
--   customers : 5,000 rows
--   orders    : 300,000 rows, orders.customer_id is UNINDEXED
--
-- Build the hosted demo DB (Docker MySQL on localhost:3306):
--   mysql -h127.0.0.1 -uroot -p qopt_demo < examples/seed-mysql.sql
--
-- The recursive CTE needs a raised depth limit (default is 1,000).
SET SESSION cte_max_recursion_depth = 1000000;

DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS customers;

CREATE TABLE customers (
    id         INT PRIMARY KEY,
    name       VARCHAR(64) NOT NULL,
    country    VARCHAR(8)  NOT NULL,
    created_at DATE        NOT NULL
) ENGINE = InnoDB;

CREATE TABLE orders (
    id          INT PRIMARY KEY,
    customer_id INT          NOT NULL,  -- intentionally UNINDEXED
    status      VARCHAR(16)  NOT NULL,
    amount      DECIMAL(10,2) NOT NULL,
    created_at  DATE         NOT NULL
) ENGINE = InnoDB;

-- 5,000 customers across 4 countries.
INSERT INTO customers (id, name, country, created_at)
WITH RECURSIVE seq(n) AS (
    SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < 5000
)
SELECT n,
       CONCAT('cust_', n),
       ELT((n % 4) + 1, 'US', 'CA', 'UK', 'DE'),
       DATE_ADD('2020-01-01', INTERVAL (n % 1500) DAY)
FROM seq;

-- 300,000 orders across the 5,000 customers and 3 statuses.
INSERT INTO orders (id, customer_id, status, amount, created_at)
WITH RECURSIVE seq(n) AS (
    SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < 300000
)
SELECT n,
       (n % 5000) + 1,
       ELT((n % 3) + 1, 'shipped', 'pending', 'cancelled'),
       ((n % 500) + 1) + 0.99,
       DATE_ADD('2024-01-01', INTERVAL (n % 365) DAY)
FROM seq;

ANALYZE TABLE customers, orders;
