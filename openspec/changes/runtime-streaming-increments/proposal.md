# Proposal: runtime-streaming-increments（deferred，不在本轮实施）

> **状态：deferred（冻结承接面，无代码开工）**。本变更是 `hot-update-and-purity-closure/changes/09-runtime-simplicity` 流式四叶（3.1/3.2/3.3/3.4）的独立承接面，按用户 2026-11-08 裁定"流式先不做、独立拆分变更"从 09 剥离。`tasks.md` 全部为 `- [ ]`，勾选即意味开工；本轮不派发、不占写面、不计入任何一级变更的完成度。

## Why

流式的实施前提在 09 当前树上均不成立：`runtime/agent/stream.go` 不存在（无任何扇出面可订）、整段超时落在 `assembly/host.go` 的 `http.Client{Timeout}`、`tests/integration/upgrade/` 目录不存在（3.1/3.3 的冻结验收面无落点）、CLI 订阅面依赖 3.1/3.2 的公开面。把它与 09 的纯核心叶（追加日志、原子提交、请求证据、导出）混在同一处，等于让已全绿的持久层收口被未开工的流式面拖住归档。故独立成变更。

## What Changes

- 主路径开启 `Stream: true`，确认既有归约仍产出同一终态（增量不参与终态判定）。
- 在 `runtime/agent/stream.go` 实现 tee 与有界非阻塞 fan-out：慢订阅者丢弃并计数，不阻塞模型。
- 流式超时改为空闲判定，消除 `http.Client.Timeout` 对长流的整体截断；显式处理 openai-go 默认重试（关闭或记录该边界）。
- CLI 实时输出：`examples/cli-host/main.go` 经公开面订阅增量并在完成前写出首段。

不做：持久 chunk 回放、断线续流、跨进程订阅平台、第二 provider、自建模型协议。

## Capabilities

### New Capabilities
- `streaming-increments`：调用期间的非阻塞增量输出、订阅隔离与临时/最终边界。

### Modified Capabilities
- 无。开启 `Stream` 的请求面属本能力条款（"唯一终态判定不变"），不改 `decision-capture` 的快照固定点语义（该叶已在 09 的 2.x 收口）。

## Impact

### 代码（预计触碰，本轮不改）
- `runtime/agent/loop.go`：`Stream` 开启点与增量 tee；不改 `ReduceChannel` 契约。
- `runtime/agent/stream.go`：新建（当前不存在）。
- `assembly/host.go`：超时由整体时长改为空闲判定；`api/model` 仅用既有 `Stream` 字段。
- `examples/cli-host/main.go`：订阅增量并实时写出。

### 验收面（冻结，不得缩水）
- `tests/integration/upgrade/`（当前不存在，需随实施同批新建）。
- 负锚：`partial_only_never_completed`、`slow_subscriber_does_not_block`、`cancel_subscription_keeps_call`、`idle_timeout_fires`、`retry_disabled_or_recorded`、`final_body_printed_once`。

### 与 09 的关系
09 的 `changes/09-runtime-simplicity/specs/streaming-increments/spec.md` 与本变更条款同源；**实施与归档以本变更为准**，09 那份随 09 收口留作历史记录，不再作为承接口。

## 重入触发条件（未决定性，先登记）

1. `tests/integration/upgrade/` 目录建立（与 0.x/2.2 的基线见证同批）。
2. `runtime/agent/stream.go` 的公开订阅面先以签名级契约冻结，再独立派发 CLI 叶（否则 3.4 只能测试补接线）。
3. 用户明示"流式开工"：本变更在此之前保持 deferred。
