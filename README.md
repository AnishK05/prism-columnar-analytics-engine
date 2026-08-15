# Prism

A miniature **vectorized, single-node OLAP engine** for learning how analytical databases actually work: Parquet on disk, Apache Arrow in memory, predicate pushdown, column pruning, row-group skipping, and a batched execution pipeline.

**Windows is the supported local setup.** Follow **[docs/WINDOWS.md](docs/WINDOWS.md)** (native PowerShell + Docker Desktop; WSL is optional).

Blueprint: **[IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md)** (decisions locked in §20).  
Interview script (fill after measurement): **[WRITEUP.md](./WRITEUP.md)**.

## What’s here now (Phase 0–5)

- `prism tables` / `prism describe` — catalog with cached row-group min/max (zone maps)
- `prism inspect` / `prism scan` — Parquet → Arrow, column pruning
- `prism scan --where` — vectorized filter (SQL three-valued logic)
- `prism agg` — hash aggregate (`COUNT`/`SUM`/`AVG`/`MIN`/`MAX`), sort, limit
- `prism sql` — Prism SQL lexer/parser/binder + the same pipeline ([docs/sql.md](docs/sql.md))
- Python generator for synthetic `events` / `users` / `products`
- Committed fixture: `testdata/tables` (8,192 `events` rows, `ts` clustered)

```bash
go test ./...
go run ./cmd/prism tables --data-dir testdata/tables
go run ./cmd/prism agg --data-dir testdata/tables --table events --group country --agg 'count,sum(amount_cents)' --order count --desc --limit 10
go run ./cmd/prism sql --data-dir testdata/tables "SELECT country, COUNT(*) FROM events GROUP BY country ORDER BY COUNT(*) DESC LIMIT 5"
```

On Windows PowerShell, use `.\` paths; see `docs/WINDOWS.md`.

The Next.js workbench, EXPLAIN, and row-group skipping are not implemented yet.

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
| 6+ | Planner, skipping, dual engine, UI | Not started |
