# Prism writeup

Interview / portfolio script. **Do not invent laptop timings.** The harness and UI are real; the resume numbers are not, until you run `--scale laptop` on the Windows machine.

Placeholder resume line (replace after that run):

> Engineered a vectorized, single-node OLAP engine in Go querying Parquet via Apache Arrow, with predicate pushdown, column pruning, and row-group skipping, sustaining **[N rows]/query at [Xx]** a naive row-at-a-time baseline **(PrismBench Q[k], this laptop)**

---

## What I built

Prism is a single-node OLAP engine. Tables live as Parquet on disk. Queries run as a vectorized pipeline over Apache Arrow batches. A second row-at-a-time executor shares the same planner so speedups are measured internally.

- Language: Go 1.22, Apache Arrow Go v18.0.0
- Storage: Apache Parquet (row groups, min/max stats)
- Memory: Apache Arrow RecordBatches, streamed
- SQL: frozen subset — `SELECT` / `WHERE` / `GROUP BY` / `ORDER BY` / `LIMIT` plus `COUNT/SUM/AVG/MIN/MAX`
- Optimizations: column pruning, predicate pushdown, row-group skipping
- Parallelism: worker pool over `(file, row-group)`; each worker opens its own Parquet handle
- Correctness: PostgreSQL oracle on `tiny`/`dev` (Q1–Q8 + Q8-wide)
- API: `prismd` (`/health`, `/tables`, `/query`, `/explain`, `/bench`)
- UI: three-page Next.js workbench (SQL, plan tree, 3-way bench charts)
- Dataset: synthetic product-analytics `events` (plus unused `users`/`products` for a later join)

Not in v1: joins, writes, DuckDB, distributed anything, a license file.

---

## How a query runs

1. Lexer / parser → AST (hand-rolled; dialect in `docs/sql.md`)
2. Binder against the catalog
3. Logical plan → rule-based optimizer (prune, push, skip)
4. Physical plan → vectorized (or row) operators
5. Parquet scan streams row groups; skipped groups are never decoded
6. Filter / hash aggregate / sort / limit on Arrow batches
7. `EXPLAIN` / workbench Plan page reports rows, bytes, and row groups skipped

Whiteboard version: [docs/architecture.md](docs/architecture.md).

---

## Dataset and clustering

Synthetic `events`, rows **sorted by `ts` within each file** so a time `WHERE` can skip row groups. Same idea as warehouse micro-partitions, not a benchmark trick.

| Scale | `events` rows | Where |
|---|---|---|
| testdata (committed) | 8,192 · 4 row groups · 2 files | `testdata/tables` |
| tiny | 100,000 | generate |
| dev | 1,000,000 | generate |
| laptop | start 10M, climb until the machine swaps | generate; **this is the resume count** |

On testdata, `ts` is clustered (non-decreasing min/max across 3 row-group boundaries), range `2024-01-01` … `2024-12-31`.

**Q2** (`ts` in the first 7 days) keeps **1 of 4** row groups. **Q6** uses `ts >= 2024-01-01`, which covers the whole year, so it does **not** skip on this fixture. Say that in the interview.

Hardware for a laptop run (paste from PowerShell):

```powershell
Get-CimInstance Win32_Processor | Select-Object Name, NumberOfCores, NumberOfLogicalProcessors
Get-CimInstance Win32_ComputerSystem | Select-Object Manufacturer, Model, TotalPhysicalMemory
git rev-parse HEAD
```

- CPU: TODO
- RAM: TODO
- Disk: TODO
- OS: Windows
- Commit: TODO

---

## Measurements

Reproduce with [`docs/WINDOWS.md`](docs/WINDOWS.md) and [`docs/benchmarks.md`](docs/benchmarks.md). Protocol: hot cache; one warmup discarded; first-run vs median/p95 of the rest. Baseline is **vectorized+opt vs row-naive (no skip/prune)**.

Checked-in `bench/results/sample.json` is **testdata, 8,192 rows**, for the UI. It is not a resume measurement. On that fixture, Q2 is the skip demo (3/4 groups skipped; prune to `ts` + `amount_cents`). Do not quote those millisecond bars as product performance.

| Query | What it shows | Rows | Vectorized | Row-naive | Row-opt | Speedup vs naive |
|---|---|---|---|---|---|---|
| Q1 | column prune | laptop TODO | | | | |
| Q2 | time skip | laptop TODO | | | | |
| Q3 | filter-heavy | | | | | |
| Q4 | low-card GROUP BY | | | | | |
| Q5 | high-card GROUP BY | | | | | |
| Q6 | resume-style | | | | | |
| Q7 | string `IN` | | | | | |
| Q8 / Q8-wide | narrow vs `SELECT *` | | | | | |

Resume bullet uses **Q[TODO]** at **[TODO] rows**, **vectorized+optimizations vs naive row-at-a-time**, **[TODO]x**, measured on this laptop.

---

## What I would say on a whiteboard

- Columnar vs row: we only decode columns the query names.
- Zone maps: if `max(ts) < predicate`, skip the row group.
- Vectorization: thousands of rows per operator call, typed tight loops.
- Two-phase aggregation: local hash maps per row-group worker, then merge.
- Why clustering matters: `user_id = 42` will not skip; a tight `ts` range will. Q6 vs Q2 is the contrast.
- Fair speedup: both engines get prune+skip; the 3-way chart separates “vectorization” from “not reading the file.”

---

## What I would build next

- Inner hash joins on `users` / `products` (tables already generated)
- `date_trunc` / `HAVING`
- Spill-to-disk when GROUP BY cardinality explodes
- DuckDB as a humility baseline (still not a v1 competitor)
- Hive-style partition directories

---

## Links

- Plan: `IMPLEMENTATION_PLAN.md`
- Architecture: `docs/architecture.md`
- Windows start-here: `WINDOWS.md`
- Windows developer runbook: `docs/WINDOWS.md`
- API: `docs/api.md`
- Bench notes: `docs/benchmarks.md`
