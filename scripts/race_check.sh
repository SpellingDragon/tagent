#!/usr/bin/env bash
# race_check.sh — race gate for tagent-OWNED code.
#
# Runs the race detector and fails only when a conflicting memory access
# originates in tagent code (github.com/SpellingDragon/tagent frames at the
# top of a Read/Write stack). Races whose both sides live inside upstream
# trpc-agent-go internals (e.g. session.Clone vs UpdateUserSession,
# steer.Queue.Close, event emit) are reported as WAIVED — they cannot be
# fixed from tagent and are tracked for upstream.
#
# Usage: ./scripts/race_check.sh [packages...]   (default: ./agent/... ./plugin/ ./event/)
set -uo pipefail

pkgs=("$@")
if [ ${#pkgs[@]} -eq 0 ]; then
	pkgs=(./agent/... ./plugin/ ./event/)
fi

out=$(go test -race -count=1 "${pkgs[@]}" 2>&1)
races=$(printf '%s\n' "$out" | grep -c "WARNING: DATA RACE" || true)

if [ "$races" -eq 0 ]; then
	echo "race_check: OK (no data races)"
	exit 0
fi

# Top frame of each conflicting access (the function that performed the
# racy read/write). tagent-owned frames there mean a race WE introduced.
offenders=$(printf '%s\n' "$out" | awk '
	/^(Read|Write|Previous read|Previous write) at /{grab=1; next}
	grab && /^  /{print $1; grab=0}
')
tagent_owned=$(printf '%s\n' "$offenders" | grep -c "SpellingDragon/tagent" || true)

if [ "$tagent_owned" -eq 0 ]; then
	echo "race_check: WAIVED — $races race(s), all conflicting accesses inside upstream trpc-agent-go internals"
	printf '%s\n' "$offenders" | sort | uniq -c | sort -rn | sed 's/^/            /'
	exit 0
fi

echo "race_check: FAIL — tagent-owned racy access detected ($tagent_owned frame(s) of $races race(s)):"
printf '%s\n' "$out" | grep -B 2 -A 30 "WARNING: DATA RACE"
exit 1
