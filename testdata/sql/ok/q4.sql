-- Q4: low-cardinality GROUP BY
SELECT country, event_type, COUNT(*), SUM(amount_cents)
FROM events
GROUP BY country, event_type
ORDER BY COUNT(*) DESC, country, event_type;
