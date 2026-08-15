# Prism benchmarks

How to measure Prism so an interviewer cannot dunk on the methodology.

**Resume numbers stay placeholders until a `laptop`-scale run is recorded on the Windows development machine.** This file and `bench/results/sample.json` exist so the harness and UI schema are real. Do not copy the checked-in testdata timings onto a resume.

## Protocol (locked)

- **Do not** drop the OS page cache on Windows (`drop_caches` is Linux-only).
- Report **first-run** and **hot cache** separately.
- One **warmup** is discarded.
- `--repeat=N` measured runs follow the warmup.
- `first_run_ms` = measured run 1.
- `hot_median_ms` / `hot_p95_ms` = median and p95 of measured runs 2–N (if `N=1`, hot equals first-run).
- Capture wall time, rows scanned, rows emitted, row groups skipped, bytes read.
- `peak_rss_bytes` is Linux `VmHWM` when `/proc` exists; otherwise 0.
- `go_mem_sys_bytes` is `runtime.MemStats.Sys` (Go heap + mapped spans). Arrow Go v18 typically allocates through the Go runtime, but process RSS can still differ.

Three-way breakdown ([IMPLEMENTATION_PLAN.md](../IMPLEMENTATION_PLAN.md) §9.2):

1. `row-naive` — row-at-a-time, `--no-skip --no-prune`
2. `row-opt` — row-at-a-time, skip + prune
3. `vectorized` — vectorized, skip + prune

Lead with **(3) vs (1)**. Show the full breakdown so skip/prune is not silently mixed into “vectorization.”

## Commands (Windows PowerShell)

From the repo root, Docker Desktop is **not** required for benches (Postgres is oracle-only).

```powershell
# fixture smoke (same as CI)
go run .\cmd\prism -- bench --scale testdata --engine all --repeat 3 --out .\bench\results\testdata.json

# 1M-row local bench (generate first if needed)
py -3 .\scripts\generate_data.py --scale dev
go run .\cmd\prism -- bench --scale dev --engine all --repeat 5 --out .\bench\results\dev.json

# laptop scale — stop before the machine swaps; start at 10M, then --rows 25000000 / 50000000
py -3 .\scripts\generate_data.py --scale laptop
go run .\cmd\prism -- bench --scale laptop --engine all --repeat 5 --out .\bench\results\laptop.json
```

Equivalent helper: `.\scripts\windows\prism.ps1 bench-dev`.

`go run .\bench --scale testdata --repeat 3` is the same harness.

## Hardware block (fill on the laptop)

Paste into this section (and into `WRITEUP.md`) after a real `laptop` run:

```powershell
Get-CimInstance Win32_Processor | Select-Object Name, NumberOfCores, NumberOfLogicalProcessors
Get-CimInstance Win32_ComputerSystem | Select-Object Manufacturer, Model, TotalPhysicalMemory
git rev-parse HEAD
go version
```

| Field | Value |
|---|---|
| CPU | TODO |
| Cores / logical | TODO |
| RAM | TODO |
| Disk | TODO |
| OS | Windows |
| Go | 1.22.x |
| Commit | TODO |
| `events` rows | TODO (10M / 25M / 50M / 100M — whatever fit) |
| Query named on the resume | TODO |
| Baseline | vectorized+opt vs row-naive (no skip/prune) |

## Interview table (fill after laptop)

| Query | What it shows | Rows | Vectorized hot ms | Row-naive hot ms | Row-opt hot ms | Speedup vs naive |
|---|---|---|---|---|---|---|
| Q1 | column prune | TODO | | | | |
| Q2 | time skip | | | | | |
| Q3 | filter-heavy | | | | | |
| Q4 | low-card GROUP BY | | | | | |
| Q5 | high-card GROUP BY | | | | | |
| Q6 | resume-style | | | | | |
| Q7 | string `IN` | | | | | |
| Q8 / Q8-wide | narrow vs `SELECT *` | | | | | |

JSON the UI can load: [`bench/results/sample.json`](../bench/results/sample.json) (testdata scale, format only).
