#!/bin/sh
# 容器入口：确保非 root 运行、密钥来自 env、优雅关闭、健康检查可用
set -e

# 1) 密钥必须来自 env（已在 compose 注入），拒绝回退 ~/.zshrc
if [ -z "$ZAI_API_KEY" ]; then
  echo "ERROR: ZAI_API_KEY 未设置（应通过 env/secret 注入，而非 ~/.zshrc）" >&2
  exit 1
fi

# 2) 确保以非 root 运行（防御性，compose 已设 user: "1000:1000"）
if [ "$(id -u)" = "0" ]; then
  echo "ERROR: 禁止以 root 运行 tagent" >&2
  exit 1
fi

# 3) 工作区权限自检
mkdir -p /app/workspace /app/logs
chown -R "$(id -u):$(id -g)" /app/workspace /app/logs 2>/dev/null || true

# 4) 优雅关闭：转发 SIGTERM 给子进程
cleanup() { kill -TERM "$CHILD" 2>/dev/null; wait "$CHILD"; exit 0; }
trap cleanup TERM INT

# 5) 前台启动（exec 让信号直达二进制）
exec /app/wechat-bot &
CHILD=$!
wait "$CHILD"
