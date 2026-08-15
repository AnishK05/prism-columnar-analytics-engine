-- Q8: narrow projection (pruning vs SELECT *)
SELECT event_id, ts, country
FROM events
WHERE country = 'US'
ORDER BY event_id
LIMIT 20;
