# Run Prism on Windows

This is the **start-here** guide for a personal Windows laptop (Win10/11, 64-bit). Native **PowerShell** is first-class. You do not need WSL, Make, Git Bash, or MinGW.

The longer developer runbook (CLI catalog, generator, Postgres oracle, bench protocol) is [`docs/WINDOWS.md`](docs/WINDOWS.md).

> **v1 software is complete** (engine, `prismd`, three-page workbench). What is still open is a **laptop-scale measurement on this machine** — generate `--scale laptop`, run `prism bench`, then fill `WRITEUP.md`. Do not copy testdata chart timings onto a resume.

---

## 1. Install tools

**winget** is the default path. Restart the terminal after installs (reboot once if Docker Desktop asks).

| Tool | Why | Install |
|---|---|---|
| Git for Windows | clone | `winget install Git.Git` |
| Go 1.22+ | engine (`apache/arrow-go` **v18.0.0** is pinned) | `winget install GoLang.Go` |
| Node.js LTS | Next.js workbench | `winget install OpenJS.NodeJS.LTS` |
| Python 3.11+ | data generator (optional until you scale up) | `winget install Python.Python.3.12` |
| Docker Desktop | Postgres oracle only (optional for the UI demo) | `winget install Docker.DockerDesktop` |

Confirm in **PowerShell**:

```powershell
git --version
go version
node --version
npm --version
py --version
```

Notes:

- Use `py -3`, not a bare `python` (that often opens the Microsoft Store stub).
- PowerShell 5.1 is enough. PowerShell 7 is nicer: `winget install Microsoft.PowerShell`.
- Docker is **not** required to open the workbench or run SQL against the committed fixture.

If a `.ps1` is blocked later:

```powershell
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned
```

---

## 2. Clone

```powershell
git clone https://github.com/AnishK05/prism-columnar-analytics-engine.git
cd prism-columnar-analytics-engine
```

Keep the clone near `C:\src\prism`, not a deep OneDrive path. Source files are LF. If `go test` ever complains about `\r` in strings:

```powershell
git config core.autocrlf false
git checkout -- .
```

Optional one-shot setup (Python deps + `web\node_modules`):

```powershell
.\scripts\windows\prism.ps1 setup
```

---

## 3. Five-minute demo (workbench + row-group skip)

The committed fixture is `testdata\tables` — 8,192 `events` rows, 4 row groups, `ts` clustered through 2024. No generator needed.

**Terminal 1 — engine**

```powershell
cd C:\src\prism-columnar-analytics-engine   # your clone path
go run .\cmd\prismd -- --listen 127.0.0.1:8080 --data-dir testdata\tables
```

Or `.\scripts\windows\prism.ps1 engine`. Leave it running. You should see `prismd 0.8.0 listen=127.0.0.1:8080`.

**Terminal 2 — UI**

```powershell
cd C:\src\prism-columnar-analytics-engine
.\scripts\windows\prism.ps1 web
```

Or by hand:

```powershell
cd web
npm install
$env:NEXT_PUBLIC_API = "http://127.0.0.1:8080"
npm run dev
```

Open **http://127.0.0.1:3000**. The header should say `prismd 0.8.0`, not `offline`.

### What to click

1. Sidebar lists `events` (8,192 rows · 4 rg · 2 files) and the schema.
2. Sample defaults to **Q2 — 7-day window (skip demo)**. Engine **vectorized**. Click **Run**.
3. Green banner: **Skipped 3 of 4 row groups** using Parquet min/max on `ts`. Results: `count = 158`, `sum_amount_cents = 157021`.
4. Open **Plan**. `ParquetScan` should show `rg kept=1 / 4 skipped=3` and a pushed `ts` predicate.
5. Switch the sample to **Q6**, Run. That is the resume-style `GROUP BY`. On this 2024-only fixture `ts >= 2024-01-01` matches the whole year, so **nothing is skipped**. That is the clustering lesson, not a bug.
6. Toggle **row-at-a-time**, rerun Q2, compare wall time (same skip/prune; the loop is the baseline).
7. Open **Bench**. Bars are the checked-in testdata sample (`8,192` rows). **Not resume numbers.**

If the header says `offline`, prismd is not up or `NEXT_PUBLIC_API` is wrong.

---

## 4. Optional: CLI without the UI

Same fixture, no Node, no Docker:

```powershell
go test ./...
go run .\cmd\prism version
go run .\cmd\prism tables --data-dir testdata\tables
go run .\cmd\prism describe events --data-dir testdata\tables
go run .\cmd\prism sql --data-dir testdata\tables --file testdata\sql\ok\q2.sql
go run .\cmd\prism explain --data-dir testdata\tables --file testdata\sql\ok\q2.sql
go run .\cmd\prism bench --scale testdata --engine all --repeat 3
```

After `go build -o prism.exe .\cmd\prism` you can run `.\prism.exe` with the same flags. SQL dialect: [`docs/sql.md`](docs/sql.md). API: [`docs/api.md`](docs/api.md).

---

## 5. Optional: larger data, oracle, laptop bench

These are **not** required to demo the workbench.

| Goal | Command |
|---|---|
| 100K rows | `.\scripts\windows\prism.ps1 data-tiny` |
| 1M rows | `.\scripts\windows\prism.ps1 data-dev` |
| Postgres correctness (testdata + tiny) | `.\scripts\windows\prism.ps1 verify` (Docker Desktop must be running, Linux containers) |
| 1M-row bench JSON | `.\scripts\windows\prism.ps1 bench-dev` |
| Resume-scale generate | `.\scripts\windows\prism.ps1 data-laptop` then climb `--rows` until the machine swaps |

`prism.ps1 engine` prefers `.\data\tables` once you have generated `events`. Point the workbench at that same process.

**Do not** load `--scale laptop` into Postgres. Oracle at `dev` is enough.

Laptop measurement protocol and the hardware block to paste into `WRITEUP.md`: [`docs/benchmarks.md`](docs/benchmarks.md).

---

## 6. What is left (not more engine code)

| Item | Status |
|---|---|
| Phases 0–14 (scan → SQL → skip → dual engine → parallel → oracle → bench harness → `prismd` → workbench → docs) | Done |
| Open the UI and *see* row-group skipping (Q2 on the fixture) | Done |
| `--scale laptop` generate + `prism bench` **on this Windows machine** | **Yours to run** |
| Fill hardware + timings in [`WRITEUP.md`](WRITEUP.md) and rewrite the 100M/10x resume line | **Yours to run** |
| Joins, `HAVING`, spill, DuckDB, a license file | Out of v1 |

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| `go` / `node` / `py` not found | New terminal after winget; check PATH |
| `python` opens the Microsoft Store | Use `py -3` |
| Workbench header `offline` | Start `prismd` first; `$env:NEXT_PUBLIC_API = "http://127.0.0.1:8080"` |
| `npm` not found | `winget install OpenJS.NodeJS.LTS`, new terminal |
| Execution policy on `.ps1` | `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned` |
| `go test` parse errors / `\r` | LF checkout; see Clone |
| Path too long / OneDrive locks parquet | Clone near `C:\src`, outside OneDrive |
| `docker compose` / pipe errors | Start Docker Desktop; Linux containers; wait until idle |
| Postgres `connection refused` | `docker compose ps`; nothing else on 5432 |
| Antivirus pegged while generating | Defender exclusion on `.\data` |

---

## WSL2 (optional)

Same `go` / `python3` / `node` commands with `/` paths. Do not make WSL the only documented path. Avoid compiling a `/mnt/c/...` checkout from WSL (filesystem performance). Clone a second copy inside the WSL filesystem, or stay on native Windows.
