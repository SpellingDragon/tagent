#!/usr/bin/env bash
# preflight.sh — commit-time preflight: one command for the local chain that must
# be green together before a change lands.
#
#   * lint.sh             gofmt/build/vet, comment + doc budgets, checker selftests,
#                         runtime contracts and the repository hygiene gates
#   * golden deterministic  the real runtime driven against the frozen scenarios
#   * go test ./...       root module (and both example modules) unit tests
#   * wechat-bot smoke    the long-range end-to-end matrix behind the smoke tag
#
# Green here means "CI is very likely green", not "the product behaves": the
# deterministic suite is mocked and smoke covers one deployment shape. CI adds
# the race job, the scheduling-floor witnesses and a clean checkout.
#
# usage: bash scripts/preflight.sh [--skip-smoke]
set -euo pipefail
cd "$(dirname "$0")/.."

skip_smoke=""
while [ $# -gt 0 ]; do
  case "$1" in
    --skip-smoke) skip_smoke=1; shift ;;
    *) echo "preflight.sh: unknown argument $1" >&2; exit 2 ;;
  esac
done

step() { printf '\npreflight: %s\n' "$*"; }
run() {
  printf '  \$ %s\n' "$*"
  "$@"
}

step "lint"
run bash scripts/lint.sh

step "golden deterministic"
run python3 scripts/run_runtime_scenarios.py --scope deterministic

step "root module tests"
run go test ./... -count=1

step "wechat-bot module tests"
(cd examples/wechat-bot && go test ./... -count=1)

step "cli-host module tests"
(cd examples/cli-host && go test ./... -count=1)

if [ -z "$skip_smoke" ]; then
  step "wechat-bot smoke (tag)"
  (cd examples/wechat-bot && go test -tags smoke ./... -run TestLocalSmoke -count=1 -timeout 30m)
else
  echo "preflight: --skip-smoke given; the smoke matrix was not run"
fi

printf '\npreflight: ok\n'
