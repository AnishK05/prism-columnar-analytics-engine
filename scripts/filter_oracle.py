"""Count rows matching a Phase-3 predicate with PyArrow compute (SQL-ish nulls).

Pandas is the wrong oracle here: `NA > 0` is often False, while SQL/`kernel.Eval`
treats `NULL > 0` as UNKNOWN (not TRUE). PyArrow compute kernels propagate nulls.

Usage:
  py -3 .\\scripts\\filter_oracle.py --data-dir testdata\\tables
  python scripts/filter_oracle.py --data-dir testdata/tables --expect 30
"""

from __future__ import annotations

import argparse
from pathlib import Path

import pyarrow as pa
import pyarrow.compute as pc
import pyarrow.parquet as pq


def count_us_purchases(table: pa.Table) -> int:
    mask = pc.and_(
        pc.greater(table["amount_cents"], pa.scalar(0, type=pa.int64())),
        pc.equal(table["country"], pa.scalar("US", type=pa.string())),
    )
    return int(pc.sum(mask.cast(pa.int64())).as_py() or 0)


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--data-dir", type=Path, required=True)
    p.add_argument("--table", default="events")
    p.add_argument("--expect", type=int, default=None, help="fail if count differs")
    args = p.parse_args()
    path = args.data_dir / args.table
    table = pq.read_table(path)
    n = count_us_purchases(table)
    print(f"pyarrow_matches {n}")
    if args.expect is not None and n != args.expect:
        raise SystemExit(f"expected {args.expect}, got {n}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
