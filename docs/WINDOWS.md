# Running Prism on Windows

This is the **canonical local setup** for Prism. The engine is meant to be developed and demoed on a personal Windows laptop — not a Linux server and not WSL.

Native PowerShell + Docker Desktop is first-class. WSL2 is optional.

> **Status (Phase 0–12):** CLI through `sql` / `explain` / `bench` works, and `prismd` serves `/health` `/tables` `/query` on `http://127.0.0.1:8080`. Dual engine, `--jobs`, Postgres oracle, and the hot-cache bench harness are on. The Next.js workbench is Phase 13. Resume timings stay placeholders until you run `--scale laptop` on this machine.

---

## What you need

Install these on Windows (Win10/11, 64-bit). **winget** is the default path; the websites work too.

| Tool | Why | Install |
|---|---|---|
| Git for Windows | clone, line endings | `winget install Git.Git` |
| Go 1.22+ | engine (`apache/arrow-go` **v18.0.0** is pinned; it supports Go 1.22) | `winget install GoLang.Go` |
| Python 3.11+ | data generator | `winget install Python.Python.3.12` |
| Docker Desktop | PostgreSQL oracle (`prism.ps1 verify`) | `winget install Docker.DockerDesktop` |

Node.js is **not** needed until Phase 13 (workbench).

Then **restart the terminal** (and reboot once after Docker Desktop if it asks).

Confirm in **PowerShell**:

```powershell
git --version
go version
py --version
docker version
docker compose version
```

Notes:

- Use the `py` launcher (`py -3`), not a bare `python` that might point at the Windows Store stub.
- Docker Desktop must be running (whale in the tray) and set to **Linux containers** when you use Postgres.
- You do **not** need Make, Git Bash, MinGW, or WSL to follow this guide.
- PowerShell 7 is nicer (`winget install Microsoft.PowerShell`) but Windows PowerShell 5.1 is enough.

### Optional: Windows Defender exclusion

Parquet generation writes many large files. If disk is mysteriously slow, exclude the repo’s `data` folder:

1. Windows Security → Virus & threat protection → Manage settings → Exclusions
2. Add folder: `<repo>\data`

### Optional: Docker Desktop memory

Settings → Resources → Memory. Leave room for Chrome + the Go process. While developing, run **Postgres only** in Docker (see below), not a full compose stack.

---

## Clone

```powershell
git clone https://github.com/AnishK05/prism-columnar-analytics-engine.git
cd prism-columnar-analytics-engine
```

Source files are LF (`.gitattributes`). Do not set `core.autocrlf=true` for this repo if it rewrites Go files to CRLF. If `go test` ever complains about `\r` in strings, run:

```powershell
git config core.autocrlf false
git checkout -- .
```

---

## Layout

```
.\data\tables\<table>\*.parquet   # generated (gitignored); run the generator
.\testdata\tables\                # tiny committed fixture (scan works immediately)
.\cmd\prism\                      # CLI → prism.exe
.\scripts\generate_data.py
.\scripts\windows\prism.ps1
.\docker-compose.yml              # Postgres 16
```

All Go/Python code uses `filepath` / `pathlib`. Do not hardcode `/`.

---

## Everyday workflow

### 0. Helper script (optional)

```powershell
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
.\scripts\windows\prism.ps1 setup
.\scripts\windows\prism.ps1 test
.\scripts\windows\prism.ps1 data-tiny
.\scripts\windows\prism.ps1 verify
.\scripts\windows\prism.ps1 bench-dev
.\scripts\windows\prism.ps1 engine
```

The `.ps1` file wraps the commands below. Raw `go` / `py` is the source of truth.

### 1. Install Python deps

```powershell
py -3 -m pip install -r .\scripts\requirements.txt
```

### 2. Tests (no extra data required)

```powershell
go test ./...
```

### 3. Scan the committed fixture (no generator)

A small `events` table lives in `testdata\tables` (8,192 rows, sorted by `ts`, multiple row groups):

```powershell
go run .\cmd\prism -- version
go run .\cmd\prism -- tables --data-dir testdata\tables
go run .\cmd\prism -- describe events --data-dir testdata\tables
go run .\cmd\prism -- describe events --json --data-dir testdata\tables
go run .\cmd\prism -- inspect --data-dir testdata\tables --table events
go run .\cmd\prism -- scan --data-dir testdata\tables --table events --columns country,amount_cents --limit 5
go run .\cmd\prism -- scan --data-dir testdata\tables --table events --where "amount_cents > 0 AND country = 'US'" --columns country,amount_cents --limit 10
py -3 .\scripts\filter_oracle.py --data-dir testdata\tables --expect 30
go run .\cmd\prism -- agg --data-dir testdata\tables --table events --group country --agg "count,sum(amount_cents)" --order count --desc --limit 10
go run .\cmd\prism -- sql --data-dir testdata\tables "SELECT country, COUNT(*) FROM events GROUP BY country ORDER BY COUNT(*) DESC LIMIT 5"
go run .\cmd\prism -- explain --data-dir testdata\tables --file testdata\sql\ok\q2.sql
go run .\cmd\prism -- sql --data-dir testdata\tables --explain --file testdata\sql\ok\q2.sql
go run .\cmd\prism -- explain --analyze --data-dir testdata\tables --file testdata\sql\ok\q2.sql
go run .\cmd\prism -- sql --engine=row --jobs=1 --data-dir testdata\tables --file testdata\sql\ok\q1.sql
go run .\cmd\prism -- sql --jobs=4 --data-dir testdata\tables --file testdata\sql\ok\q1.sql
py -3 .\scripts\agg_oracle.py --data-dir testdata\tables
py -3 .\scripts\verify_manifest.py --data-dir testdata\tables
go run .\cmd\prism -- bench --scale testdata --repeat 3
```

The filter oracle uses PyArrow compute (not pandas) so `NULL > 0` stays unknown, matching the engine. After `go build -o prism.exe .\cmd\prism` you can run `.\prism.exe` with the same flags.

SQL dialect: [docs/sql.md](sql.md). `--engine=row` is the fair speedup baseline (same column prune and row-group skip as vectorized). `--jobs N` or `$env:PRISM_PARALLELISM` runs a worker per row group; each worker opens its own Parquet file handle (no shared Arrow reader lock). Without `ORDER BY`, result order is undefined.

### 4. Generate larger tables

```powershell
py -3 .\scripts\generate_data.py --scale tiny
# later: --scale dev      (1M rows)
# later: --scale laptop   (starts at 10M; then --rows 25000000 / 50000000 if it fits)
```

Output: `.\data\tables\events\*.parquet` plus `users` and `products`, and `.\data\tables\manifest.json` (row count, `sum(event_id)`, min/max `ts`). Then:

```powershell
go run .\cmd\prism -- inspect --table events
go run .\cmd\prism -- describe events
go run .\cmd\prism -- scan --table events --columns country,amount_cents --limit 5
py -3 .\scripts\verify_manifest.py --data-dir data\tables
```

Default data dir is `.\data\tables`, or set `PRISM_DATA_DIR`.

Do **not** build one giant pandas DataFrame. The generator writes Parquet in batches and shows a tqdm bar.

### 5. Postgres oracle (tiny / dev only)

```powershell
docker compose up -d postgres
py -3 .\scripts\load_postgres.py --scale testdata
py -3 .\scripts\verify_against_postgres.py --scale testdata
py -3 .\scripts\load_postgres.py --scale tiny
py -3 .\scripts\verify_against_postgres.py --scale tiny
# after generating --scale dev:
py -3 .\scripts\load_postgres.py --scale dev
py -3 .\scripts\verify_against_postgres.py --scale dev
```

Or `.\scripts\windows\prism.ps1 verify` (compose up, testdata + tiny, and dev if `data\tables` is already a 1M generate).

Postgres: `localhost:5432`, user `postgres`, password `prism`, db `prism_oracle`. Override with `DATABASE_URL`.

**Do not** load `--scale laptop` into Postgres. The loader refuses it. Oracle at `dev` is enough.

Stop later with `docker compose down`.

### 6. Benchmarks

See **[docs/benchmarks.md](benchmarks.md)**. Short version:

```powershell
go run .\cmd\prism -- bench --scale testdata --engine all --repeat 3
go run .\cmd\prism -- bench --scale dev --engine all --repeat 5 --out .\bench\results\dev.json
```

Hot cache only on Windows (no `drop_caches`). Warmup discarded; first-run and hot median/p95 are reported separately. The 3-way breakdown is row-naive / row-opt / vectorized.

### 7. HTTP API (`prismd`)

Native engine (not Docker) while iterating. Default listen is loopback so Windows does not prompt for a public firewall exception.

```powershell
go run .\cmd\prismd -- --listen 127.0.0.1:8080 --data-dir testdata\tables
# or: .\scripts\windows\prism.ps1 engine
```

In a second PowerShell:

```powershell
curl.exe http://127.0.0.1:8080/health
curl.exe http://127.0.0.1:8080/tables
curl.exe http://127.0.0.1:8080/tables/events
$body = '{"sql":"SELECT country, COUNT(*) FROM events GROUP BY country ORDER BY COUNT(*) DESC LIMIT 5","engine":"vectorized","explain":true}'
curl.exe -s -X POST http://127.0.0.1:8080/query -H "Content-Type: application/json" -d $body
Invoke-RestMethod -Method POST -Uri http://127.0.0.1:8080/query -ContentType application/json -Body $body
```

Contract: [`docs/api.md`](api.md). CORS is `*` so the Phase 13 workbench on `:3000` can call this process. Query timeout defaults to 60s. Scan queries without `LIMIT` are capped at 100 rows (`truncated: true`); aggregates are capped at 100k.

`GET /bench` returns `bench/results/sample.json` (UI format). Live benches stay on `prism bench`.

### Not in this phase

| Command | When |
|---|---|
| Next.js workbench | Phase 13 |

---

## Scale on a laptop

There is no extra hardware. Climb this ladder and **stop before the machine swaps**:

| `--scale` | rows (`events`) | use |
|---|---|---|
| `testdata` | 8,192 | committed fixture / CI |
| `tiny` | 100K | first local generate |
| `dev` | 1M | later benches |
| `laptop` | start 10M, then 25M / 50M / 100M | resume number, if it fits |

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| `python` opens the Microsoft Store | Use `py -3` |
| `go` / `py` not found after install | Close the terminal, open a new one; check PATH |
| `docker compose` fails, daemon not running | Start Docker Desktop; wait until it is idle |
| `error during connect: ... pipe` | Docker Desktop not up, or Linux engine not selected |
| Postgres `connection refused` | `docker compose ps`; port 5432 not stolen by a local Postgres |
| Antivirus CPU pegged while generating | Defender exclusion on `.\data` |
| `go test` weird parse errors / `\r` | LF checkout; see Clone section |
| Execution policy on `.ps1` | `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned` |
| Path too long | Keep the clone near `C:\src\prism`, not a deep OneDrive path |
| OneDrive locking parquet files | Clone outside OneDrive/Documents if files go “online-only” |
| `unknown column` | `inspect` prints the schema; names are case-sensitive |

---

## WSL2 (optional, not required)

If you already live in Ubuntu-on-Windows, you can develop there with the same `go` / `python3` / `docker` commands, using `/` paths. Do **not** make WSL the only documented path.

Do not put the repo on `/mnt/c/...` and compile from WSL if you can avoid it (filesystem performance). Clone a second copy inside the WSL filesystem, or just use native Windows.

---

## What we are not doing on Windows

- Requiring Make, bash, or MSYS
- `echo 3 > /proc/sys/vm/drop_caches` (Linux-only; benches are hot-cache)
- Assuming `/tmp` or `chmod +x`
- Shipping only `scripts/*.sh`
- A license file
- DuckDB as a dependency
