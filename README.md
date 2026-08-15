# Prism

A miniature **vectorized, single-node OLAP engine** for learning how analytical databases actually work: Parquet on disk, Apache Arrow in memory, predicate pushdown, column pruning, row-group skipping, and a batched execution pipeline.

**Windows is the supported local setup.** Follow **[docs/WINDOWS.md](docs/WINDOWS.md)** (native PowerShell + Docker Desktop; WSL is optional).

Blueprint: **[IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md)** (decisions locked in §20).  
Interview script (fill after measurement): **[WRITEUP.md](./WRITEUP.md)**.

## What’s here now (Phase 0–11)

- `prism tables` / `prism describe` — catalog with cached row-group min/max (zone maps); `--json` includes ts range for `manifest.json`
- `prism inspect` / `prism scan` — Parquet → Arrow, column pruning
- `prism scan --where` — vectorized filter (SQL three-valued logic) with row-group skipping
- `prism agg` — hash aggregate (`COUNT`/`SUM`/`AVG`/`MIN`/`MAX`), sort, limit
- `prism sql` — Prism SQL lexer/parser/binder + the same pipeline ([docs/sql.md](docs/sql.md))
- `prism explain` — physical plan (text or JSON); `--analyze` adds bytes/rows
- Dual engine: `--engine=vectorized|row` (same skip/prune; row path is a per-row loop)
- Parallel row-group workers: `--jobs` / `PRISM_PARALLELISM` (partial + merge aggregation)
- Row-group skipping from Parquet min/max (zone maps); Q2 on the fixture keeps 1 of 4 groups
- Python generator for synthetic `events` / `users` / `products` (batched writes, `--seed`, tqdm)
- PostgreSQL oracle: `scripts/load_postgres.py` + `scripts/verify_against_postgres.py` (tiny/dev, never laptop)
- `prism bench` — hot-cache protocol, 3-way breakdown, JSON for the UI (`bench/results/sample.json`)
- Committed fixture: `testdata/tables` (8,192 `events` rows, `ts` clustered)

```bash
go test ./...
go run ./cmd/prism tables --data-dir testdata/tables
go run ./cmd/prism describe events --json --data-dir testdata/tables
go run ./cmd/prism sql --data-dir testdata/tables "SELECT country, COUNT(*) FROM events GROUP BY country ORDER BY COUNT(*) DESC LIMIT 5"
go run ./cmd/prism sql --engine=row --jobs=1 --data-dir testdata/tables --file testdata/sql/ok/q1.sql
go run ./cmd/prism sql --jobs=4 --data-dir testdata/tables --file testdata/sql/ok/q1.sql
go run ./cmd/prism explain --data-dir testdata/tables --file testdata/sql/ok/q2.sql
go run ./cmd/prism bench --scale testdata --repeat 3
python scripts/verify_manifest.py --data-dir testdata/tables
```

On Windows PowerShell, use `.\` paths; see `docs/WINDOWS.md`. Oracle: `docs/WINDOWS.md` §5. Benches: [`docs/benchmarks.md`](docs/benchmarks.md).

Each parallel worker opens its own Parquet file handle for one row group, so there is no shared Arrow reader lock. Speedup vs `--jobs=1` is measured on `dev`/`laptop` with `prism bench` — **do not put a number on the resume until laptop-scale is run on the Windows machine**.

The Next.js workbench is not implemented yet.

## Resume line (placeholders — rewrite after Phase 11)

> Engineered a vectorized, single-node OLAP engine in Go querying Parquet via Apache Arrow, with predicate pushdown, column pruning, and row-group skipping, sustaining 100M+ rows/query at 10x a row-at-a-time baseline

Those numbers are a direction, not a quota. Measure on this laptop, name the query and baseline, then replace.

## Stack (locked)

Go 1.22 · Apache Arrow Go **v18.0.0** · Apache Parquet · PostgreSQL (correctness oracle only) · Python / NumPy / PyArrow · Next.js / TypeScript (later) · Docker Desktop

Pinned `github.com/apache/arrow-go/v18 v18.0.0` so Go 1.22 works. Newer Arrow Go releases need a newer toolchain.

## Status

| Phase | Description | State |
|---|---|---|
| Plan | Blueprint + Windows runbook | Done |
| 0 | Scaffold, CLI, Compose, CI | Done |
| 1 | Parquet → Arrow scan + generator | Done |
| 2 | Catalog stats, tables/describe | Done |
| 3 | Vectorized `--where` filters | Done |
| 4 | Hash aggregate, sort, limit | Done |
| 5 | SQL lexer / parser / binder | Done |
| 6 | Planner, optimizer, EXPLAIN | Done |
| 7 | Row-group skipping (zone maps) | Done |
| 8 | Dual engine (vectorized + row) | Done |
| 9 | Parallel row-group workers | Done |
| 10 | Postgres oracle + laptop-scale generator | Done (laptop row count is still a Windows-machine measurement) |
| 11 | Benchmark harness | Done (sample.json is testdata; resume numbers stay placeholders) |
| 12+ | HTTP, UI, writeup numbers | Not started |
