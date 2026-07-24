#!/usr/bin/env bash
# check-openspec.sh — OpenSpec spec-hygiene gate.
#
# Root-cause guard against the failure mode where archives wrote delta-formatted
# text straight into main specs (## ADDED/MODIFIED Requirements with no
# ## Purpose), leaving openspec/specs/ un-strict-validatable.
#
# Run this BEFORE archiving a change and BEFORE committing spec changes:
#   scripts/check-openspec.sh
# Wire it into CI or a pre-commit hook (core.hooksPath) to enforce.
#
# It fails (exit 1) if any main spec is not strict-valid. Active in-flight
# changes under openspec/changes/ are reported but do NOT fail the gate
# (they are WIP); only openspec/specs/ (the source of truth) is enforced.
set -euo pipefail

cd "$(dirname "$0")/.."

if ! command -v openspec >/dev/null 2>&1; then
  echo "openspec CLI not found; install it before running the spec gate." >&2
  exit 2
fi

echo "== openspec validate --all --strict =="
out="$(openspec validate --all --strict 2>&1 || true)"
echo "$out"

# Fail only on spec/ failures (main specs = source of truth). change/ failures
# are WIP and reported separately.
spec_fail="$(printf '%s\n' "$out" | grep -E '✗ spec/' || true)"
if [ -n "$spec_fail" ]; then
  echo "" >&2
  echo "FAIL: main specs are not strict-valid:" >&2
  printf '%s\n' "$spec_fail" >&2
  echo "" >&2
  echo "Fix: each openspec/specs/<cap>/spec.md MUST use '# <cap> Specification'," >&2
  echo "'## Purpose' and '## Requirements' (NOT '## ADDED/MODIFIED Requirements')," >&2
  echo "and every '### Requirement:' MUST contain SHALL/MUST + a '#### Scenario:'." >&2
  exit 1
fi

change_fail="$(printf '%s\n' "$out" | grep -E '✗ change/' || true)"
if [ -n "$change_fail" ]; then
  echo ""
  echo "NOTE: in-flight changes still failing strict (not gated — WIP):"
  printf '%s\n' "$change_fail"
fi

echo ""
echo "OK: all main specs pass validate --strict."
