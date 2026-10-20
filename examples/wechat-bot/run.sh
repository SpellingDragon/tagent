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


选项:
    -n, --name NAME     实例名称 (默认: default)
    -h, --help          显示帮助信息

环境变量（码面实际读取的）:
    <provider>.api_key_env   配置里被 agent 引用的每个 provider 密钥；启动前逐个校验
                             (默认配置: ZAI_API_KEY + DEEPSEEK_API_KEY)
    TAGENT_CONFIG            配置文件路径 (默认: tagent.yaml; 任何严格 schema 文档均可装载)
    TAGENT_API_ENDPOINT      LLM API 地址覆盖 (设置后 entry agent 的端点改指向该地址)
    TAGENT_SESSION_ID        会址前缀标签 (默认: wechat-session, 拼作 <label>:<entry>:)
    TAGENT_WECHAT_WORKSPACE_DIR  附件/长文本落盘根 (覆盖 app.wechat.workspace_dir)
    TAGENT_HTTP_PORT         宿主 HTTP 面端口 (默认: 8089, 只绑 127.0.0.1)
    TAGENT_RL_AUTH_TOKEN     宿主 HTTP 面凭据; 空则只信回环对端 (mail-poller 同名读取)
    LOG_ARCHIVE_DIR          日志归档目录 (默认: logs/archive/)
    LOG_ARCHIVE_KEEP         归档保留份数 (默认: 50)

RL 训练:

    ./run.sh rl 入口已退役:rl 包只保留 capture/export/reward,宿主 main.go 无 RL 分支,
    启动 RL 模式无从谈起。参数基线留在 tagent.rl.yaml (严格 schema 文档,可直接
    TAGENT_CONFIG=tagent.rl.yaml 装载),重接线时以它为起点。
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
# api_key_env_of <provider> [config] — providers 注册表里该 provider 声明的密钥变量名
api_key_env_of() {
    local config_file="${2:-${TAGENT_CONFIG:-tagent.yaml}}"
    sed -nE "s/^[[:space:]]+$1:[[:space:]]*\{.*api_key_env:[[:space:]]*\"?([A-Za-z_][A-Za-z0-9_]*)\"?.*/\1/p" "$SCRIPT_DIR/$config_file" 2>/dev/null | head -1
}

# required_keys [config] — 配置里被 agent 实际引用的密钥变量：全局 provider 一个，
# 加每个 agent 显式覆盖的 provider 各一个（省略 provider 的 agent 回退全局）。
# 声明了但没被引用的 provider 不算必需（可选连接器的密钥可缺）。
required_keys() {
    local config_file="${1:-${TAGENT_CONFIG:-tagent.yaml}}"
    local p
    {
        sed -nE 's/^provider:[[:space:]]*([A-Za-z_][A-Za-z0-9_]*).*/\1/p' "$SCRIPT_DIR/$config_file" 2>/dev/null
        sed -nE 's/^[[:space:]]+provider:[[:space:]]*([A-Za-z_][A-Za-z0-9_]*).*/\1/p' "$SCRIPT_DIR/$config_file" 2>/dev/null
    } | sort -u | while read -r p; do api_key_env_of "$p" "$config_file"; done | sort -u
}

check_api_key() {
    local config_file="${TAGENT_CONFIG:-tagent.yaml}"
    local keys k fallback missing=0
    keys=$(required_keys "$config_file")

    if [[ -z "$keys" ]]; then
        echo "错误: 无法从 $config_file 解出任何 api_key_env（配置是否缺 provider 声明？）"
        exit 1
    fi

    # 【仅本地开发】缺失的 key 从 ~/.zshrc 回退读取（开发者本机便利）。
    # 注意：这与容器生产的严格策略是有意区分的两级设计——
    #   本地开发(run.sh)：允许 ~/.zshrc 回退，但会提示
    #   容器生产(entrypoint.sh)：严格只认 env/secret 注入，拒绝任何文件回退
    for k in $keys; do
        if [[ -z "$(printenv "$k" 2>/dev/null)" ]]; then
            fallback=$(zsh -c "source ~/.zshrc 2>/dev/null && echo \$$k" 2>/dev/null)
            if [[ -n "$fallback" ]]; then
                export "$k=$fallback"
                echo "提示: $k 从 ~/.zshrc 读取（仅限本地开发；容器部署须经 env 注入，见 entrypoint.sh）"
            fi
        fi
    done

    # 硬校验：agent 会真发请求的每个 provider 密钥都必须有值——否则缺失会在
    # 启动后第一回合才暴露（knowledge 走 deepseek，entry 走 zhipu）。
    for k in $keys; do
        [[ -n "$(printenv "$k" 2>/dev/null)" ]] && continue
        missing=1
        echo "错误: $k 环境变量未设置 (配置: $config_file)"
        echo
        echo "请设置环境变量:"
        echo "  export $k=your_api_key_here"
        echo
        echo "或在 ~/.zshrc 中添加:"
        echo "  export $k=your_api_key_here"
        echo
    done
    [[ "$missing" = 1 ]] && exit 1
    return 0
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
                         # 存活探针: curl -fsS http://127.0.0.1:\${TAGENT_HTTP_PORT:-8089}/healthz
                         # (设了 TAGENT_RL_AUTH_TOKEN 则加 -H "Authorization: Bearer \$TAGENT_RL_AUTH_TOKEN")

  说明:systemd 直接管理二进制(Type=simple + Restart=always + SIGTERM 优雅关闭 +
       journald 收日志),安全加固(NoNewPrivileges/ProtectSystem=strict/ReadWritePaths
       白名单/资源上限)对标 docker-compose。数据目录(.wechat-config/data、data/*、
       workspace)须在 ReadWritePaths 内且属 tagent 用户。
EOF
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
        rl|rl-start|rl-stop)
            echo "错误: ./run.sh $1 已退役——rl 包只保留 capture/export/reward，宿主无 RL 分支；参数基线见 tagent.rl.yaml" >&2
            exit 1
            ;;
        -n|--name)
            INSTANCE_NAME="$2"
            PID_FILE="${LOG_DIR}/.pid-${INSTANCE_NAME}"
            LOG_FILE="${LOG_DIR}/wechat-bot-${INSTANCE_NAME}.log"
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
    *)            run_foreground  ;;
esac
