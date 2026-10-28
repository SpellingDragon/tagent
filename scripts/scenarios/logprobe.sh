#!/bin/sh
# logprobe.sh — 机器人日志快速体检：只输出计数与截断行，防超长 WS 帧 JSON 刷屏
# usage: logprobe.sh <logfile> [pattern...]
L="${1:?usage: logprobe.sh <logfile> [pattern...]}"
shift || true
echo "file: $L ($(wc -c < "$L") bytes, mtime $(date -r "$L" '+%m-%d %T'))"
echo "patterns total:"
for p in GROUP_MESSAGE_CREATE C2C_MESSAGE_CREATE group-mention chat-gate gate-audit ops-cmd dispatch panic; do
  printf '  %-26s %s\n' "$p" "$(grep -ac "$p" "$L" 2>/dev/null)"
done
[ $# -gt 0 ] && { echo "custom:"; for p in "$@"; do printf '  %-26s %s\n' "$p" "$(grep -ac "$p" "$L")"; done; }
echo "tail (each line cut to 100 chars):"
tail -8 "$L" | cut -c1-100
