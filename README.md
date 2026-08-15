# Prism

A miniature **vectorized, single-node OLAP engine** for learning how analytical databases actually work: Parquet on disk, Apache Arrow in memory, predicate pushdown, column pruning, row-group skipping, and a batched execution pipeline.

**Windows is the supported local setup.** Follow **[docs/WINDOWS.md](docs/WINDOWS.md)** (native PowerShell + Docker Desktop; WSL is optional).

Blueprint: **[IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md)** (decisions locked in §20).  
Interview script (fill after measurement): **[WRITEUP.md](./WRITEUP.md)**.

The engine is **not implemented yet**. Implementation starts at Phase 0 of the plan.

## Resume line (placeholders — rewrite after Phase 11)

> Engineered a vectorized, single-node OLAP engine in Go querying Parquet via Apache Arrow, with predicate pushdown, column pruning, and row-group skipping, sustaining 100M+ rows/query at 10x a row-at-a-time baseline

Those numbers are a direction, not a quota. Measure on this laptop, name the query and baseline, then replace.

## Stack (locked)

Go · Apache Arrow · Apache Parquet · PostgreSQL (correctness oracle only) · Python / NumPy / PyArrow · Next.js / TypeScript · Docker Desktop

No DuckDB, no joins, no INSERT SQL, no license file in v1. Dataset is synthetic `events`.

## Status

| Phase | Description | State |
|---|---|---|
| Plan | Blueprint + Windows runbook | Locked |
| v1 engine | Scan / filter / agg SQL over Parquet | Not started |
| Bench | Laptop-scale PrismBench + dual executor | Not started |
| UI | Three-page workbench | Not started |
