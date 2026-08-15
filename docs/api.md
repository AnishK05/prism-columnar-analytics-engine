# prismd HTTP API

`prismd` is the Phase 12 daemon. The Next.js workbench (Phase 13) talks to this process. It never queries Postgres.

```powershell
go run .\cmd\prismd -- --listen 127.0.0.1:8080 --data-dir testdata\tables
```

| Flag | Default | Notes |
|---|---|---|
| `--listen` | `127.0.0.1:8080` | `PRISM_LISTEN` overrides |
| `--data-dir` | `PRISM_DATA_DIR` or `.\data\tables` | same as the CLI |
| `--timeout` | `60s` | per-query context |
| `--jobs` | `PRISM_PARALLELISM` / GOMAXPROCS | row-group workers |
| `--cors` | `*` | `Access-Control-Allow-Origin` |
| `--bench-file` | `bench/results/sample.json` | `GET /bench` |

## Routes

| Method | Path | Body | Success |
|---|---|---|---|
| `GET` | `/health` | | `{ ok, version, data_dir }` |
| `GET` | `/tables` | | `{ data_dir, tables: [{ name, files, rows, row_groups, compressed_bytes }] }` |
| `GET` | `/tables/{name}` | | describe JSON (`rows`, `min_ts_ms`, schema, …) |
| `POST` | `/query` | `{ sql, engine, explain, limit }` | result + profile ([IMPLEMENTATION_PLAN.md](../IMPLEMENTATION_PLAN.md) §13) |
| `POST` | `/explain` | `{ sql, engine, analyze }` | `{ plan, text }` |
| `GET` | `/bench` | | checked-in sample JSON (not a live run) |

Errors: `{ "error": "...", "pos": 12 }` (`pos` is a 1-based SQL column when the parser reports one). Typical status: 400 parse/bind, 404 unknown table, 504 timeout.

## `POST /query`

```json
{
  "sql": "SELECT country, COUNT(*) FROM events GROUP BY country ORDER BY COUNT(*) DESC LIMIT 5",
  "engine": "vectorized",
  "explain": true,
  "limit": 100
}
```

- `engine`: `vectorized` (default) or `row`.
- `explain`: include `profile.plan` (physical tree). Always includes skip/prune counters on `profile`.
- `limit`: extra cap on returned rows after SQL `LIMIT`. Default **100** for non-aggregate scans, **100000** hard cap for aggregates. `0` means the hard cap. Never dump a full `SELECT *` of laptop-scale data over HTTP.

Response matches §13: `columns`, `types`, `rows`, `truncated`, `profile` (`elapsed_ms`, `engine`, `rows_read`, `rows_emitted`, `bytes_read`, `row_groups_*`, optional `plan`).

## CORS

Every response (including `OPTIONS`) sets `Access-Control-Allow-Origin` so `http://localhost:3000` can call prismd without a proxy.
