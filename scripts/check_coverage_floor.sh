#!/usr/bin/env bash
# check_coverage_floor.sh — package-level coverage floor for memory, rl and evolution.
#
# usage: scripts/check_coverage_floor.sh
#
# Each package's measured statement coverage is compared against the floor
# recorded below. A package below its floor exits 1, so a suite that loses
# coverage fails before merge. The floors are the measured values of the suite
# when they were recorded, and they only ratchet up: raise a floor after a batch
# adds coverage, never lower it.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

# FLOORS holds "package:floor" pairs, the floor being measured statement coverage
# in percent.
FLOORS=(
  "memory:32.5"
  "rl:26.5"
  "evolution:37.9"
)

status=0
for entry in "${FLOORS[@]}"; do
  pkg="${entry%%:*}"
  floor="${entry#*:}"
  output=""
  if ! output="$(go test "./${pkg}/" -count=1 -cover 2>&1)"; then
    printf '%s: suite failed, no coverage measured\n%s\n' "$pkg" "$output" >&2
    status=1
    continue
  fi
  coverage="$(printf '%s\n' "$output" | sed -n 's/.*coverage: \([0-9][0-9.]*\)% of statements.*/\1/p' | tail -n 1)"
  if [ -z "$coverage" ]; then
    printf '%s: no coverage reported by go test -cover\n' "$pkg" >&2
    status=1
    continue
  fi
  if awk -v got="$coverage" -v floor="$floor" 'BEGIN { exit !(got + 0 >= floor + 0) }'; then
    printf '%-10s coverage %.1f%% (floor %s%%) ok\n' "$pkg" "$coverage" "$floor"
  else
    printf '%-10s coverage %.1f%% (floor %s%%) BELOW FLOOR\n' "$pkg" "$coverage" "$floor" >&2
    status=1
  fi
done

if [ "$status" -ne 0 ]; then
  echo "coverage floor: failed" >&2
  exit 1
fi

echo "coverage floor: passed"
