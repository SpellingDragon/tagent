#!/usr/bin/env bash
# proc_fingerprint.sh — 进程身份权威指纹核验（防 pgrep 误匹配 / PID 复用）
# 用法: ./scripts/proc_fingerprint.sh <PID> [expect_binary_substring]
# 输出: 每行 KEY=VALUE；BIN_MATCH=yes/no 为最终判定行。退出码 0=指纹成立, 1=不成立
# 依据: /proc/<pid>/exe（真实二进制，防 cmdline 子串误匹配）+ starttime（防 PID 复用）
#       + go version -m vcs.revision（Go 二进制构建版本，判"跑的是哪版代码"）
set -u
PID="${1:?usage: proc_fingerprint.sh <PID> [expect_binary_substring]}"
EXPECT="${2:-}"
[ -d "/proc/$PID" ] || { echo "ALIVE=no"; exit 1; }
echo "ALIVE=yes"
EXE=$(readlink "/proc/$PID/exe" 2>/dev/null || echo "UNREADABLE")
echo "EXE=$EXE"
START=$(awk '{print $22}' "/proc/$PID/stat" 2>/dev/null)
echo "STARTTIME_TICKS=$START"
CMD_HEAD=$(tr '\0' ' ' < "/proc/$PID/cmdline" 2>/dev/null | cut -c1-120)
echo "CMDLINE=$CMD_HEAD"
if command -v go >/dev/null 2>&1 && [[ "$EXE" == /* ]]; then
  REV=$(go version -m "$EXE" 2>/dev/null | awk '/vcs.revision/{print $2}')
  MOD=$(go version -m "$EXE" 2>/dev/null | awk '/vcs.modified/{print $2}')
  echo "VCS_REVISION=${REV:-none}"
  echo "VCS_MODIFIED=${MOD:-unknown}"
fi
if [ -n "$EXPECT" ]; then
  if [[ "$EXE" == *"$EXPECT"* ]]; then echo "BIN_MATCH=yes"; exit 0
  else echo "BIN_MATCH=no (expected substring: $EXPECT)"; exit 1; fi
fi
exit 0
