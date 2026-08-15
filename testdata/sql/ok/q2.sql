-- Q2: 7-day window (row-group skipping)
SELECT COUNT(*), SUM(amount_cents)
FROM events
WHERE ts >= TIMESTAMP '2024-01-01' AND ts < TIMESTAMP '2024-01-08';
