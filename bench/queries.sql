-- PrismBench Q1–Q8 (frozen for v1). Run with:
--   go run ./cmd/prism sql --data-dir testdata/tables --file testdata/sql/ok/q1.sql
--   go run ./cmd/prism sql --engine=row --jobs=1 --file testdata/sql/ok/q1.sql

-- Q1 Full scan agg
SELECT COUNT(*), SUM(amount_cents) FROM events;

-- Q2 Selective time filter (see testdata/sql/ok/q2.sql)
-- Q3 Low-selectivity filter
-- Q4 Group by low-cardinality
-- Q5 Group by high-cardinality
-- Q6 Resume query
-- Q7 String IN
-- Q8 Narrow projection
--
-- Individual files live in testdata/sql/ok/q1.sql … q8.sql
