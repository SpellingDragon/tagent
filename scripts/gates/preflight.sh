#!/usr/bin/env bash
# preflight.sh — commit-time preflight: one command for the local chain that must
# be green together before a change lands. It lives next to the gates it drives;
# scripts/preflight.sh is the stable forwarding entry point.
#
#   * lint.sh             gofmt/build/vet, comment + doc budgets, checker selftests,
#                         runtime contracts and the repository hygiene gates
#   * golden deterministic  the real runtime driven against the frozen scenarios
#   * go test ./...       root module (and both example modules) unit tests
#   * wechat-bot smoke    the long-range end-to-end matrix behind the smoke tag
#   * event integrity     the same runner/checker entry points CI runs: ten
#                         independent short rounds, the six long matrix shards and
#                         the fault mutations
#
# Green here means "CI is very likely green", not "the product behaves": the
# deterministic suite is mocked and smoke covers one deployment shape. CI adds
# the race job, the scheduling-floor witnesses and a clean checkout.
#
# The event entry runs by default. There is no opt-in switch that would close the
# long shards by default: a preflight that silently drops them is not the local
# equivalent of the MR gate. --skip-smoke only skips the wechat smoke matrix; the
# run then says it is not full MR-equivalent evidence, and the exit code is
# unchanged.
#
# usage: bash scripts/preflight.sh [--skip-smoke] [--skip-event-long]
set -euo pipefail
# The chain runs from the repository root: every step below names its paths from
# there, and the gates it calls resolve their own homes the same way.
cd "$(dirname "$0")/../.."

skip_smoke=""
skip_event_long=""
while [ $# -gt 0 ]; do
  case "$1" in
    --skip-smoke) skip_smoke=1; shift ;;
    --skip-event-long) skip_event_long=1; shift ;;
    *) echo "preflight.sh: unknown argument $1" >&2; exit 2 ;;
  esac
done

step() { printf '\npreflight: %s\n' "$*"; }
run() {
  printf '  \$ %s\n' "$*"
  "$@"
}

step "lint"
run bash scripts/gates/lint.sh

# 配置链两钉：golden 登记（声明/发布事实类型＋唯一入口，lint 的 architecture
# 契约段已跑）与静态唯一路径检查（第二装载/发布路径、特性专属装配路径都红）。
# lint 已跑两门；这里再显式跑一次，让提交前的输出直接点名配置链。
step "config liveness (chain truth)"
run python3 scripts/gates/test_check_config_liveness.py
run python3 scripts/gates/check_config_liveness.py

step "golden deterministic"
run python3 scripts/scenarios/run_runtime_scenarios.py --scope deterministic

step "root module tests"
run go test ./... -count=1

step "wechat-bot module tests"
(cd examples/wechat-bot && go test ./... -count=1)

step "cli-host module tests"
(cd examples/cli-host && go test ./... -count=1)

if [ -z "$skip_smoke" ]; then
  step "wechat-bot smoke (tag)"
  (cd examples/wechat-bot && go test -tags smoke ./... -run TestLocalSmoke -count=1 -timeout 75m)
else
  echo "preflight: --skip-smoke given; the smoke matrix was not run"
  echo "preflight: smoke 未运行，本次 preflight 不具备完整 MR 等价证据"
fi

# Event integrity runs the same entry points CI runs. The short scope is ten
# independent runner processes; the long scope is the six (seed, window) shards,
# one independent process each; mutations runs the frozen fault registry. These
# stay unconditional: --skip-smoke must not turn the long shards into an opt-in.
event_out="${TMPDIR:-/tmp}/event-consumption-integrity/preflight"

step "event integrity short"
run python3 scripts/scenarios/run_runtime_scenarios.py \
  --contract golden/runtime/event_integrity_scenarios.json \
  --scope event-short --repeat 10 --out "$event_out/event-short"

step "event integrity long"
if [ -n "$skip_event_long" ]; then
  echo "preflight: --skip-event-long given; the long shards were not run"
  echo "preflight: 长跑分片未运行，本次 preflight 不具备完整 MR 等价证据"
else
# One bound for both sides: the 256-window shard folds on every turn and rewrites
# the ledger shard on every commit, so it is the slowest of the six and projects
# to ~7400s on a runner (run 41406032474) and ~2800s locally. The 1200s this
# replaces was calibrated when all six shards measured 533-878s.
#
# The budget follows 03.10's recalibration (one shard 4180s, six serial shards
# 424min) with this file's 1.5 margin, exactly as ci.yml does: a local chain that
# used a different bound could not stand in for the shard CI judges.
#
# The generation logs are pinned under the shard's own out directory so the same
# files the runner judges are the files that stay behind after a failure; a
# default TMPDIR path would let a local rerun overwrite the record it reports on.
#
# All three seeds run here, because the six (seed, window) combinations are the
# surface CI judges and a local chain that runs one seed is not the same claim.
# The parity is mechanical: the workflow's matrix carries the same seed list.
for seed in 1 7 42; do
  for window in 256 32768; do
    run env TAGENT_CALIBRATION_RUN_DIR="$event_out/event-long/seed-$seed-window-$window/generations" \
      python3 scripts/scenarios/run_runtime_scenarios.py \
      --contract golden/runtime/event_integrity_scenarios.json \
      --scope event-long --seed "$seed" --window "$window" --repeat 1 --timeout 6270 \
      --generation-logs "$event_out/event-long/seed-$seed-window-$window/generations" \
      --out "$event_out/event-long/seed-$seed-window-$window"
  done
done
fi

step "event integrity mutations"
run python3 scripts/gates/check_event_integrity_mutations.py --all --json --out "$event_out/mutations"

printf '\npreflight: ok\n'
