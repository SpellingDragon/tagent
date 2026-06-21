#!/bin/bash
#
# tagent WeChat Bot 启动脚本
#
# 功能：
# 1. 前台/后台运行（微信模式）
# 2. AReaL 训练启动/停止（areal / areal-stop / areal-log）
# 3. 支持优雅关闭
# 4. 日志查看
#
# 用法:
#   ./run.sh                    前台运行（微信模式）
#   ./run.sh start              后台启动（微信模式）
#   ./run.sh stop               停止 tagent
#   ./run.sh restart            重启 tagent
#   ./run.sh status             查看状态
#   ./run.sh log                查看 tagent 日志
#   ./run.sh rl                 前台运行（RL 训练模式，自动使用 tagent.rl.yaml）
#   ./run.sh rl-start           后台启动（RL 训练模式）
#   ./run.sh areal              前台启动 AReaL 训练
#   ./run.sh areal-start        后台启动 AReaL 训练
#   ./run.sh areal-stop         停止 AReaL 训练
#   ./run.sh areal-log          查看 AReaL 训练日志
#
# RL 训练完整流程:
#   Terminal 1: ./run.sh rl        (启动 tagent RL 模式)
#   Terminal 2: ./run.sh areal     (启动 AReaL 训练)
#   （rl 命令自动设置 TAGENT_CONFIG=tagent.rl.yaml + RL session 参数）

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 默认配置
INSTANCE_NAME="${INSTANCE_NAME:-default}"
LOG_DIR="${SCRIPT_DIR}/logs"
PID_FILE="${LOG_DIR}/.pid-${INSTANCE_NAME}"
LOG_FILE="${LOG_DIR}/wechat-bot-${INSTANCE_NAME}.log"

# AReaL 配置
AREAL_PID_FILE="${LOG_DIR}/.pid-areal"
AREAL_LOG_FILE="${LOG_DIR}/areal-training.log"
AREAL_CONFIG="${AREAL_CONFIG:-${SCRIPT_DIR}/areal_config.yaml}"
AREAL_DIR="${AREAL_DIR:-$(dirname "$SCRIPT_DIR")/AReaL}"
AREAL_N_GPUS="${AREAL_N_GPUS:-8}"
AREAL_TRAIN_SCRIPT="${SCRIPT_DIR}/train_tagent.py"
AREAL_PYTHON="${AREAL_PYTHON:-python3}"
AREAL_USE_TORCHRUN="${AREAL_USE_TORCHRUN:-true}"

# 可观测默认值（用户可通过环境变量覆盖）
export TAGENT_HTTP_PORT="${TAGENT_HTTP_PORT:-8089}"

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

    rl              前台运行 (RL 训练模式，自动使用 tagent.rl.yaml)
    rl-start        后台启动 (RL 训练模式)
    rl-stop         停止 RL 模式机器人 (同 stop)

    areal           前台启动 AReaL 训练
    areal-start     后台启动 AReaL 训练
    areal-stop      停止 AReaL 训练
    areal-log       查看 AReaL 训练日志

选项:
    -n, --name NAME     实例名称 (默认: default)
    -d, --debug         开启 debug 日志 (等价于 LOG_LEVEL=debug)
    --otlp ENDPOINT     启用 OTLP 追踪导出 (等价于 OTEL_EXPORTER_OTLP_ENDPOINT)
    -h, --help          显示帮助信息

环境变量:
    ZAI_API_KEY              API 密钥 (默认配置 tagent.yaml 必需)
    AREAL_API_KEY            AReaL proxy 认证 key (RL 配置 tagent.rl.yaml 必需)
    TAGENT_CONFIG            配置文件路径 (默认: tagent.yaml; ./run.sh rl 自动设为 tagent.rl.yaml)
    LOG_LEVEL                日志级别: debug/info/warn/error (默认: info)
    TAGENT_HTTP_PORT         HTTPAPI 监听端口 (默认: 8089, 端点: /healthz /task)
    TAGENT_API_ENDPOINT      LLM API 地址覆盖 (设置后 LLM 请求路由到指定地址)
    TAGENT_USER_ID           持久事件循环用户 ID (默认: wechat-user)
    TAGENT_SESSION_ID        持久事件循环会话 ID (默认: wechat-session)
    OTEL_EXPORTER_OTLP_ENDPOINT  OTLP gRPC 端点 (可选)
    AREAL_DIR                AReaL 安装路径 (默认: ../AReaL)
    AREAL_CONFIG             AReaL 训练配置 (默认: areal_config.yaml)
    AREAL_N_GPUS             AReaL 训练 GPU 数量 (默认: 8)
    AREAL_PYTHON             Python 解释器 (默认: python3)
    AREAL_USE_TORCHRUN       是否使用 torchrun (默认: true, 设为 false 则直接 python)
    AREAL_EXTRA_ARGS         AReaL 训练额外参数 (如 scheduler.type=local)

典型 RL 训练流程:
    # 0. 安装 AReaL (首次)
    cd ../AReaL && pip install -e . && cd -

    # Terminal 1: 启动 tagent (RL 模式, 自动使用 tagent.rl.yaml)
    AREAL_API_KEY=your-key ./run.sh rl

    # Terminal 2: 启动 AReaL 训练
    ./run.sh areal

    # 本地调试 (单 GPU, 无 torchrun):
    AREAL_USE_TORCHRUN=false AREAL_N_GPUS=1 \
    AREAL_EXTRA_ARGS="scheduler.type=local" ./run.sh areal
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

get_areal_pid() {
    if [[ -f "$AREAL_PID_FILE" ]]; then
        cat "$AREAL_PID_FILE"
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

is_areal_running() {
    local pid=$(get_areal_pid)
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
        return 0
    fi
    return 1
}

# ============================================================================
# 检查 API Key
# ============================================================================
check_api_key() {
    # 根据配置文件确定需要检查的 API key 环境变量
    local config_file="${TAGENT_CONFIG:-tagent.yaml}"
    local key_env="ZAI_API_KEY"  # 默认

    # 尝试从配置文件提取 api_key_env 字段（跳过注释行）
    local extracted
    extracted=$(grep '^[[:space:]]*api_key_env:' "$SCRIPT_DIR/$config_file" 2>/dev/null | head -1 | sed -E 's/.*: *"?([^"]*)"?/\1/')
    if [[ -n "$extracted" ]]; then
        key_env="$extracted"
    fi

    # 检查对应的环境变量
    local key_value
    key_value=$(printenv "$key_env" 2>/dev/null)

    # ZAI_API_KEY 尝试从 ~/.zshrc 加载（兼容现有用户配置）
    if [[ -z "$key_value" && "$key_env" == "ZAI_API_KEY" ]]; then
        key_value=$(zsh -c 'source ~/.zshrc 2>/dev/null && echo $ZAI_API_KEY' 2>/dev/null)
        if [[ -n "$key_value" ]]; then
            export ZAI_API_KEY="$key_value"
        fi
    fi

    if [[ -z "$key_value" ]]; then
        echo "错误: $key_env 环境变量未设置 (配置: $config_file)"
        echo
        echo "请设置环境变量:"
        echo "  export $key_env=your_api_key_here"
        echo
        if [[ "$key_env" == "ZAI_API_KEY" ]]; then
            echo "或在 ~/.zshrc 中添加:"
            echo "  export $key_env=your_api_key_here"
        fi
        echo
        echo "RL 训练模式:"
        echo "  $key_env=your_key ./run.sh rl"
        exit 1
    fi
}

# ============================================================================
# 构建二进制（如果需要）
# ============================================================================
ensure_binary() {
    # 始终运行 go build —— 若源码未变则极快（无重编译），若已变则自动重建
    echo "检查构建..."
    cd "$SCRIPT_DIR" && go build -o wechat-bot . || {
        echo "构建失败"
        exit 1
    }
}

# ============================================================================
# 设置 RL 训练模式环境变量
# ============================================================================
setup_rl_env() {
    # 使用 RL 配置文件（除非用户已显式指定其他配置）
    export TAGENT_CONFIG="${TAGENT_CONFIG:-tagent.rl.yaml}"

    # RL 模式使用固定的 user/session ID（与 areal_config.yaml 中的 adapter 参数一致）
    export TAGENT_USER_ID="${TAGENT_USER_ID:-rl-user}"
    export TAGENT_SESSION_ID="${TAGENT_SESSION_ID:-rl-session}"

    echo "=============================================="
    echo "  RL 训练模式"
    echo "=============================================="
    echo "  配置:      $TAGENT_CONFIG"
    echo "  User ID:   $TAGENT_USER_ID"
    echo "  Session:   $TAGENT_SESSION_ID"
    echo "  HTTPAPI:   http://localhost:${TAGENT_HTTP_PORT}"
    echo "=============================================="
    echo
    echo "  提示: 在另一个终端运行 ./run.sh areal 启动训练"
    echo
}

# ============================================================================
# 前台运行
# ============================================================================
run_foreground() {
    echo "=============================================="
    echo "  tagent WeChat Bot [前台模式]"
    echo "=============================================="
    echo "  实例:  $INSTANCE_NAME"
    echo "  配置:  ${TAGENT_CONFIG:-tagent.yaml}"
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
    echo "  配置:  ${TAGENT_CONFIG:-tagent.yaml}"
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
        echo "tagent:  ✓ 运行中 (PID: $pid)"
        if [[ -f "$LOG_FILE" ]]; then
            echo "  日志:  $LOG_FILE"
            echo "  大小:  $(du -h "$LOG_FILE" | cut -f1)"
        fi
    else
        echo "tagent:  ✗ 未运行"
        if [[ -f "$PID_FILE" ]]; then
            echo "清理残留 PID 文件..."
            rm -f "$PID_FILE"
        fi
    fi
    echo

    # AReaL 状态
    if is_areal_running; then
        local areal_pid=$(get_areal_pid)
        echo "AReaL:   ✓ 运行中 (PID: $areal_pid)"
        if [[ -f "$AREAL_LOG_FILE" ]]; then
            echo "  日志:  $AREAL_LOG_FILE"
        fi
    else
        echo "AReaL:   ✗ 未运行"
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
# 检查 AReaL 环境
# ============================================================================
check_areal_env() {
    if [[ ! -d "$AREAL_DIR" ]]; then
        echo "错误: AReaL 目录不存在: $AREAL_DIR"
        echo
        echo "请设置 AREAL_DIR 环境变量指向 AReaL 安装路径:"
        echo "  export AREAL_DIR=/path/to/AReaL"
        exit 1
    fi

    if [[ ! -f "$AREAL_CONFIG" ]]; then
        echo "错误: AReaL 配置文件不存在: $AREAL_CONFIG"
        echo
        echo "请设置 AREAL_CONFIG 环境变量或创建配置文件"
        exit 1
    fi

    if [[ ! -f "$AREAL_TRAIN_SCRIPT" ]]; then
        echo "错误: 训练脚本不存在: $AREAL_TRAIN_SCRIPT"
        exit 1
    fi

    # 检查 AReaL Python 包是否可导入
    if ! "$AREAL_PYTHON" -c "import areal" 2>/dev/null; then
        echo "错误: 无法导入 areal Python 包"
        echo
        echo "请先安装 AReaL:"
        echo "  cd $AREAL_DIR && pip install -e ."
        exit 1
    fi
}

# ============================================================================
# 构建 AReaL 训练命令
# ============================================================================
build_areal_cmd() {
    local cmd

    if [[ "$AREAL_USE_TORCHRUN" == "true" ]]; then
        cmd="torchrun --nproc_per_node=$AREAL_N_GPUS $AREAL_TRAIN_SCRIPT"
    else
        cmd="$AREAL_PYTHON $AREAL_TRAIN_SCRIPT"
    fi

    cmd="$cmd --config $AREAL_CONFIG"

    # 附加额外参数
    if [[ -n "${AREAL_EXTRA_ARGS}" ]]; then
        cmd="$cmd ${AREAL_EXTRA_ARGS}"
    fi

    echo "$cmd"
}

# ============================================================================
# AReaL 训练 — 前台
# ============================================================================
run_areal_foreground() {
    check_areal_env

    echo "=============================================="
    echo "  AReaL RL 训练 [前台模式]"
    echo "=============================================="
    echo "  训练脚本:  $AREAL_TRAIN_SCRIPT"
    echo "  配置:      $AREAL_CONFIG"
    echo "  AReaL:     $AREAL_DIR"
    echo "  GPU 数:    $AREAL_N_GPUS"
    echo "  torchrun:  $AREAL_USE_TORCHRUN"
    echo "  tagent:    http://localhost:${TAGENT_HTTP_PORT}"
    echo "=============================================="
    echo

    # 环境变量传递给 adapter
    export TAGENT_URL="${TAGENT_URL:-http://localhost:${TAGENT_HTTP_PORT}}"
    export TAGENT_USER_ID="${TAGENT_USER_ID:-rl-user}"
    export TAGENT_SESSION_ID="${TAGENT_SESSION_ID:-rl-session}"

    mkdir -p "$LOG_DIR"

    local cmd
    cmd=$(build_areal_cmd)

    echo "执行: $cmd"
    echo

    exec bash -c "cd '$AREAL_DIR' && $cmd" 2>&1 | tee "$AREAL_LOG_FILE"
}

# ============================================================================
# AReaL 训练 — 后台
# ============================================================================
do_areal_start() {
    check_areal_env

    echo "=============================================="
    echo "  AReaL RL 训练 [后台模式]"
    echo "=============================================="
    echo

    if is_areal_running; then
        local pid=$(get_areal_pid)
        echo "错误: AReaL 训练已在运行 (PID: $pid)"
        exit 1
    fi

    mkdir -p "$LOG_DIR"

    export TAGENT_URL="${TAGENT_URL:-http://localhost:${TAGENT_HTTP_PORT}}"
    export TAGENT_USER_ID="${TAGENT_USER_ID:-rl-user}"
    export TAGENT_SESSION_ID="${TAGENT_SESSION_ID:-rl-session}"

    local cmd
    cmd=$(build_areal_cmd)

    echo "启动 AReaL 训练 (后台)..."
    echo "日志文件: $AREAL_LOG_FILE"
    echo "执行: $cmd"
    echo

    nohup bash -c "cd '$AREAL_DIR' && $cmd" >> "$AREAL_LOG_FILE" 2>&1 &
    local pid=$!
    echo "$pid" > "$AREAL_PID_FILE"

    sleep 2
    if is_areal_running; then
        echo "✓ AReaL 训练已启动 (PID: $pid)"
        echo "  查看日志: ./run.sh areal-log"
    else
        echo "✗ 启动失败，请查看日志: $AREAL_LOG_FILE"
        rm -f "$AREAL_PID_FILE"
        exit 1
    fi
}

# ============================================================================
# 停止 AReaL 训练
# ============================================================================
do_areal_stop() {
    echo "=============================================="
    echo "  AReaL RL 训练 [停止]"
    echo "=============================================="
    echo

    if ! is_areal_running; then
        echo "AReaL 训练未在运行"
        rm -f "$AREAL_PID_FILE"
        exit 0
    fi

    local pid=$(get_areal_pid)
    echo "正在停止 AReaL 训练 (PID: $pid)..."

    # 先尝试优雅停止整个进程组
    kill -TERM "$pid" 2>/dev/null

    local count=0
    while is_areal_running && [[ $count -lt 15 ]]; do
        sleep 1
        ((count++))
        echo -n "."
    done
    echo

    if is_areal_running; then
        echo "进程未响应，强制关闭..."
        kill -9 "$pid" 2>/dev/null
        sleep 1
    fi

    rm -f "$AREAL_PID_FILE"

    if ! is_areal_running; then
        echo "✓ AReaL 训练已停止"
    else
        echo "✗ 停止失败"
        exit 1
    fi
}

# ============================================================================
# 查看 AReaL 日志
# ============================================================================
do_areal_log() {
    if [[ ! -f "$AREAL_LOG_FILE" ]]; then
        echo "未找到日志文件: $AREAL_LOG_FILE"
        echo "提示: AReaL 训练可能尚未启动"
        exit 1
    fi

    echo "=============================================="
    echo "  AReaL 训练日志: $AREAL_LOG_FILE"
    echo "=============================================="
    echo "按 Ctrl+C 退出"
    echo

    tail -f "$AREAL_LOG_FILE"
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
        rl)
            COMMAND="rl-fg"
            shift
            ;;
        rl-start)
            COMMAND="rl-start"
            shift
            ;;
        rl-stop)
            COMMAND="stop"
            shift
            ;;
        areal)
            COMMAND="areal-fg"
            shift
            ;;
        areal-start)
            COMMAND="areal-start"
            shift
            ;;
        areal-stop)
            COMMAND="areal-stop"
            shift
            ;;
        areal-log)
            COMMAND="areal-log"
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
        --otlp)
            export OTEL_EXPORTER_OTLP_ENDPOINT="$2"
            shift 2
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
    start)        do_start        ;;
    stop)         do_stop         ;;
    restart)      do_restart      ;;
    status)       do_status       ;;
    log)          do_log          ;;
    rl-fg)        setup_rl_env; run_foreground ;;
    rl-start)     setup_rl_env; do_start       ;;
    areal-fg)     run_areal_foreground ;;
    areal-start)  do_areal_start  ;;
    areal-stop)   do_areal_stop   ;;
    areal-log)    do_areal_log    ;;
    *)            run_foreground  ;;
esac
