-- Q3: low-selectivity filter
SELECT COUNT(*), SUM(amount_cents) FROM events WHERE country = 'US';
