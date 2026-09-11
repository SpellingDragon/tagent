#!/bin/bash
# restart-maintenance.sh — tagent 重启保险 v2（cron 守护）
#
# v1 (restart-tagent.sh) 的缺陷（2026-09-08 事故复盘）:
#   - `flock -n <lock> bash v1.sh` 的锁 fd 被 cron 派生链继承，重启后的 bot
#     进程携带锁 fd 常驻 → 此后 cron 每分钟 flock -n 静默失败(rc=1 无输出)，
#     转世保险彻底失效且无任何痕迹。
#   - /tmp/tagent_restart.done 哨兵在 /tmp（机器重启即清，清理策略波及）→
#     幂等门可能提前失效，与锁失效叠加形成盲区。
#   - 目标 PID 硬编码在 crontab，重启后即过期。
#
# v2 设计:
#   1. 新锁路径 /tmp/tagent_maintenance.lock —— 旧 bot 手上的旧锁管不着这把新锁。
#   2. 健康则静默退出（不刷日志），bot 挂了才重建 —— 这才是"保险"该有的行为。
#   3. 目标 PID 自发现: pidfile 优先，退化为 :8089 监听探测。
#   4. done 哨兵持久化到 $BASE/run/（工作分区，不怕 /tmp 清理）。
#   5. 重启用 python os.posix_spawn 显式 close fd3..fd1023 —— 根治 fd 继承，
#      下一代 bot 不再持有任何锁 fd。
#
# crontab 行（由部署方维护，勿改锁路径）:
#   * * * * * /usr/bin/flock -n /tmp/tagent_maintenance.lock bash \
#     /home/lighthouse/tagent/examples/wechat-bot/restart-maintenance.sh \
#     >> /home/lighthouse/tagent/examples/wechat-bot/logs/restart.log 2>&1 # tagent_restart_marker
set -u
BASE=/home/lighthouse/tagent/examples/wechat-bot
LOGF="$BASE/logs/wechat-bot-default.log"
PIDF="$BASE/logs/.pid-default"
SNAP=/tmp/tagent_env.snapshot
NEWBIN=/tmp/wechat-bot.new
DONEDIR="$BASE/run"
DONEF="$DONEDIR/restart.done"
HEALTH=http://127.0.0.1:8089/healthz
log(){ echo "[$(date '+%F %T')] $*"; }
mkdir -p "$DONEDIR" 2>/dev/null

# ---- 0. 幂等门: 哨兵 pid 仍健康则无事可做 ----
if [ -f "$DONEF" ]; then
    BPID=$(cat "$DONEF" 2>/dev/null)
    if [ -n "$BPID" ] && kill -0 "$BPID" 2>/dev/null && curl -sf -m 3 "$HEALTH" >/dev/null 2>&1; then
        exit 0   # 健康, 静默退出 —— 不写日志, 不打扰
    fi
    log "note: done sentinel pid=$BPID no longer healthy — insurance engaging"
fi

# ---- 1. 目标进程自发现 ----
OLD_PID=""
if [ -s "$PIDF" ]; then
    C=$(cat "$PIDF" 2>/dev/null)
    [ -n "$C" ] && kill -0 "$C" 2>/dev/null && OLD_PID="$C"
fi
if [ -z "$OLD_PID" ]; then
    OLD_PID=$(ss -tlnp 2>/dev/null | grep ':8089 ' | grep -oE 'pid=[0-9]+' | head -1 | cut -d= -f2)
fi

# bot 活着且健康 → 一切正常, 静默退出（含首次部署场景）
if [ -n "$OLD_PID" ] && curl -sf -m 3 "$HEALTH" >/dev/null 2>&1; then
    echo "$OLD_PID" > "$DONEF" 2>/dev/null   # 基线校准: 当前健康 pid 即基线
    exit 0
fi

log "=== insurance session start (old_pid=${OLD_PID:-none}, shell $$) ==="

# ---- 2. 构建新二进制（服务还活着时构建失败零代价） ----
# env 快照双地点回退：/tmp 优先（本次转世刚写的），$BASE/run/env.snapshot 兜底
# （上次成功转世归档的——/tmp 在机器重启后被清时仍然有据可依）。
cd "$BASE" || { log "FATAL: cd $BASE failed"; exit 1; }
if [ ! -f "$SNAP" ] && [ -f "$BASE/run/env.snapshot" ]; then
    SNAP="$BASE/run/env.snapshot"
    log "note: /tmp snapshot missing, using archived fallback $SNAP"
fi
if [ -f "$SNAP" ]; then
    export GOROOT="$(grep '^GOROOT=' "$SNAP" | head -1 | cut -d= -f2-)"
    export GOPATH="$(grep '^GOPATH=' "$SNAP" | head -1 | cut -d= -f2-)"
fi
GOROOT="${GOROOT:-/usr/local/go}"
GOPATH="${GOPATH:-$HOME/go}"
export PATH="$GOROOT/bin:$PATH"
if ! go build -o "$NEWBIN" . ; then
    log "FAIL: build error — service untouched, will retry next minute. ABORT"
    exit 1
fi
log "build ok: $(stat -c%s "$NEWBIN") bytes -> $NEWBIN"

# ---- 3. 原子换 binary（旧进程不受 mv 影响; 保留 .prev 回滚点） ----
cp -p "$BASE/wechat-bot" "$BASE/wechat-bot.prev" || { log "FATAL: backup failed"; exit 1; }
mv -f "$NEWBIN" "$BASE/wechat-bot" || { log "FATAL: swap failed"; exit 1; }
chmod +x "$BASE/wechat-bot"
log "binary swapped (prev kept)"

# ---- 4. 若旧进程还在(僵而不死/healthz 卡死), 优雅终结 ----
if [ -n "$OLD_PID" ] && kill -0 "$OLD_PID" 2>/dev/null; then
    log "sending SIGTERM to $OLD_PID"
    kill -TERM "$OLD_PID" 2>/dev/null
    for i in $(seq 1 15); do
        kill -0 "$OLD_PID" 2>/dev/null || { log "old process exited after ${i}s"; break; }
        sleep 1
    done
    if kill -0 "$OLD_PID" 2>/dev/null; then
        log "grace timeout, SIGKILL $OLD_PID"
        kill -KILL "$OLD_PID" 2>/dev/null
        sleep 1
    fi
    kill -0 "$OLD_PID" 2>/dev/null && { log "FATAL: old process refuses to die"; exit 1; }
    log "old process down"
fi

# ---- 4b. 转世档案（spawn 前落盘：新进程醒来即见，无竞速；D1/D5 执行期修订） ----
if cat > "$DONEDIR/REINCARNATION_NOTICE" <<EOF
reincarnated_at: $(date +%F\ %T)
old_pid: ${OLD_PID:-unknown}
new_pid: pending
reason: ${REASON:-sentinel-dead}
build_sha: $(sha256sum "$BASE/wechat-bot" | cut -c1-12)
binary_size: $(stat -c%s "$BASE/wechat-bot")
log_pointer: $BASE/logs/restart.log
note: staged before spawn (no race); consumed (renamed) by the new process on boot
EOF
then log "reincarnation notice staged (pre-spawn): $DONEDIR/REINCARNATION_NOTICE"
else log "WARN: notice staging failed (non-blocking)"
fi
# ---- 5. 重启: posix_spawn 显式关闭继承 fd（根治锁/管道 fd 泄漏给 bot） ----
python3 - "$BASE" "$SNAP" <<'PYEOF' >> "$LOGF" 2>&1 &
import os, sys
base, snap = sys.argv[1], sys.argv[2]
env = {}
if os.path.isfile(snap):
    for line in open(snap, encoding='utf-8', errors='surrogateescape').read().splitlines():
        if '=' in line:
            k, v = line.split('=', 1)
            env[k] = v
env.setdefault('HOME', os.path.expanduser('~'))
env.setdefault('PATH', '/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin')
os.chdir(base)
# 关键: spawn 前关闭所有非标准 fd —— 不带走外层 flock 锁/日志/管道
file_actions = [(os.POSIX_SPAWN_CLOSE, fd) for fd in range(3, 1024)]
os.posix_spawn('./wechat-bot', ['./wechat-bot'], env, file_actions=file_actions)
sys.exit(0)
PYEOF
SPAWN_PID=$!
echo "$SPAWN_PID" > "$PIDF"
log "launched spawn wrapper pid=$SPAWN_PID (fds 3+ closed before exec)"

# ---- 6. 健康门控 ----
for i in $(seq 1 30); do
    sleep 2
    if curl -sf -m 3 "$HEALTH" > /tmp/tagent_healthz.json 2>/dev/null; then
        NEW_PID=$(ss -tlnp 2>/dev/null | grep ':8089 ' | grep -oE 'pid=[0-9]+' | head -1 | cut -d= -f2)
        echo "${NEW_PID:-$SPAWN_PID}" > "$DONEDIR/restart.done"
        log "RESTART OK: healthz=$(cat /tmp/tagent_healthz.json) new_pid=${NEW_PID:-?} after ~$((i*2))s"
# reincarnation notice: metadata archive for the next process (task 2.1, D3)
        # 归档 env 快照后再清 /tmp：下次转世时若 /tmp 被清理（重启即清），
        # $BASE/run/env.snapshot 作为兜底环境来源（含 API keys 等启动必需变量）。
        # 注意：回退场景下 $SNAP 可能已是归档件本身——cp 到自身无害，但 rm 只许
        # 清 /tmp 原件，绝不碰归档件。
        if [ "$SNAP" != "$BASE/run/env.snapshot" ]; then
            cp -f "$SNAP" "$BASE/run/env.snapshot" 2>/dev/null && chmod 600 "$BASE/run/env.snapshot"
            rm -f "$SNAP"
        fi
        log "=== insurance session end (SUCCESS) ==="
        exit 0
    fi
done
log "FAIL: healthz not up in 60s — manual check required. rollback: cp wechat-bot.prev wechat-bot"
log "=== insurance session end (FAILED) ==="
exit 1
