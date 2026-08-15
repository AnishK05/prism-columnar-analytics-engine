# Prism

A miniature **vectorized, single-node OLAP engine** for learning how analytical databases actually work: Parquet on disk, Apache Arrow in memory, predicate pushdown, column pruning, row-group skipping, and a batched execution pipeline.

**Windows is the supported local setup.** Follow **[docs/WINDOWS.md](docs/WINDOWS.md)** (native PowerShell + Docker Desktop; WSL is optional).

Blueprint: **[IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md)** (decisions locked in §20).  
Interview script (fill after measurement): **[WRITEUP.md](./WRITEUP.md)**.

## What’s here now (Phase 0–1)

- `go run ./cmd/prism version`
- `prism inspect` — Parquet schema, row groups, min/max stats
- `prism scan` — read selected columns as Arrow batches (column pruning)
- Python generator for synthetic `events` / `users` / `products`
- Docker Compose Postgres 16 (oracle, used later)
- Committed fixture: `testdata/tables` (8,192 `events` rows)

```bash
go test ./...
go run ./cmd/prism inspect --data-dir testdata/tables --table events
go run ./cmd/prism scan --data-dir testdata/tables --table events --columns country,amount_cents --limit 5
```

On Windows PowerShell, use `.\` paths; see `docs/WINDOWS.md`.

SQL, filters, aggregations, and the Next.js workbench are not implemented yet.

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
| 2+ | Catalog stats, SQL, exec, UI | Not started |
