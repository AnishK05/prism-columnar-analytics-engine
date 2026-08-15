# Prism SQL (v1)

Hand-rolled dialect. The parser **rejects** anything we cannot execute, with `not supported in v1`.

## Supported

```sql
SELECT [ALL] select_item [, ...]
FROM table_name
[WHERE predicate]
[GROUP BY column [, ...]]
[ORDER BY column [ASC|DESC] [, ...]]
[LIMIT integer];
```

Select items: `*`, columns, `COUNT(*)`, `COUNT(col)`, `SUM`, `AVG`, `MIN`, `MAX`, optional `AS alias`.

Predicates: comparisons (`=`, `<>`, `<`, `<=`, `>`, `>=`), `AND` / `OR` / `NOT`, `IS [NOT] NULL`, `[NOT] IN (...)`, `[NOT] BETWEEN a AND b`.

Literals: integers, floats, `'strings'` (`''` escape), `TRUE` / `FALSE` / `NULL`, `TIMESTAMP '2024-01-01'` (UTC, milliseconds).

Identifiers: `name` or `"quoted"`. Keywords are case-insensitive.

`--` line comments are allowed.

## Not in v1

`JOIN`, `HAVING`, `DISTINCT`, `CASE`, `UNION`, `WITH`, `OFFSET`, DML, subqueries, `SELECT` arithmetic (parsed, then rejected at bind).

`ORDER BY` / `GROUP BY` use names or select aliases, not ordinals.

## Examples

```sql
SELECT country, COUNT(*), SUM(amount_cents)
FROM events
WHERE amount_cents > 0
GROUP BY country
ORDER BY COUNT(*) DESC
LIMIT 10;
```

```powershell
go run .\cmd\prism -- sql --data-dir testdata\tables "SELECT COUNT(*) FROM events"
go run .\cmd\prism -- sql --file testdata\sql\ok\resume.sql --data-dir testdata\tables
go run .\cmd\prism -- sql --ast "SELECT * FROM events WHERE country = 'US' LIMIT 5"
go run .\cmd\prism -- explain --data-dir testdata\tables --file testdata\sql\ok\q2.sql
go run .\cmd\prism -- sql --engine=row --jobs=1 --data-dir testdata\tables --file testdata\sql\ok\q1.sql
go run .\cmd\prism -- sql --jobs=4 --json --data-dir testdata\tables --file testdata\sql\ok\q1.sql
go run .\cmd\prism -- bench --scale testdata --repeat 3
go run .\cmd\prismd -- --listen 127.0.0.1:8080 --data-dir testdata\tables
```

`--engine=vectorized` (default) uses Arrow batch kernels. `--engine=row` decodes the same pruned/skipped scan into a per-row loop (the speedup baseline). `--jobs` / `PRISM_PARALLELISM` runs a worker pool over row groups with partial + merge aggregation. Without `ORDER BY`, row order is undefined — compare results as multisets.
