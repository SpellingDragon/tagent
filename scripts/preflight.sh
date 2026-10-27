#!/usr/bin/env bash
# preflight.sh — commit-time preflight entry point, stable at this path for every
# caller (CI, docs, muscle memory). The chain itself lives in
# scripts/gates/preflight.sh, next to the gates it drives; this file only forwards,
# so there is exactly one copy of the steps and no second place for them to drift.
#
# usage: bash scripts/preflight.sh [--skip-smoke]
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$here/gates/preflight.sh" "$@"
