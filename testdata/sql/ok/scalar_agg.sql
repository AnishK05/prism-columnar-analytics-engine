-- scalar aggregates
SELECT COUNT(*), SUM(amount_cents), AVG(amount_cents), MIN(qty), MAX(qty)
FROM events;
