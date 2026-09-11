#!/bin/bash
# restart-tagent.sh — tagent testator/executer script (遗嘱执行人)
#
# Invoked DETACHED from the agent's own process tree:
#   setsid nohup bash restart-tagent.sh <old_pid> >> logs/restart.log 2>&1 &
# The agent dies mid-way; this script must survive and leave evidence for the
# reincarnated agent to verify (销假). Every outcome lands in restart.log.
#
# Phases (1-2 run while old process still serves => build failure costs nothing):
#   0. preflight: old pid alive & holds :8089
#   1. env snapshot from /proc/<pid>/environ (exact env inheritance)
#   2. build new binary to /tmp (system Go 1.24, tagent needs >=1.24)
#   3. atomic binary swap (mv; old kept as wechat-bot.prev)
#   4. SIGTERM old (15s grace) -> SIGKILL fallback
#   4b. stage REINCARNATION_NOTICE (scene block for the next process)
#   5. relaunch with snapshot env (python execve), session detached
#   6. healthz loop 30x2s -> OK/FAIL verdict in restart.log
set -u
BASE=/home/lighthouse/tagent/examples/wechat-bot
OLD_PID="${1:?usage: restart-tagent.sh <old_pid>}"
LOGF="$BASE/logs/wechat-bot-default.log"
PIDF="$BASE/logs/.pid-default"
SNAP=/tmp/tagent_env.snapshot
NEWBIN=/tmp/wechat-bot.new
DONEDIR="$BASE/run"
DONEF="$DONEDIR/restart.done"
mkdir -p "$DONEDIR" 2>/dev/null
log(){ echo "[$(date '+%F %T')] $*"; }

# idempotence gate (crond fires every minute until we succeed/remove marker)
if [ -f /tmp/tagent_restart.done ]; then log "marker present, skip (already restarted)"; exit 0; fi
log "=== restart session start (target pid=$OLD_PID, shell $$) ==="

# 0. preflight
kill -0 "$OLD_PID" 2>/dev/null || { log "FATAL: pid $OLD_PID not alive, abort"; exit 1; }
ss -tlnp 2>/dev/null | grep -q ":8089 .*pid=$OLD_PID," || { log "FATAL: pid $OLD_PID does not hold :8089, abort"; exit 1; }
log "preflight ok: pid alive and holds :8089"

# 1. env snapshot (contains secrets -> chmod 600, removed after success)
tr '\0' '\n' < "/proc/$OLD_PID/environ" > "$SNAP" || { log "FATAL: env snapshot failed"; exit 1; }
chmod 600 "$SNAP"
log "env snapshot: $(wc -l < "$SNAP") vars"

# 2. build (old service stays up meanwhile)
cd "$BASE" || exit 1
export GOROOT="$(grep '^GOROOT=' "$SNAP" | head -1 | cut -d= -f2-)"
export GOPATH="$(grep '^GOPATH=' "$SNAP" | head -1 | cut -d= -f2-)"
export PATH="$GOROOT/bin:$PATH"
if ! go build -o "$NEWBIN" . ; then
    log "FAIL: build error - old binary untouched, service continues on old process. ABORT (no downtime)."
    exit 1
fi
log "build ok: $(stat -c%s "$NEWBIN") bytes -> $NEWBIN"

# 3. swap (running old process unaffected by mv; backup kept)
cp -p "$BASE/wechat-bot" "$BASE/wechat-bot.prev" || { log "FATAL: backup failed"; exit 1; }
mv -f "$NEWBIN" "$BASE/wechat-bot" || { log "FATAL: swap failed"; exit 1; }
chmod +x "$BASE/wechat-bot"
log "binary swapped (prev kept as wechat-bot.prev)"

# 4. graceful stop
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

# 4b. reincarnation notice (staged pre-spawn: next process reads it on boot).
#     Gap fixed 2026-09-11: manual hot-swap produced no scene block — the
#     reincarnated agent booted blind and the old agent had to hand-stage one.
if cat > "$DONEDIR/REINCARNATION_NOTICE" <<EOF
reincarnated_at: $(date +%F\ %T)
old_pid: $OLD_PID
new_pid: pending
reason: manual-hotswap
build_sha: $(sha256sum "$BASE/wechat-bot" | cut -c1-12)
binary_size: $(stat -c%s "$BASE/wechat-bot")
log_pointer: $BASE/logs/restart.log
note: staged by restart-tagent.sh before spawn; consumed (renamed) by the new process on boot
EOF
then log "reincarnation notice staged (pre-spawn): $DONEDIR/REINCARNATION_NOTICE"
else log "WARN: notice staging failed (non-blocking)"
fi

# 5. relaunch with exact env (python execve re-injection), detached session
cd "$BASE" || exit 1
setsid nohup python3 -c "
import os
env = dict(l.split('=',1) for l in open('$SNAP', encoding='utf-8', errors='surrogateescape').read().splitlines() if '=' in l)
os.chdir('$BASE')
os.execve('./wechat-bot', ['./wechat-bot'], env)
" >> "$LOGF" 2>&1 &
NEW_PID=$!
echo "$NEW_PID" > "$PIDF"
log "launched pid=$NEW_PID (env re-injected)"

# 6. health check loop
for i in $(seq 1 30); do
    sleep 2
    if curl -sf --max-time 3 http://127.0.0.1:8089/healthz > /tmp/tagent_healthz.json 2>/dev/null; then
        LISTEN_PID=$(ss -tlnp 2>/dev/null | grep ':8089 ' | grep -oE 'pid=[0-9]+' | head -1 | cut -d= -f2)
        echo "${LISTEN_PID:-$NEW_PID}" > "$DONEF"  # handshake: insurance baseline = real listening pid
        touch /tmp/tagent_restart.done; log "RESTART OK: healthz=$(cat /tmp/tagent_healthz.json) new_pid=${LISTEN_PID:-?} after ~$((i*2))s"
        # archive env snapshot for the next reincarnation (same fallback as insurance v2)
        if [ "$SNAP" != "$DONEDIR/env.snapshot" ]; then
            cp -f "$SNAP" "$DONEDIR/env.snapshot" 2>/dev/null && chmod 600 "$DONEDIR/env.snapshot"
        fi
        rm -f "$SNAP"
        log "=== restart session end (SUCCESS) ==="
        exit 0
    fi
    if ! kill -0 "$NEW_PID" 2>/dev/null; then
        touch /tmp/tagent_restart.done; log "FAIL: new process $NEW_PID died during startup - see tail of $LOGF; rollback hint: cp wechat-bot.prev wechat-bot && restart"
        log "=== restart session end (FAILED) ==="
        exit 1
    fi
done
touch /tmp/tagent_restart.done; log "FAIL: healthz not up in 60s (process may still be initializing) - manual check required"
log "=== restart session end (TIMEOUT) ==="
exit 1
