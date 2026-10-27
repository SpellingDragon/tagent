#!/usr/bin/env bash
# lint.sh — the repository's code-lint rules, single place for:
#   * the directory set the comment-policy baseline was generated over
#   * module checksums, formatting, build and vet (all three modules)
#   * the comment-policy ratchet (no new violations beyond the recorded baseline)
#   * the markdown document-budget ratchet (total docs may only shrink)
#   * the checker selftests CI runs (runtime/concept/doc-budget/restart/race)
#   * the runtime contracts CI runs (plan/architecture/concept/implementation)
#   * the frozen event-ownership direct-call gate (event_ownership in golden)
#   * the comment policy measured over product packages at zero findings
#   * the OpenSpec strict spec-hygiene gate when the CLI is installed
#
# This file holds the rules; scripts/lint.sh is the stable forwarding entry point
# every caller uses, so CI, the docs and a batch author run the same command.
#
# usage: lint.sh [--strict] [--update-baseline] [--only policy|fmt|build|vet|contracts]
#   --strict           require zero findings (the converged end state, §D8)
#   --update-baseline  rewrite the baseline after a batch lowered counts
#   --only contracts   skip the Go fmt/build/vet/policy blocks; the checker selftests,
#                      the runtime contracts below and the tail gates still run
#
# CI runs this script; a batch author runs the same command, so "it passed
# locally" and "CI is green" cannot mean different things.
set -euo pipefail
# Every path below is named from the repository root (the rule tables, the gate
# scripts and the guests they read), so the script states its own root instead of
# trusting the caller's cwd.
cd "$(dirname "$0")/../.."

# POLICY_DIRS is the canonical scan set: both modules, every package directory
# that holds Go sources. Keeping it here (rather than in each caller) prevents a
# baseline generated over one set from being checked against another.
POLICY_DIRS=(. examples/wechat-bot examples/cli-host)

only=""
strict=""
update=""
while [ $# -gt 0 ]; do
  case "$1" in
    --strict) strict=1; shift ;;
    --update-baseline) update=1; shift ;;
    --only) only="${2:-}"; shift 2 ;;
    *) echo "lint.sh: unknown argument $1" >&2; exit 2 ;;
  esac
done

run() {
  printf '  \$ %s\n' "$*"
  "$@"
}

if [ -z "$only" ] || [ "$only" = "fmt" ]; then
  echo "lint: gofmt"
  unformatted="$(gofmt -l $(printf '%s ' "${POLICY_DIRS[@]}" | tr ' ' '\n' | sed '/^$/d') 2>/dev/null | grep -v '^$' || true)"
  if [ -n "$unformatted" ] && [ -z "$update" ]; then
    echo "lint: gofmt needs these files:"
    echo "$unformatted" | sed 's/^/  /'
    exit 1
  fi
fi

if [ -z "$only" ] || [ "$only" = "build" ]; then
  echo "lint: modules"
  run go mod verify
  echo "lint: build"
  run go build ./...
  (cd examples/wechat-bot && go build ./...)
  (cd examples/cli-host && go build ./...)
fi

if [ -z "$only" ] || [ "$only" = "vet" ]; then
  echo "lint: vet"
  run go vet ./...
  (cd examples/wechat-bot && go vet ./...)
  (cd examples/cli-host && go vet ./...)
fi

if [ -z "$only" ] || [ "$only" = "policy" ]; then
  echo "lint: comment policy (ratchet)"
  args=(./scripts/comment_policy)
  [ -n "$update" ] && args+=(-update-baseline)
  [ -n "$strict" ] && args+=(-strict)
  run go run "${args[@]}" "${POLICY_DIRS[@]}"

  # markdown 总量棘轮：基线 scripts/gates/doc_budget.json，口径见 check_doc_budget.py docstring。
  # 只降不升——--update-baseline 传下去的是 --tighten，它只会把基线往下写。
  echo "lint: markdown doc budget (ratchet)"
  doc_args=(scripts/gates/check_doc_budget.py)
  [ -n "$update" ] && doc_args+=(--tighten)
  run python3 "${doc_args[@]}"
fi

# 检查器自测（CI 的 format-and-contract 与 validators 两个 job 跑的就是这些负例）：
# 只打印规则却退出 0 的检查器、或某条规则变得不可达，先在这里红，而不是等到 CI。
echo "lint: checker selftests"
run python3 -m unittest discover -s scripts/gates -p "test_check_runtime_*.py"
run python3 scripts/gates/test_check_concept_budget.py
run python3 -m unittest discover -s scripts/gates -p "test_check_doc_budget.py"
run python3 scripts/scenarios/test_verify_restart_prefix.py
run bash scripts/gates/test_race_check.sh

# 运行时契约（CI 的 format-and-contract job）：计划/场景契约在无 task-graph 时
# 没有判定对象，如实打印 skip；架构政策、概念预算与实现覆盖对现树恒判定。
#
# 活动 graph 是 openspec/changes/*/task-graph.json（单层通配，归档在
# openspec/changes/archive/ 下，不匹配）。有活动 graph 时计划门必须检查活动
# 变更本身——检查固定的归档路径是对象错位。无活动 graph 时保持旧行为（skip）。
shopt -s nullglob
active_graphs=(openspec/changes/*/task-graph.json)
shopt -u nullglob
echo "lint: runtime plan contract"
if [ "${#active_graphs[@]}" -gt 0 ]; then
  for active_graph in "${active_graphs[@]}"; do
    run python3 scripts/gates/check_runtime_plan.py --change-dir "$(dirname "$active_graph")" --json
  done
else
  echo "  (no active task-graph.json; the plan contract gate has no object, skipped)"
fi
echo "lint: architecture policy"
run python3 scripts/gates/check_runtime_architecture.py --check-policy --json
echo "lint: concept budget"
run python3 scripts/gates/check_concept_budget.py --json
echo "lint: architecture implementation coverage"
run python3 scripts/gates/check_runtime_architecture.py --implementation --json
echo "lint: event ownership (frozen direct-call surface)"
if ! go run ./scripts/codetools event-ownership --check; then
  echo "lint: a frozen owner makes a forbidden/unregistered direct call, or a frozen symbol cannot be resolved"
  exit 1
fi
echo "lint: scenarios contract"
if [ "${#active_graphs[@]}" -gt 0 ]; then
  # v1 金样的判定对象是归档的 runtime-rebuild graph（其 task_ids 属于该变更），
  # 不能把活动 graph 塞给 v1 规则；活动 graph 的 v2 合同在下面单独检查。
  run python3 scripts/gates/check_runtime_evidence.py --check-contract --plan-graph openspec/changes/archive/2026-10-18-runtime-rebuild/task-graph.json --json
  event_graph=""
  for active_graph in "${active_graphs[@]}"; do
    if grep -q '"domain_profile"[[:space:]]*:[[:space:]]*"event-integrity"' "$active_graph"; then
      event_graph="$active_graph"
    fi
  done
  if [ -n "$event_graph" ]; then
    echo "lint: event scenarios contract"
    run python3 scripts/gates/check_runtime_evidence.py --check-contract --contract golden/runtime/event_integrity_scenarios.json --plan-graph "$event_graph" --json
  fi
else
  echo "  (no active task-graph.json; the scenarios contract gate has no object, skipped)"
fi

# 产品包注释测量模式（CI 的 format-and-contract job）：棘轮之外的零容忍面。
# 与 CI 同述：命中数必须可解析且为 0；解析不到数字按 unparsed 拒收，不静默放行。
echo "lint: comment policy (product packages, zero findings)"
product_policy_out="$(go run ./scripts/comment_policy -no-baseline *.go api runtime memory evolution telemetry rl tool internal cli 2>&1)"
printf '%s\n' "$product_policy_out"
product_policy_count="$(printf '%s\n' "$product_policy_out" | sed -n 's/^comment_policy: \([0-9][0-9]*\) finding(s).*/\1/p')"
if [ "$product_policy_count" != "0" ]; then
  echo "lint: comment_policy reported '${product_policy_count:-unparsed}' finding(s) over product packages" >&2
  exit 1
fi

# OpenSpec 主规格 strict（CI 的 openspec job）。CLI 未安装时如实标注跳过——
# 该 job 自己会装 CLI 并把门跑完，本地不把「没装工具」伪装成通过。
if command -v openspec >/dev/null 2>&1; then
  echo "lint: openspec strict (main specs)"
  if ! bash scripts/gates/check-openspec.sh; then
    echo "lint: main specs are not strict-valid" >&2
    exit 1
  fi
else
  echo "lint: openspec CLI absent; the spec-hygiene gate is left to the CI openspec job"
fi

# 标识不承载迭代编号（specs/architecture-guardrails：测试标识不承载迭代编号）
# Mechanical gate: the production chain parses no text prefixes off event
# bodies - discrimination goes through event Type and Meta keys (event-typing).
prefix_hits=$(grep -rEn "HasPrefix\(|TrimPrefix\(|strings\.Split\(" --include="*.go" . \
  | grep -v "_test.go" | grep -v "/examples/" \
  | grep -E "\.Content|\.Summary|Meta\[" || true)
if [ -n "$prefix_hits" ]; then
  echo "lint: text-prefix parsing on event bodies in production code:" >&2
  printf '%s\n' "$prefix_hits" | sed 's/^/  /' >&2
  exit 1
fi
echo "lint: event bodies carry no text-prefix parsing"

if ! go run ./scripts/codetools name-check . examples/wechat-bot; then
  echo "lint: test identifiers carry batch/round numbering (rename them; keep domain vocabulary like Int64/L1/V2)"
  exit 1
fi

# 文档必须只引用存在的文件（docs/.dev 是带日期的历史纪要，按设计排除）
if ! go run ./scripts/codetools doc-refs docs/wiki docs/*.md; then
  echo "lint: documentation cites files that no longer exist"
  exit 1
fi

# 生成物必须与源码注释同步（specs/code-documentation：生成式 API 文档与新鲜度）
if ! bash scripts/gates/gen_godoc.sh --check; then
  echo "lint: docs/api is stale — run scripts/gates/gen_godoc.sh and commit the result"
  exit 1
fi

# 脚本与 CI 不得把读者指向变更过程工件（索引目标限制见 specs/code-documentation）
if ! go run ./scripts/codetools proc-refs scripts .github/workflows; then
  echo "lint: scripts or CI cite change artifacts — state the contract in docs/wiki or specs instead"
  exit 1
fi

# 索引内容必须非空、不是运行期产物、顶层目录已登记（specs/architecture-guardrails：组合根物理边界与追踪卫生）
if ! go run ./scripts/codetools tracked-hygiene; then
  echo "lint: tracked content violates the repository hygiene rules listed above"
  exit 1
fi

# 包布局：文件名只写小写下划线、每包恰一份正名骨架断言、测试文件镜像生产文件
# （判据与理由见 docs/comment-gate-tooling.md#package-layout）
if ! go run ./scripts/codetools package-layout "${POLICY_DIRS[@]}"; then
  echo "lint: package layout violates the rules above — rename to lower_snake.go, merge into skeleton_assert.go, or mirror the production file"
  exit 1
fi

# 包布局门自己的负例见证：只打印不拒绝、或某条规则变得不可达，都在这里红
if ! bash scripts/gates/test_check_package_layout.sh; then
  echo "lint: the package-layout checker no longer rejects its own negative fixtures"
  exit 1
fi

# 机器消费的点号模块引用必须可解析或显式登记（specs/architecture-guardrails：机器消费的点号模块引用必须可解析）
if ! go run ./scripts/codetools dotted-refs; then
  echo "lint: a dotted module reference resolves nowhere and carries no allowlist entry"
  exit 1
fi

# docs 标题的提交不得夹带代码路径（git mv 即时入索引，无 pathspec 的提交会把它扫进来）
if ! go run ./scripts/codetools commit-scope; then
  echo "lint: a docs-titled commit carried code paths — split it"
  exit 1
fi

echo "lint: ok"
