#!/usr/bin/env python3
"""Compare two Go benchmark runs against a degradation threshold.

Usage:
    bench_compare.py BEFORE.txt AFTER.txt [--threshold 0.10]

BEFORE/AFTER are the outputs of

    go test ./memory/ledger -v -run '^$' \
        -bench 'BenchmarkLocalKVGet|BenchmarkSegmentWindowRead' -benchmem -count=6

Each side must carry at least one iteration line for every benchmark: an empty
run ("[no test files]" or zero iterations) is invalid evidence and exits 2.
The comparison uses the median ns/op of the iteration lines, so one noisy
sample never decides the gate, and reports the percentage change per benchmark.
Exit 0 when every benchmark stays within the threshold, 1 when one degrades
past it, and 2 when either input is not a valid benchmark run.
"""

from __future__ import annotations

import argparse
import re
import sys

BENCHMARKS = ("BenchmarkLocalKVGet", "BenchmarkSegmentWindowRead")
LINE = re.compile(r"^(Benchmark\w+?)(?:-\d+)?\s+(\d+)\s+([0-9.]+)\s+ns/op")
REQUIRED = 2


def parse(path: str, label: str) -> dict[str, list[float]]:
    samples: dict[str, list[float]] = {name: [] for name in BENCHMARKS}
    with open(path, "r", encoding="utf-8") as handle:
        for raw in handle:
            match = LINE.match(raw.strip())
            if not match:
                continue
            name = match.group(1)
            if name in samples and int(match.group(2)) > 0:
                samples[name].append(float(match.group(3)))
    missing = [name for name, values in samples.items() if len(values) < REQUIRED]
    if missing:
        raise SystemExit(
            f"{label}: {path} has too few iteration lines for {', '.join(missing)}; "
            f"zero-iteration output is not a benchmark run"
        )
    return samples


def median(values: list[float]) -> float:
    ordered = sorted(values)
    middle = len(ordered) // 2
    if len(ordered) % 2:
        return ordered[middle]
    return (ordered[middle - 1] + ordered[middle]) / 2


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("before")
    parser.add_argument("after")
    parser.add_argument("--threshold", type=float, default=0.10)
    args = parser.parse_args()

    before = parse(args.before, "before")
    after = parse(args.after, "after")

    print(f"{'benchmark':34} {'before ns/op':>13} {'after ns/op':>12} {'delta':>9}  samples")
    worst = 0.0
    failed = False
    for name in BENCHMARKS:
        old, new = median(before[name]), median(after[name])
        delta = (new - old) / old if old else 0.0
        worst = max(worst, delta)
        verdict = "FAIL" if delta > args.threshold else "ok"
        if delta > args.threshold:
            failed = True
        print(
            f"{name:34} {old:13.2f} {new:12.2f} {delta:+8.1%}  "
            f"{len(before[name])}/{len(after[name])}  {verdict}"
        )
    print(
        f"threshold {args.threshold:.0%}: worst {worst:+.1%} -> "
        f"{'DEGRADED' if failed else 'within threshold'}"
    )
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
