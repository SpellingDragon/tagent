#!/usr/bin/env bash
# trajectory-pack.sh — 按 mtime 时间窗收集执行日志并打包（MANIFEST + tar.gz + sha256）
# Usage:
#   bash scripts/trajectory-pack.sh --since 'YYYY-MM-DD HH:MM' --until 'YYYY-MM-DD HH:MM' \
#        --out /abs/out-dir --src /dirA --glob '*.log' [--src /dirB --glob 'patB'] ...
# 行为:
#   1) 收集 mtime ∈ [since, until] 且匹配对应 --glob 的常规文件 → OUT/files/（跨 src 基名须唯一，后者覆盖）
#   2) 生成 OUT/MANIFEST.txt（mtime+大小+路径，按时间排序）
#   3) 打包 OUT.tar.gz 并输出 sha256
# 设计要点: 时间窗用 epoch 整数比较（stat -c %Y vs date -d +%s），
#   杜绝 2026-10-02 实踩的「字符串 case 通配长度错配 → 0 命中」类 bug。
set -euo pipefail

SINCE=""; UNTIL=""; OUT=""
SRCS=(); GLOBS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --since) SINCE="$2"; shift 2;;
    --until) UNTIL="$2"; shift 2;;
    --out)   OUT="$2"; shift 2;;
    --src)   SRCS+=("$2"); shift 2;;
    --glob)  GLOBS+=("$2"); shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done
[ -n "$SINCE" ] && [ -n "$UNTIL" ] && [ -n "$OUT" ] && [ ${#SRCS[@]} -gt 0 ] || { echo "usage: --since/--until/--out/--src required" >&2; exit 2; }
[ ${#GLOBS[@]} -eq 0 ] || [ ${#GLOBS[@]} -eq ${#SRCS[@]} ] || { echo "--glob 数须与 --src 一致（或缺省）" >&2; exit 2; }

S=$(date -d "$SINCE" +%s); E=$(date -d "$UNTIL" +%s)
[ "$S" -le "$E" ] || { echo "since > until" >&2; exit 2; }
mkdir -p "$OUT/files"

n=0
for i in "${!SRCS[@]}"; do
  d=${SRCS[$i]}; g=${GLOBS[$i]:-*}
  [ -d "$d" ] || { echo "skip non-dir: $d" >&2; continue; }
  for f in "$d"/$g; do
    [ -f "$f" ] || continue
    m=$(stat -c '%Y' "$f")
    if [ "$m" -ge "$S" ] && [ "$m" -le "$E" ]; then
      cp -p "$f" "$OUT/files/"; n=$((n+1))  # -p 保原始 mtime：清单即溯源证据
    fi
  done
done

stat -c '%y  %8s  %n' "$OUT"/files/* 2>/dev/null | sort > "$OUT/MANIFEST.txt" || true
T="${OUT%/}.tar.gz"
tar -czf "$T" -C "$(dirname "$OUT")" "$(basename "$OUT")"
echo "PACKED files=$n tarball=$T size=$(stat -c %s "$T") sha256=$(sha256sum "$T" | cut -d' ' -f1)"
