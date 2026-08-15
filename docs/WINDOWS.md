# Running Prism on Windows

This is the **canonical local setup** for Prism. The engine is meant to be developed and demoed on a personal Windows laptop — not a Linux server and not WSL.

Native PowerShell + Docker Desktop is first-class. WSL2 is optional.

> **Status (Phase 0–1):** CLI `version` / `inspect` / `scan` work. Generate Parquet with Python. Postgres in Docker is scaffolded for later oracle tests. SQL, filters, and the Next.js workbench are not built yet.

---

## What you need

Install these on Windows (Win10/11, 64-bit). **winget** is the default path; the websites work too.

| Tool | Why | Install |
|---|---|---|
| Git for Windows | clone, line endings | `winget install Git.Git` |
| Go 1.22+ | engine (`apache/arrow-go` **v18.0.0** is pinned; it supports Go 1.22) | `winget install GoLang.Go` |
| Python 3.11+ | data generator | `winget install Python.Python.3.12` |
| Docker Desktop | PostgreSQL oracle (Phase 10; optional now) | `winget install Docker.DockerDesktop` |

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
go run .\cmd\prism -- inspect --data-dir testdata\tables --table events
go run .\cmd\prism -- scan --data-dir testdata\tables --table events --columns country,amount_cents --limit 5
```

After `go build -o prism.exe .\cmd\prism` you can run `.\prism.exe` with the same flags.

### 4. Generate larger tables

```powershell
py -3 .\scripts\generate_data.py --scale tiny
# later: --scale dev      (1M rows)
# later: --scale laptop   (starts at 10M)
```

Output: `.\data\tables\events\*.parquet` plus `users` and `products`. Then:

```powershell
go run .\cmd\prism -- inspect --table events
go run .\cmd\prism -- scan --table events --columns country,amount_cents --limit 5
```

Default data dir is `.\data\tables`, or set `PRISM_DATA_DIR`.

Do **not** build one giant pandas DataFrame. The generator writes Parquet in batches.

### 5. Postgres (optional until Phase 10)

```powershell
docker compose up -d postgres
```

Postgres: `localhost:5432`, user `postgres`, password `prism`, db `prism_oracle`.

Stop later with `docker compose down`.

### Not in this phase

| Command | When |
|---|---|
| `prismd` HTTP API | Phase 12 |
| Next.js workbench | Phase 13 |
| `verify_against_postgres.py` | Phase 10 |
| `prism bench` | Phase 11 |
| SQL (`SELECT ...`) | Phase 5+ |

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
