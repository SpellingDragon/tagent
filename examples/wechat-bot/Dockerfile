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
