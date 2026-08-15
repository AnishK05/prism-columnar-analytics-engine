# Prism writeup

Interview / portfolio script. **Fill this in after Phase 11 (measured numbers) and tighten in Phase 14.** Do not invent timings.

Placeholder resume line (replace):

> Engineered a vectorized, single-node OLAP engine in Go querying Parquet via Apache Arrow, with predicate pushdown, column pruning, and row-group skipping, sustaining **[N rows]/query at [Xx]** a naive row-at-a-time baseline **(PrismBench Q[k], this laptop)**

---

## What I built

Prism is a single-node OLAP engine. Tables live as Parquet on disk. Queries run as a vectorized pipeline over Apache Arrow batches. A second row-at-a-time executor shares the same planner so speedups are measured internally.

- Language: Go
- Storage: Apache Parquet (row groups, min/max stats)
- Memory: Apache Arrow RecordBatches
- SQL: frozen subset — `SELECT` / `WHERE` / `GROUP BY` / `ORDER BY` / `LIMIT` plus `COUNT/SUM/AVG/MIN/MAX`
- Optimizations: column pruning, predicate pushdown, row-group skipping
- Correctness: PostgreSQL oracle on `tiny`/`dev`
- UI: thin Next.js workbench (SQL, plan, vectorized-vs-row bench)
- Dataset: synthetic product-analytics `events` (plus unused `users`/`products` for a later join)

Not in v1: joins, writes, DuckDB, distributed anything.

---

## How a query runs

1. Lexer / parser → AST (hand-rolled, dialect in `IMPLEMENTATION_PLAN.md` §6)
2. Binder against the catalog
3. Logical plan → rule-based optimizer (prune, push, skip config)
4. Physical plan → vectorized (or row) operators
5. Parquet scan streams row groups; skipped groups are never decoded
6. Filter / hash aggregate / sort / limit on Arrow batches
7. `EXPLAIN ANALYZE` reports rows, bytes, and row groups skipped

---

## Dataset and clustering

Synthetic `events` fact table, rows **sorted by `ts` within each file** so a time `WHERE` can skip row groups. That is the same idea as warehouse micro-partitions / clustering, not a benchmark trick.

Scale used on this machine: **[TODO: rows, file count, disk size]**.

Hardware (**[TODO: paste from `Get-CimInstance`]**):

- CPU:
- RAM:
- Disk:
- OS: Windows
- Commit:

---

## Measurements

Reproduce with [`docs/WINDOWS.md`](docs/WINDOWS.md) and [`docs/benchmarks.md`](docs/benchmarks.md) (hot cache; Windows cannot drop page cache). The harness is `prism bench`; fill this table from a **laptop-scale** JSON on this machine, not from `bench/results/sample.json`.

| Query | What it shows | Rows | Vectorized | Row-at-a-time naive | Row-at-a-time + skip/prune | Speedup vs naive |
|---|---|---|---|---|---|---|
| Q1 | column prune | TODO | | | | |
| Q2 | time skip | | | | | |
| Q3 | filter-heavy | | | | | |
| Q4 | low-card GROUP BY | | | | | |
| Q5 | high-card GROUP BY | | | | | |
| Q6 | resume-style | | | | | |
| Q7 | string `IN` | | | | | |
| Q8 | wide vs narrow | | | | | |

Resume bullet uses **Q[TODO]** at **[TODO] rows**, **vectorized+optimizations vs naive row-at-a-time**, **[TODO]x**. The bench page shows the 3-way breakdown so this is not a hidden skip-only win.

---

## What I would say on a whiteboard

- Columnar vs row: we only decode columns the query names.
- Zone maps: if `max(ts) < predicate`, skip the row group.
- Vectorization: thousands of rows per operator call, typed tight loops, no `interface{}` in the inner loop.
- Two-phase aggregation: local hash maps per row-group worker, then merge.
- Why clustering matters: `user_id = 42` will not skip; `ts` range will.

---

## What I would build next

- Inner hash joins on `users` / `products` (tables already generated)
- `date_trunc` / `HAVING`
- Spill-to-disk when GROUP BY cardinality explodes
- DuckDB as a humility baseline
- Hive-style partition directories

---

## Links

- Plan: `IMPLEMENTATION_PLAN.md`
- Windows runbook: `docs/WINDOWS.md`
- Bench notes: `docs/benchmarks.md` (after Phase 11)
