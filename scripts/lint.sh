#!/usr/bin/env bash
# lint.sh — the repository's code-lint entry point, stable at this path for every
# caller (CI, docs, muscle memory). The gate itself lives in scripts/gates/lint.sh,
# next to the checkers it drives; this file only forwards, so there is exactly one
# copy of the rules and no second place for them to drift.
#
# usage: lint.sh [--strict] [--update-baseline] [--only policy|fmt|build|vet|contracts]
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec bash "$here/gates/lint.sh" "$@"
