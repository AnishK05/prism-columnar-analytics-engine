-- Q8-wide: same filter as Q8, all columns (pruning contrast)
SELECT *
FROM events
WHERE country = 'US'
ORDER BY event_id
LIMIT 20;
