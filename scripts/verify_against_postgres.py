#!/usr/bin/env python3
"""Compare Prism Q1–Q8 (and Q8-wide) against PostgreSQL.

Sorts unordered queries. Floats use math.isclose. Timestamps normalize to UTC ms.

  py -3 .\\scripts\\verify_against_postgres.py --scale testdata
  py -3 .\\scripts\\verify_against_postgres.py --scale tiny
"""

from __future__ import annotations

import argparse
import json
import math
import os
import subprocess
import sys
from datetime import datetime, timezone
from decimal import Decimal
from pathlib import Path

try:
    import psycopg
except ImportError:
    psycopg = None  # type: ignore[misc, assignment]


def repo_root() -> Path:
    return Path(__file__).resolve().parent.parent


def default_dsn() -> str:
    return os.environ.get(
        "DATABASE_URL",
        "postgres://postgres:prism@127.0.0.1:5432/prism_oracle?sslmode=disable",
    )


def default_data_dir(scale: str, repo: Path) -> Path:
    if scale == "testdata":
        return repo / "testdata" / "tables"
    return repo / "data" / "tables"


def prism_cmd(root: Path) -> list[str]:
    env = os.environ.get("PRISM_BIN")
    if env:
        return [env]
    return ["go", "run", "./cmd/prism"]


def load_queries(root: Path, oracle_only: bool = True) -> list[dict]:
    spec = json.loads((root / "bench" / "queries.json").read_text(encoding="utf-8"))
    out = []
    for q in spec["queries"]:
        if oracle_only and not q.get("oracle", True):
            continue
        out.append(q)
    return out


def as_ms(v) -> int | None:
    if v is None:
        return None
    if isinstance(v, datetime):
        if v.tzinfo is None:
            v = v.replace(tzinfo=timezone.utc)
        return int(v.timestamp() * 1000)
    if isinstance(v, str):
        s = v.strip()
        if s.endswith("Z"):
            s = s[:-1] + "+00:00"
        try:
            d = datetime.fromisoformat(s)
        except ValueError:
            for fmt in ("%Y-%m-%d %H:%M:%S.%f", "%Y-%m-%d %H:%M:%S", "%Y-%m-%d"):
                try:
                    d = datetime.strptime(v.strip().rstrip("Z"), fmt)
                    break
                except ValueError:
                    d = None
            if d is None:
                return None
        if d.tzinfo is None:
            d = d.replace(tzinfo=timezone.utc)
        return int(d.timestamp() * 1000)
    return None


def to_float(v):
    if isinstance(v, Decimal):
        return float(v)
    if isinstance(v, bool):
        return None
    if isinstance(v, (int, float)):
        return float(v)
    if isinstance(v, str):
        try:
            return float(v)
        except ValueError:
            return None
    return None


def cells_equal(a, b) -> bool:
    if a is None and b is None:
        return True
    if a is None or b is None:
        return False
    if isinstance(a, bool) or isinstance(b, bool):
        return bool(a) == bool(b)
    fa, fb = to_float(a), to_float(b)
    if fa is not None and fb is not None:
        if math.isnan(fa) and math.isnan(fb):
            return True
        # Integers that fit in a float mantissa compare exactly.
        if fa.is_integer() and fb.is_integer() and abs(fa) < 2**53 and abs(fb) < 2**53:
            return int(fa) == int(fb)
        return math.isclose(fa, fb, rel_tol=1e-9, abs_tol=1e-6)
    ma, mb = as_ms(a), as_ms(b)
    if ma is not None and mb is not None:
        return ma == mb
    return str(a) == str(b)


def sort_key(row: list) -> str:
    return json.dumps(row, default=str, sort_keys=False)


def normalize_rows(rows: list[list], ordered: bool) -> list[list]:
    if ordered:
        return rows
    return sorted(rows, key=sort_key)


def run_prism(root: Path, data_dir: Path, sql: str) -> dict:
    cmd = prism_cmd(root) + [
        "sql",
        "--json",
        "--json-limit",
        "0",
        "--data-dir",
        str(data_dir),
        sql,
    ]
    proc = subprocess.run(cmd, cwd=root, capture_output=True, text=True)
    if proc.returncode != 0:
        raise SystemExit(f"prism failed: {' '.join(cmd)}\n{proc.stderr}")
    return json.loads(proc.stdout)


def run_postgres(conn, sql: str) -> list[list]:
    with conn.cursor() as cur:
        cur.execute(sql)
        rows = cur.fetchall()
        return [list(r) for r in rows]


def main() -> int:
    p = argparse.ArgumentParser(description="Compare Prism vs Postgres on Q1–Q8")
    p.add_argument("--scale", choices=["testdata", "tiny", "dev"], required=True)
    p.add_argument("--data-dir", type=Path, default=None)
    p.add_argument("--dsn", default=default_dsn())
    p.add_argument("--repo", type=Path, default=repo_root())
    args = p.parse_args()
    if psycopg is None:
        raise SystemExit("psycopg is required: py -3 -m pip install -r scripts/requirements.txt")

    root = args.repo.resolve()
    data_dir = args.data_dir
    if data_dir is None:
        data_dir = default_data_dir(args.scale, root)
    elif not data_dir.is_absolute():
        data_dir = (root / data_dir).resolve()

    queries = load_queries(root)
    conn = psycopg.connect(args.dsn)
    failed = 0
    try:
        for q in queries:
            path = root / q["file"]
            sql = path.read_text(encoding="utf-8")
            ordered = bool(q.get("ordered"))
            prism = run_prism(root, data_dir, sql)
            if prism.get("truncated"):
                print(f"{q['id']}: prism result truncated", file=sys.stderr)
                failed += 1
                continue
            prow = normalize_rows(prism.get("rows") or [], ordered)
            grow = normalize_rows(run_postgres(conn, sql), ordered)
            if len(prow) != len(grow):
                print(
                    f"{q['id']}: row count prism={len(prow)} postgres={len(grow)}",
                    file=sys.stderr,
                )
                failed += 1
                continue
            mismatch = 0
            for i, (a, b) in enumerate(zip(prow, grow)):
                if len(a) != len(b):
                    mismatch += 1
                    if mismatch <= 3:
                        print(f"{q['id']} row {i}: width {len(a)} vs {len(b)}", file=sys.stderr)
                    continue
                for c, (x, y) in enumerate(zip(a, b)):
                    if not cells_equal(x, y):
                        mismatch += 1
                        if mismatch <= 3:
                            print(
                                f"{q['id']} row {i} col {c}: prism={x!r} postgres={y!r}",
                                file=sys.stderr,
                            )
                        break
            if mismatch:
                print(f"{q['id']}: {mismatch} mismatched rows", file=sys.stderr)
                failed += 1
            else:
                print(f"{q['id']} ok rows={len(prow)} ordered={ordered} ({q.get('showcase')})")
    finally:
        conn.close()

    if failed:
        print(f"verify_against_postgres FAILED {failed} queries", file=sys.stderr)
        return 1
    print(f"verify_against_postgres ok scale={args.scale} queries={len(queries)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
