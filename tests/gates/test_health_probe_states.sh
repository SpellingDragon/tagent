#!/usr/bin/env bash
# tests/gates/test_health_probe_states.sh — 08/8.2 N5 锚：运维脚本五态探测门。
#
# 冻结的验收面（改这里＝改验收面，须随派发单走）：
#   1. 五态各自可辨：200→healthy 继续 / 401、403→auth_fail 告警退出 /
#      超时→timeout 等待窗 / 拒连→unavailable 等待窗。
#   2. 负锚 auth_fail_never_kills：401/403 路径的 kill/stop 动作计数为 0，
#      且带 instrument 正控（影子 kill 必须先记到数，否则"0 计数"是死仪器）。
#   3. 结构锚（前移证据）：前置探测守卫在首个 kill -TERM/-KILL 之前，
#      脚本内不再残留 "401"/"403" 内联专判。
#   4. 语义不回退：重启后健康门控循环（90×2s）与 RESTART OK 判据仍在。
#
# 运行方式：被测脚本面向 Linux（ss、/proc、flock、stat -c），宿主非 Linux 时
# 不跑整脚本；本门按标记区间**逐字提取**两个脚本的 health-probe-block，
# 用真实 curl 打本地 stub（200/401/403/只连不应答/关闭端口）驱动分支判定。
#
# usage: bash tests/gates/test_health_probe_states.sh
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.." || exit 1

SCRIPTS=(
    "examples/wechat-bot/restart-tagent.sh"
    "examples/wechat-bot/restart-maintenance.sh"
)

TMP="$(mktemp -d "${TMPDIR:-/tmp}/health-probe-states.XXXXXX")" || exit 1
STUB_PID=""
PASSED=0
FAILED=0

cleanup() {
    if [ -n "$STUB_PID" ]; then
        kill "$STUB_PID" 2>/dev/null
        wait "$STUB_PID" 2>/dev/null
    fi
    rm -rf "$TMP"
}
trap cleanup EXIT

check() { # $1=描述 $2=实测 $3=期望
    if [ "$2" = "$3" ]; then
        PASSED=$((PASSED + 1))
        printf '    ok   %s [%s]\n' "$1" "$2"
    else
        FAILED=$((FAILED + 1))
        printf '    FAIL %s: got [%s] want [%s]\n' "$1" "$2" "$3"
    fi
}

# ---- 本地 stub：200 / 401 / 403 / 只连不应答（超时）/ 未监听端口（拒连）----
PORTS="$(python3 - <<'PY'
import socket
ports = []
for _ in range(5):
    s = socket.socket()
    s.bind(('127.0.0.1', 0))
    ports.append(s.getsockname()[1])
    s.close()
print(' '.join(str(p) for p in ports))
PY
)" || { echo "FATAL: cannot allocate stub ports"; exit 1; }
set -- $PORTS
P_OK="$1"; P_401="$2"; P_403="$3"; P_HANG="$4"; P_REFUSED="$5"

cat > "$TMP/stub.py" <<'PY'
import socket
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer


def handler_for(code):
    class H(BaseHTTPRequestHandler):
        def do_GET(self):
            body = b'ok'
            self.send_response(code)
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *a):
            pass

    return H


def hang(port):
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(('127.0.0.1', port))
    s.listen(16)
    held = []
    while True:
        conn, _ = s.accept()   # 接住但永不应答 -> curl -m 超时
        held.append(conn)


for spec in sys.argv[1:]:
    port, mode = spec.split(':')
    port = int(port)
    if mode == 'hang':
        threading.Thread(target=hang, args=(port,), daemon=True).start()
    else:
        srv = HTTPServer(('127.0.0.1', port), handler_for(int(mode)))
        threading.Thread(target=srv.serve_forever, daemon=True).start()
print('READY', flush=True)
while True:
    time.sleep(1)
PY

python3 "$TMP/stub.py" "$P_OK:200" "$P_401:401" "$P_403:403" "$P_HANG:hang" \
    > "$TMP/stub.log" 2>&1 &
STUB_PID=$!

ready=0
for _ in $(seq 1 100); do
    if grep -q READY "$TMP/stub.log" 2>/dev/null; then ready=1; break; fi
    sleep 0.1
done
if [ "$ready" != 1 ]; then
    echo "FATAL: stub server did not come up:"; cat "$TMP/stub.log"; exit 1
fi

# ---- 提取被测脚本的探测块并驱动一次判定 ----
extract_block() { # $1=script $2=out
    awk '/^# >>> health-probe-block/{f=1;next} /^# <<< health-probe-block/{f=0} f' "$1" > "$2"
}

run_probe() { # $1=block $2=port $3=kill log -> CLASS/CODE/RC
    (
        PORT="$2"
        PROBE_WAIT_TRIES=1
        PROBE_WAIT_INTERVAL=0
        PROBE_MAX_TIME=1
        PROBE_OUT=/dev/null
        KILL_LOG="$3"
        log() { :; }
        kill() { printf '%s\n' "$*" >> "$KILL_LOG"; }   # 影子 kill：只记数，不发信号
        . "$1"
        rc=0
        probe_guard || rc=$?
        printf 'CLASS=%s\nCODE=%s\nRC=%s\n' "$PROBE_CLASS" "$PROBE_CODE" "$rc"
    )
}

guards_precede_first_kill() { # $1=script
    local first_kill last_guard probe_at
    first_kill="$(grep -nE '^[[:space:]]*kill[[:space:]]+-(TERM|KILL)' "$1" | head -1 | cut -d: -f1)"
    [ -n "$first_kill" ] || return 1
    last_guard="$(grep -n 'PROBE_RC" = 3' "$1" | tail -1 | cut -d: -f1)"
    [ -n "$last_guard" ] || return 1
    [ "$last_guard" -lt "$first_kill" ] || return 1
    probe_at="$(grep -n 'probe_guard' "$1" | head -1 | cut -d: -f1)"
    [ -n "$probe_at" ] || return 1
    [ "$probe_at" -lt "$first_kill" ] || return 1
    return 0
}

STATES_TOTAL=0
STATES_OK=0
idx=0
for s in "${SCRIPTS[@]}"; do
    idx=$((idx + 1))
    echo "== $s =="

    # 4. 语法
    if bash -n "$s" 2>"$TMP/syntax-$idx.err"; then check "bash -n" "ok" "ok"
    else check "bash -n" "$(cat "$TMP/syntax-$idx.err")" "ok"; fi

    block="$TMP/block-$idx.sh"
    extract_block "$s" "$block"
    if [ -s "$block" ]; then check "health-probe-block present" "ok" "ok"
    else check "health-probe-block present" "missing" "ok"; continue; fi

    killlog="$TMP/kill-$idx.log"
    : > "$killlog"

    # 2a. instrument 正控：影子 kill 必须先记到数（否则下面的 0 计数无意义）
    ( kill() { printf '%s\n' "$*" >> "$killlog"; }; kill -TERM 999999 )
    check "negative-anchor instrument positive control (shadowed kill records)" \
        "$(wc -l < "$killlog" | tr -d ' ')" "1"
    : > "$killlog"

    # 1. 五态
    states_here=0
    for row in "healthy|$P_OK|healthy|0" \
               "auth_fail-401|$P_401|auth_fail|3" \
               "auth_fail-403|$P_403|auth_fail|3" \
               "timeout|$P_HANG|timeout|0" \
               "refused|$P_REFUSED|unavailable|0"; do
        name="${row%%|*}"; rest="${row#*|}"
        port="${rest%%|*}"; rest="${rest#*|}"
        want_class="${rest%%|*}"; want_rc="${rest##*|}"

        states_here=$((states_here + 1))
        STATES_TOTAL=$((STATES_TOTAL + 1))

        eval "$(run_probe "$block" "$port" "$killlog")"
        kills="$(wc -l < "$killlog" | tr -d ' ')"

        verdict="PASS"
        if [ "$CLASS" != "$want_class" ] || [ "$RC" != "$want_rc" ] || [ "$kills" != "0" ]; then
            verdict="FAIL"
            FAILED=$((FAILED + 1))
        else
            PASSED=$((PASSED + 1))
            STATES_OK=$((STATES_OK + 1))
        fi
        printf '  [%d/5] %-14s port=%s -> class=%s rc=%s kills=%s : %s\n' \
            "$states_here" "$name" "$port" "$CLASS" "$RC" "$kills" "$verdict"
    done
    echo "  states asserted: $states_here/5"

    # 2b. 负锚 auth_fail_never_kills：401/403 路径零 kill，且守卫先于首个 kill
    : > "$killlog"
    eval "$(run_probe "$block" "$P_401" "$killlog")"
    k401="$(wc -l < "$killlog" | tr -d ' ')"
    : > "$killlog"
    eval "$(run_probe "$block" "$P_403" "$killlog")"
    k403="$(wc -l < "$killlog" | tr -d ' ')"
    if [ "$k401" = "0" ] && [ "$k403" = "0" ] && guards_precede_first_kill "$s"; then
        PASSED=$((PASSED + 1))
        echo "  negative anchor auth_fail_never_kills: PASS (kill(401)=$k401 kill(403)=$k403, guard<first-kill)"
    else
        FAILED=$((FAILED + 1))
        echo "  negative anchor auth_fail_never_kills: FAIL (kill(401)=$k401 kill(403)=$k403, guard order=$(guards_precede_first_kill "$s" && echo ok || echo violated))"
    fi

    # 3. 内联专判已集中（不再有 "401"/"403" 硬编码分支）
    check "no inline 401/403 branch left" "$(grep -cE '"[0-9]*40[13]"' "$s")" "0"

    # 4. 重启后健康门控语义不回退
    check "post-restart health loop 90x2s kept" "$(grep -cF 'for i in $(seq 1 90)' "$s")" "1"
    check "RESTART OK verdict kept" "$([ "$(grep -cF 'RESTART OK' "$s")" -ge 1 ] && echo present || echo missing)" "present"

    echo
done

# 5. restart-tagent.sh 的 healthz body 证据行（-o 目标未被探针块吞掉）
check "restart-tagent healthz body file kept" \
    "$([ "$(grep -cF '/tmp/tagent_healthz.json' "examples/wechat-bot/restart-tagent.sh")" -ge 1 ] && echo present || echo missing)" \
    "present"

echo "------------------------------------------------------------"
echo "states asserted: $STATES_OK/$STATES_TOTAL (5 per script x ${#SCRIPTS[@]} scripts)"
echo "checks passed: $PASSED, failed: $FAILED"
if [ "$FAILED" != "0" ]; then
    echo "RESULT: FAIL"
    exit 1
fi
echo "RESULT: PASS"
exit 0
