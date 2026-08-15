-- WHERE comparisons, AND, IN, BETWEEN, IS NOT NULL
SELECT country, amount_cents
FROM events
WHERE amount_cents > 0
  AND country IN ('US', 'CA', 'GB')
  AND qty BETWEEN 1 AND 10
  AND event_type IS NOT NULL
LIMIT 20;
