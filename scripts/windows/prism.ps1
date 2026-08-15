#Requires -Version 5.1
<#
.SYNOPSIS
  Windows helper verbs for Prism. Wraps the same go / py / docker commands as docs/WINDOWS.md.
.EXAMPLE
  .\scripts\windows\prism.ps1 setup
  .\scripts\windows\prism.ps1 data-tiny
  .\scripts\windows\prism.ps1 test
#>
param(
    [Parameter(Position = 0)]
    [ValidateSet("setup", "data-tiny", "data-dev", "test", "engine", "web", "verify", "bench-dev", "help")]
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

switch ($Command) {
    "help" {
        Write-Host @"
prism.ps1 commands:
  setup       Check tools and install Python deps
  data-tiny   Generate 100K-row events table
  data-dev    Generate 1M-row events table
  test        go test ./...
  engine      Phase 12 (prints version for now)
  web         Phase 13 (not implemented)
  verify      Phase 10 (starts with docker compose postgres hint)
  bench-dev   Phase 11 (not implemented)
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
            Write-Warning "docker not found; Postgres oracle (Phase 10) needs Docker Desktop."
        }
        Write-Host "setup ok"
    }
    "data-tiny" {
        Invoke-Py (Join-Path $RepoRoot "scripts\generate_data.py") --scale tiny
    }
    "data-dev" {
        Invoke-Py (Join-Path $RepoRoot "scripts\generate_data.py") --scale dev
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
        Write-Host "Postgres oracle is Phase 10."
        Write-Host "When you need it: docker compose up -d postgres"
    }
    "bench-dev" {
        Write-Host "Benchmark harness is Phase 11."
    }
}
