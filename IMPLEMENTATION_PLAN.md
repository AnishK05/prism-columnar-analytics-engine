# Prism — Columnar Analytics Engine

## Implementation Plan

This document is the working blueprint for **Prism**: a miniature, single-node OLAP engine. The goal is not to compete with DuckDB or ClickHouse. The goal is to *build enough of a real analytical engine* that you can explain, demonstrate, and benchmark the internals that those systems are built on.

**Decisions in §20 are locked.** Implementation PRs follow this plan and check boxes in §18. Do not expand the SQL dialect or add DuckDB/joins until v1 is done.

**Primary development machine: a personal Windows laptop.** There is no cloud/demo box. Native Windows is first-class (PowerShell + Docker Desktop). WSL2 is optional, not required. The runbook is [`docs/WINDOWS.md`](docs/WINDOWS.md).

**Resume target (placeholder numbers — replace after Phase 11):**

> Engineered a vectorized, single-node OLAP engine in Go querying Parquet via Apache Arrow, with predicate pushdown, column pruning, and row-group skipping, sustaining 100M+ rows/query at 10x a row-at-a-time baseline

Those figures are a *direction*, not a quota. Design the system, measure on this Windows machine, then rewrite the bullet with the real scale, the named query, and the named baseline. A correct 20M-row demo with a documented 6x beats an OOM at 100M.

---

## 1. North star

Build a query engine that:

1. Stores tables as **Apache Parquet** files on local disk (columnar on disk).
2. Executes a useful **SQL subset** over those tables.
3. Uses **Apache Arrow RecordBatches** as the in-memory execution format (columnar in RAM).
4. Runs a **vectorized** operator pipeline (thousands of rows per call, not one).
5. Applies three storage-aware optimizations that actually matter on large scans:
   - **column pruning**
   - **predicate pushdown**
   - **row-group skipping** using Parquet min/max statistics
6. Ships a **row-at-a-time baseline executor** so the speedup claim is an apples-to-apples measurement on the *same engine*, not a vibes comparison to PostgreSQL.
7. Checks **correctness** against PostgreSQL on the same SQL (oracle only — not a published competitor). Optional pandas checks in Python tests are fine. **No DuckDB in v1.**
8. Exposes a small **HTTP API + thin Next.js workbench** so you can type SQL, see the physical plan, see which row groups were skipped, and look at timings.

If a feature does not make (1)–(8) stronger, it is stretch or out of scope.

---

## 2. Learning goals (what this project is actually for)

This is an undergrad systems project. The value is the *concepts you can explain on a whiteboard*, not the number of SQL features.

By the end you should be able to talk fluently about:

| Concept | What you will have actually built |
|---|---|
| Columnar vs row storage | Parquet on disk + Arrow in memory vs a row-at-a-time struct scan |
| OLAP vs OLTP | Scan/agg-heavy queries, no transactions, no point-lookup B-trees |
| Vectorized execution | Operators that consume/produce Arrow batches (~8K–64K rows) |
| Late materialization | Filter on 1–2 columns, then assemble the projected output |
| Predicate pushdown | `WHERE` compiled into scan so unused data is never decoded |
| Column pruning | Only requested columns are read from Parquet |
| Zone maps / row-group skipping | Skip entire Parquet row groups using min/max stats |
| Query planning | SQL AST → logical plan → physical plan |
| Query optimization | Rewrite rules, not a full cost-based optimizer |
| Hash aggregation | Vectorized GROUP BY with hash tables over keys |
| Parallel execution | Goroutine pool over row groups / files |
| Memory management | Streaming batches; not `ReadAll()` into RAM |
| Execution pipelines | Volcano-style iterators, but batched |
| Performance methodology | Warm/cold runs, honest baselines, explainable speedups |

Interview framing: “I built a baby Snowflake/DuckDB scan-and-agg engine so I would understand why Parquet + vectorization + stats skipping beat a row store for analytics.”

---

## 3. Scope

### 3.1 In scope (v1 — ship this)

- Single-node, read-mostly engine. No WAL, no transactions, no MVCC.
- Local filesystem Parquet tables. One table = one directory of `.parquet` files (Hive-style optional later).
- SQL subset documented in §6.
- Vectorized operators: `Scan`, `Filter`, `Project`, `HashAggregate`, `OrderBy`, `Limit`.
- Naive row-at-a-time executor for the *same* logical plans (the speedup baseline).
- Optimizer: column pruning, predicate pushdown, row-group skipping. Optional: simple filter reordering.
- Aggregates: `COUNT`, `COUNT(*)`, `SUM`, `AVG`, `MIN`, `MAX`.
- `GROUP BY` on 1–3 columns (typed keys: int64, string, date/timestamp, bool).
- `EXPLAIN` / `EXPLAIN ANALYZE` with per-operator timing, rows in/out, bytes read, row groups skipped.
- Python data generator (NumPy / PyArrow) that can emit laptop-scale fact tables (10M–100M, whatever fits).
- PostgreSQL as a **correctness oracle only** (same SQL, compare results). Not a UI benchmark competitor in v1.
- Docker Compose for Postgres (oracle) + engine + web UI. Works with **Docker Desktop on Windows**.
- Thin Next.js query workbench (three pages): editor, plan visualizer, benchmark page (vectorized vs row).
- Benchmark harness with a small suite of named queries (PrismBench Q1–Q8).
- Tests: parser, planner, optimizer rewrites, operator correctness, golden SQL results.
- Windows-first local workflow: PowerShell helpers + Python/Go/npm commands. Makefile is optional convenience for CI/Linux, never the only entry point.
- `WRITEUP.md` (interview script, filled after measurement) and `docs/WINDOWS.md` (how to run on Windows).

### 3.2 Stretch (v1.5 — only after v1 is fast and correct)

- Inner **hash join** on equi-join keys (enables a 2-table star schema).
- `COUNT(DISTINCT …)` and `HAVING`.
- Dictionary-encoded group keys / Arrow dictionary arrays.
- Spill-to-disk for aggregations that exceed a memory budget.
- Partition directories (`date=2024-01-01/...`) and partition pruning.
- Simple cost-based choices (e.g. broadcast vs build side — only relevant once joins exist).
- DuckDB as a *second* baseline (explicitly deferred; easy to demoralize v1).
- NYC Taxi or mini-TPC-H as a second dataset after the synthetic `events` table works.

### 3.3 Out of scope (do not build)

- Distributed execution, shuffle, consensus, multi-node.
- Inserts/updates/deletes, compaction, transactions.
- Full SQL: window functions, CTEs, subqueries, correlated subqueries, `UNION`, `CASE` (unless a tiny `CASE` sneaks in later), UDFs.
- Cost-based optimizer with histograms/MCVs (zone maps are enough).
- Custom Parquet writer / custom compression codec.
- SIMD kernels written in assembly, GPU, JIT (Cranelift/LLVM).
- Auth, multi-tenancy, a real catalog service, object storage as a hard dependency.
- Matching DuckDB or Postgres on *every* query. We win on *scan + filter + group-by* over Parquet. That is the point.
- **Joins in v1.** Inner hash joins are stretch. Still generate `users` and `products` so adding joins later is natural.
- **INSERT / UPDATE / DELETE SQL.** The generator writes Parquet; `prism register` (or a data-dir walk) is enough. No write-path SQL.
- **A license file.** Skip SPDX/MIT/etc. unless you later want one.
- **DuckDB** in the bench suite or UI.
- **WSL as a hard requirement.** Native Windows must work.

---

## 4. Tech stack and why

| Piece | Choice | Why |
|---|---|---|
| Engine language | **Go** (locked) | Official Apache Arrow Go, fast iteration, goroutines for parallel scans. Do not rewrite in Rust. |
| In-memory format | **Apache Arrow** (`apache/arrow-go`) | Industry-standard columnar batches; zero-copy-ish handoff from Parquet; the whole point of “vectorized.” |
| On-disk format | **Apache Parquet** | Row groups + column chunks + page stats are *the* teaching vehicle for pruning/skipping/pushdown. |
| SQL parser | **Hand-rolled recursive descent for a tiny dialect** | You learn query compilation. A Vitess/TiDB parser hides that and pulls in a huge surface you will not execute. |
| Correctness oracle | **PostgreSQL 16** in Docker Desktop | Interview-friendly: “I validated results against Postgres.” Not a storage engine and not a UI competitor in v1. |
| Data generation | **Python 3 + NumPy + PyArrow** | Generate Parquet identically to what the engine reads. Pandas for tiny oracle checks is fine. No DuckDB. |
| API | **Go `net/http`** (stdlib or a thin chi/echo) | `POST /query`, `POST /explain`, `GET /tables`. |
| UI | **Next.js + TypeScript** (thin, three pages) | Query workbench + plan viz + vectorized-vs-row charts. |
| Charts | **Recharts** (or Chart.js) | Benchmark bars: vectorized vs row-at-a-time. |
| Containerization | **Docker Desktop + Compose** | Postgres oracle, optional full stack. Must work on Windows. |
| Local DX | **PowerShell scripts + raw `go`/`py`/`npm`** | Makefile optional for CI. See `docs/WINDOWS.md`. |
| Bench / CI | **Go tests + `prism bench`** | Reproducible JSON the UI can plot. |

**PostgreSQL’s role (locked: oracle-only).** Postgres is **not** the storage engine. Prism never queries Postgres at runtime for user SQL. Use it in `scripts/verify_against_postgres.py` on `tiny`/`dev`. Do not put Postgres bars on the bench page in v1 — that comparison is unfair both ways and the resume speedup is **Prism-vectorized vs Prism-row-at-a-time**. Adding a labeled “external baseline” later is allowed; it is not v1.

---

## 5. Architecture

```
                        ┌─────────────────────────────────────┐
                        │           Next.js workbench         │
                        │  SQL editor · plan viz · bench UI   │
                        └─────────────────┬───────────────────┘
                                          │ HTTP JSON
                        ┌─────────────────▼───────────────────┐
                        │              prismd API             │
                        │     /query  /explain  /tables       │
                        └─────────────────┬───────────────────┘
                                          │
     SQL text                             ▼
     ┌────────┐   AST    ┌─────────┐  logical   ┌───────────┐  physical
     │ Lexer  │────────► │ Parser  │──────────► │  Binder   │──────────►
     └────────┘          └─────────┘            │  (catalog)│
                                                └─────┬─────┘
                                                      │
                                                      ▼
                                              ┌──────────────┐
                                              │  Optimizer   │
                                              │  prune cols  │
                                              │  push preds  │
                                              │  skip stats  │
                                              └──────┬───────┘
                                                     │
                         ┌───────────────────────────┼───────────────────────────┐
                         ▼                           ▼                           ▼
                  Vectorized exec              Row-at-a-time               EXPLAIN
                  (Arrow batches)              baseline                    ANALYZE
                         │                           │
                         └─────────────┬─────────────┘
                                       ▼
                              ┌─────────────────┐
                              │  Parquet scan   │
                              │  column prune   │
                              │  rg skip + page │
                              │  Arrow batches  │
                              └────────┬────────┘
                                       │
                                       ▼
                              data/tables/<name>/*.parquet

     Side channel: Python generator writes the same Parquet;
                   Postgres gets a CSV/COPY load for oracle checks.
```

### 5.1 Component responsibilities

**Catalog.** In-memory registry: table name → glob of parquet files, Arrow schema, per-file/per-row-group statistics (min, max, null count, row count). Rebuilt at startup by walking `PRISM_DATA_DIR`. No Postgres catalog required.

**Binder.** Resolve column names, types, `*` expansion, aggregate vs scalar, `GROUP BY` validity (every non-agg SELECT expr is grouped). This is where most SQL UX errors should come from, with decent messages.

**Logical plan.** Tree of `Scan`, `Filter`, `Project`, `Aggregate`, `Sort`, `Limit`. No execution yet. Easy to pretty-print.

**Optimizer.** Rule-based, applied in a fixed order (see §8). Each rule has a unit test: plan-in / plan-out.

**Physical plan.** Concrete operators with chosen batch size, selected Parquet columns, pushed predicates, and a parallelism degree. `EXPLAIN` prints this tree.

**Executors.** Two physical backends sharing the same physical plan:

- `exec/vectorized` — Arrow arrays, tight loops, hash agg.
- `exec/row` — `struct { fields []interface{} }` or typed Go structs, one row per `Next()`. Intentionally naive. This is the baseline, not a strawman so weak it is embarrassing (it should still be a correct iterator engine).

**Scan.** The only component that talks to Parquet. Everything above it should be format-agnostic Arrow.

### 5.2 Process model

- CLI: `prism sql "SELECT ..."`, `prism explain ...`, `prism bench`, `prism describe <table>`.
- Daemon: `prismd` listens on `:8080`, used by the UI.
- Same packages underneath. No logic living only in the HTTP layer.

---

## 6. SQL surface (v1 dialect)

Call it **Prism SQL**. Document it in `docs/sql.md` when implementation starts. Keep it small enough to parse by hand.

```sql
-- supported
SELECT [ALL] select_item [, ...]
FROM table_name
[WHERE predicate]
[GROUP BY column [, ...]]
[ORDER BY column [ASC|DESC] [, ...]]
[LIMIT integer];

select_item:
    expression [AS alias]
  | *

expression:
    literal                          -- 1, 1.5, 'str', TRUE, NULL
  | column
  | ( expression )
  | expression +|-|*|/ expression
  | AGG(expression)                  -- COUNT, SUM, AVG, MIN, MAX
  | COUNT(*)

predicate:
    comparison                       -- =, <>, <, <=, >, >=
  | predicate AND predicate
  | predicate OR predicate
  | NOT predicate
  | expression IS [NOT] NULL
  | expression [NOT] BETWEEN a AND b
  | expression [NOT] IN (literal [, ...])
  | ( predicate )
```

**v1 rules:**

- One table per query (no joins).
- No subqueries, no `HAVING`, no `DISTINCT` (except maybe `COUNT(DISTINCT col)` as stretch).
- `ORDER BY` / `GROUP BY` use column names or select aliases, not ordinals, in v1 (ordinals are a nice 10-line later add).
- Types: `int64`, `float64`, `bool`, `utf8`, `timestamp[ms]`, `date32`. No decimals in v1 (Arrow decimal is painful; generate money as `int64` cents).
- Identifiers: unquoted `[A-Za-z_][A-Za-z0-9_]*`, optional `"quoted"`.
- String literals: single quotes, `''` escape.

**Why this subset is enough.** The resume query class is:

```sql
SELECT country, event_type, COUNT(*), SUM(amount_cents), AVG(amount_cents)
FROM events
WHERE ts >= TIMESTAMP '2024-01-01'
  AND country IN ('US', 'CA', 'GB')
  AND amount_cents > 0
GROUP BY country, event_type
ORDER BY COUNT(*) DESC
LIMIT 20;
```

That single pattern exercises scan, prune, pushdown, skip, vectorized filter, hash agg, sort, and limit. Write 8 variations of it and you have a benchmark suite.

**Dialect is frozen for v1.** No `HAVING`, `CASE`, `date_trunc`, `DISTINCT`, or joins until §18 is checked. Reject them with a clear parser/binder error (`JOIN not supported in v1`). If the workbench demo later feels weak without day buckets, `date_trunc` is the first additive function — still not v1.

---

## 7. Dataset and workload (PrismBench)

Do **not** start by downloading a random CSV. Generate a dataset whose statistics *make skipping interesting*.

### 7.1 Tables

**`events`** — fact table, the large star. Rough schema:

| column | type | notes |
|---|---|---|
| `event_id` | int64 | monotonic, unique |
| `user_id` | int64 | zipf-ish, ~1M distinct |
| `ts` | timestamp[ms] | sorted *within each file* (critical for row-group skipping) |
| `event_type` | utf8 (low cardinality) | `view`, `click`, `add_cart`, `purchase`, `refund` |
| `country` | utf8 (low cardinality) | ~20 values |
| `device` | utf8 | `ios`, `android`, `web` |
| `amount_cents` | int64 | 0 for non-purchase; heavy tail for purchase |
| `qty` | int64 | |
| `product_id` | int64 | ~100K distinct |
| `session_id` | int64 | |

**`users`** — ~1M rows. Generated now, joined later (stretch).

**`products`** — ~100K rows. Same.

### 7.2 Physical layout (this is a feature, not an accident)

- Write Parquet with **row group size ~ 128K–512K rows** (tunable).
- Files are **time-partitioned**: `data/tables/events/part-0000.parquet`, each file covering a time slice, rows **sorted by `ts`**.
- Enable Parquet **column statistics** (min/max/nullcount) — PyArrow does this by default.
- Compression: **zstd** or **snappy**. Pick one and keep it consistent.
- Dictionary encoding on `event_type`, `country`, `device`.

Why sorted `ts`? So `WHERE ts >= X AND ts < Y` skips most row groups. If timestamps are random, skipping demos look fake and speedups collapse. Call this out in the writeup — it is how real warehouses cluster data (Snowflake micro-partitions, BigQuery date partitioning, Spark `SORT BY`).

### 7.3 Scale targets (replaceable — laptop decides)

There is **no external hardware**. Everything runs on the same Windows PC that develops the project. Do not chase 100M or 10x as a goal of the design; chase a correct, streaming, skip/prune/vectorized engine, then measure.

| alias | rows in `events` | when to use |
|---|---|---|
| `tiny` | 100K | unit tests, CI, first Windows smoke |
| `dev` | 1M | local UI, fast benches |
| `laptop` | **as large as this machine can generate and query without swapping** | the number that actually goes on the resume |
| `stretch-scale` | 100M or more | only if `laptop` was easy and disk/RAM allow |

Start `laptop` at **10M**, then 25M / 50M / 100M. Stop at the largest scale that:

- generates with batched PyArrow writes (never a giant DataFrame)
- queries without the OS swapping to death
- still demos skipping (time-sorted files)

**Memory reality check.** 100M rows × 10 columns × ~8 bytes ≈ 8 GB uncompressed. The engine **must stream row groups**. Peak RAM should be `parallelism × batch_size × projected_columns`, plus hash-agg state. Do not assume 16 GB — detect RAM at bench time (`Get-CimInstance Win32_ComputerSystem`) and record it in `docs/benchmarks.md`. If generation or query OOMs, drop scale; do not add a swap hack and call it 100M.

After Phase 11, replace every “100M+” / “10x” in the README and `WRITEUP.md` with the measured row count, query id, and speedup.

### 7.4 Query set (Q1–Q8)

Write these down in `bench/queries.sql` and never “tune the engine for a secret query.”

- **Q1 Full scan agg** — `SELECT COUNT(*), SUM(amount_cents) FROM events` (column pruning: 1 col + implied count).
- **Q2 Selective time filter** — count/sum in a 7-day window (row-group skipping should shine).
- **Q3 Low-selectivity filter** — `country = 'US'` (~20–30%): skipping weak, vectorized filter strong.
- **Q4 Group by low-cardinality** — `GROUP BY country, event_type` (tiny hash table, CPU bound).
- **Q5 Group by high-cardinality** — `GROUP BY user_id` with a filter (hash table pressure).
- **Q6 Filter + group + top-N** — the “resume query” in §6.
- **Q7 String predicate** — `event_type IN (...)` (dictionary / byte compare path).
- **Q8 Wide projection vs narrow** — `SELECT *` vs 2 columns, same filter, to demo pruning.

Each query has:

- SQL text
- expected shape (columns, rough row count)
- what optimization it is meant to showcase
- a Postgres equivalent for the oracle

---

## 8. Optimizer (rule-based, v1)

No cost model. Fixed pipeline, each rule idempotent-ish:

1. **Bind & expand `*`**
2. **Column pruning** — compute referenced columns from projection, filters, group keys, order keys. Push the set into `Scan.Keep`.
3. **Predicate pushdown** — split `WHERE` into:
   - *pushable*: conjuncts that reference only scan columns and use the supported ops (`=`, `<`, `>`, `BETWEEN`, `IN` on literals)
   - *residual*: everything else, remains a `Filter` operator
4. **Stats-based row-group skipping** — not a plan rewrite so much as scan configuration: attach predicates to the scan; at open-time, evaluate each row group’s min/max.
5. **Trivial constant folding** — `AND TRUE` disappears; `AND FALSE` becomes empty scan. Cheap and looks good in EXPLAIN.
6. **Limit pushdown** (only when there is no `ORDER BY`/`GROUP BY`) — `LIMIT 10` on a raw scan can stop early.

**Explicitly not in v1:** join reordering, magic filter ordering based on selectivity histograms, projection pushdown through aggregates beyond “group keys + agg inputs.”

**EXPLAIN output should show the effect**, e.g.:

```
Limit 20
  Sort COUNT(*) DESC
    HashAggregate keys=[country, event_type] aggs=[count(*), sum(amount_cents)]
      Filter residual=(amount_cents > 0)
        ParquetScan table=events
          files=12  row_groups=800  kept_row_groups=24
          columns=[ts, country, event_type, amount_cents]   -- pruned 6 cols
          pushed=[ts >= ..., country IN (...)]
          bytes_read=...  rows_in=3.1M  rows_out=1.8M
```

If EXPLAIN cannot show pruning/skipping numbers, the optimization is not demoable and does not belong on a resume.

---

## 9. Execution model

### 9.1 Vectorized (primary)

**Pull-based batched volcano.** Each operator implements:

```go
type RecordBatch = arrow.Record  // or a thin wrapper

type Operator interface {
    Open() error
    Next() (RecordBatch, error)  // nil record => EOF
    Close() error
    Stats() OperatorStats
}
```

- Batch size default: **8192 or 16384** rows (make it a flag). Too small → call overhead. Too big → cache misses and huge hash-probe working sets.
- Filters produce a **boolean selection vector** (or Arrow `Boolean` array) and either:
  - **compact** the batch (gather surviving rows), or
  - pass the selection vector downstream (late materialization).
- v1 recommendation: **compact after filter**. Simpler, still a huge win over row-at-a-time. Late materialization is a stretch comment in EXPLAIN later.
- Typed kernels per Arrow type. **No `interface{}` in the inner loop.** A `switch array.DataType().ID()` at batch start, then a tight `for i := 0; i < n; i++` over raw values.
- Nulls: Arrow validity bitmaps. Handle them correctly from day one; retrofitting nulls is misery.

**Hash aggregate.** For each input batch:

- Build a composite key (for 1–3 columns). For strings, hash bytes; consider interning.
- Probe `map[uint64][]int32` with collision lists, or a Swiss-table style if you want extra credit. A plain Go `map` of struct keys is acceptable for v1 if keys are `int64`/`string` — but measure it. For Q5 (`GROUP BY user_id`) the map will dominate.
- Maintain per-group accumulators: `count`, `sum`, `min`, `max`. `AVG` = sum/count at finish.
- Emit one output batch (or several if cardinality is huge).

**Parallelism.** `Scan` lists row groups (or files), `errgroup` with a worker pool (`GOMAXPROCS`), each worker reads a row group into Arrow, runs the *partial* pipeline (filter + local agg), then a merge:

- For non-agg queries: merge batches (order not preserved unless `ORDER BY`).
- For agg queries: **two-phase aggregation** — local hash maps per worker, then merge maps. This is the same idea as Spark’s map-side combine / ClickHouse aggregating merge.

Do not parallelize by spawning a goroutine per row. Parallelize **row groups**.

### 9.2 Row-at-a-time (baseline)

Same logical plan. `Next() (Row, error)` where `Row` is a slice of Go values.

- May still use Parquet as the file format, but **decode to rows immediately** (e.g. read Arrow batch then split, or use a row-oriented parquet decoder path).
- Filter is `if !eval(row) { continue }`.
- Agg is `for row := range ... { m[key].sum += row.amount }`.

Rules for a fair 10x:

- Both executors get the **same** column pruning and row-group skipping (those are storage optimizations, not vectorization). If you only give skipping to the vectorized path, you are measuring two things at once.
- Then add a second experiment: vectorized+optimizations vs row-at-a-time **without** pruning/skipping, and show a breakdown:
  1. row-at-a-time, no skip/prune
  2. row-at-a-time, skip+prune
  3. vectorized, skip+prune
- The resume speedup should **lead with (3) vs (1)** labeled as a *naive row-at-a-time scan*, and the writeup/UI **must** show the full breakdown so an interviewer cannot trap you. The 10x in the placeholder bullet is not a requirement; the labeled measurement is.

### 9.3 What “10x” will actually come from

In order of typical impact on this design:

1. **Not decoding skipped row groups** (can be 10–100× *alone* on Q2).
2. **Not reading unused columns** (often 2–8× on wide tables).
3. **Vectorized filter/agg** (often 3–15× vs row iterators + GC + interface{}).
4. **Parallel row-group scan** (close to core count on scans).

If you only implement (3) and skip (1)(2), the speedup may be small. Implement all four; they are the course. Whatever ratio you measure is the resume ratio.

---

## 10. Repository layout (target)

```
prism-columnar-analytics-engine/
├── IMPLEMENTATION_PLAN.md          # this file (source of truth)
├── README.md                       # demo, build, architecture sketch
├── WRITEUP.md                      # interview script (filled after Phase 11)
├── docs/
│   ├── WINDOWS.md                  # canonical local runbook (Windows)
│   ├── sql.md
│   ├── architecture.md
│   └── benchmarks.md               # how to reproduce measured numbers
├── .gitattributes                  # LF for source files (Windows-safe)
├── docker-compose.yml
├── Dockerfile                      # engine
├── go.mod / go.sum
├── cmd/
│   ├── prism/main.go               # CLI → prism.exe on Windows
│   └── prismd/main.go              # HTTP server → prismd.exe
├── internal/
│   ├── catalog/                    # table registry, schemas, stats cache
│   ├── parquetscan/                # file/rg iteration, skip, prune, Arrow batches
│   ├── sql/
│   │   ├── token.go
│   │   ├── lexer.go
│   │   ├── ast.go
│   │   └── parser.go
│   ├── bind/                       # name resolution, type check
│   ├── plan/                       # logical + physical plan types, pretty print
│   ├── optimizer/                  # rewrite rules + tests
│   ├── exec/
│   │   ├── operator.go             # shared interfaces + stats
│   │   ├── vectorized/             # scan, filter, project, hashagg, sort, limit
│   │   └── row/                    # baseline iterators
│   ├── kernel/                     # typed filter/arithmetic kernels
│   ├── engine/                     # SQL string → result (wires the pipeline)
│   └── server/                     # HTTP handlers
├── web/                            # Next.js app
├── scripts/
│   ├── generate_data.py            # cross-platform
│   ├── load_postgres.py
│   ├── verify_against_postgres.py
│   └── windows/                    # PowerShell entry points (Phase 0)
│       └── prism.ps1
├── bench/
│   ├── queries.sql
│   ├── run.go                      # `go run ./bench` works on Windows
│   └── results/                    # gitignore JSON outputs; keep a sample
├── testdata/                       # tiny parquet fixtures committed to git
└── .github/workflows/ci.yml        # go test + python + web build
```

Optional: a `Makefile` for Linux CI only. **Do not make `make` the documented Windows workflow.**

Keep `internal/` strict: the CLI and HTTP are thin. This is what you walk through in an intern interview.

**Windows path rules for implementers:**

- Always `filepath.Join` / `pathlib.Path` — never `'data/tables/' + name` with assumed `/`.
- Data dir default: `.\data\tables\<table>\*.parquet` (same relative layout as Unix).
- Binaries are `prism.exe` / `prismd.exe`.
- Scripts the user runs must be `.py` (cross-platform) or `.ps1` (Windows). No bash-only `scripts/*.sh` as the sole path.
- `.gitattributes` forces LF on `.go`, `.py`, `.ts`, `.yml` so Git-for-Windows does not checkout CRLF into the compiler.

---

## 11. Phased implementation

Each phase has a **learning goal**, **deliverable**, **tests**, and a **done when**. Do not start Phase N+1 until Phase N is demoable. Timeboxes are effort, not calendar promises — skip them if they get in the way, keep the sequence.

**Progress:** Phase 0–3 are implemented (`tables`/`describe`, vectorized `--where` filters, zone-map catalog).

---

### Phase 0 — Scaffolding and demo story

**Learning:** project shape, Go modules, Docker, how you will demo this in 5 minutes.

**Do:**

- Go module, `cmd/prism` that prints `prism 0.0.1` (`go run ./cmd/prism` on Windows).
- `docker-compose.yml` with Postgres 16 and a volume for `./data` (Docker Desktop).
- `scripts/windows/prism.ps1` verbs: `setup`, `data-tiny`, `test`, `engine`, `web`, `verify`. Document the same verbs as raw commands in `docs/WINDOWS.md`.
- Optional Makefile for CI — not required to develop.
- `.gitattributes` with LF for source files.
- README: what Prism is, **link `docs/WINDOWS.md` first** for local run, link this plan. (`docs/WINDOWS.md` and `WRITEUP.md` already exist — keep them accurate.)
- Decide data directory: `./data/tables/<table>/*.parquet`.
- CI workflow: `go test ./...` on empty packages is fine.

**Done when:** `docker compose up postgres` works on Windows; `go run ./cmd/prism` works in PowerShell; `docs/WINDOWS.md` matches the actual commands.

---

### Phase 1 — Parquet → Arrow scan (no SQL)

**Learning:** Parquet footer, row groups, column chunks, Arrow RecordBatches, schema.

**Do:**

- Python `generate_data.py` for `tiny` (100K) and `dev` (1M).
- Go: open a parquet file with `apache/arrow-go` parquet reader, print schema, row group count, per-column min/max from metadata.
- `ParquetScan` operator: iterate row groups, read **selected columns** into Arrow records of `batchSize`.
- CLI: `prism scan --table events --columns country,amount_cents --limit 5`.
- Log bytes read if the reader exposes it; if not, approximate from file size × columns fraction.

**Pitfall:** Arrow Go APIs change across major versions. **Pinned: `github.com/apache/arrow-go/v18 v18.0.0`** (Go 1.22). Do not mix `github.com/apache/arrow/go/v12`. Newer `arrow-go` (v18.3+) needs Go 1.23+; v18.7 needs Go 1.25.

**Done when:** you can dump 5 rows of a projected subset, and a test proves that requesting 2 of 10 columns does not allocate arrays for the other 8.

---

### Phase 2 — Catalog + stats cache

**Learning:** table metadata, zone maps.

**Do:**

- On startup, walk `data/tables/*`, infer table names, unify schemas (fail if files disagree).
- Cache row-group stats: `min`, `max`, `nullCount`, `rowCount` per column per row group per file.
- CLI: `prism tables`, `prism describe events` (schema + file count + total rows + sample stats).
- Persist nothing; rebuild is fine at this scale (100M rows of *stats* is tiny — thousands of row groups).

**Done when:** `describe` shows total rows matching the generator, and stats for `ts` look sorted/increasing across row groups. **Shipped in Phase 2–3.**

---

### Phase 3 — Vectorized filter kernels (still no SQL)

**Learning:** selection vectors, null bitmaps, typed tight loops.

**Do:**

- Expression subset as a Go AST you construct in tests (not SQL yet): `col amount_cents > 0 AND country = "US"`.
- Compile to a `Filter` operator over Arrow batches.
- Support: comparisons on int64/float64/utf8/timestamp, `AND`/`OR`/`NOT`, `IS NULL`, `IN` list.
- CLI: `prism scan --where 'amount_cents > 0' --columns country,amount_cents`.

**Tests:** hand-built Arrow batches with nulls, empty batches, all-true, all-false, mixed.

**Done when:** filter matches a Python/pandas predicate on `testdata/events_tiny.parquet` exactly, including null semantics (`NULL > 0` is not TRUE). **Shipped in Phase 2–3** (`scripts/filter_oracle.py` uses PyArrow compute so nulls match SQL, not pandas `NA > 0` → False).

---

### Phase 4 — Hash aggregate + project + sort + limit

**Learning:** hash tables for GROUP BY, two-phase parallel agg later.

**Do:**

- `HashAggregate` with keys and `COUNT/SUM/AVG/MIN/MAX`.
- `Project` (column reorder, simple arithmetic like `amount_cents / 100.0` can wait).
- `Sort` + `Limit` (top-N: do not sort a billion rows if `LIMIT 20` — implement a heap for `ORDER BY x LIMIT k` as soon as it hurts).
- CLI still flag-based is OK: `prism agg --group country --agg count,sum(amount_cents)`.

**Done when:** grouped counts on `tiny` match pandas `groupby().agg()` bit-for-bit (int sums exact; avg within 1e-9 relative or compare via `SUM`/`COUNT` instead).

---

### Phase 5 — SQL lexer, parser, AST, binder

**Learning:** how a query compiler front-end works.

**Do:**

- Lexer with tests (punctuation, numbers, strings, keywords case-insensitive).
- Recursive-descent parser producing AST.
- Pretty-print AST (round-trip-ish tests).
- Binder against catalog: unknown column/table errors, `*` expansion, type check comparisons, validate GROUP BY.
- Error messages with byte offset: `parse error at column 14: expected FROM`.

**Do not** parse SQL you cannot execute. Reject with a clear error (`JOIN not supported in v1`).

**Done when:** a table-driven parser test covers every construct in §6, plus a folder of `testdata/sql/*.sql` that bind against `tiny`.

---

### Phase 6 — Planner + optimizer rules + EXPLAIN

**Learning:** logical vs physical plans; why optimizations are rewrites.

**Do:**

- AST → logical plan (always: Scan → Filter → Project/Aggregate → Sort → Limit).
- Optimizer rules from §8.
- Physical plan: attach scan column lists + pushed predicates.
- `EXPLAIN` text format first; JSON format for the UI (`explain: true` on `/query`).
- Golden tests: SQL in, plan string out.

**Done when:** for Q2, EXPLAIN shows pushed `ts` predicate and a pruned column list. (Actual skipping numbers come once scan uses stats in Phase 7.)

---

### Phase 7 — Row-group skipping + predicate pushdown that actually skips

**Learning:** zone maps; this is the Snowflake micro-partition story.

**Do:**

- Evaluate pushed predicates against row-group min/max:
  - `col > k` skip if `max(col) <= k`
  - `col = k` skip if `k < min or k > max`
  - `col IN (...)` skip if no literal overlaps `[min, max]`
  - `AND` can skip if any conjunct skips; `OR` can skip only if all skip
- Count `row_groups_total` vs `row_groups_read`; surface in stats and EXPLAIN.
- Tests with a fixture file engineered so group 0 is `ts` in 2020 and group 1 is `ts` in 2024; a 2024 predicate must not read group 0 (assert via a test hook / bytes / callback).

**Done when:** Q2 on `dev` reads a small fraction of row groups; test hook proves a skip.

---

### Phase 8 — Wire the engine + row-at-a-time baseline

**Learning:** one pipeline, two backends; fairness in measurement.

**Do:**

- `engine.Run(sql, opts)` → `Result` (Arrow table or JSON rows) + `Profile`.
- `--engine=vectorized|row` flag and HTTP field.
- Row executor reuses binder/optimizer/scan skipping (see §9.2).
- Result serialization: for CLI, table writer; for HTTP, JSON `{ columns, types, rows, profile }`. Cap default returned rows (e.g. 1000) even if agg is small.

**Done when:** same SQL, both engines, identical results on `tiny` for Q1–Q8.

---

### Phase 9 — Parallel execution

**Learning:** morsel-driven / row-group parallelism; partial aggregation.

**Do:**

- Worker pool over row groups.
- Partial + merge aggregation.
- `PRISM_PARALLELISM` env / `--jobs`.
- Careful: result order without `ORDER BY` is undefined; tests must compare as **multisets** (sort before diff) except when `ORDER BY` is present.

**Done when:** Q1 on `dev` scales noticeably 1 → 4 cores (not necessarily linear). Document if Go’s parquet reader serializes on a lock — if it does, parallelize **across files** first.

---

### Phase 10 — PostgreSQL oracle + Python generator at laptop scale

**Learning:** correctness > speed; how to generate tens of millions of rows without melting RAM.

**Do:**

- Batched PyArrow writer for `resume` scale; tqdm progress; deterministic `--seed`.
- `load_postgres.py`: COPY into Postgres for `tiny` and `dev` only (do not load `laptop` scale into Postgres; oracle at `dev` is enough if types match).
- `verify_against_postgres.py`: run each Q1–Q8, compare with type-aware equality (sort if no ORDER BY; floats via exact AVG-from-sums or rounded).
- Generator should also emit a `manifest.json`: row counts, checksum of `sum(event_id)`, min/max ts — engine `describe` should match.

**Done when:** `.\scripts\windows\prism.ps1 verify` (or the documented `py` commands) passes on `tiny` and `dev`. `laptop` scale generation completes on this Windows machine without swapping, and `prism describe events` shows the row count you will actually put on the resume (10M, 50M, 100M — whatever fit).

---

### Phase 11 — Benchmark harness (make the resume number real)

**Learning:** how to measure so an interviewer cannot dunk on you.

**Do:**

- `prism bench --scale=dev|laptop --engine=all --repeat=5`
- Protocol:
  - On Windows **do not** try to drop the OS page cache (`drop_caches` is Linux). Report **hot cache** (median of runs 2–N) and **first-run** separately.
  - Warmup run discarded
  - Report median and p95 of the rest
  - Capture: wall time, rows scanned, rows emitted, row groups skipped, bytes read, peak RSS if easy (`runtime.MemStats` is Go heap only — mention that Arrow buffers may live elsewhere; Arrow Go is often pure Go buffers — check and document)
  - Record hardware in `docs/benchmarks.md` via PowerShell (`Get-CimInstance Win32_Processor`, `Win32_ComputerSystem`, commit SHA)
- Breakdown experiment from §9.2.
- **Do not put a number on the resume until this phase has been run at `laptop` scale.** If you get 4×, the resume says 4×. If you get 30× on Q2 and 6× on Q4, say “up to 30× on selective scans, ~6–10× on group-by” or pick one query and name it. The placeholder 100M/10x is discarded, not stretched.

**Done when:** you have a table you would show in an interview, and the UI can load a checked-in `bench/results/sample.json`.

---

### Phase 12 — HTTP API (`prismd`)

**Do:**

```
GET  /health
GET  /tables
GET  /tables/:name
POST /query      { sql, engine, explain, limit }
POST /bench      { scale, query_id }          # optional, can stay CLI-only
```

- CORS for local Next.js.
- Timeouts (e.g. 60s).
- Errors as JSON `{ error, pos? }`.
- Never return huge JSON results; default `limit` 100 for non-agg, unlimited for small agg results with a hard cap (e.g. 100k).

**Done when:** `curl` a GROUP BY and get JSON + profile.

---

### Phase 13 — Next.js workbench

**Learning:** explaining internals visually is how this project *lands* in a demo.

**Keep the UI modest.** Three pages:

1. **Workbench** — SQL textarea (Monaco is nice, textarea is enough), engine toggle (vectorized / row), Run, results table, error banner, runtime ms.
2. **Plan** — tree from EXPLAIN JSON: operator name, rows in/out, time, skipped row groups, pruned columns. A nested list is enough; a pretty graph (react-flow) is stretch.
3. **Bench** — bar chart of Q1–Q8 vectorized vs row; a second chart for skip rates. Load from sample JSON or live `/bench`.

Visual design: clean dark-ish dashboard, not a design-system science fair. Table names + schema in a sidebar.

**Done when:** a stranger following `docs/WINDOWS.md` can compose-up (or run engine+web natively), open the UI, run Q6, and *see* that most row groups were skipped.

---

### Phase 14 — Polish for portfolio

- README with architecture diagram, demo GIF/screenshot, link to `docs/WINDOWS.md`.
- `docs/architecture.md` in your own words (this is interview prep).
- `docs/WINDOWS.md` updated so every command still works.
- Sample queries in the UI.
- **No license file** unless you later choose one.
- Fill `WRITEUP.md`: what you built, what you measured on this laptop, what you would do next (joins, spill, cost-based).
- Optional: blog post. High ROI for intern recruiting.

---

## 12. Testing strategy

| Layer | How |
|---|---|
| Lexer/parser | Table-driven Go tests; invalid SQL snapshots |
| Binder | Unknown cols, GROUP BY violations |
| Optimizer | Golden logical/physical plans |
| Kernels | Arrow batches with nulls, empty, large |
| Skipping | Fixture parquet with disjoint min/max |
| SQL integration | `tiny` parquet in `testdata/`; Q1–Q8 vs expected JSON |
| Oracle | Python script vs Postgres on `dev` |
| Engine equivalence | vectorized vs row on `tiny` |
| Bench smoke | `prism bench --scale=tiny` in CI (correctness of harness, not perf) |

**Float policy:** prefer comparing `SUM` and `COUNT` to reconstructing `AVG`. If you compare floats, use a relative tolerance and document it.

**Null policy:** match SQL three-valued logic. Write tests. Postgres is the referee.

---

## 13. Frontend and API contracts (so you do not bikeshed later)

`POST /query` request:

```json
{
  "sql": "SELECT country, COUNT(*) FROM events WHERE amount_cents > 0 GROUP BY country",
  "engine": "vectorized",
  "explain": true,
  "limit": 100
}
```

Response:

```json
{
  "columns": ["country", "count"],
  "types": ["utf8", "int64"],
  "rows": [["US", 123], ["CA", 45]],
  "truncated": false,
  "profile": {
    "elapsed_ms": 42.1,
    "engine": "vectorized",
    "rows_read": 1800000,
    "rows_emitted": 20,
    "bytes_read": 12345678,
    "row_groups_total": 800,
    "row_groups_skipped": 776,
    "plan": { "op": "HashAggregate", "children": [ ... ] }
  }
}
```

This JSON *is* the UI spec. If the engine produces it, the frontend is straightforward.

---

## 14. Docker / developer workflow

**Canonical local instructions: [`docs/WINDOWS.md`](docs/WINDOWS.md).** Native Windows + Docker Desktop is the supported setup. WSL2 is a convenience, not a requirement.

```yaml
# docker-compose services (target)
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: prism
      POSTGRES_DB: prism_oracle
    ports: ["5432:5432"]
  engine:
    build: .
    ports: ["8080:8080"]
    volumes: ["./data:/data"]
    environment:
      PRISM_DATA_DIR: /data/tables
  web:
    build: ./web
    ports: ["3000:3000"]
    environment:
      NEXT_PUBLIC_API: http://localhost:8080
```

During development, prefer **native** engine + web (faster iterate) and Docker **only for Postgres**:

```powershell
docker compose up -d postgres
py -3 scripts\generate_data.py --scale tiny
go test ./...
go run .\cmd\prismd
cd web; npm install; npm run dev
py -3 scripts\verify_against_postgres.py --scale tiny
go run .\cmd\prism -- bench --scale=dev
```

The same verbs should exist as `.\scripts\windows\prism.ps1 <verb>`.

Do not require Kubernetes, object storage, Make, or bash.

**Docker Desktop notes (Windows):** cap VM memory in Settings so generation + Postgres + Chrome can coexist; put `.\data` in a Defender exclusion if generation is disk-bound; use Linux containers.

---

## 15. Performance and resume-number rules (non-negotiable)

1. **Measure before you write the bullet.** The 100M / 10x figures are *placeholders*, not a spec the engine must hit.
2. **Name the query.** “Nx on PrismBench Q2 (7-day window, YM rows on this laptop)” is credible. “10x faster analytics” is not.
3. **Name the baseline.** Lead with (3) vs (1) from §9.2; show the breakdown in the UI/writeup.
4. **Single node, this Windows machine, command in `docs/WINDOWS.md`.**
5. If GC noise dominates, document `GOGC` / `GOMEMLIMIT`; do not hide it.
6. If Arrow/Parquet already vectorize internally, your row-at-a-time path must still be a *real* row loop over decoded values, not “the same batch code with batch size 1” only — though `batch size = 1` is a useful extra data point in the writeup.
7. Report **hot cache** on Windows. Do not pretend you dropped page cache.

---

## 16. Suggested demo script (5 minutes)

1. Open UI, show `DESCRIBE` equivalent / schema of `events` at whatever `laptop` scale you actually generated.
2. Run Q1, show column pruning in the plan (1–2 columns read).
3. Run Q2, show a large fraction of row groups skipped and a fast wall time.
4. Toggle engine to **row-at-a-time**, rerun Q2, show the slowdown.
5. Run Q6 (the “resume query”), show GROUP BY result.
6. Flip to Bench page, show the bar chart.
7. Optionally: `EXPLAIN` of a query that **cannot** skip (random `user_id = 42`) and talk about why clustering matters.

That last contrast is what makes this sound like you understand warehouses, not that you called an Arrow API.

---

## 17. Risks and pitfalls

| Risk | Mitigation |
|---|---|
| Arrow Go parquet API is awkward / version churn | Spike in Phase 1; pin version; wrap in `parquetscan` |
| Laptop OOM on large generation | Batched writes; scale ladder 10M → 25M → 50M → 100M; stop before swap |
| “10x” fails because baseline also uses Arrow batches | Force the row engine through a per-row interface; profile both; **use the measured ratio** |
| Skipping does nothing because data is shuffled | Sort by `ts` at generation; document clustering |
| Hash agg on `user_id` explodes RAM | Q5 stays filtered; memory budget + error “group cardinality too high” |
| Parser rabbit hole | Freeze dialect in §6; reject the rest |
| UI swallows the semester | Cap at three pages; CLI-only already matches the engine bullet |
| Comparing to Postgres unfairly | Oracle only in v1; resume speedup is internal |
| Nulls/UTF-8/timezone bugs | Postgres oracle; timestamps as UTC ms |
| Parallelism no-op due to reader lock | Parallelize files first; measure |
| Windows path / CRLF / `make` breakage | `filepath`, `.gitattributes` LF, PowerShell runbook, no bash-only scripts |
| Defender scanning every Parquet write | Exclude `.\data`; document in `docs/WINDOWS.md` |
| Docker Desktop RAM fight with Chrome + Go | Compose Postgres only while developing; cap Docker VM memory |

---

## 18. What “done” means for v1

You can claim v1 when **all** of the following are true:

- [ ] Q1–Q8 run on a generated `events` table at the largest `laptop` scale that this Windows machine can handle (document the row count; 100M is not required).
- [ ] Vectorized path is correct vs row path on `tiny`, and vs Postgres on `dev`.
- [ ] EXPLAIN ANALYZE reports column prune + row-group skip counts that match reality.
- [ ] Bench harness produces a checked-in sample and a documented `laptop` run in `docs/benchmarks.md`.
- [ ] CLI + `prismd` + a three-page workbench that can run SQL and show a plan.
- [ ] `docs/WINDOWS.md` can get a Windows clone to a first query without WSL or Make.
- [ ] `WRITEUP.md` filled with measured numbers (query named, baseline named, hardware named).
- [ ] You can explain, without notes: Parquet row groups, why sorting `ts` helps, what a selection vector is, and how two-phase aggregation works.

The resume line is then updated to the **measured** numbers. The placeholder 100M/10x is deleted.

---

## 19. Mapping to the original concept list

| Prompt concept | Where it lives |
|---|---|
| Columnar storage | Parquet files + Arrow batches |
| Storage engine design | `parquetscan` + catalog stats (read-only “engine”) |
| Query planning | AST → logical → physical |
| Query optimization | Rule-based optimizer |
| Predicate pushdown | Scan-attached predicates |
| Column pruning | `Scan.Keep` |
| Vectorized execution | `exec/vectorized` |
| Compression | Parquet zstd/snappy (use, do not reimplement) |
| Memory management | Streaming batches, agg memory, no full-table load |
| Parallel query execution | Row-group/file workers + agg merge |
| Data partitioning | Time-sorted files; optional hive dirs as stretch |
| OLAP workloads | PrismBench Q1–Q8 |
| Execution pipelines | Batched volcano |
| Performance optimization | Bench-driven; skip/prune/vectorize/parallel |

Industry analogues to mention in a writeup (you are not reimplementing these products): Snowflake micro-partitions, BigQuery columnar + capac, Databricks Photon/Spark whole-stage codegen vs vectorization, DuckDB vectorized engine, Velox, ClickHouse sparse indexes / granules.

---

## 20. Locked decisions

Answered and frozen. Do not re-litigate during v1. Implementation PRs check §18 and do not expand dialect until those boxes are ticked.

| # | Question | Decision |
|---|---|---|
| 1 | Engine language | **Go.** Not Rust. |
| 2 | Next.js in v1 | **Yes — thin 3-page workbench** (workbench, plan, bench). |
| 3 | DuckDB baseline | **No for v1.** Stretch only, later. |
| 4 | Inner hash joins | **Stretch.** Still generate `users` and `products`. |
| 5 | Extra SQL (`HAVING`, `CASE`, `date_trunc`, `DISTINCT`) | **None in v1.** Dialect in §6 is frozen. |
| 6 | Writes / INSERT SQL | **No.** Generator writes Parquet; catalog walks the data dir (optional `prism register`). |
| 7 | Hardware | **Personal Windows laptop only.** No external/cloud box. Placeholder 100M/10x are replaced after measurement. Scale = largest `laptop` size that does not swap. |
| 8 | Dataset | **Synthetic `events` (+ `users` / `products`).** Not NYC Taxi / TPC-H in v1. |
| 9 | Resume numbers | **Placeholders.** Rewrite after Phase 11 with named query, named baseline, measured rows and speedup. |
| 10 | License | **None.** Do not add `LICENSE`. |
| 11 | Writeup + Windows runbook | **`WRITEUP.md`** (fill after measurement) and **`docs/WINDOWS.md`** (canonical local setup, created now). |
| 12 | Name | **Prism.** |
| 13 | This plan | **Source of truth.** Implementation follows §21. |

Also locked from recommendations you deferred:

- **Postgres is oracle-only in v1** (not a UI bench competitor).
- Resume speedup **leads with vectorized+optimizations vs naive row-at-a-time without skip/prune**, with the full 3-way breakdown in the writeup/UI.
- **Native Windows is first-class.** WSL2 optional. No Make/bash as the only workflow.

---

## 21. Implementation sequence (checklist)

Use this as the actual build order:

1. Phase 0 scaffold (Windows runbook commands must work as they are implemented)
2. Phase 1 parquet scan + Python tiny generator
3. Phase 2 catalog/stats
4. Phase 3 vectorized filter
5. Phase 4 agg/sort/limit
6. Phase 5 SQL
7. Phase 6 plans + EXPLAIN
8. Phase 7 skipping that you can prove
9. Phase 8 dual engine
10. Phase 9 parallel
11. Phase 10 scale + oracle (stop at the laptop's real max)
12. Phase 11 bench (then rewrite the resume number)
13. Phase 12 API
14. Phase 13 UI
15. Phase 14 polish (`WRITEUP.md`, keep `docs/WINDOWS.md` accurate)

**Vertical slices you can demo early:** after Phase 4 you already have a columnar engine without SQL; after Phase 8 you have the resume technical core; after Phase 11 you have the resume *numbers*; after Phase 13 you have the portfolio piece.

---

## 22. Notes for whoever implements this (including a future coding agent)

- Prefer small, tested packages over a clever one-file engine.
- Do not add a query-planner framework, LLVM, or a custom file format.
- Every optimization needs a test that would fail if the optimization were a no-op.
- When Arrow APIs fight you, wrap them; do not leak parquet types above `parquetscan`.
- Match Postgres null semantics rather than inventing “nice” ones.
- If a phase is taking too long, cut UI polish and join dreams, not tests or EXPLAIN stats.
- **Windows:** `filepath.Join`, LF via `.gitattributes`, PowerShell + Python entry points, `prism.exe`, no `drop_caches`, no bash-only scripts, Docker Desktop for Postgres.
- **Do not add a license file.**
- **Do not add DuckDB or JOIN** until §18 is complete.
- Update `docs/WINDOWS.md` in the same PR whenever you add a command the user must run.

Implementation starts at Phase 0 on a feature branch and moves down the checklist.
