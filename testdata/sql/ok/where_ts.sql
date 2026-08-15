-- timestamp literal
SELECT country FROM events
WHERE ts >= TIMESTAMP '2024-01-01'
  AND ts < TIMESTAMP '2025-01-01'
LIMIT 5;
