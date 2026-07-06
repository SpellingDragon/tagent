## Why

wechat-bot 示例在数据采集端很完整（OTLP span、Trajectory、日志），但在数据查看端完全空白：OTLP trace 需要外部 Jaeger 才能看到，RL trajectory 只写 JSONL 文件无运行时查询能力，HTTPAPI 存在但未启动。同时示例使用 `type: memory` 纯内存存储，重启后全部丢失，且磁盘上残留 2489 个旧 FileBackend 的单事件 JSON 文件。需要让示例启动后即可在本地观测全链路数据并核验 RL 记录，同时使用 FileSegmentStore 实现持久化。

## What Changes

- **启动 HTTPAPI 服务**：在 main.go 中启动 `agent.HTTPAPI`，暴露 `/healthz`、`/trajectories`、`/trajectory/{id}` 端点，启动后即可通过 HTTP 查询 RL 轨迹数据
- **切换 memory store 为 file**：tagent.yaml 中 `type: memory` → `type: file`，使用 FileSegmentStore（时间窗口段存储 + RustViking KV），重启后数据持久化
- **run.sh 默认设置可观测环境**：默认设置 `TAGENT_TRAJECTORY_FILE`、`TAGENT_HTTP_PORT`、数据目录路径；OTLP 保持可选（需要外部 Jaeger）
- **清理旧 FileBackend 残留**：删除 `.wechat-config/agent-events/` 目录（旧版每事件一文件产物），确保使用 FileSegmentStore 的新路径
- **HTTPAPI 端口配置**：通过 `TAGENT_HTTP_PORT` 环境变量控制 HTTPAPI 监听端口（默认 8089）

## Capabilities

### New Capabilities
- `example-rl-visibility`: wechat-bot 示例启动 HTTPAPI 服务，暴露 trajectory 查询端点，支持本地核验 RL 数据记录
- `example-file-memory-wiring`: wechat-bot 示例使用 FileSegmentStore 作为 memory backend，配置数据目录路径，实现重启后数据持久化

### Modified Capabilities
（无现有 spec 需要修改）

## Impact

- `examples/wechat-bot/main.go`: 新增 HTTPAPI 启动逻辑（goroutine 中 `http.ListenAndServe`）
- `examples/wechat-bot/tagent.yaml`: memory type 从 `memory` 改为 `file`，配置 path 为 `.wechat-config/data`
- `examples/wechat-bot/run.sh`: 新增 `TAGENT_HTTP_PORT` 环境变量默认值，帮助文档更新
- `examples/wechat-bot/.gitignore`: 新增 `.wechat-config/data/` 排除规则
- `examples/wechat-bot/.wechat-config/agent-events/`: 删除旧 FileBackend 残留文件
- 运行时依赖：需要 rustviking CLI 二进制文件（FileSegmentStore 的 KV 后端）
