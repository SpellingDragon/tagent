## Context

wechat-bot 示例当前在数据采集端很完整，但数据查看端空白。三个问题：
1. OTLP trace 需要外部 Jaeger 才能查看
2. RL trajectory 只写 JSONL 文件，`agent.HTTPAPI` 存在但未启动
3. 使用 `type: memory` 纯内存存储，重启丢数据；磁盘残留 2489 个旧 FileBackend 单事件文件

框架已具备的基础设施：
- `agent.HTTPAPI`：暴露 `/healthz`、`/trajectories`、`/trajectory/{id}`、`/task` 端点
- `agent.TrajectoryStore`：内存 FIFO 存储 + JSONL 导出
- `memory.FileSegmentStore`：时间窗口段存储 + RustViking KV 后端
- `tagent.go` 的 `ensureRustVikingConfig()`：自动生成 rustviking config.toml
- `tagent.go` 的 `resolveMemoryStore()`：根据 `type: file` 自动创建 FileSegmentStore + Lifecycle + Compactor

rustviking 二进制位置：`/Users/pengweiye/Documents/codes/rustviking/target/release/rustviking`（不在 PATH 中，需在 YAML 中指定）

## Goals / Non-Goals

**Goals:**
- 启动 wechat-bot 后，可通过 HTTP 端点查询 RL trajectory 数据
- 使用 FileSegmentStore 持久化事件，重启后数据不丢失
- run.sh 一键设置所有可观测 + RL + 存储环境变量
- 清理旧 FileBackend 残留文件

**Non-Goals:**
- 不内建 OTLP 后端（Jaeger/Tempo 仍需外部启动，保持可选）
- 不修改框架代码（仅改示例 + 配置 + 脚本）
- 不完成 `memory-storage-production-hardening` 的 80+ tasks（FileSegmentStore 当前状态够用于示例）
- 不提交 `file_backend.go` 的删除（那是更大范围 change 的工作）

## Decisions

### D1: HTTPAPI 启动方式 — goroutine + 可配置端口

**决策：** 在 main.go 中用 goroutine 启动 `http.ListenAndServe`，端口通过 `TAGENT_HTTP_PORT` 环境变量控制（默认 8089）。

**理由：** HTTPAPI 需要与 wechat bot 同时运行，goroutine 是最简单的方式。端口可配置避免与 wechat 服务端口冲突。

**替代方案：**
- 用 `errgroup` 管理两个 goroutine — 过度工程，示例不需要
- 把 HTTPAPI 集成到 wechat bot 的 HTTP server — wechat bot 没有 HTTP server（用 WebSocket），不兼容

### D2: memory store 切换 — `type: localfile`（纯本地文件系统，不依赖 rustviking）

**决策：** 新增 `type: localfile` 存储类型，使用 `LocalFileKV`（JSON 文件持久化的 KV store）替代 RustVikingClient。tagent.yaml 中 tagent 和 recall agent 的 memory 改为 `type: localfile, path: .wechat-config/data`。

**理由：**
- rustviking CLI 二进制需要单独编译安装，增加示例使用门槛
- `memory-storage-production-hardening` 的 80+ tasks 尚未完成，`type: file` 依赖的 RustVikingClient 不够稳定
- `LocalFileKV` 实现简单（内存 map + JSON 文件 flush），无外部依赖，适合示例场景
- 复用现有 `FileSegmentStore`（通过 `KVStore` 接口），不需要改动 segment store 逻辑
- `InMemRelationStore` 已有 WAL + snapshot 持久化，不需要改动

**实现细节：**
- `LocalFileKV` struct：`sync.Mutex` + `map[string]string` + `filePath string`
- 启动时从 `kv.json` 加载全部数据到内存
- 每次写入操作（KVPut/KVDelete/KVBatch）后同步 flush 到 `kv.json`
- KVScan/KVRange 纯内存操作（从 map 中过滤）
- KVGet key 不存在时返回 error（与 MockRustVikingClient 一致）

**recall 共享：** recall 和 tagent 使用相同的 `path`，`resolveMemoryStore` 通过 `namedFileStores` registry 共享同一个 `FileSegmentStore` 实例（与 `type: memory` 的 `namedMemStores` 模式一致）。

### D3: run.sh 默认环境变量

**决策：** run.sh 设置以下默认值（用户可通过环境变量覆盖）：

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `TAGENT_TRAJECTORY_FILE` | `.wechat-config/trajectories.jsonl` | RL 轨迹 JSONL 导出路径 |
| `TAGENT_HTTP_PORT` | `8089` | HTTPAPI 监听端口 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | (不设置) | OTLP 端点，保持可选 |

**理由：** trajectory 和 HTTPAPI 是本地可核验的核心能力，应默认开启。OTLP 需要外部后端，保持可选。

### D4: 旧文件清理 — 删除 + .gitignore

**决策：**
- 删除 `.wechat-config/agent-events/` 目录（旧 FileBackend 产物）
- `.gitignore` 已排除 `.wechat-config/`，新数据目录 `.wechat-config/data/` 自动被排除
- `trajectories.jsonl` 已在 `.gitignore` 中

**理由：** 旧文件是历史残留，当前代码（`type: file`）使用新的 KV key schema（`{pid}:evt:{window_ts}:{seq}`），不会写入 `agent-events/` 目录。

## Risks / Trade-offs

- **[LocalFileKV 性能]** → 每次写入都同步 flush JSON 文件，大量写入时性能差。示例场景（低并发、短运行时间）可接受。生产环境应使用 rustviking（RocksDB）或 bbolt。
- **[LocalFileKV 并发]** → 单进程内 `sync.Mutex` 保护，不支持多进程并发访问。示例单进程运行，可接受。
- **[HTTPAPI 安全性]** → HTTPAPI 无认证，监听在 localhost。示例场景可接受，生产环境需加认证。
- **[FileSegmentStore 生产就绪度]** → `memory-storage-production-hardening` 有 80+ tasks 未完成。对于示例场景（低并发、短运行时间）足够，但不适合生产。
