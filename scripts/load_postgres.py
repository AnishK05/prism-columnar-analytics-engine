#!/usr/bin/env python3
"""COPY Prism Parquet tables into Postgres (oracle only).

Loads testdata / tiny / dev. Refuses laptop — oracle at dev is enough.

  docker compose up -d postgres
  py -3 .\\scripts\\load_postgres.py --scale testdata
  py -3 .\\scripts\\load_postgres.py --scale tiny
"""

from __future__ import annotations

import argparse
import csv
import datetime as dt
import io
import os
import sys
from pathlib import Path

import numpy as np
import pyarrow.parquet as pq

try:
    import psycopg
except ImportError:
    psycopg = None  # type: ignore[misc, assignment]

EVENT_COLS = [
    "event_id",
    "user_id",
    "ts",
    "event_type",
    "country",
    "device",
    "amount_cents",
    "qty",
    "product_id",
    "session_id",
]

ALLOWED_SCALES = {"testdata", "tiny", "dev"}

DDL = """
DROP TABLE IF EXISTS events;
CREATE TABLE events (
  event_id BIGINT NOT NULL,
  user_id BIGINT,
  ts TIMESTAMP WITHOUT TIME ZONE,
  event_type TEXT,
  country TEXT,
  device TEXT,
  amount_cents BIGINT,
  qty BIGINT,
  product_id BIGINT,
  session_id BIGINT
);
"""


def default_dsn() -> str:
    return os.environ.get(
        "DATABASE_URL",
        "postgres://postgres:prism@127.0.0.1:5432/prism_oracle?sslmode=disable",
    )


def default_data_dir(scale: str, repo: Path) -> Path:
    if scale == "testdata":
        return repo / "testdata" / "tables"
    return repo / "data" / "tables"


def fmt_cell(v) -> str:
    if v is None:
        return r"\N"
    if isinstance(v, (np.bool_,)):
        return "true" if bool(v) else "false"
    if isinstance(v, (np.integer,)):
        return str(int(v))
    if isinstance(v, (np.floating,)):
        if np.isnan(v):
            return r"\N"
        return repr(float(v))
    if isinstance(v, dt.datetime):
        if v.tzinfo is not None:
            v = v.astimezone(dt.timezone.utc).replace(tzinfo=None)
        frac = v.microsecond
        if frac:
            return v.strftime("%Y-%m-%d %H:%M:%S.%f")[:-3]
        return v.strftime("%Y-%m-%d %H:%M:%S")
    if isinstance(v, np.datetime64):
        # ns since epoch → UTC naive datetime
        ms = int(v.astype("datetime64[ms]").astype(np.int64))
        v = dt.datetime.fromtimestamp(ms / 1000.0, tz=dt.timezone.utc).replace(tzinfo=None)
        return fmt_cell(v)
    return str(v)


def copy_events(conn, events_dir: Path, batch_size: int) -> int:
    files = sorted(events_dir.glob("*.parquet"))
    if not files:
        raise SystemExit(f"no parquet files in {events_dir}")
    total = 0
    copy_sql = (
        "COPY events ("
        + ", ".join(EVENT_COLS)
        + r") FROM STDIN WITH (FORMAT csv, NULL '\N')"
    )
    with conn.cursor() as cur:
        with cur.copy(copy_sql) as copy:
            for path in files:
                pf = pq.ParquetFile(path)
                for rec in pf.iter_batches(batch_size=batch_size, columns=EVENT_COLS):
                    buf = io.StringIO()
                    writer = csv.writer(buf, lineterminator="\n")
                    n = rec.num_rows
                    cols = {name: rec.column(name).to_pylist() for name in EVENT_COLS}
                    for i in range(n):
                        writer.writerow([fmt_cell(cols[c][i]) for c in EVENT_COLS])
                    copy.write(buf.getvalue())
                    total += n
                print(f"copied {path.name} ({pf.metadata.num_rows} rows)", flush=True)
    return total


def main() -> int:
    p = argparse.ArgumentParser(description="Load Prism Parquet into Postgres (oracle)")
    p.add_argument("--scale", choices=sorted(ALLOWED_SCALES | {"laptop"}), required=True)
    p.add_argument("--data-dir", type=Path, default=None)
    p.add_argument("--dsn", default=default_dsn())
    p.add_argument("--batch", type=int, default=50_000)
    p.add_argument("--repo", type=Path, default=Path(__file__).resolve().parent.parent)
    args = p.parse_args()
    if args.scale == "laptop":
        print(
            "refusing to load laptop scale into Postgres "
            "(oracle is testdata / tiny / dev only)",
            file=sys.stderr,
        )
        return 2
    if psycopg is None:
        raise SystemExit("psycopg is required: py -3 -m pip install -r scripts/requirements.txt")

    repo = args.repo.resolve()
    data_dir = args.data_dir
    if data_dir is None:
        data_dir = default_data_dir(args.scale, repo)
    elif not data_dir.is_absolute():
        data_dir = (repo / data_dir).resolve()
    events_dir = data_dir / "events"
    if not events_dir.is_dir():
        raise SystemExit(f"missing events table at {events_dir}")

    conn = psycopg.connect(args.dsn, autocommit=True)
    try:
        with conn.cursor() as cur:
            cur.execute(DDL)
        n = copy_events(conn, events_dir, args.batch)
        with conn.cursor() as cur:
            cur.execute("ANALYZE events")
            cur.execute("SELECT COUNT(*) FROM events")
            got = int(cur.fetchone()[0])
    finally:
        conn.close()
    if got != n:
        print(f"row count mismatch copied={n} postgres={got}", file=sys.stderr)
        return 1
    print(f"load_postgres ok scale={args.scale} rows={got} dsn={args.dsn}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
