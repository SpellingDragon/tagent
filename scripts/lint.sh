#!/usr/bin/env bash
# lint.sh — the repository's code-lint entry point, single place for:
#   * the directory set the comment-policy baseline was generated over
#   * formatting, build and vet
#   * the comment-policy ratchet (no new violations beyond the recorded baseline)
#   * the markdown document-budget ratchet (total docs may only shrink)
#
# usage: lint.sh [--strict] [--update-baseline] [--only policy|fmt|build|vet]
#   --strict           require zero findings (the converged end state, §D8)
#   --update-baseline  rewrite the baseline after a batch lowered counts
#
# CI runs this script; a batch author runs the same command, so "it passed
# locally" and "CI is green" cannot mean different things.
set -euo pipefail

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
  echo "lint: build"
  run go build ./...
  (cd examples/wechat-bot && go build ./...)
fi

if [ -z "$only" ] || [ "$only" = "vet" ]; then
  echo "lint: vet"
  run go vet ./...
  (cd examples/wechat-bot && go vet ./...)
fi

if [ -z "$only" ] || [ "$only" = "policy" ]; then
  echo "lint: comment policy (ratchet)"
  args=(./scripts/comment_policy)
  [ -n "$update" ] && args+=(-update-baseline)
  [ -n "$strict" ] && args+=(-strict)
  run go run "${args[@]}" "${POLICY_DIRS[@]}"

  # markdown 总量棘轮：基线 scripts/doc_budget.json，口径见 check_doc_budget.py docstring。
  # 只降不升——--update-baseline 传下去的是 --tighten，它只会把基线往下写。
  echo "lint: markdown doc budget (ratchet)"
  doc_args=(scripts/check_doc_budget.py)
  [ -n "$update" ] && doc_args+=(--tighten)
  run python3 "${doc_args[@]}"
fi

# 标识不承载迭代编号（specs/architecture-guardrails：测试标识不承载迭代编号）
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
if ! bash scripts/gen_godoc.sh --check; then
  echo "lint: docs/api is stale — run scripts/gen_godoc.sh and commit the result"
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
if ! bash scripts/test_check_package_layout.sh; then
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
