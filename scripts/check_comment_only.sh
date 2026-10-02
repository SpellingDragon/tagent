#!/usr/bin/env bash
# Verifies that, relative to a baseline revision, only documentation comments
# changed in the given paths. Compiler/tool directives are part of the compared
# text, so adding or dropping one fails the gate.
#
# usage: check_comment_only.sh <baseline-ref> [path...]
set -euo pipefail

base_ref="${1:?usage: check_comment_only.sh <baseline-ref> [path...]}"
shift
# Drop the conventional "--" ref/pathspec separator if the caller passed one:
# after an explicit ref, a second "--" is matched as a literal pathspec by
# git diff and silently yields an empty set (= vacuous pass).
paths=()
for a in "$@"; do [ "$a" = "--" ] || paths+=("$a"); done
[ ${#paths[@]} -eq 0 ] && paths=(.)

tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
go build -o "$tmp/codetools" ./scripts/codetools

# Untracked .go files are part of the batch: git diff ignores them unless the
# caller declared intent-to-add, so declare it here (no content staging) and
# re-collect. Without this, "git add" never sees a new file and the gate
# vacuously passes on whole new source files.
git add -A -N -- "${paths[@]}" 2>/dev/null || true
files="$(git diff --name-only "$base_ref" -- "${paths[@]}" | grep -E '\.go$' || true)"
if [ -z "$files" ]; then
  echo "check_comment_only: no .go changes vs ${base_ref}"
  exit 0
fi

mkdir -p "$tmp/base"
git archive "$base_ref" | tar -x -C "$tmp/base"
# Classify by git status, never by filesystem probes: the worktree can hold a
# deleted path as untracked residue, and the real gate is git's own D.
#   D (deleted)   → pass to the tool: MISSING-HEAD is the hard reject (F-P1-1:
#                   the old -f prefilter made whole-file deletions exit 0)
#   A (added)     → nothing to compare; a wholly-new file is comment-only by
#                   construction, but the batch may only exit 0 if EVERY change
#                   is an addition (a pure-addition batch carries no edits)
#   M/R/T/...     → compare baseline vs worktree under the tool
statuses="$(git diff --name-status "$base_ref" -- "${paths[@]}" | grep -E '\.go($|[[:space:]])' || true)"
deleted=$(echo "$statuses" | awk '$1 == "D" { print $2 }' || true)
modified=$(echo "$statuses" | awk '$1 != "A" && $1 != "D" { if ($1 ~ /^R/ && NF >= 3) print $3; else print $2 }' || true)
additions=$(echo "$statuses" | awk '$1 == "A" { print $2 }' || true)

# git-side hard reject first: a deleted path must be rejected even when stale
# untracked residue still sits in the worktree (the tool reads the filesystem,
# git knows the deletion).
if [ -n "$deleted" ]; then
  echo "check_comment_only: REJECT deleted .go file(s) — a comment-only batch must not remove code:"
  echo "$deleted" | sed 's/^/  /'
  exit 1
fi

all=()
[ -n "$modified" ] && while IFS= read -r f; do [ -f "$tmp/base/$f" ] && [ -f "$f" ] && all+=("$f"); done <<< "$modified"

if [ "${#all[@]}" -eq 0 ]; then
  if [ -n "$additions" ]; then
    echo "check_comment_only: pure additions only ($(echo "$additions" | wc -l | tr -d ' ') new .go file(s))"
    exit 0
  fi
  echo "check_comment_only: no comparable .go changes vs ${base_ref}"
  exit 0
fi
"$tmp/codetools" comment-check --base-root "$tmp/base" --head-root . "${all[@]}"
