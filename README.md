# Prism

A miniature **vectorized, single-node OLAP engine** for learning how analytical databases actually work: Parquet on disk, Apache Arrow in memory, predicate pushdown, column pruning, row-group skipping, and a batched execution pipeline.

This repository is in the **planning stage**. The engine is not implemented yet.

**Read this first:** [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md)

That document is the project blueprint: scope, architecture, SQL dialect, dataset, optimizer, phased build order, benchmarking rules, and open questions.

## Resume target (numbers TBD by measurement)

> Engineered a vectorized, single-node OLAP engine in Go querying Parquet via Apache Arrow, with predicate pushdown, column pruning, and row-group skipping, sustaining 100M+ rows/query at 10x a row-at-a-time baseline

## Stack (planned)

Go · Apache Arrow · Apache Parquet · PostgreSQL (correctness oracle) · Python/NumPy/PyArrow · Next.js/TypeScript · Docker

## Status

| Phase | Description | State |
|---|---|---|
| Plan | Detailed implementation plan | In review |
| v1 engine | Scan / filter / agg SQL over Parquet | Not started |
| Bench | 100M-row PrismBench + dual executor | Not started |
| UI | Query workbench + plan visualizer | Not started |
