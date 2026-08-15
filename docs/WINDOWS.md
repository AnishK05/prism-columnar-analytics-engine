# Running Prism on Windows

This is the **canonical local setup** for Prism. The engine is meant to be developed and demoed on a personal Windows laptop — not a Linux server and not WSL.

Native PowerShell + Docker Desktop is first-class. WSL2 is optional.

> **Status:** the engine is not implemented yet. This file is the runbook implementation will follow (Phase 0 onward). If a command below does not exist in the repo yet, that phase has not landed. When a PR adds a command, it must update this file in the same PR.

---

## What you need

Install these on Windows (Win10/11, 64-bit). **winget** is the default path; the websites work too.

| Tool | Why | Install |
|---|---|---|
| Git for Windows | clone, line endings | `winget install Git.Git` |
| Go 1.22+ | engine | `winget install GoLang.Go` |
| Python 3.11+ | data generator + Postgres oracle scripts | `winget install Python.Python.3.12` |
| Node.js 20 LTS | Next.js workbench | `winget install OpenJS.NodeJS.LTS` |
| Docker Desktop | PostgreSQL oracle (and optional full stack) | `winget install Docker.DockerDesktop` |

Then **restart the terminal** (and reboot once after Docker Desktop if it asks).

Confirm in **PowerShell**:

```powershell
git --version
go version
py --version
node --version
npm --version
docker version
docker compose version
```

Notes:

- Use the `py` launcher (`py -3`), not a bare `python` that might point at the Windows Store stub.
- Docker Desktop must be running (whale in the tray) and set to **Linux containers**.
- You do **not** need Make, Git Bash, MinGW, or WSL to follow this guide.
- PowerShell 7 is nicer (`winget install Microsoft.PowerShell`) but Windows PowerShell 5.1 is enough.

### Optional: Windows Defender exclusion

Parquet generation writes many large files. If disk is mysteriously slow, exclude the repo’s `data` folder:

1. Windows Security → Virus & threat protection → Manage settings → Exclusions
2. Add folder: `<repo>\data`

### Optional: Docker Desktop memory

Settings → Resources → Memory. Leave room for Chrome + the Go process. While developing, run **Postgres only** in Docker (see below), not the full compose stack.

---

## Clone

```powershell
git clone https://github.com/AnishK05/prism-columnar-analytics-engine.git
cd prism-columnar-analytics-engine
```

Source files are LF (`/.gitattributes`). Do not set `core.autocrlf=true` for this repo if it rewrites Go files to CRLF. If `go test` ever complains about `\r` in strings, run:

```powershell
git config core.autocrlf false
git checkout -- .
```

---

## Layout you will use

```
.\data\tables\events\*.parquet     # generated, gitignored
.\cmd\prism\                       # CLI → prism.exe
.\cmd\prismd\                      # HTTP API → prismd.exe
.\web\                             # Next.js
.\scripts\generate_data.py
.\scripts\windows\prism.ps1        # helper verbs (added in Phase 0)
```

All Go/Python code must use `filepath` / `pathlib` so `.\data\tables\events` works. Do not hardcode `/`.

---

## Everyday workflow (native engine, Docker Postgres)

This is the supported loop. Commands land as the matching phases land.

### 1. Oracle database (Phase 0+)

```powershell
docker compose up -d postgres
```

Postgres: `localhost:5432`, user `postgres`, password `prism`, db `prism_oracle`.

Stop later with `docker compose down`. Data in the compose volume survives unless you pass `-v`.

### 2. Generate synthetic tables (Phase 1+)

```powershell
py -3 -m pip install --user numpy pyarrow pandas
py -3 .\scripts\generate_data.py --scale tiny
# later: --scale dev     (1M rows)
# later: --scale laptop  (start at 10M, climb if the machine is happy)
```

Do **not** build one giant pandas DataFrame. The generator writes Parquet in batches under `.\data\tables\`.

### 3. Tests (Phase 0+)

```powershell
go test ./...
```

### 4. CLI (Phase 1+)

```powershell
go run .\cmd\prism -- help
go run .\cmd\prism -- describe events
go run .\cmd\prism -- sql "SELECT country, COUNT(*) FROM events GROUP BY country"
```

After `go build -o prism.exe .\cmd\prism` you can run `.\prism.exe`.

### 5. API + workbench (Phases 12–13)

Terminal A:

```powershell
$env:PRISM_DATA_DIR = (Resolve-Path .\data\tables).Path
go run .\cmd\prismd
```

Terminal B:

```powershell
cd web
npm install
$env:NEXT_PUBLIC_API = "http://localhost:8080"
npm run dev
```

Open http://localhost:3000.

### 6. Correctness vs Postgres (Phase 10)

```powershell
py -3 .\scripts\load_postgres.py --scale tiny
py -3 .\scripts\verify_against_postgres.py --scale tiny
```

Only `tiny` and `dev` go into Postgres. Laptop-scale data stays Parquet-only.

### 7. Benchmarks (Phase 11)

```powershell
go run .\cmd\prism -- bench --scale=dev --engine=all --repeat=5
```

Windows cannot drop the OS file cache the way Linux can. Report **first run** and **hot cache** (median of runs 2–N) separately. Hardware for `docs/benchmarks.md`:

```powershell
Get-CimInstance Win32_ComputerSystem | Select-Object Manufacturer, Model, TotalPhysicalMemory, NumberOfLogicalProcessors
Get-CimInstance Win32_Processor | Select-Object Name, MaxClockSpeed, NumberOfCores
```

---

## Helper script (Phase 0)

Once `.\scripts\windows\prism.ps1` exists:

```powershell
# If Windows blocks local scripts:
Set-ExecutionPolicy -Scope CurrentUser RemoteSigned

.\scripts\windows\prism.ps1 setup
.\scripts\windows\prism.ps1 data-tiny
.\scripts\windows\prism.ps1 test
.\scripts\windows\prism.ps1 engine
.\scripts\windows\prism.ps1 web
.\scripts\windows\prism.ps1 verify
.\scripts\windows\prism.ps1 bench-dev
```

That script is a wrapper around the commands above, not a second build system. The Python/Go/npm commands remain the source of truth.

---

## Full Docker stack (optional demo)

Useful when you want “one compose up” for a demo, not for daily engine work (bind mounts + Windows filesystem are slower).

```powershell
docker compose up --build
```

Then: API `:8080`, web `:3000`, Postgres `:5432`. Generate data **on the host** first so `.\data` is populated; compose bind-mounts it.

If compose volume paths fail, enable **Docker Desktop → Settings → Resources → File sharing** for the drive that holds the repo (usually `C:`).

---

## Scale on a laptop

There is no extra hardware. Climb this ladder and **stop before the machine swaps**:

| `--scale` | rows (events) | use |
|---|---|---|
| `tiny` | 100K | first smoke, tests |
| `dev` | 1M | UI + oracle |
| `laptop` | start 10M, then 25M / 50M / 100M | resume number, if it fits |

If generation or a query makes Windows use a huge page file / freeze, drop the row count. Streaming is mandatory; loading an entire table into RAM is a bug.

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
| `npm` / native module issues | Use Node 20 LTS, not 22+ experimental; delete `web\node_modules` and reinstall |
| Execution policy on `.ps1` | `Set-ExecutionPolicy -Scope CurrentUser RemoteSigned` |
| Path too long | Keep the clone near `C:\src\prism`, not a deep OneDrive path |
| OneDrive locking parquet files | Clone outside OneDrive/Documents if files go “online-only” |
| Port 3000 / 8080 in use | `netstat -ano | findstr :8080` then stop that PID, or change the port |

---

## WSL2 (optional, not required)

If you already live in Ubuntu-on-Windows, you can develop there with the same `go` / `python3` / `docker` commands, using `/` paths. Do **not** make WSL the only documented path. Check in PowerShell on real Windows before calling a phase done.

Do not put the repo on `/mnt/c/...` and compile from WSL if you can avoid it (filesystem performance). Clone a second copy inside the WSL filesystem, or just use native Windows.

---

## What we are not doing on Windows

- Requiring Make, bash, or MSYS
- `echo 3 > /proc/sys/vm/drop_caches` (Linux-only; benches are hot-cache)
- Assuming `/tmp` or `chmod +x`
- Shipping only `scripts/*.sh`
- A license file
- DuckDB as a dependency
