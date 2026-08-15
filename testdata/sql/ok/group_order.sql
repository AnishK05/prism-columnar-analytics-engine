-- GROUP BY + ORDER BY aggregate + LIMIT
SELECT country, event_type, COUNT(*) AS n, SUM(amount_cents)
FROM events
WHERE amount_cents > 0
GROUP BY country, event_type
ORDER BY COUNT(*) DESC
LIMIT 20;
