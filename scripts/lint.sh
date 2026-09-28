#!/usr/bin/env bash
# lint.sh — the repository's code-lint entry point, single place for:
#   * the directory set the comment-policy baseline was generated over
#   * formatting, build and vet
#   * the comment-policy ratchet (no new violations beyond the recorded baseline)
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
POLICY_DIRS=(. examples/wechat-bot)

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
fi

echo "lint: ok"

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
