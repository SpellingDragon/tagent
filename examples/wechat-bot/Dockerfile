# syntax=docker/dockerfile:1

# ---------- 构建阶段：Go 二进制（静态链接）----------
FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/wechat-bot .

# ---------- 工具阶段：node 运行时 + openspec CLI ----------
# plan 子 agent 的 spec 工具后端是 openspec（npm 包）；skills（url-fetcher 等）
# 也需要 node。此处用官方 node 镜像安装 openspec，运行阶段只拷贝必要产物，
# 避免在最终镜像里保留 npm 缓存与构建工具。
FROM node:22-bookworm-slim AS tooling
RUN npm install -g @fission-ai/openspec --no-audit --no-fund \
 && npm cache clean --force

# ---------- 运行阶段：debian-slim + 运行时依赖 ----------
# 选型说明：本 bot 需要 tmux（action 异步任务层）、node（openspec/skills）、
# 以及 shell（entrypoint）——distroless/static 三者皆无，故改用 debian-slim。
# 安全加固（非 root / cap_drop / no-new-privileges / 资源上限 / 仅绑本地端口）
# 全部保留于 docker-compose.yml，不因基础镜像变化而削弱。
FROM debian:bookworm-slim

# 运行时依赖：
#   tmux            — action 工具的异步任务层
#   curl            — 健康检查探针（GET /healthz）
#   libstdc++6/zlib1g — 拷贝来的 node 二进制所需运行时库
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl tmux libstdc++6 zlib1g \
 && rm -rf /var/lib/apt/lists/*

# node 运行时 + openspec CLI（自工具阶段拷贝，固定 node 22 版本）
COPY --from=tooling /usr/local/bin/node /usr/local/bin/node
COPY --from=tooling /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s /usr/local/lib/node_modules/openspec/bin/openspec.js /usr/local/bin/openspec \
 && chmod +x /usr/local/bin/openspec

WORKDIR /app

# 应用产物与只读资源（配置 / prompts / skills / openspec 模板 / 知识库）
COPY --from=build /out/wechat-bot /app/wechat-bot
COPY entrypoint.sh /app/entrypoint.sh
COPY tagent.yaml /app/tagent.yaml
COPY resources /app/resources
COPY skills /app/skills
COPY openspec /app/openspec
COPY knowledge_base /app/knowledge_base

# 可写目录（read_only 根文件系统下由 compose 挂载 tmpfs/volume 覆盖）：
#   .wechat-config — localfile 记忆存储   data/trajectories — 轨迹 dump
#   openspec/changes — plan 规格化产出    workspace / logs — 运行期暂存
RUN mkdir -p /app/.wechat-config /app/data/trajectories \
      /app/openspec/changes /app/workspace /app/logs \
 && chown -R 1000:1000 /app

# 显式常规用户（非 root）
USER 1000:1000

ENV HOME=/app \
    XDG_CONFIG_HOME=/app/.config \
    NODE_PATH=/usr/local/lib/node_modules \
    TAGENT_CONFIG=/app/tagent.yaml \
    TAGENT_HTTP_PORT=8089 \
    TAGENT_WECHAT_WORKSPACE_DIR=workspace

EXPOSE 8089
ENTRYPOINT ["/app/entrypoint.sh"]
