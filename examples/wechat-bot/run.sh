#!/bin/bash
#
# tagent WeChat Bot 启动脚本
#
# 功能：
# 1. 前台/后台运行（微信模式）
# 3. 支持优雅关闭
# 4. 日志查看
# 5. 启动时旧日志自动归档清空（logs/archive/，保留最近 N 份）
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
#
# RL 训练完整流程:
#   Terminal 1: ./run.sh rl        (启动 tagent RL 模式)
#   （rl 命令自动设置 TAGENT_CONFIG=tagent.rl.yaml + RL session 参数）

# 获取脚本所在目录
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# 加载 .env（wizard.sh 生成）为默认值：已导出的 shell 环境变量**优先**（不覆盖），保留
# `KEY=x ./run.sh` 的临时覆盖能力（与本文档 RL 示例一致）。仅接受合法 KEY=VALUE 行，跳过注释。
if [[ -f "${SCRIPT_DIR}/.env" ]]; then
    while IFS= read -r _line || [[ -n "$_line" ]]; do
        [[ "$_line" =~ ^[[:space:]]*(#|$) ]] && continue
        [[ "$_line" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]] || continue
        _k="${BASH_REMATCH[1]}"; _v="${BASH_REMATCH[2]}"
        [[ -n "${!_k:-}" ]] && continue   # 已设置则跳过（env 优先于 .env）
        export "${_k}=${_v}"
    done < "${SCRIPT_DIR}/.env"
    unset _line _k _v
fi

# 默认配置
INSTANCE_NAME="${INSTANCE_NAME:-default}"
LOG_DIR="${SCRIPT_DIR}/logs"
PID_FILE="${LOG_DIR}/.pid-${INSTANCE_NAME}"
LOG_FILE="${LOG_DIR}/wechat-bot-${INSTANCE_NAME}.log"
LOG_ARCHIVE_DIR="${LOG_ARCHIVE_DIR:-${LOG_DIR}/archive}"
LOG_ARCHIVE_KEEP="${LOG_ARCHIVE_KEEP:-50}"


# 可观测默认值（用户可通过环境变量覆盖）
export TAGENT_HTTP_PORT="${TAGENT_HTTP_PORT:-8089}"

# ============================================================================
# 显示帮助
# ============================================================================
show_help() {
    cat << EOF
用法: $0 [命令] [选项]

命令:
    setup           初始化向导（检查依赖+引导密钥+生成 .env，等价 ./wizard.sh）
    build           仅构建二进制（go build -o wechat-bot .；供 systemd/CI 部署预构建）
    systemd         打印 systemd 裸机常驻部署指引（unit 模板见 deploy/tagent-wechat.service）
    (无命令)         前台运行机器人
    start           后台启动机器人
    stop            停止后台运行的机器人
    restart         重启机器人
    status          查看机器人运行状态
    log             查看日志 (tail -f)

    rl              前台运行 (RL 训练模式，自动使用 tagent.rl.yaml)
    rl-start        后台启动 (RL 训练模式)
    rl-stop         停止 RL 模式机器人 (同 stop)


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
    LOG_ARCHIVE_DIR          日志归档目录 (默认: logs/archive/)
    LOG_ARCHIVE_KEEP         归档保留份数 (默认: 50)

典型 RL 训练流程:

    # Terminal 1: 启动 tagent (RL 模式, 自动使用 tagent.rl.yaml)
    AREAL_API_KEY=your-key ./run.sh rl


    # 本地调试 (单 GPU, 无 torchrun):
EOF
}

# ============================================================================
# 启动时归档旧日志
# ============================================================================
# 把已有日志移动到 archive/ 带时间戳归档，本次启动从空日志开始；
# 只保留最近 LOG_ARCHIVE_KEEP 份归档（删最旧）。文件不存在/为空则跳过。
archive_log() {
    local log_file="$1"
    [[ ! -s "$log_file" ]] && return 0

    local base ts
    base="$(basename "$log_file")"
    ts="$(date +%Y%m%d-%H%M%S)"
    mkdir -p "$LOG_ARCHIVE_DIR"
    mv "$log_file" "${LOG_ARCHIVE_DIR}/${base%.log}-${ts}.log"
    echo "已归档旧日志: ${LOG_ARCHIVE_DIR}/${base%.log}-${ts}.log"

    # 归档保留上限：超出时删最旧（按修改时间排序）
    local count excess
    count=$(ls -1 "$LOG_ARCHIVE_DIR" 2>/dev/null | wc -l | tr -d ' ')
    excess=$((count - LOG_ARCHIVE_KEEP))
    if ((excess > 0)); then
        ls -1t "$LOG_ARCHIVE_DIR" | tail -n "$excess" | while read -r f; do
            rm -f "${LOG_ARCHIVE_DIR:?}/$f"
        done
        echo "已清理最旧 $excess 份归档（保留最近 ${LOG_ARCHIVE_KEEP} 份）"
    fi
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
    # 根据配置文件确定需要检查的 API key 环境变量
    local config_file="${TAGENT_CONFIG:-tagent.yaml}"
    local key_env="ZAI_API_KEY"  # 默认

    # 尝试从配置文件提取 api_key_env 字段（跳过注释行）
    local extracted
    extracted=$(grep '^[[:space:]]*api_key_env:' "$SCRIPT_DIR/$config_file" 2>/dev/null | head -1 | sed -E 's/[[:space:]]*#.*$//' | sed -E 's/.*: *"?([^"]*)"?/\1/' | tr -d '[:space:]')
    if [[ -n "$extracted" ]]; then
        key_env="$extracted"
    fi

    # 【仅本地开发】缺失的 key 从 ~/.zshrc 回退读取（开发者本机便利）。
    # 注意：这与容器生产的严格策略是有意区分的两级设计——
    #   本地开发(run.sh)：允许 ~/.zshrc 回退，但会提示
    #   容器生产(entrypoint.sh)：严格只认 env/secret 注入，拒绝任何文件回退
    # 多 provider 配置下逐个检查 yaml 中出现的全部 api_key_env（主 agent
    # 与子 agent/工具可能分属不同 provider，如 tencent_hy + zhipu）。
    local all_keys k fallback
    all_keys=$(grep '^[[:space:]]*api_key_env:' "$SCRIPT_DIR/$config_file" 2>/dev/null \
        | sed -E 's/[[:space:]]*#.*$//' | sed -E 's/.*: *"?([^"]*)"?/\1/' | tr -d '[:space:]' | sort -u)
    [[ -z "$all_keys" ]] && all_keys="$key_env"

    for k in $all_keys ZAI_API_KEY; do
        if [[ -z "$(printenv "$k" 2>/dev/null)" ]]; then
            fallback=$(zsh -c "source ~/.zshrc 2>/dev/null && echo \$$k" 2>/dev/null)
            if [[ -n "$fallback" ]]; then
                export "$k=$fallback"
                echo "提示: $k 从 ~/.zshrc 读取（仅限本地开发；容器部署须经 env 注入，见 entrypoint.sh）"
            fi
        fi
    done

    # 硬校验：首个 key（原有行为）+ 主 agent 实际 provider 的 key（否则
    # 缺失会在启动后才暴露）。其余 key 属可选 provider（yaml 声明但未被
    # agent 引用时不设也可启动），仅尽力回退导出不阻断。
    local main_provider main_key
    main_provider=$(awk '/^agents:/{a=1; next} a && /^  [^ ]/{n++} a && n==1 && /provider:/{print $2; exit}' "$SCRIPT_DIR/$config_file" 2>/dev/null)
    if [[ -n "$main_provider" ]]; then
        main_key=$(awk -v p="$main_provider" '$1==p":"{f=1; next} f && /^[^ ]/{exit} f && /api_key_env:/{sub(/[[:space:]]*#.*$/,""); gsub(/["'"'"']/, "", $2); print $2; exit}' "$SCRIPT_DIR/$config_file" 2>/dev/null)
    fi

    local key_value
    key_value=$(printenv "$key_env" 2>/dev/null)
    local main_key_value="present"
    [[ -n "$main_key" ]] && main_key_value=$(printenv "$main_key" 2>/dev/null)

    if [[ -z "$key_value" || -z "$main_key_value" ]]; then
        for k in "$key_env" ${main_key:+"$main_key"}; do
            [[ -n "$(printenv "$k" 2>/dev/null)" ]] && continue
            echo "错误: $k 环境变量未设置 (配置: $config_file)"
            echo
            echo "请设置环境变量:"
            echo "  export $k=your_api_key_here"
            echo
            echo "或在 ~/.zshrc 中添加:"
            echo "  export $k=your_api_key_here"
            echo
        done
        echo "RL 训练模式:"
        echo "  KEY=your_key ./run.sh rl"
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
# systemd 裸机部署指引（常驻服务；unit 模板见 deploy/tagent-wechat.service）
# ============================================================================
show_systemd_guide() {
    cat <<EOF
==============================================
  tagent WeChat Bot · systemd 裸机常驻部署
==============================================
  unit 模板:  ${SCRIPT_DIR}/deploy/tagent-wechat.service
  完整指南:   ${SCRIPT_DIR}/deploy/README.md

  快速步骤(约定部署路径 /opt/tagent/wechat-bot;需 sudo):
    1) 构建二进制:       cd ${SCRIPT_DIR} && ./run.sh build
    2) 初始化密钥:       ./wizard.sh          # 生成 .env(chmod 600,已被 gitignore)
    3) 专用用户 + 装 unit:
         sudo useradd -r -s /usr/sbin/nologin tagent 2>/dev/null || true
         sudo install -m 644 deploy/tagent-wechat.service /etc/systemd/system/
         # 部署路径 ≠ /opt/tagent/wechat-bot 时,替换 unit 内路径后再 daemon-reload:
         #   sudo sed -i "s#/opt/tagent/wechat-bot#${SCRIPT_DIR}#g" /etc/systemd/system/tagent-wechat.service
         sudo chown -R tagent:tagent "${SCRIPT_DIR}"
    4) 启动 + 开机自启:  sudo systemctl daemon-reload && sudo systemctl enable --now tagent-wechat
    5) 观测:             systemctl status tagent-wechat
                         journalctl -u tagent-wechat -f
                         curl -fsS http://127.0.0.1:${TAGENT_HTTP_PORT}/healthz

  说明:systemd 直接管理二进制(Type=simple + Restart=always + SIGTERM 优雅关闭 +
       journald 收日志),安全加固(NoNewPrivileges/ProtectSystem=strict/ReadWritePaths
       白名单/资源上限)对标 docker-compose。数据目录(.wechat-config/data、data/*、
       workspace)须在 ReadWritePaths 内且属 tagent 用户。
EOF
}

# ============================================================================
# 设置 RL 训练模式环境变量
# ============================================================================
setup_rl_env() {
    # 使用 RL 配置文件（除非用户已显式指定其他配置）
    export TAGENT_CONFIG="${TAGENT_CONFIG:-tagent.rl.yaml}"

    # RL 模式使用固定的 user/session ID
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
    archive_log "$LOG_FILE"

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
    archive_log "$LOG_FILE"

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

# ============================================================================

# ============================================================================

# ============================================================================

# ============================================================================

# ============================================================================

# ============================================================================
# 解析参数
# ============================================================================
COMMAND=""
while [[ $# -gt 0 ]]; do
    case $1 in
        setup|wizard|init)
            COMMAND="setup"
            shift
            ;;
        start|stop|restart|status|log|build|systemd)
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
    setup)        exec "${SCRIPT_DIR}/wizard.sh" ;;
    build)        ensure_binary; echo "✓ 构建完成: ${SCRIPT_DIR}/wechat-bot" ;;
    systemd)      show_systemd_guide ;;
    start)        do_start        ;;
    stop)         do_stop         ;;
    restart)      do_restart      ;;
    status)       do_status       ;;
    log)          do_log          ;;
    rl-fg)        setup_rl_env; run_foreground ;;
    rl-start)     setup_rl_env; do_start       ;;
    *)            run_foreground  ;;
esac
