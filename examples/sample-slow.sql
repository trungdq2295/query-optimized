-- Engine: Postgres
-- Complaint: takes ~6 min, need under 30s
-- orders ~80M rows, customers ~2M rows
SELECT *
FROM orders o
JOIN customers c ON c.id = o.customer_id
WHERE YEAR(o.created_at) = 2024
  AND o.status = 'shipped'
ORDER BY o.created_at DESC
LIMIT 20 OFFSET 100000;
