#!/usr/bin/env bash
# Verifies a test-file consolidation preserved the test surface: production code
# untouched, and every test/benchmark function still present with an identical
# comment-free body, assertion count and t.Parallel count (renames via map file).
#
# usage: check_test_merge.sh <baseline-ref> <dir>... [--map FILE] [--explain FILE]
set -euo pipefail

base_ref="${1:?usage: check_test_merge.sh <baseline-ref> <dir>... [--map F] [--explain F]}"
shift
dirs=(); extra=()
while [ $# -gt 0 ]; do
  case "$1" in
    --map|--explain) extra+=("$1" "$2"); shift 2 ;;
    *) dirs+=("$1"); shift ;;
  esac
done
[ ${#dirs[@]} -gt 0 ] || { echo "usage: check_test_merge.sh <baseline-ref> <dir>... [--map F] [--explain F]" >&2; exit 2; }

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
go build -o "$tmp/codetools" ./scripts/codetools
mkdir -p "$tmp/base"
git archive "$base_ref" | tar -x -C "$tmp/base"

# A T batch may touch no production Go file at all. scripts/ holds the gates
# themselves and is exempt; everything else must be byte-identical.
prod_changed="$(git diff --name-only "$base_ref" -- "${dirs[@]}" | grep -E '\.go$' | grep -v '_test\.go$' \
  | grep -v '^scripts/' || true)"
if [ -n "$prod_changed" ]; then
  echo "merge-check: PRODUCTION-CHANGED (a T batch must not touch production code):"
  echo "$prod_changed" | sed 's/^/  /'
  exit 1
fi

"$tmp/codetools" merge-check --base-root "$tmp/base" --head-root . ${extra[@]+"${extra[@]}"} "${dirs[@]}"
