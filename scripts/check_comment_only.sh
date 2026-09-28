#!/usr/bin/env bash
# Verifies that, relative to a baseline revision, only documentation comments
# changed in the given paths. Compiler/tool directives are part of the compared
# text, so adding or dropping one fails the gate.
#
# usage: check_comment_only.sh <baseline-ref> [path...]
set -euo pipefail

base_ref="${1:?usage: check_comment_only.sh <baseline-ref> [path...]}"
shift
paths=("$@"); [ ${#paths[@]} -eq 0 ] && paths=(.)

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
go build -o "$tmp/codetools" ./scripts/codetools

files="$(git diff --name-only "$base_ref" -- "${paths[@]}" | grep -E '\.go$' || true)"
if [ -z "$files" ]; then
  echo "check_comment_only: no .go changes vs ${base_ref}"
  exit 0
fi

mkdir -p "$tmp/base"
git archive "$base_ref" | tar -x -C "$tmp/base"
# Untracked files have no baseline; the gate compares only files present in both.
tracked=""
for f in $files; do
  if [ -f "$tmp/base/$f" ] && [ -f "$f" ]; then tracked="$tracked $f"; fi
done
if [ -z "$tracked" ]; then
  echo "check_comment_only: no comparable files (all changes are additions/deletions)"
  exit 0
fi
"$tmp/codetools" comment-check --base-root "$tmp/base" --head-root . $tracked
