#!/usr/bin/env python3
"""Attribute the resident growth of the 300-round smoke with pprof.

The smoke writes a heap profile; this script tops it by inuse_space (retained)
and alloc_space (churn), attributes every tagent frame to KiB/round, and adds
the W1 merged increment (01+04+05) as a separate estimated row: the red
self-proof slope minus the 48d8eb93 review numbers, per ORCHESTRATION F29.
The W1 increment is an estimate of the merged-tree weight, not a profile frame,
so it carries its own column and never pretends to be one source.

Usage:
    pprof_attribution.py --run          # run the smoke, then attribute
    pprof_attribution.py --profile P --log L   # attribute an existing run

Exit 0 when the log proves rounds=300 and the profile yields at least one
tagent frame, 2 when either proof is missing.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

MODULE = "github.com/SpellingDragon/tagent/"
# The red self-proof (W2 first day, W1 merged increment in tree) and the two
# 48d8eb93 review measurements of the plan-period tree, in KiB/round.
RED_SLOPE_KIB = 208510 / 1024
REVIEW_KIB = (175.0 + 159.0) / 2.0
W1_INCREMENT_KIB = RED_SLOPE_KIB - REVIEW_KIB

LINE = re.compile(r"^\s*([0-9.]+)([kKMG]?B)\s+[0-9.]+%\s+[0-9.]+%\s+[0-9.]+[kKMG]?B\s+[0-9.]+%\s+(\S+)")


def run_smoke(profile: str, log: str) -> None:
    smoke = Path("examples/wechat-bot")
    command = [
        "go", "test", "-tags", "smoke", "-run", "^TestLocalSmoke$",
        "-count=1", "-timeout", "2h", "-v",
        "-memprofile", profile, "./...",
    ]
    print("run: " + " ".join(command) + f"  (cwd {smoke})", flush=True)
    with open(log, "w", encoding="utf-8") as sink:
        subprocess.run(command, cwd=smoke, stdout=sink, stderr=subprocess.STDOUT)


def parse_top(profile: str, sample: str) -> list[tuple[float, str]]:
    command = [
        "go", "tool", "pprof", "-top", "-nodecount", "120",
        f"-sample_index={sample}", "-unit=KB", profile,
    ]
    result = subprocess.run(command, capture_output=True, text=True)
    if result.returncode != 0:
        raise SystemExit(f"pprof {sample} failed: {result.stderr.strip()}")
    rows: list[tuple[float, str]] = []
    for raw in result.stdout.splitlines():
        match = LINE.match(raw)
        if not match:
            continue
        value, unit = float(match.group(1)), match.group(2).lower()
        if unit == "kb":
            kib = value
        elif unit == "mb":
            kib = value * 1024
        elif unit == "gb":
            kib = value * 1024 * 1024
        else:
            kib = value / 1024
        name = match.group(3)
        if MODULE in name:
            rows.append((kib, name))
    return rows


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", default="/tmp/tagent-residency-mem.pprof")
    parser.add_argument("--log", default="/tmp/tagent-residency-smoke.log")
    parser.add_argument("--rounds", type=int, default=300)
    parser.add_argument("--run", action="store_true", help="run the smoke before reading the profile")
    args = parser.parse_args()

    if args.run:
        run_smoke(args.profile, args.log)
    log = Path(args.log)
    if not log.exists():
        raise SystemExit(f"no smoke log at {log}; pass --run or a log path")
    text = log.read_text(encoding="utf-8", errors="replace")
    if f"rounds={args.rounds}" not in text:
        raise SystemExit(f"{log} carries no rounds={args.rounds} line; a zero-round run is not attribution evidence")
    slope = re.search(r"\[residency\].*slope=(-?\d+) B/round", text)
    gate = re.search(r"\[residency\].*gate=(\S+)", text)
    print(f"tree: working tree; red self-proof slope {RED_SLOPE_KIB:.1f} KiB/round "
          f"(48d8eb93 review {REVIEW_KIB:.1f} KiB/round); W1 merged increment estimate "
          f"{W1_INCREMENT_KIB:+.1f} KiB/round")
    if slope:
        print(f"this run: slope {int(slope.group(1)) / 1024:.1f} KiB/round, gate {gate.group(1) if gate else '?'}")
    inuse = parse_top(args.profile, "inuse_space")
    alloc = parse_top(args.profile, "alloc_space")
    if not alloc:
        raise SystemExit("the profile names no tagent allocation frame; attribution is empty")
    heap = re.search(r"heap=(\d+)->(\d+)", text)
    print()
    print(f"{'source (alloc_space)':78} {'W1 incr':>8} {'alloc KiB/r':>12}")
    print("-" * 104)
    for kib, name in sorted(alloc, reverse=True)[:15]:
        print(f"{name:78} {'—':>8} {kib / args.rounds:12.2f}")
    print("-" * 104)
    print(f"{'W1 merged increment (01+04+05, red - review, estimate)':78} {W1_INCREMENT_KIB:8.1f} {'—':>12}")
    if heap:
        before, after = int(heap.group(1)), int(heap.group(2))
        print(f"retained at last sample: heap {before} -> {after} bytes "
              f"= {(after - before) / args.rounds / 1024:.1f} KiB/round over {args.rounds} rounds")
    print("note: inuse_space is written at process exit and no longer names the run's "
          "resident set; the retention number above is the log's stable-window heap, "
          "and alloc_space attributes the churn that feeds it.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
