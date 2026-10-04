#!/usr/bin/env bash
# Generates docs/api/ from the source's own documentation comments: one Markdown
# file per package plus an index. The output is derived, never hand-edited — change
# the comments and regenerate.
#
# usage: gen_godoc.sh [--check]
#   --check  regenerate into a temporary tree and fail if docs/api/ is stale or
#            contains output this generator would not write
#
# GODOC_OUT overrides the destination (used to verify the generator before the
# repository adopts docs/api/). Stale files are reported, never deleted: removal
# stays a human decision.
set -euo pipefail

check=0
[ "${1:-}" = "--check" ] && check=1
out="${GODOC_OUT:-docs/api}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

gen_root="$tmp/gen"
mkdir -p "$gen_root"

# list_pkgs prints every package of both modules, tagged with its module dir.
list_pkgs() {
  (go list -f '{{.ImportPath}}' ./...) 2>/dev/null
  (cd examples/wechat-bot && go list -f '{{.ImportPath}}' ./...) 2>/dev/null | sed 's#^#bot:#'
}

# slug maps an import path to a file name.
slug() { echo "$1" | sed 's#/#__#g; s#\.#_#g'; }

index="$gen_root/index.md"
{
  echo "# API 文档（生成物）"
  echo
  echo "本目录由 \`scripts/gen_godoc.sh\` 从源码文档注释生成，禁止手工编辑。"
  echo
  echo "| 包 | 文件 |"
  echo "|---|---|"
} > "$index"

count=0
while IFS= read -r pkg; do
  [ -n "$pkg" ] || continue
  mod=""
  case "$pkg" in
    bot:*) mod="examples/wechat-bot"; pkg="${pkg#bot:}" ;;
  esac
  name="$(slug "$pkg")"
  # Index lines (契约:/规格:) are source-side navigation pointers, not API prose: go doc
  # concatenates every file-level index of a package into a run-on blob in the overview.
  # The pointer duty is enforced by scripts/comment_policy, so the generated page drops
  # lines that start with an index marker.
  if [ -n "$mod" ]; then
    body="$(cd "$mod" && go doc -all "$pkg" 2>/dev/null)" || body=""
  else
    body="$(go doc -all "$pkg" 2>/dev/null)" || body=""
  fi
  if [ -z "$body" ]; then
    echo "no documentation" > "$gen_root/$name.md"
  else
    printf '%s\n' "$body" | sed -E -e '/(契约|规格):/d' -e 's#^[[:space:]]*docs/[^[:space:]]+$##' | cat -s > "$gen_root/$name.md"
  fi
  count=$((count + 1))
  printf '| `%s` | [%s.md](%s.md) |\n' "$pkg" "$name" "$name" >> "$index"
done < <(list_pkgs)

# Documentation coverage is judged in one place only — scripts/comment_policy
# (rules missing-package-doc / missing-symbol-doc). Restating it here would create a
# second witness that can drift from the first.
printf '\n包总数：%d。文档覆盖由 scripts/comment_policy 单点判定。\n' "$count" >> "$index"

if [ "$check" -eq 1 ]; then
  if [ ! -d "$out" ]; then
    echo "gen_godoc: $out does not exist; run scripts/gen_godoc.sh and commit it"
    exit 1
  fi
  stale=0
  while IFS= read -r f; do
    base="$(basename "$f")"
    [ "$base" = "README.md" ] && continue
    if [ ! -f "$gen_root/$base" ]; then
      echo "STALE $f (generator no longer writes it)"
      stale=1
    fi
  done < <(find "$out" -maxdepth 1 -name '*.md')
  if ! diff -r -q "$gen_root" "$out" >/dev/null 2>&1; then
    echo "gen_godoc: $out is out of date with the source comments; regenerate and commit"
    diff -r -q "$gen_root" "$out" | head -10
    exit 1
  fi
  [ "$stale" -eq 0 ] || exit 1
  echo "gen_godoc: $out matches the source ($count packages)"
  exit 0
fi

mkdir -p "$out"
cp "$gen_root"/*.md "$out"/
echo "gen_godoc: wrote $((count + 1)) files to $out"
