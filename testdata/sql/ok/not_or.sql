-- NOT / OR / quoted ident
SELECT "country" FROM events
WHERE NOT (country = 'XX') OR country = 'US'
LIMIT 3;
