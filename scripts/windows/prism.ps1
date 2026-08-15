#Requires -Version 5.1
<#
.SYNOPSIS
  Windows helper verbs for Prism. Wraps the same go / py / docker commands as docs/WINDOWS.md.
.EXAMPLE
  .\scripts\windows\prism.ps1 setup
  .\scripts\windows\prism.ps1 data-tiny
  .\scripts\windows\prism.ps1 test
  .\scripts\windows\prism.ps1 verify
  .\scripts\windows\prism.ps1 bench-dev
#>
param(
    [Parameter(Position = 0)]
    [ValidateSet("setup", "data-tiny", "data-dev", "data-laptop", "test", "engine", "web", "verify", "bench-dev", "help")]
    [string]$Command = "help"
)

$ErrorActionPreference = "Stop"
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
Set-Location $RepoRoot

function Invoke-Py {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$PyArgs)
    if (Get-Command py -ErrorAction SilentlyContinue) {
        & py -3 @PyArgs
    }
    elseif (Get-Command python -ErrorAction SilentlyContinue) {
        & python @PyArgs
    }
    else {
        throw "Python not found. Install Python 3.11+ and use the py launcher (py -3)."
    }
    if ($LASTEXITCODE -ne 0) {
        throw "python exited $LASTEXITCODE"
    }
}

function Invoke-Go {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$GoArgs)
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "go not found. Install Go 1.22+."
    }
    & go @GoArgs
    if ($LASTEXITCODE -ne 0) {
        throw "go exited $LASTEXITCODE"
    }
}

function Wait-Postgres {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        throw "docker not found. Postgres oracle needs Docker Desktop."
    }
    docker compose up -d postgres
    if ($LASTEXITCODE -ne 0) {
        throw "docker compose up failed"
    }
    for ($i = 0; $i -lt 30; $i++) {
        docker compose exec -T postgres pg_isready -U postgres -d prism_oracle | Out-Null
        if ($LASTEXITCODE -eq 0) {
            return
        }
        Start-Sleep -Seconds 2
    }
    throw "postgres did not become ready (docker compose ps)"
}

switch ($Command) {
    "help" {
        Write-Host @"
prism.ps1 commands:
  setup        Check tools and install Python deps
  data-tiny    Generate 100K-row events table
  data-dev     Generate 1M-row events table
  data-laptop  Generate 10M-row events table (stop before the machine swaps)
  test         go test ./...
  engine       Phase 12 (prints version for now)
  web          Phase 13 (not implemented)
  verify       Postgres oracle: testdata + tiny (compose up, load, compare Q1-Q8)
  bench-dev    prism bench --scale=dev --engine=all --repeat=5
"@
    }
    "setup" {
        Invoke-Go version
        Invoke-Py --version
        Invoke-Py -m pip install -r (Join-Path $RepoRoot "scripts\requirements.txt")
        if (Get-Command docker -ErrorAction SilentlyContinue) {
            docker compose version
        }
        else {
            Write-Warning "docker not found; Postgres oracle (verify) needs Docker Desktop."
        }
        Write-Host "setup ok"
    }
    "data-tiny" {
        Invoke-Py (Join-Path $RepoRoot "scripts\generate_data.py") --scale tiny
    }
    "data-dev" {
        Invoke-Py (Join-Path $RepoRoot "scripts\generate_data.py") --scale dev
    }
    "data-laptop" {
        Invoke-Py (Join-Path $RepoRoot "scripts\generate_data.py") --scale laptop
    }
    "test" {
        Invoke-Go test ./...
    }
    "engine" {
        Write-Host "HTTP API (prismd) is Phase 12. CLI scan works now:"
        Invoke-Go run ./cmd/prism -- version
        Write-Host "Example: go run ./cmd/prism scan --table events --columns country,amount_cents --limit 5"
    }
    "web" {
        Write-Host "Next.js workbench is Phase 13. Skip for now."
    }
    "verify" {
        Wait-Postgres
        Invoke-Py (Join-Path $RepoRoot "scripts\verify_manifest.py") --data-dir testdata\tables
        Invoke-Py (Join-Path $RepoRoot "scripts\load_postgres.py") --scale testdata
        Invoke-Py (Join-Path $RepoRoot "scripts\verify_against_postgres.py") --scale testdata
        $events = Join-Path $RepoRoot "data\tables\events"
        if (-not (Test-Path $events)) {
            Invoke-Py (Join-Path $RepoRoot "scripts\generate_data.py") --scale tiny
        }
        Invoke-Py (Join-Path $RepoRoot "scripts\verify_manifest.py") --data-dir data\tables
        Invoke-Py (Join-Path $RepoRoot "scripts\load_postgres.py") --scale tiny
        Invoke-Py (Join-Path $RepoRoot "scripts\verify_against_postgres.py") --scale tiny
        $man = Join-Path $RepoRoot "data\tables\manifest.json"
        if (Test-Path $man) {
            $scale = (Get-Content $man -Raw | ConvertFrom-Json).scale
            if ($scale -eq "dev") {
                Invoke-Py (Join-Path $RepoRoot "scripts\load_postgres.py") --scale dev
                Invoke-Py (Join-Path $RepoRoot "scripts\verify_against_postgres.py") --scale dev
            }
        }
        Write-Host "verify ok"
    }
    "bench-dev" {
        $events = Join-Path $RepoRoot "data\tables\events"
        if (-not (Test-Path $events)) {
            Invoke-Py (Join-Path $RepoRoot "scripts\generate_data.py") --scale dev
        }
        Invoke-Go run ./cmd/prism -- bench --scale dev --engine all --repeat 5 --out bench\results\dev.json
    }
}
