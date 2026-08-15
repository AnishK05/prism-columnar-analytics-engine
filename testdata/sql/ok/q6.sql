-- Q6: resume-class filter + group + top-N
SELECT country, event_type, COUNT(*), SUM(amount_cents), AVG(amount_cents)
FROM events
WHERE ts >= TIMESTAMP '2024-01-01'
  AND country IN ('US', 'CA', 'GB')
  AND amount_cents > 0
GROUP BY country, event_type
ORDER BY COUNT(*) DESC
LIMIT 20;
