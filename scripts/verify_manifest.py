#!/usr/bin/env python3
"""Compare generator manifest.json to prism describe + SUM(event_id).

  py -3 .\\scripts\\verify_manifest.py --data-dir testdata\\tables
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path


def repo_root() -> Path:
    return Path(__file__).resolve().parent.parent


def prism_cmd(root: Path) -> list[str]:
    env = os.environ.get("PRISM_BIN")
    if env:
        return [env]
    return ["go", "run", "./cmd/prism"]


def run_prism(root: Path, args: list[str]) -> str:
    cmd = prism_cmd(root) + args
    proc = subprocess.run(
        cmd,
        cwd=root,
        check=False,
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise SystemExit(
            f"prism failed ({proc.returncode}): {' '.join(cmd)}\n{proc.stderr}"
        )
    return proc.stdout


def main() -> int:
    p = argparse.ArgumentParser(description="Check manifest.json vs describe / SUM(event_id)")
    p.add_argument("--data-dir", type=Path, default=Path("testdata") / "tables")
    p.add_argument("--repo", type=Path, default=repo_root())
    args = p.parse_args()
    root = args.repo.resolve()
    data_dir = args.data_dir
    if not data_dir.is_absolute():
        data_dir = (root / data_dir).resolve()
    man_path = data_dir / "manifest.json"
    if not man_path.is_file():
        raise SystemExit(f"missing {man_path}")
    man = json.loads(man_path.read_text(encoding="utf-8"))
    events = man["tables"]["events"]

    desc_raw = run_prism(root, ["describe", "events", "--json", "--data-dir", str(data_dir)])
    desc = json.loads(desc_raw)
    errors: list[str] = []
    if desc["rows"] != events["rows"]:
        errors.append(f"rows describe={desc['rows']} manifest={events['rows']}")
    if desc.get("min_ts_ms") != events.get("min_ts_ms"):
        errors.append(f"min_ts_ms describe={desc.get('min_ts_ms')} manifest={events.get('min_ts_ms')}")
    if desc.get("max_ts_ms") != events.get("max_ts_ms"):
        errors.append(f"max_ts_ms describe={desc.get('max_ts_ms')} manifest={events.get('max_ts_ms')}")
    if desc["files"] != len(events.get("files", [])):
        errors.append(f"files describe={desc['files']} manifest={len(events.get('files', []))}")

    sql = "SELECT COUNT(*), SUM(event_id) FROM events"
    js_raw = run_prism(root, ["sql", "--json", "--json-limit", "0", "--data-dir", str(data_dir), sql])
    js = json.loads(js_raw)
    if not js.get("rows"):
        errors.append("empty COUNT/SUM result")
    else:
        row = js["rows"][0]
        count, checksum = int(row[0]), int(row[1])
        if count != events["rows"]:
            errors.append(f"COUNT(*)={count} manifest rows={events['rows']}")
        if checksum != events["sum_event_id"]:
            errors.append(f"SUM(event_id)={checksum} manifest={events['sum_event_id']}")

    if errors:
        print("verify_manifest FAILED", file=sys.stderr)
        for e in errors:
            print(f"  {e}", file=sys.stderr)
        return 1
    print(
        f"verify_manifest ok scale={man.get('scale')} rows={events['rows']} "
        f"sum_event_id={events['sum_event_id']} ts=[{events['min_ts_ms']} .. {events['max_ts_ms']}]"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
