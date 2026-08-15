# Prism — Columnar Analytics Engine

## Implementation Plan

This document is the working blueprint for **Prism**: a miniature, single-node OLAP engine. The goal is not to compete with DuckDB or ClickHouse. The goal is to *build enough of a real analytical engine* that you can explain, demonstrate, and benchmark the internals that those systems are built on.

**Resume target (tunable, must stay honest):**

> Engineered a vectorized, single-node OLAP engine in Go querying Parquet via Apache Arrow, with predicate pushdown, column pruning, and row-group skipping, sustaining 100M+ rows/query at 10x a row-at-a-time baseline

Treat that sentence as a *product spec*, not marketing. Every phase below exists to make that sentence true, measurable, and defensible in an interview.

---

## 1. North star

Build a query engine that:

1. Stores tables as **Apache Parquet** files on local disk (columnar on disk).
2. Executes a useful **SQL subset** over those tables.
3. Uses **Apache Arrow RecordBatches** as the in-memory execution format (columnar in RAM).
4. Runs a **vectorized** operator pipeline (thousands of rows per call, not one).
5. Applies three storage-aware optimizations that actually matter at 100M+ rows:
   - **column pruning**
   - **predicate pushdown**
   - **row-group skipping** using Parquet min/max statistics
6. Ships a **row-at-a-time baseline executor** so the 10x claim is an apples-to-apples measurement on the *same engine*, not a vibes comparison to PostgreSQL.
7. Checks **correctness** against PostgreSQL (and optionally DuckDB/pandas) on the same SQL.
8. Exposes a small **HTTP API + Next.js workbench** so you can type SQL, see the physical plan, see which row groups were skipped, and look at timings.

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
- Naive row-at-a-time executor for the *same* logical plans (the 10x baseline).
- Optimizer: column pruning, predicate pushdown, row-group skipping. Optional: simple filter reordering.
- Aggregates: `COUNT`, `COUNT(*)`, `SUM`, `AVG`, `MIN`, `MAX`.
- `GROUP BY` on 1–3 columns (typed keys: int64, string, date/timestamp, bool).
- `EXPLAIN` / `EXPLAIN ANALYZE` with per-operator timing, rows in/out, bytes read, row groups skipped.
- Python data generator (NumPy / PyArrow) that can emit 100M+ row fact tables.
- PostgreSQL as a **correctness oracle** (same SQL, compare results).
- Docker Compose for Postgres + engine + web UI.
- Next.js query workbench: editor, results grid, plan visualizer, benchmark page.
- Benchmark harness with a small suite of named queries (PrismBench Q1–Q8).
- Tests: parser, planner, optimizer rewrites, operator correctness, golden SQL results.

### 3.2 Stretch (v1.5 — only after v1 is fast and correct)

- Inner **hash join** on equi-join keys (enables a 2-table star schema).
- `COUNT(DISTINCT …)` and `HAVING`.
- Dictionary-encoded group keys / Arrow dictionary arrays.
- Spill-to-disk for aggregations that exceed a memory budget.
- Partition directories (`date=2024-01-01/...`) and partition pruning.
- Simple cost-based choices (e.g. broadcast vs build side — only relevant once joins exist).
- DuckDB as a *second* baseline in the bench suite (very educational, not required).

### 3.3 Out of scope (do not build)

- Distributed execution, shuffle, consensus, multi-node.
- Inserts/updates/deletes, compaction, transactions.
- Full SQL: window functions, CTEs, subqueries, correlated subqueries, `UNION`, `CASE` (unless a tiny `CASE` sneaks in later), UDFs.
- Cost-based optimizer with histograms/MCVs (zone maps are enough).
- Custom Parquet writer / custom compression codec.
- SIMD kernels written in assembly, GPU, JIT (Cranelift/LLVM).
- Auth, multi-tenancy, a real catalog service, object storage as a hard dependency.
- Matching DuckDB or Postgres on *every* query. We win on *scan + filter + group-by* over Parquet. That is the point.

> **Open question:** Are inner hash joins part of the resume-worthy core, or stretch? Recommendation: **stretch**. Scan/filter/agg is enough to make the 10x claim and teach the important internals. Joins are a second project’s worth of hash-table, build/probe, and memory-spill work. Include a *schema that could support joins* (dimension tables generated) so adding them later is natural.

---

## 4. Tech stack and why

| Piece | Choice | Why |
|---|---|---|
| Engine language | **Go** | Matches the resume line. Fast enough, excellent goroutines for parallel scans, official Apache Arrow Go, faster iteration than Rust for an undergrad timeline. |
| In-memory format | **Apache Arrow** (`apache/arrow-go`) | Industry-standard columnar batches; zero-copy-ish handoff from Parquet; the whole point of “vectorized.” |
| On-disk format | **Apache Parquet** | Row groups + column chunks + page stats are *the* teaching vehicle for pruning/skipping/pushdown. |
| SQL parser | **Hand-rolled recursive descent for a tiny dialect** | You learn query compilation. A Vitess/TiDB parser hides that and pulls in a huge surface you will not execute. |
| Correctness oracle | **PostgreSQL 16** in Docker | Interview-friendly: “I validated results against Postgres.” |
| Data generation / extra checks | **Python 3 + NumPy + PyArrow** | Generate Parquet identically to what the engine reads; optional pandas/DuckDB cross-check. |
| API | **Go `net/http`** (stdlib or a thin chi/echo) | `POST /query`, `POST /explain`, `GET /tables`. |
| UI | **Next.js + TypeScript** | Query workbench + plan viz + charts. Keep it thin. |
| Charts | **Recharts** (or Chart.js) | Benchmark bars: vectorized vs row-at-a-time vs Postgres. |
| Containerization | **Docker Compose** | One-command demo: generate data, boot engine, boot UI, boot Postgres. |
| Bench / CI | **Go tests + a `make bench`** | Reproducible numbers; store a results JSON the UI can plot. |

**Go vs Rust.** Rust would be a slightly more “systems” flex (memory, SIMD, no GC pauses) and is what DuckDB-adjacent people expect. It is also slower to get a parser + HTTP + UI glue working, and Arrow/Parquet in Rust (`arrow-rs`) has a steeper API. **Default: Go.** If you specifically want Rust intern-signal, switch before Phase 1 — do not rewrite later.

> **Open question:** Confirm **Go** as the engine language (recommended, matches resume) vs **Rust**. This is the one decision that is expensive to reverse.

**PostgreSQL’s role.** Postgres is **not** the storage engine. Prism never queries Postgres at runtime for user SQL. Postgres is:

1. A correctness oracle in tests (`scripts/verify_against_postgres.py`).
2. An optional *external* baseline in the benchmark page, with a huge caveat: Postgres will be row-oriented heap scans unless we also load `pg_columnar` / `cstore` / `parquet_fdw`, which we will **not** do in v1. Comparing a specialized Parquet scanner to vanilla Postgres is directionally interesting and interview-useful if you *say the comparison is unfair in Postgres’s favor on OLTP and unfair in Prism’s favor on analytics*. The **10x number on the resume must come from Prism-vectorized vs Prism-row-at-a-time**, not vs Postgres.

> **Open question:** Do you want Postgres as oracle-only (recommended), or also as a published benchmark competitor in the UI? Recommendation: oracle always; competitor as a clearly labeled “external baseline.”

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

> **Open question:** Any must-have SQL you want in v1 that is missing (`HAVING`, `CASE`, date trunc, joins)? Recommendation: none of those in v1. Add `date_trunc('day', ts)` later if the UI demos feel weak — it is a nice grouping key.

---

## 7. Dataset and workload (PrismBench)

Do **not** start by downloading a random CSV. Generate a dataset whose statistics *make skipping interesting*.

### 7.1 Tables

**`events`** — fact table, the 100M+ row star. Rough schema:

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

### 7.3 Scale targets

| alias | rows in `events` | when to use |
|---|---|---|
| `tiny` | 100K | unit tests, CI |
| `dev` | 1M | local UI, fast benches |
| `resume` | **100M** | the number on the resume |
| `stress` | 500M–1B | optional, if you have disk/RAM |

**Memory reality check.** 100M rows × 10 columns × ~8 bytes ≈ 8 GB uncompressed. You will **not** load it all. The engine streams row groups. Peak RAM should be `parallelism × batch_size × projected_columns`, plus hash-agg state. Design for a **16 GB laptop**. If 100M does not fit comfortably during *generation*, generate as many 10M files and concatenate; do not hold a pandas DataFrame of 100M rows. Use PyArrow streaming / batched writes.

> **Open question:** What machine will you demo on (RAM, CPU cores)? If it is an 8 GB laptop, make `resume` 50M or keep 100M but be ruthless about streaming and a smaller row-group parallelism. The resume number is allowed to change; **honesty + a live demo** beat an OOM.

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
- The resume “10x a row-at-a-time baseline” should cite **(3) vs (2)** or **(3) vs (1)** explicitly. **(3) vs (2)** is the cleaner “vectorization” claim. **(3) vs (1)** is the cleaner “engine” claim. Pick one and label it.

> **Open question:** Which 10x baseline do you want on the resume — vectorization-only (same skip/prune), or full-engine vs naive? Recommendation: **lead with (3) vs (1)** on the resume (bigger, still honest if labeled “naive row-at-a-time scan”), and show the breakdown in the writeup/UI so an interviewer cannot trap you.

### 9.3 What “10x” will actually come from

In order of typical impact on this design:

1. **Not decoding skipped row groups** (can be 10–100× *alone* on Q2).
2. **Not reading unused columns** (often 2–8× on wide tables).
3. **Vectorized filter/agg** (often 3–15× vs row iterators + GC + interface{}).
4. **Parallel row-group scan** (close to core count on scans).

If you only implement (3) and skip (1)(2), you may miss 10×. Implement all four; they are the course.

---

## 10. Repository layout (target)

```
prism-columnar-analytics-engine/
├── IMPLEMENTATION_PLAN.md          # this file
├── README.md                       # demo, build, architecture sketch
├── Makefile
├── docker-compose.yml
├── Dockerfile                      # engine
├── go.mod / go.sum
├── cmd/
│   ├── prism/main.go               # CLI
│   └── prismd/main.go              # HTTP server
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
│   ├── generate_data.py
│   ├── load_postgres.py
│   └── verify_against_postgres.py
├── bench/
│   ├── queries.sql
│   ├── run.go                      # or scripts/bench.sh wrapping `prism bench`
│   └── results/                    # gitignore JSON outputs; keep a sample
├── testdata/                       # tiny parquet fixtures committed to git
├── docs/
│   ├── sql.md
│   ├── architecture.md
│   └── benchmarks.md               # how to reproduce resume numbers
└── .github/workflows/ci.yml        # go test + python lint + web build
```

Keep `internal/` strict: the CLI and HTTP are thin. This is what you walk through in an intern interview.

---

## 11. Phased implementation

Each phase has a **learning goal**, **deliverable**, **tests**, and a **done when**. Do not start Phase N+1 until Phase N is demoable. Timeboxes are effort, not calendar promises — skip them if they get in the way, keep the sequence.

---

### Phase 0 — Scaffolding and demo story

**Learning:** project shape, Go modules, Docker, how you will demo this in 5 minutes.

**Do:**

- Go module, `cmd/prism` that prints `prism 0.0.1`.
- `docker-compose.yml` with Postgres 16 and a volume for `./data`.
- `Makefile`: `make engine`, `make test`, `make web`, `make up`, `make data-tiny`.
- README skeleton: what Prism is, how to run, link to this plan.
- Decide data directory: `./data/tables/<table>/*.parquet`.
- CI workflow: `go test ./...` on empty packages is fine.

**Done when:** `docker compose up postgres` works; `go run ./cmd/prism` works.

---

### Phase 1 — Parquet → Arrow scan (no SQL)

**Learning:** Parquet footer, row groups, column chunks, Arrow RecordBatches, schema.

**Do:**

- Python `generate_data.py` for `tiny` (100K) and `dev` (1M).
- Go: open a parquet file with `apache/arrow-go` parquet reader, print schema, row group count, per-column min/max from metadata.
- `ParquetScan` operator: iterate row groups, read **selected columns** into Arrow records of `batchSize`.
- CLI: `prism scan --table events --columns country,amount_cents --limit 5`.
- Log bytes read if the reader exposes it; if not, approximate from file size × columns fraction.

**Pitfall:** Arrow Go APIs change across major versions. Pin a version in `go.mod` and document it. Prefer `apache/arrow-go/v18` (or whatever is current stable when you start) and do not mix `github.com/apache/arrow/go/v12`.

**Done when:** you can dump 5 rows of a projected subset, and a test proves that requesting 2 of 10 columns does not allocate arrays for the other 8.

---

### Phase 2 — Catalog + stats cache

**Learning:** table metadata, zone maps.

**Do:**

- On startup, walk `data/tables/*`, infer table names, unify schemas (fail if files disagree).
- Cache row-group stats: `min`, `max`, `nullCount`, `rowCount` per column per row group per file.
- CLI: `prism tables`, `prism describe events` (schema + file count + total rows + sample stats).
- Persist nothing; rebuild is fine at this scale (100M rows of *stats* is tiny — thousands of row groups).

**Done when:** `describe` shows total rows matching the generator, and stats for `ts` look sorted/increasing across row groups.

---

### Phase 3 — Vectorized filter kernels (still no SQL)

**Learning:** selection vectors, null bitmaps, typed tight loops.

**Do:**

- Expression subset as a Go AST you construct in tests (not SQL yet): `col amount_cents > 0 AND country = "US"`.
- Compile to a `Filter` operator over Arrow batches.
- Support: comparisons on int64/float64/utf8/timestamp, `AND`/`OR`/`NOT`, `IS NULL`, `IN` list.
- CLI: `prism scan --where 'amount_cents > 0' --columns country,amount_cents`.

**Tests:** hand-built Arrow batches with nulls, empty batches, all-true, all-false, mixed.

**Done when:** filter matches a Python/pandas predicate on `testdata/events_tiny.parquet` exactly, including null semantics (`NULL > 0` is not TRUE).

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

### Phase 10 — PostgreSQL oracle + Python generator at resume scale

**Learning:** correctness > speed; how to generate 100M rows without melting RAM.

**Do:**

- Batched PyArrow writer for `resume` scale; tqdm progress; deterministic `--seed`.
- `load_postgres.py`: COPY into Postgres for `tiny` and `dev` only (100M into Postgres is optional and slow; oracle at `dev` is enough if types match).
- `verify_against_postgres.py`: run each Q1–Q8, compare with type-aware equality (sort if no ORDER BY; floats via exact AVG-from-sums or rounded).
- Generator should also emit a `manifest.json`: row counts, checksum of `sum(event_id)`, min/max ts — engine `describe` should match.

**Done when:** `make verify` passes on `tiny` and `dev`. `make data-resume` completes on your machine and `prism describe events` shows ≥ 100M rows.

---

### Phase 11 — Benchmark harness (make the resume number real)

**Learning:** how to measure so an interviewer cannot dunk on you.

**Do:**

- `prism bench --scale=dev|resume --engine=all --repeat=5`
- Protocol:
  - drop OS page cache only if you document it (`echo 3 > /proc/sys/vm/drop_caches` — often not allowed; otherwise report **hot cache** and **first-run cold-ish**)
  - warmup run discarded
  - report median and p95 of the rest
  - capture: wall time, rows scanned, rows emitted, row groups skipped, bytes read, peak RSS if easy (`runtime.MemStats` is Go heap only — mention that Arrow buffers may be off-heap / in C allocated memory; Arrow Go is often pure Go buffers — check and document)
- Write `docs/benchmarks.md` with hardware (`lscpu`, RAM), commit SHA, command, table.
- Breakdown experiment from §9.2.
- **Do not put a number on the resume until this phase has been run at `resume` scale.** If you only get 4×, the resume says 4×. If you get 30× on Q2 and 6× on Q4, say “up to 30× on selective scans, ~6–10× on group-by” or pick one query and name it.

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
- Never return 100M JSON rows; default `limit` 100 for non-agg, unlimited for small agg results with a hard cap (e.g. 100k).

**Done when:** `curl` a GROUP BY and get JSON + profile.

---

### Phase 13 — Next.js workbench

**Learning:** explaining internals visually is how this project *lands* in a demo.

**Keep the UI modest.** Three pages:

1. **Workbench** — SQL textarea (Monaco is nice, textarea is enough), engine toggle (vectorized / row), Run, results table, error banner, runtime ms.
2. **Plan** — tree from EXPLAIN JSON: operator name, rows in/out, time, skipped row groups, pruned columns. A nested list is enough; a pretty graph (react-flow) is stretch.
3. **Bench** — bar chart of Q1–Q8 vectorized vs row; a second chart for skip rates. Load from sample JSON or live `/bench`.

Visual design: clean dark-ish dashboard, not a design-system science fair. Table names + schema in a sidebar.

**Done when:** a stranger can `docker compose up`, open the UI, run Q6, and *see* that 90%+ of row groups were skipped.

---

### Phase 14 — Polish for portfolio

- README with architecture diagram, demo GIF/screenshot, reproduce-bench instructions.
- `docs/architecture.md` in your own words (this is interview prep).
- Sample queries in the UI.
- License (MIT is fine).
- A 1-page `WRITEUP.md`: what you built, what you measured, what you would do next (joins, spill, cost-based).
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

Host workflow (primary during development):

```
make data-tiny          # python generator
make test               # go test ./...
make run-engine         # prismd
make run-web            # next dev
make verify             # oracle on tiny
make bench-dev
```

Do not require Kubernetes, object storage, or a Makefile that only works in CI.

---

## 15. Performance and resume-number rules (non-negotiable)

1. **Measure before you write the bullet.** The 100M / 10x figures in the prompt are *targets*, not facts.
2. **Name the query.** “10x on PrismBench Q2 (7-day window, 100M rows)” is credible. “10x faster analytics” is not.
3. **Name the baseline.** See §9.2.
4. **Single node, one machine, command in the repo.**
5. If GC noise dominates, document `GOGC` / `GOMEMLIMIT`; do not hide it.
6. If Arrow/Parquet already vectorize internally, your row-at-a-time path must still be a *real* row loop over decoded values, not “the same batch code with batch size 1” only — though `batch size = 1` is a useful extra data point in the writeup.

---

## 16. Suggested demo script (5 minutes)

1. Open UI, show `DESCRIBE` equivalent / schema of `events` at 100M rows.
2. Run Q1, show column pruning in the plan (1–2 columns read).
3. Run Q2, show 90%+ row groups skipped and a fast wall time.
4. Toggle engine to **row-at-a-time**, rerun Q2, show the slowdown.
5. Run Q6 (resume query), show GROUP BY result.
6. Flip to Bench page, show the bar chart.
7. Optionally: `EXPLAIN` of a query that **cannot** skip (random `user_id = 42`) and talk about why clustering matters.

That last contrast is what makes this sound like you understand warehouses, not that you called an Arrow API.

---

## 17. Risks and pitfalls

| Risk | Mitigation |
|---|---|
| Arrow Go parquet API is awkward / version churn | Spike in Phase 1; pin version; wrap in `parquetscan` |
| 100M row generation OOMs in pandas | Batched writes only; never a giant DataFrame |
| “10x” fails because baseline also uses Arrow batches | Force the row engine through a per-row interface; profile both |
| Skipping does nothing because data is shuffled | Sort by `ts` at generation; document clustering |
| Hash agg on `user_id` explodes RAM | Q5 stays filtered; memory budget + error “group cardinality too high” |
| Parser rabbit hole | Freeze dialect in §6; reject the rest |
| UI swallows the semester | Phase 13 is last; a CLI-only engine already matches the resume line |
| Comparing to Postgres unfairly | Oracle vs competitor labeled; resume 10x is internal |
| Nulls/UTF-8/timezone bugs | Postgres oracle; timestamps as UTC ms |
| Parallelism no-op due to reader lock | Parallelize files first; measure |

---

## 18. What “done” means for v1

You can claim v1 when **all** of the following are true:

- [ ] Q1–Q8 run on a generated `events` table of at least 100M rows (or whatever number you actually measured).
- [ ] Vectorized path is correct vs row path on `tiny`, and vs Postgres on `dev`.
- [ ] EXPLAIN ANALYZE reports column prune + row-group skip counts that match reality.
- [ ] Bench harness produces a checked-in sample and a documented `resume` run.
- [ ] CLI + `prismd` + a workbench that can run SQL and show a plan.
- [ ] README can get a new clone to a first query in a short, boring set of commands.
- [ ] You can explain, without notes: Parquet row groups, why sorting `ts` helps, what a selection vector is, and how two-phase aggregation works.

The resume line is then updated to the **measured** numbers.

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

## 20. Open questions (please answer before / during Phase 0)

These are the decisions that change the plan. Recommendations are included so work can start if you do not care.

### Language and stack

1. **Go or Rust for the engine?**  
   **Recommendation: Go.** Reverse this only if you explicitly want Rust intern-signal and accept slower UI/API glue.

2. **Is Next.js required for v1 or is CLI + EXPLAIN enough for the engine bullet?**  
   **Recommendation: keep a thin Next.js workbench.** It is in the stated tech stack and makes the demo 10× more convincing. Cap it at the three pages in Phase 13.

3. **DuckDB as an extra baseline?**  
   **Recommendation: stretch.** Excellent for curiosity (“we are 0.3× DuckDB on Q4, which is expected”) but easy to demoralize yourself.

### Scope

4. **Inner hash joins in v1?**  
   **Recommendation: no.** Generate `users`/`products` anyway.

5. **Any SQL must-haves beyond §6?** (`HAVING`, `CASE`, `date_trunc`, `DISTINCT`)  
   **Recommendation: `date_trunc` if grouping by day feels necessary for the demo; otherwise no.**

6. **Writes?** Some intern projects add “load a parquet file” as a command.  
   **Recommendation: `prism register` / generator writes files; no INSERT SQL.**

### Data and hardware

7. **Demo hardware (RAM, cores, OS)?** This sets whether 100M is comfortable.  
   **Recommendation: design for 16 GB / 4+ cores; stream everything.**

8. **Dataset flavor?** Synthetic product-analytics `events` (this plan) vs NYC Taxi vs mini-TPC-H.  
   **Recommendation: synthetic `events`.** Full control over clustering, cardinality, and licensing. NYC Taxi is a nice *second* dataset later (“we ran the same engine on public data”).

9. **Resume numbers:** keep 100M+ / 10x as *targets* and replace with measured values?  
   **Recommendation: yes, always.**

### Product / portfolio

10. **Public GitHub, MIT license, your name on README?** Assume yes.

11. **Do you want a blog-style `WRITEUP.md` as part of v1?**  
    **Recommendation: yes.** It is the interview script.

12. **Name / branding:** repo is `prism-columnar-analytics-engine`. Keep **Prism**?  
    **Recommendation: yes.**

13. **Should this plan live as the source of truth, and implementation PRs check boxes in §18?**  
    **Recommendation: yes. Do not expand dialect until v1 is checked.**

---

## 21. Implementation sequence (checklist)

Use this as the actual build order:

1. Phase 0 scaffold  
2. Phase 1 parquet scan + Python tiny generator  
3. Phase 2 catalog/stats  
4. Phase 3 vectorized filter  
5. Phase 4 agg/sort/limit  
6. Phase 5 SQL  
7. Phase 6 plans + EXPLAIN  
8. Phase 7 skipping that you can prove  
9. Phase 8 dual engine  
10. Phase 9 parallel  
11. Phase 10 scale + oracle  
12. Phase 11 bench (then rewrite the resume number)  
13. Phase 12 API  
14. Phase 13 UI  
15. Phase 14 polish  

**Vertical slices you can demo early:** after Phase 4 you already have a columnar engine without SQL; after Phase 8 you have the resume technical core; after Phase 11 you have the resume *numbers*; after Phase 13 you have the portfolio piece.

---

## 22. Notes for whoever implements this (including a future coding agent)

- Prefer small, tested packages over a clever one-file engine.
- Do not add a query-planner framework, LLVM, or a custom file format.
- Every optimization needs a test that would fail if the optimization were a no-op.
- When Arrow APIs fight you, wrap them; do not leak parquet types above `parquetscan`.
- Match Postgres null semantics rather than inventing “nice” ones.
- If a phase is taking too long, cut UI polish and join dreams, not tests or EXPLAIN stats.

When this plan and the answers to §20 are agreed, implementation starts at Phase 0 on a feature branch and moves down the checklist.
