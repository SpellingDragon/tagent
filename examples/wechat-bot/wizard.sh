#!/usr/bin/env bash
#
# tagent WeChat Bot — 本地部署初始化向导
#
# 一条命令完成「拉取代码后的本地化快速部署」前置：
#   ① 检查依赖（go≥1.24 / tmux 硬性；node / openspec / rustviking 软性可选）
#   ② 交互式引导填写 API 密钥（不回显、不入 shell history）
#   ③ 引导设置 agent 工作根（TAGENT_WORKING_DIR，可设为项目 clone 根，使 agent 操作其下所有仓库）
#   ④ 生成 .env（chmod 600，已被 .gitignore 白名单模式忽略）
#   ⑤ 工作区权限初始化（POSIX ACL：给服务用户追加 rwX 含已有文件，**不改**原有 owner/group/mode）
#   ⑥ 验证主密钥连通性（轻量 embedding 端点，不消耗 chat 额度）
#   ⑦ 提示下一步（./run.sh 会自动加载 .env；远端常驻见 ./run.sh systemd）
#
# 设计原则：个人助手定位——纯 bash、零额外依赖、人在环引导式，非无人值守 CI/CD。
# 幂等：已有 .env 时以其值为默认（回车保留），旧文件自动备份为 .env.bak.<ts>；用户已存在/ACL 已设则跳过。
#
# 用法：
#   ./wizard.sh              交互式向导（7 步）
#   ./wizard.sh --check      仅检查依赖，不引导密钥
#   ./wizard.sh --verify     仅验证已配置密钥的连通性（读 .env / 环境）
#   ./wizard.sh --perms      仅做工作区权限初始化（读 .env 的 TAGENT_WORKING_DIR + ACL）
#   NONINTERACTIVE=1 ./wizard.sh   非交互（CI/管道）：跳过 read，仅用已有环境值

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
ENV_FILE="${ENV_FILE:-.env}"
CONFIG_FILE="${TAGENT_CONFIG:-tagent.yaml}"
# 连通性验证端点（zhipu embedding，轻量、不耗 chat 额度；与 chat 同平台同 key）
VERIFY_URL="https://open.bigmodel.cn/api/paas/v4/embeddings"
# 服务用户（权限初始化用；应与 systemd unit 的 User= 一致，见 deploy/tagent-wechat.service）
SERVICE_USER="${TAGENT_SERVICE_USER:-tagent}"

# ── 输出样式（仅 tty 上色，管道/重定向自动降级为纯文本）──────────────────
if [ -t 1 ]; then
  C_RED=$'\033[31m'; C_GRN=$'\033[32m'; C_YLW=$'\033[33m'
  C_BLU=$'\033[34m'; C_BLD=$'\033[1m';  C_RST=$'\033[0m'
else
  C_RED=""; C_GRN=""; C_YLW=""; C_BLU=""; C_BLD=""; C_RST=""
fi
ok()   { printf '  %s✓%s %s\n' "$C_GRN" "$C_RST" "$*"; }
warn() { printf '  %s⚠%s %s\n' "$C_YLW" "$C_RST" "$*"; }
err()  { printf '  %s✗%s %s\n' "$C_RED" "$C_RST" "$*" >&2; }
info() { printf '  %s•%s %s\n' "$C_BLU" "$C_RST" "$*"; }
step() { printf '\n%s[%s] %s%s\n' "$C_BLD" "$1" "$2" "$C_RST"; }
die()  { printf '\n%s错误:%s %s\n' "$C_RED" "$C_RST" "$*" >&2; exit 1; }

INTERACTIVE=1
if [ "${NONINTERACTIVE:-0}" = "1" ] || [ ! -t 0 ]; then
  INTERACTIVE=0
fi

# 载入已有 .env 作为默认值（不覆盖当前 shell 已导出的非空变量）
load_existing_env() {
  [ -f "$ENV_FILE" ] || return 0
  # 仅读 KEY=VALUE 行，忽略注释；用 subshell source 避免污染（set -a 自动导出）
  set -a
  # shellcheck disable=SC1090
  . "./$ENV_FILE" 2>/dev/null || true
  set +a
}

# ── 步骤 1：依赖检查 ──────────────────────────────────────────────────────
go_version_ok() {
  local v major minor
  v=$(go version 2>/dev/null | grep -oE 'go[0-9]+\.[0-9]+' | head -1 | sed 's/^go//')
  [ -z "$v" ] && return 1
  major="${v%%.*}"; minor="${v##*.}"
  [ "$major" -gt 1 ] && return 0
  [ "$major" -eq 1 ] && [ "$minor" -ge 24 ] && return 0
  return 1
}

DEP_HARD_FAIL=0
check_deps() {
  step "1/7" "检查依赖"
  # go（硬性，≥1.24）
  if command -v go >/dev/null 2>&1; then
    if go_version_ok; then
      ok "go $(go version | grep -oE 'go[0-9]+\.[0-9]+\.[0-9]+') (≥1.24)"
    else
      err "go 版本过低（需 ≥1.24）：$(go version)"; DEP_HARD_FAIL=1
    fi
  else
    err "未找到 go —— 必需（构建二进制）。安装：https://go.dev/dl/ 或 brew install go"; DEP_HARD_FAIL=1
  fi
  # tmux（硬性，action 异步任务层）
  if command -v tmux >/dev/null 2>&1; then
    ok "tmux $(tmux -V | grep -oE '[0-9]+\.[0-9]+' | head -1)（action 异步任务层）"
  else
    err "未找到 tmux —— 必需（exec 工具的异步任务层）。安装：brew install tmux / apt install tmux"; DEP_HARD_FAIL=1
  fi
  # node（软性，openspec / skills 后端）
  if command -v node >/dev/null 2>&1; then
    ok "node $(node -v)（openspec / skills 后端）"
    if command -v openspec >/dev/null 2>&1; then
      ok "openspec CLI（plan 子 agent 的 spec 工具）"
    else
      warn "未找到 openspec CLI —— plan 子 agent 的 spec 工具将不可用"
      info "安装：npm install -g @fission-ai/openspec"
    fi
  else
    warn "未找到 node —— plan 子 agent（openspec）与 skills（url-fetcher 等）将降级"
    info "如需用到 plan/skills：安装 node 22 后 npm install -g @fission-ai/openspec"
  fi
  # rustviking（可选，file 型记忆的向量 KV）
  if command -v rustviking >/dev/null 2>&1; then
    ok "rustviking（file 型记忆的向量 KV 后端）"
  else
    info "未找到 rustviking（可选）—— 默认 localfile 记忆后端无需它；仅 memory.type: file 才需要"
  fi
  # curl（连通验证用，非硬性）
  command -v curl >/dev/null 2>&1 && ok "curl（连通性验证）" || warn "未找到 curl —— 将跳过连通性验证"

  [ "$DEP_HARD_FAIL" = "1" ] && die "硬性依赖缺失，请先安装后重试（go≥1.24 + tmux）"
  ok "硬性依赖齐备"
}

# ── 步骤 2：API 密钥引导 ──────────────────────────────────────────────────
# 收集到的非空密钥（KEY=VALUE），供步骤 3 写 .env
ENV_ENTRIES=()

# prompt_key <变量名> <展示名> <端点> <required:1/0>
prompt_key() {
  local varname="$1" label="$2" endpoint="$3" required="$4"
  local current="${!varname:-}"
  local masked=""
  [ -n "$current" ] && masked="（已设置：${current:0:4}****，回车保留 / 输入新值覆盖）"

  printf '  %s%s%s %s\n' "$C_BLD" "$label" "$C_RST" "$masked"
  [ -n "$endpoint" ] && printf '    端点 %s\n' "$endpoint"

  if [ "$INTERACTIVE" != "1" ]; then
    # 非交互：只用已有值
    if [ -n "$current" ]; then
      ENV_ENTRIES+=("${varname}=${current}")
    elif [ "$required" = "1" ]; then
      die "非交互模式下 ${varname} 未设置（必需）。请先 export ${varname}=... 或用交互模式"
    fi
    return 0
  fi

  local input
  while true; do
    printf '    粘贴密钥（输入不回显）: '
    read -r -s input
    printf '\n'
    if [ -n "$input" ]; then
      current="$input"
      break
    elif [ -n "$current" ]; then
      info "保留已设置的 ${varname}"
      break
    elif [ "$required" != "1" ]; then
      info "跳过 ${varname}（可选）"
      return 0
    else
      warn "${varname} 为必需项，不能为空（或 Ctrl+C 退出后手动编辑 .env）"
    fi
  done
  ENV_ENTRIES+=("${varname}=${current}")
}

collect_keys() {
  step "2/7" "配置 API 密钥（写入 .env，不回显、不入 history）"
  info "主模型 provider：zhipu（GLM Coding Plan，glm-5.3-flash / glm-4.7）"
  prompt_key "ZAI_API_KEY" "ZAI_API_KEY（必需）" "https://open.bigmodel.cn/api/coding/paas/v4" 1

  printf '\n  %s可选 provider（回车逐个跳过；tagent.yaml 未引用则无需填）%s\n' "$C_BLD" "$C_RST"
  prompt_key "DEEPSEEK_API_KEY"  "DEEPSEEK_API_KEY"  "https://api.deepseek.com/v1"                 0
  prompt_key "MOONSHOT_API_KEY"  "MOONSHOT_API_KEY"  "https://api.moonshot.cn/v1"                  0
  prompt_key "TENCENT_HY_API_KEY" "TENCENT_HY_API_KEY" "https://api.lkeap.cloud.tencent.com/plan/v3" 0
  prompt_key "TENCENT_API_KEY"   "TENCENT_API_KEY"   "https://tokenhub.tencentmaas.com/v1"         0
}

# ── 步骤 3：agent 工作根引导（TAGENT_WORKING_DIR）──────────────────────────
# 工作根 = file tools 的 base_dir 与 exec 命令 cwd 的共同基准（见 config.go WorkingDir）。留空 = 进程
# 目录（部署目录）；设为项目 clone 根（如 /home/user/codes）→ agent 可操作该目录下所有仓库,而 tagent
# 自身配置/资源/数据路径不受影响。写入 .env 的 TAGENT_WORKING_DIR（run.sh/systemd 加载）。
collect_working_dir() {
  step "3/7" "设置 agent 工作根目录（TAGENT_WORKING_DIR）"
  if [ -n "${TAGENT_WORKING_DIR:-}" ]; then
    ok "工作根已由环境/.env 设定：$TAGENT_WORKING_DIR"
    ENV_ENTRIES+=("TAGENT_WORKING_DIR=${TAGENT_WORKING_DIR}")   # 确保写入 .env
    return 0
  fi
  if [ "$INTERACTIVE" != "1" ]; then
    info "非交互且未设 TAGENT_WORKING_DIR → 工作根留空（= 进程目录）"
    return 0
  fi
  info "工作根 = agent 文件操作/命令执行的根（file base_dir 与 exec cwd 恒一致 = 单一文件系统视图）"
  info "留空 → 部署目录；或填项目 clone 根绝对路径（如 /home/user/codes）使 agent 能操作其下所有仓库"
  printf '    工作根目录（回车跳过）: '
  local input; read -r input
  if [ -n "$input" ]; then
    case "$input" in "~"|"~/"*) input="$HOME${input#\~}" ;; esac          # 展开 ~
    if [ -d "$input" ]; then
      TAGENT_WORKING_DIR="$(cd "$input" && pwd)"                          # 绝对化 + 规范化
      ENV_ENTRIES+=("TAGENT_WORKING_DIR=${TAGENT_WORKING_DIR}")
      ok "工作根：$TAGENT_WORKING_DIR（步骤 5 将对其做 ACL 权限初始化）"
    else
      warn "目录不存在，已忽略：$input（工作根留空 = 进程目录）"
    fi
  else
    info "工作根留空（= 进程目录）"
  fi
}

# ── 步骤 4：生成 .env ─────────────────────────────────────────────────────
generate_env() {
  step "4/7" "生成 ${ENV_FILE}"
  if [ -f "$ENV_FILE" ]; then
    local bak="${ENV_FILE}.bak.$(date +%Y%m%d-%H%M%S)"
    cp "$ENV_FILE" "$bak"
    info "已备份旧 .env → $(basename "$bak")"
  fi
  {
    echo "# tagent WeChat Bot — 由 wizard.sh 于 $(date '+%Y-%m-%d %H:%M:%S') 生成"
    echo "# 重新运行 ./wizard.sh 可更新；本文件含密钥，已被 .gitignore 白名单模式忽略。"
    echo ""
    local kv
    for kv in "${ENV_ENTRIES[@]}"; do
      echo "$kv"
    done
  } > "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  ok "写入 ${ENV_FILE}（chmod 600，${#ENV_ENTRIES[@]} 个密钥）"
  # 双保险：确认 .env 未被 git 追踪
  if git -C "$SCRIPT_DIR" ls-files --error-unmatch "$ENV_FILE" >/dev/null 2>&1; then
    err "警告：${ENV_FILE} 竟被 git 追踪！请立即 git rm --cached ${ENV_FILE}"
  fi
}

# ── 步骤 5：工作区权限初始化（POSIX ACL，不改原有 owner/group/mode）──────────
# 需求：让 tagent 服务用户能读写工作区（含**已有文件**），但不动工作区原有的用户/组/权限。
# 方案：POSIX ACL（setfacl）追加 named-user 权限——
#   · mask ≥ owning group ⇒ 原有 group 有效权限不变；owner/other 完全不受影响（符合"不影响原本权限"）；
#   · -R 递归覆盖**已有**文件（rwX：文件 rw、目录 rwx），满足"能改动 clone 根下已有文件"；
#   · -d 默认 ACL ⇒ 工作区内**新建**文件自动继承 tagent 权限。
# 降级：非 Linux / 无 setfacl / 无 sudo / 未设工作根 → 告警 + 手动指引，不中断（可选增强）。
setup_permissions() {
  step "5/7" "工作区权限初始化（ACL，服务用户 ${SERVICE_USER}）"
  local wd="${TAGENT_WORKING_DIR:-}"
  if [ -z "$wd" ]; then
    info "未设工作根 → 工作区 = 部署目录（wizard 运行目录），跳过 ACL 初始化"
    return 0
  fi
  if [ ! -d "$wd" ]; then
    warn "工作根不存在：$wd（跳过 ACL；请确认 clone 根路径）"
    return 0
  fi
  if [ "$(uname -s)" != "Linux" ]; then
    warn "非 Linux（$(uname -s)）→ 跳过 ACL（POSIX setfacl 为 Linux；macOS 用 chmod +a，见 deploy/README.md）"
    return 0
  fi
  if ! command -v setfacl >/dev/null 2>&1; then
    warn "未找到 setfacl —— 无法自动做 ACL 初始化"
    info "安装：apt install acl（或 yum install acl）后重跑 ./wizard.sh --perms"
    info "手动：sudo setfacl -R -m u:${SERVICE_USER}:rwX '$wd' && sudo setfacl -R -d -m u:${SERVICE_USER}:rwX '$wd'"
    return 0
  fi

  # sudo 前缀（非 root 时需要）
  local SUDO=""
  if [ "$(id -u)" != "0" ]; then
    if command -v sudo >/dev/null 2>&1; then
      SUDO="sudo"
    else
      warn "非 root 且无 sudo —— 跳过权限初始化（请用 root 或授 sudo 后重跑 ./wizard.sh --perms）"
      return 0
    fi
  fi

  # 交互确认（sudo 系统级操作前，人在环）
  if [ "$INTERACTIVE" = "1" ]; then
    printf '  将创建系统用户 %s（若无）并对 %s 追加 ACL（需 sudo，不改原有 owner/group/mode）。继续？[y/N] ' "$SERVICE_USER" "$wd"
    local ans; read -r ans
    case "$ans" in [yY]*) ;; *) info "跳过权限初始化"; return 0 ;; esac
  fi

  # 1) 创建专用系统用户（幂等：已存在则跳过）——无登录 shell，最小权限
  if id "$SERVICE_USER" >/dev/null 2>&1; then
    ok "系统用户 ${SERVICE_USER} 已存在"
  elif $SUDO useradd -r -s /usr/sbin/nologin "$SERVICE_USER" 2>/dev/null; then
    ok "已创建系统用户 ${SERVICE_USER}（-r 系统账户 / nologin shell）"
  else
    warn "useradd 失败（权限不足？）—— 继续尝试 ACL（用户不存在时 ACL 会失败）"
  fi

  # 2) ACL 追加：-R 覆盖已有文件（rwX）+ -d 默认 ACL 使新建继承。**不改** owner/group/mode。
  info "对 $wd 追加 u:${SERVICE_USER}:rwX（递归已有文件 + 默认继承新建）..."
  if $SUDO setfacl -R -m "u:${SERVICE_USER}:rwX" "$wd" 2>/dev/null \
     && $SUDO setfacl -R -d -m "u:${SERVICE_USER}:rwX" "$wd" 2>/dev/null; then
    ok "ACL 完成：${SERVICE_USER} 可读写 $wd（含已有文件），原有 owner/group/mode 未改动"
    info "验证：getfacl '$wd' 2>/dev/null | grep -i ${SERVICE_USER}"
    info "注意：ls -l 的 group 位此后显示为 ACL mask（原有 group 有效权限未变，属正常现象）"
  else
    warn "setfacl 失败（文件系统可能未启用 ACL 支持，或 sudo 权限不足）"
    info "手动备选：将 ${SERVICE_USER} 加入 $wd 所属组并 g+rwX，或以 ${SERVICE_USER} 身份重新 clone"
  fi
}

# ── 步骤 6：连通性验证 ────────────────────────────────────────────────────
verify_connectivity() {
  step "6/7" "验证主密钥连通性"
  local key="${ZAI_API_KEY:-}"
  if [ -z "$key" ]; then
    warn "ZAI_API_KEY 未设置，跳过验证"
    return 0
  fi
  if ! command -v curl >/dev/null 2>&1; then
    warn "curl 未找到，跳过验证"
    return 0
  fi
  if [ "$INTERACTIVE" = "1" ]; then
    printf '  现在验证 ZAI_API_KEY？（轻量 embedding 请求，不耗 chat 额度）[Y/n] '
    local ans; read -r ans
    case "$ans" in [nN]*) info "跳过验证"; return 0;; esac
  fi
  info "请求 ${VERIFY_URL} ..."
  local resp
  resp=$(curl -sS -m 20 "$VERIFY_URL" \
    -H "Authorization: Bearer ${key}" \
    -H "Content-Type: application/json" \
    -d '{"model":"embedding-3","input":"tagent-wizard-connectivity-check","dimensions":512}' 2>&1) || {
      warn "请求失败（网络/超时？）：$(printf '%s' "$resp" | head -c 160)"
      return 0
    }
  if printf '%s' "$resp" | grep -q '"data"'; then
    ok "ZAI_API_KEY 有效，端点可达 ✓"
  elif printf '%s' "$resp" | grep -qiE '"error"|invalid|unauthor|1301|1302'; then
    err "验证失败（密钥无效/无权限）：$(printf '%s' "$resp" | head -c 200)"
    err "请检查 ZAI_API_KEY，或重新运行 ./wizard.sh"
    return 1
  else
    warn "无法判定（响应异常）：$(printf '%s' "$resp" | head -c 160)"
  fi
}

# ── 步骤 7：下一步 ────────────────────────────────────────────────────────
next_steps() {
  step "7/7" "下一步"
  cat <<EOF
  ${C_GRN}初始化完成！${C_RST}启动 WeChat Bot：

    ./run.sh              前台运行（Ctrl+C 停止）
    ./run.sh start        后台启动
    ./run.sh status       查看状态
    ./run.sh log          跟踪日志
    ./run.sh stop         停止

  ${C_BLU}远端常驻部署（裸机 systemd）${C_RST}
    • ./run.sh systemd    打印 systemd 部署指引（unit 模板见 deploy/tagent-wechat.service）
    • 部署指南：deploy/README.md（依赖 / 步骤 / 数据目录备份 / 运维 / 故障排查）
    • 工作根（TAGENT_WORKING_DIR）已写入 .env；仅重做权限可跑 ./wizard.sh --perms

  ${C_BLU}说明${C_RST}
    • run.sh 启动时自动加载本目录 .env（无需手动 export）
    • RL 训练模式：./run.sh rl（配 AREAL_API_KEY）+ 另一终端 ./run.sh areal
    • 容器部署（可选）：见 Dockerfile / docker-compose.yml（podman/docker 均兼容）
    • 重新配置：再次运行 ./wizard.sh（会备份旧 .env）
EOF
}

# ── 主流程 ────────────────────────────────────────────────────────────────
main() {
  printf '\n%s══════════════════════════════════════════════════════%s\n' "$C_BLD" "$C_RST"
  printf '%s  tagent WeChat Bot · 本地部署初始化向导%s\n' "$C_BLD" "$C_RST"
  printf '%s══════════════════════════════════════════════════════%s\n' "$C_BLD" "$C_RST"
  [ "$INTERACTIVE" != "1" ] && info "非交互模式（NONINTERACTIVE=1 或无 tty）：跳过密钥输入，仅用已有环境值"

  case "${1:-}" in
    --check)  check_deps; exit 0 ;;
    --verify) load_existing_env; verify_connectivity; exit $? ;;
    --perms)  load_existing_env; setup_permissions; exit 0 ;;
    --help|-h)
      sed -n '2,22p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0 ;;
  esac

  check_deps
  load_existing_env
  collect_keys
  collect_working_dir
  generate_env
  setup_permissions
  # 让验证步骤读到刚收集的 ZAI_API_KEY
  local kv
  for kv in "${ENV_ENTRIES[@]}"; do
    case "$kv" in ZAI_API_KEY=*) export "${kv}" ;; esac
  done
  verify_connectivity || true
  next_steps
}

main "$@"
