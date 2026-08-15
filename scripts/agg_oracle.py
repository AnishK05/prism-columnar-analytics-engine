"""Group-by oracle for Phase 4. Prints country, COUNT(*), SUM(amount_cents).

Uses PyArrow hash aggregate (no pandas). Int sums are exact.

  py -3 .\\scripts\\agg_oracle.py --data-dir testdata\\tables
"""

from __future__ import annotations

import argparse
from pathlib import Path

import pyarrow.parquet as pq


def main() -> int:
    p = argparse.ArgumentParser()
    p.add_argument("--data-dir", type=Path, required=True)
    p.add_argument("--table", default="events")
    args = p.parse_args()
    table = pq.read_table(args.data_dir / args.table, columns=["country", "amount_cents"])
    grouped = table.group_by("country").aggregate(
        [
            ("country", "count"),
            ("amount_cents", "sum"),
        ]
    ).sort_by("country")
    print("country\tcount\tsum_amount_cents")
    countries = grouped.column("country")
    counts = grouped.column("country_count")
    sums = grouped.column("amount_cents_sum")
    for i in range(grouped.num_rows):
        cty = countries[i].as_py()
        print(f"{cty}\t{int(counts[i].as_py())}\t{int(sums[i].as_py())}")
    print(f"groups {grouped.num_rows}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
