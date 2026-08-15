-- Q1: full scan aggregate (column prune)
SELECT COUNT(*), SUM(amount_cents) FROM events;
