# How Prism runs a query

This is the interview-prep version of the engine. The code is the source of truth; this page is the story you tell at a whiteboard.

## What Prism is

A **single-node, read-mostly OLAP engine**. Fact tables live as Apache Parquet on disk. Execution happens on Apache Arrow batches in RAM. There is no WAL, no MVCC, no INSERT SQL, and no joins in v1. PostgreSQL is a **correctness oracle only** — Prism never queries it at runtime.

The resume comparison is **Prism vectorized vs Prism row-at-a-time**, not vs Postgres or DuckDB.

## Path of a SELECT

```
SQL text
  → lexer / recursive-descent parser   (internal/sql)
  → binder + catalog                   (internal/sql, internal/catalog)
  → logical plan                       (Scan, Filter, Project, Agg, Sort, Limit)
  → rule-based optimizer               (column prune, predicate pushdown, row-group skip)
  → physical request                   (files, kept row groups, columns, jobs)
  → executor                           (vectorized Arrow kernels  or  per-row loop)
  → Parquet scan                       (one handle per worker, stream row groups)
```

`prism sql` and `prismd` `POST /query` share `engine.Run`. The workbench is a thin client of that JSON.

## Three storage tricks that actually matter

**Column pruning.** The binder collects columns the query needs. The scan decodes only those Parquet column chunks. `SELECT COUNT(*), SUM(amount_cents)` reads one column. `SELECT *` reads ten. Q8 vs Q8-wide is the demo.

**Predicate pushdown.** A `WHERE` on `ts` or `country` is attached to the scan. Residual predicates (things we cannot prove from stats) still run in the filter operator.

**Row-group skipping (zone maps).** Each Parquet row group stores min/max. If `max(ts) < 2024-01-01` or `min(ts) >= 2024-01-08`, that group is never opened. This only works if `ts` is **clustered** — the generator writes timestamps in sorted order on purpose, the same idea as Snowflake micro-partitions. A predicate on `user_id = 42` will not skip; say that out loud.

On the committed fixture (8,192 rows, 4 row groups), **Q2** (7-day window) keeps 1 group and skips 3. **Q6** uses `ts >= 2024-01-01`, which covers the whole 2024 file, so it does **not** skip. That is not a bug; it is why clustering + predicate shape both matter.

## Vectorized vs row-at-a-time

Both engines get the **same** prune and skip. If you only skip on the vectorized path, you are measuring two things at once.

- **Vectorized:** operators consume Arrow arrays (thousands of rows per call). Hash aggregate is a typed hash table.
- **Row:** decode the same batches, then a Go `for` loop and `if !eval(row)`.

The honest 3-way bench is:

1. row-naive — no skip, no prune
2. row-opt — skip + prune
3. vectorized — skip + prune

Lead with **(3) vs (1)**. Show the full table so an interviewer cannot claim the win is “just skipping.”

## Parallelism

`--jobs` (or `PRISM_PARALLELISM`) is a worker pool over `(file, row-group)` morsels. Each worker opens its **own** Parquet handle — there is no shared Arrow reader lock. Aggregates are two-phase: local hash maps, then merge. Without `ORDER BY`, result order is a multiset.

## Memory

The engine must **stream** row groups. Peak RAM should be roughly `jobs × batch × projected columns` plus hash-agg state. Do not `ReadAll()` a 100M-row table. If the laptop swaps, drop scale; do not invent 100M.

## What the UI is for

The workbench exists so you can **point at** prune, skip, and the 3-way bench. It is not a product. Three pages: SQL, plan tree, charts from `GET /bench`.
