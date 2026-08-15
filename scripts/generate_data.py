#!/usr/bin/env python3
"""Generate synthetic Prism tables as Parquet (batched, no giant DataFrame)."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

import numpy as np
import pyarrow as pa
import pyarrow.parquet as pq

EVENT_TYPES = np.array(["view", "click", "add_cart", "purchase", "refund"])
EVENT_TYPE_P = np.array([0.60, 0.20, 0.10, 0.08, 0.02])
COUNTRIES = np.array(
    [
        "US",
        "CA",
        "GB",
        "DE",
        "FR",
        "IN",
        "JP",
        "AU",
        "BR",
        "MX",
        "ES",
        "IT",
        "NL",
        "SE",
        "KR",
        "SG",
        "AE",
        "ZA",
        "PL",
        "IE",
    ]
)
DEVICES = np.array(["ios", "android", "web"])
PRODUCT_CATEGORIES = np.array(
    ["electronics", "home", "apparel", "grocery", "beauty", "sports", "toys"]
)

# Unix ms: 2024-01-01 .. 2024-12-31
TS_START_MS = 1_704_067_200_000
TS_END_MS = 1_735_603_199_000

SCALES = {
    "testdata": {
        "events": 8_192,
        "users": 256,
        "products": 64,
        "rows_per_file": 4_096,
        "row_group": 2_048,
        "batch": 2_048,
    },
    "tiny": {
        "events": 100_000,
        "users": 1_000,
        "products": 100,
        "rows_per_file": 50_000,
        "row_group": 25_000,
        "batch": 25_000,
    },
    "dev": {
        "events": 1_000_000,
        "users": 10_000,
        "products": 1_000,
        "rows_per_file": 250_000,
        "row_group": 128_000,
        "batch": 50_000,
    },
    "laptop": {
        "events": 10_000_000,
        "users": 100_000,
        "products": 10_000,
        "rows_per_file": 1_000_000,
        "row_group": 256_000,
        "batch": 100_000,
    },
}

EVENTS_SCHEMA = pa.schema(
    [
        pa.field("event_id", pa.int64()),
        pa.field("user_id", pa.int64()),
        pa.field("ts", pa.timestamp("ms")),
        pa.field("event_type", pa.string()),
        pa.field("country", pa.string()),
        pa.field("device", pa.string()),
        pa.field("amount_cents", pa.int64()),
        pa.field("qty", pa.int64()),
        pa.field("product_id", pa.int64()),
        pa.field("session_id", pa.int64()),
    ]
)

USERS_SCHEMA = pa.schema(
    [
        pa.field("user_id", pa.int64()),
        pa.field("country", pa.string()),
        pa.field("signup_ts", pa.timestamp("ms")),
        pa.field("device", pa.string()),
    ]
)

PRODUCTS_SCHEMA = pa.schema(
    [
        pa.field("product_id", pa.int64()),
        pa.field("category", pa.string()),
        pa.field("price_cents", pa.int64()),
    ]
)


def _parquet_writer(path: Path, schema: pa.Schema, dict_cols: list[str]) -> pq.ParquetWriter:
    path.parent.mkdir(parents=True, exist_ok=True)
    present = [c for c in dict_cols if schema.get_field_index(c) != -1]
    return pq.ParquetWriter(
        where=str(path),
        schema=schema,
        compression="zstd",
        write_statistics=True,
        use_dictionary=present or False,
    )


def _write_table(writer: pq.ParquetWriter, table: pa.Table, row_group: int) -> None:
    writer.write_table(table, row_group_size=row_group)


def write_products(out_dir: Path, n: int, seed: int, row_group: int) -> dict:
    rng = np.random.default_rng(seed + 1)
    table = pa.table(
        {
            "product_id": np.arange(1, n + 1, dtype=np.int64),
            "category": rng.choice(PRODUCT_CATEGORIES, size=n),
            "price_cents": rng.integers(199, 50_000, size=n, dtype=np.int64),
        },
        schema=PRODUCTS_SCHEMA,
    )
    path = out_dir / "products" / "part-0000.parquet"
    writer = _parquet_writer(path, PRODUCTS_SCHEMA, ["category"])
    try:
        _write_table(writer, table, row_group)
    finally:
        writer.close()
    return {"rows": n, "files": [str(path.relative_to(out_dir))]}


def write_users(out_dir: Path, n: int, seed: int, row_group: int) -> dict:
    rng = np.random.default_rng(seed + 2)
    signup = rng.integers(TS_START_MS - 365 * 24 * 3600 * 1000, TS_START_MS, size=n, dtype=np.int64)
    table = pa.table(
        {
            "user_id": np.arange(1, n + 1, dtype=np.int64),
            "country": rng.choice(COUNTRIES, size=n),
            "signup_ts": signup,
            "device": rng.choice(DEVICES, size=n, p=[0.4, 0.35, 0.25]),
        },
        schema=USERS_SCHEMA,
    )
    path = out_dir / "users" / "part-0000.parquet"
    writer = _parquet_writer(path, USERS_SCHEMA, ["country", "device"])
    try:
        _write_table(writer, table, row_group)
    finally:
        writer.close()
    return {"rows": n, "files": [str(path.relative_to(out_dir))]}


def write_events(
    out_dir: Path,
    n: int,
    n_users: int,
    n_products: int,
    seed: int,
    rows_per_file: int,
    row_group: int,
    batch: int,
) -> dict:
    rng = np.random.default_rng(seed)
    # Sorted timestamps so row-group skipping works later.
    ts = np.linspace(TS_START_MS, TS_END_MS, n, dtype=np.int64)

    files: list[str] = []
    sum_event_id = 0
    event_id = 1
    part = 0
    dest = out_dir / "events"

    while event_id <= n:
        file_rows = min(rows_per_file, n - event_id + 1)
        path = dest / f"part-{part:04d}.parquet"
        writer = _parquet_writer(path, EVENTS_SCHEMA, ["event_type", "country", "device"])
        written = 0
        try:
            while written < file_rows:
                b = min(batch, file_rows - written)
                start_id = event_id + written
                ids = np.arange(start_id, start_id + b, dtype=np.int64)
                # Power-law-ish user ids (many repeats on low ids).
                user_ids = 1 + (rng.random(b) ** 2.5 * (n_users - 1)).astype(np.int64)
                et = rng.choice(EVENT_TYPES, size=b, p=EVENT_TYPE_P)
                purchase = (et == "purchase") | (et == "refund")
                amount = np.zeros(b, dtype=np.int64)
                if purchase.any():
                    # lognormal cents, clipped
                    raw = np.exp(rng.normal(6.5, 0.8, size=int(purchase.sum())))
                    cents = np.clip(raw * 10.0, 50, 500_000).astype(np.int64)
                    amount[purchase] = cents
                    amount[et == "refund"] *= -1
                qty = np.ones(b, dtype=np.int64)
                qty[purchase] = rng.integers(1, 5, size=int(purchase.sum()), dtype=np.int64)
                sl = slice(start_id - 1, start_id - 1 + b)
                table = pa.table(
                    {
                        "event_id": ids,
                        "user_id": user_ids,
                        "ts": ts[sl],
                        "event_type": et,
                        "country": rng.choice(COUNTRIES, size=b),
                        "device": rng.choice(DEVICES, size=b, p=[0.4, 0.35, 0.25]),
                        "amount_cents": amount,
                        "qty": qty,
                        "product_id": 1 + rng.integers(0, n_products, size=b, dtype=np.int64),
                        "session_id": rng.integers(1, n // 2 + 2, size=b, dtype=np.int64),
                    },
                    schema=EVENTS_SCHEMA,
                )
                _write_table(writer, table, row_group)
                sum_event_id += int(ids.sum())
                written += b
        finally:
            writer.close()
        files.append(str(path.relative_to(out_dir)))
        print(f"wrote {path} ({file_rows} rows)", flush=True)
        event_id += file_rows
        part += 1

    return {
        "rows": n,
        "files": files,
        "min_ts_ms": int(ts[0]),
        "max_ts_ms": int(ts[-1]),
        "sum_event_id": sum_event_id,
    }


def generate(out_dir: Path, scale: str, seed: int, rows_override: int | None) -> dict:
    cfg = dict(SCALES[scale])
    if rows_override is not None:
        cfg["events"] = rows_override
    out_dir = out_dir.resolve()
    events = write_events(
        out_dir,
        n=cfg["events"],
        n_users=cfg["users"],
        n_products=cfg["products"],
        seed=seed,
        rows_per_file=cfg["rows_per_file"],
        row_group=cfg["row_group"],
        batch=cfg["batch"],
    )
    users = write_users(out_dir, cfg["users"], seed, cfg["row_group"])
    products = write_products(out_dir, cfg["products"], seed, cfg["row_group"])
    manifest = {
        "scale": scale,
        "seed": seed,
        "out_dir": str(out_dir),
        "tables": {"events": events, "users": users, "products": products},
    }
    dest = out_dir / "manifest.json"
    dest.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print(f"manifest {dest}", flush=True)
    return manifest


def main() -> int:
    p = argparse.ArgumentParser(description="Generate synthetic Prism Parquet tables")
    p.add_argument("--scale", choices=sorted(SCALES), default="tiny")
    p.add_argument(
        "--out",
        type=Path,
        default=Path("data") / "tables",
        help="tables root (default ./data/tables)",
    )
    p.add_argument("--seed", type=int, default=42)
    p.add_argument("--rows", type=int, default=None, help="override events row count")
    args = p.parse_args()
    generate(args.out, args.scale, args.seed, args.rows)
    return 0


if __name__ == "__main__":
    sys.exit(main())
