SELECT country, COUNT(*) FROM events GROUP BY country HAVING COUNT(*) > 10;
