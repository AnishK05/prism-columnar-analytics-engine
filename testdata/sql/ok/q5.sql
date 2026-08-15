-- Q5: high-cardinality GROUP BY with a filter
SELECT user_id, COUNT(*)
FROM events
WHERE country = 'US'
GROUP BY user_id
ORDER BY COUNT(*) DESC, user_id
LIMIT 50;
