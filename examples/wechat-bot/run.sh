#!/bin/bash
#
# tagent WeChat Bot 启动脚本
#
# 功能：
# 1. 前台/后台运行
# 2. 支持优雅关闭
# 3. 日志查看
#
# 用法:
#   ./run.sh              前台运行
#   ./run.sh start        后台启动
#   ./run.sh stop         停止
#   ./run.sh restart      重启
#   ./run.sh status       查看状态
#   ./run.sh log          查看日志

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 默认配置
INSTANCE_NAME="${INSTANCE_NAME:-default}"
LOG_DIR="${SCRIPT_DIR}/logs"
PID_FILE="${LOG_DIR}/.pid-${INSTANCE_NAME}"
LOG_FILE="${LOG_DIR}/wechat-bot-${INSTANCE_NAME}.log"

# ============================================================================
# 显示帮助
# ============================================================================
show_help() {
    cat << EOF
用法: $0 [命令] [选项]

命令:
    (无命令)         前台运行机器人
    start           后台启动机器人
    stop            停止后台运行的机器人
    restart         重启机器人
    status          查看机器人运行状态
    log             查看日志 (tail -f)

选项:
    -n, --name NAME     实例名称 (默认: default)
    -d, --debug        开启 debug 日志 (等价于 LOG_LEVEL=debug)
    -h, --help          显示帮助信息

环境变量:
    ZAI_API_KEY       API 密钥 (必需)
    CONFIG_PATH       配置文件路径 (默认: config.yaml)
    LOG_LEVEL         日志级别: debug/info/warn/error (默认: info)
EOF
}

# ============================================================================
# 获取 PID
# ============================================================================
get_pid() {
    if [[ -f "$PID_FILE" ]]; then
        cat "$PID_FILE"
    fi
}

# ============================================================================
# 检查进程是否运行
# ============================================================================
is_running() {
    local pid=$(get_pid)
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
        return 0
    fi
    return 1
}

# ============================================================================
# 检查 API Key
# ============================================================================
check_api_key() {
    if [[ -z "${ZAI_API_KEY}" ]]; then
        # 尝试从 ~/.zshrc 加载
        ZAI_API_KEY=$(zsh -c 'source ~/.zshrc 2>/dev/null && echo $ZAI_API_KEY' 2>/dev/null)
        export ZAI_API_KEY
    fi

    if [[ -z "${ZAI_API_KEY}" ]]; then
        echo "错误: ZAI_API_KEY 环境变量未设置"
        echo
        echo "请设置环境变量:"
        echo "  export ZAI_API_KEY=your_api_key_here"
        echo
        echo "或在 ~/.zshrc 中添加:"
        echo "  export ZAI_API_KEY=your_api_key_here"
        exit 1
    fi
}

# ============================================================================
# 构建二进制（如果需要）
# ============================================================================
ensure_binary() {
    if [[ ! -x "$SCRIPT_DIR/wechat-bot" ]]; then
        echo "构建 wechat-bot..."
        cd "$SCRIPT_DIR" && go build -o wechat-bot . || {
            echo "构建失败"
            exit 1
        }
        echo "构建完成"
    fi
}

# ============================================================================
# 前台运行
# ============================================================================
run_foreground() {
    echo "=============================================="
    echo "  tagent WeChat Bot [前台模式]"
    echo "=============================================="
    echo "  实例:  $INSTANCE_NAME"
    echo "  日志:  $LOG_FILE"
    echo "=============================================="
    echo

    check_api_key
    ensure_binary
    mkdir -p "$LOG_DIR"

    exec "$SCRIPT_DIR/wechat-bot" 2>&1 | tee "$LOG_FILE"
}

# ============================================================================
# 后台启动
# ============================================================================
do_start() {
    echo "=============================================="
    echo "  tagent WeChat Bot [后台模式]"
    echo "=============================================="
    echo "  实例:  $INSTANCE_NAME"
    echo "=============================================="
    echo

    check_api_key
    ensure_binary

    if is_running; then
        local pid=$(get_pid)
        echo "错误: 机器人已在运行 (PID: $pid)"
        exit 1
    fi

    mkdir -p "$LOG_DIR"

    echo "启动机器人 (后台)..."
    echo "日志文件: $LOG_FILE"

    nohup "$SCRIPT_DIR/wechat-bot" >> "$LOG_FILE" 2>&1 &
    local pid=$!
    echo "$pid" > "$PID_FILE"

    sleep 1
    if is_running; then
        echo "✓ 机器人已启动 (PID: $pid)"
        echo "  查看日志: ./run.sh log"
    else
        echo "✗ 启动失败，请查看日志: $LOG_FILE"
        rm -f "$PID_FILE"
        exit 1
    fi
}

# ============================================================================
# 停止机器人
# ============================================================================
do_stop() {
    echo "=============================================="
    echo "  tagent WeChat Bot [停止]"
    echo "=============================================="
    echo

    if ! is_running; then
        echo "机器人未在运行"
        rm -f "$PID_FILE"
        exit 0
    fi

    local pid=$(get_pid)
    echo "正在停止机器人 (PID: $pid)..."

    kill -TERM "$pid" 2>/dev/null

    local count=0
    while is_running && [[ $count -lt 10 ]]; do
        sleep 1
        ((count++))
        echo -n "."
    done
    echo

    if is_running; then
        echo "进程未响应，强制关闭..."
        kill -9 "$pid" 2>/dev/null
        sleep 1
    fi

    rm -f "$PID_FILE"

    if ! is_running; then
        echo "✓ 机器人已停止"
    else
        echo "✗ 停止失败"
        exit 1
    fi
}

# ============================================================================
# 查看状态
# ============================================================================
do_status() {
    echo "=============================================="
    echo "  tagent WeChat Bot [状态]"
    echo "=============================================="
    echo "  实例:  $INSTANCE_NAME"
    echo

    if is_running; then
        local pid=$(get_pid)
        echo "状态: ✓ 运行中"
        echo "PID:  $pid"
        if [[ -f "$LOG_FILE" ]]; then
            echo "日志:  $LOG_FILE"
            echo "大小:  $(du -h "$LOG_FILE" | cut -f1)"
        fi
    else
        echo "状态: ✗ 未运行"
        if [[ -f "$PID_FILE" ]]; then
            echo "清理残留 PID 文件..."
            rm -f "$PID_FILE"
        fi
    fi
}

# ============================================================================
# 查看日志
# ============================================================================
do_log() {
    if [[ ! -f "$LOG_FILE" ]]; then
        echo "未找到日志文件: $LOG_FILE"
        echo "提示: 机器人可能尚未启动"
        exit 1
    fi

    echo "=============================================="
    echo "  日志文件: $LOG_FILE"
    echo "  实例名称: $INSTANCE_NAME"
    echo "=============================================="
    echo "按 Ctrl+C 退出"
    echo

    tail -f "$LOG_FILE"
}

# ============================================================================
# 重启
# ============================================================================
do_restart() {
    echo "重启机器人..."
    do_stop
    echo
    do_start
}

# ============================================================================
# 解析参数
# ============================================================================
COMMAND=""
while [[ $# -gt 0 ]]; do
    case $1 in
        start|stop|restart|status|log)
            COMMAND="$1"
            shift
            ;;
        -n|--name)
            INSTANCE_NAME="$2"
            PID_FILE="${LOG_DIR}/.pid-${INSTANCE_NAME}"
            LOG_FILE="${LOG_DIR}/wechat-bot-${INSTANCE_NAME}.log"
            shift 2
            ;;
        -d|--debug)
            export LOG_LEVEL="debug"
            shift
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            echo "未知选项: $1"
            show_help
            exit 1
            ;;
    esac
done

# ============================================================================
# 执行命令
# ============================================================================
case "$COMMAND" in
    start)   do_start   ;;
    stop)    do_stop    ;;
    restart) do_restart ;;
    status)  do_status  ;;
    log)     do_log     ;;
    *)       run_foreground ;;
esac
