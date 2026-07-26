# ---------- 构建阶段 ----------
FROM golang:1.24-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 静态链接，便于运行阶段使用 distroless（无 libc 依赖）
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/wechat-bot .

# ---------- 运行阶段（非 root + 最小攻击面）----------
# 若 entrypoint 需要 shell，改用 gcr.io/distroless/base 而非 static
FROM gcr.io/distroless/static-debian12:nonroot

# 注意：plan 子 agent 的 spec 工具后端是 openspec CLI（npm 包，需 node 运行时）。
# distroless/static 不含 node/shell，故本镜像默认【不】提供 openspec——这是有意
# 的最小攻击面取舍。plan agent 在 CLI 缺失时会明确报告“环境缺少 openspec CLI”
# 并停止（缺失能力反问上层），而非静默失败。若需启用 plan 的规格化能力：
#   方案一（推荐）：多阶段从 node:slim 拷贝 node 运行时 + 全局 openspec 到 /app，
#                  并设 spec 工具 properties.bin 指向它（需评估镜像体积/攻击面）；
#   方案二：改用非 distroless 基础镜像并 apt/npm 安装（放弃最小攻击面）。
# 二者均属部署决策，不在框架代码内强制。

COPY --from=build /out/wechat-bot /app/wechat-bot
COPY entrypoint.sh /app/entrypoint.sh

# 工作区与数据卷（运行时可写，由 compose 挂载）
RUN mkdir -p /app/workspace /app/home /app/logs \
 && chown -R 1000:1000 /app

# 显式常规用户（非 root）
USER 1000:1000

ENV HOME=/app/home \
    XDG_CONFIG_HOME=/app/home/.config \
    TAGENT_HTTP_PORT=8089

WORKDIR /app
EXPOSE 8089

# 注意：distroless/static 无 sh，若 entrypoint 需 shell 请改用 distroless/base
ENTRYPOINT ["/app/entrypoint.sh"]
