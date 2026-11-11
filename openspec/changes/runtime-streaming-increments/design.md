# Design: runtime-streaming-increments（deferred）

## D1 拆分来源与边界

本变更承载原 `hot-update-and-purity-closure/changes/09-runtime-simplicity` 的流式四叶，逐条对应关系如下（编号沿用 09，便于与原账目对齐）：

| 本变更叶 | 原 09 叶 | 目标文件 | 能力条款 |
|---|---|---|---|
| 3.1 | 3.1 主路径 `Stream: true` 终态 parity | `runtime/agent/loop.go`、`assembly/host.go` | `streaming-increments`「唯一终态判定不变」 |
| 3.2 | 3.2 tee + 有界非阻塞 fan-out | `runtime/agent/stream.go`（新建） | 「增量不成为模型背压」 |
| 3.3 | 3.3 空闲超时 + 重试边界 | `assembly/host.go` | 「长流不被整体超时截断」 |
| 3.4 | 3.4 CLI 实时输出 | `examples/cli-host/main.go` | 「调用期间提供增量输出」 |

边界：只做增量观察面与非阻塞分发，不改 `ReduceChannel` 契约、不改事实链形态、不落盘 chunk。持久层与请求证据的收口留在 09（已勾），不回流。

## D2 状态：deferred 的机械含义

- `tasks.md` 全为 `- [ ]`；本变更在 `openspec list` 中可见但**不计入** `hot-update-and-purity-closure` 的完成度，也不占任何派发单的写面。
- `specs/streaming-increments/spec.md` 是冻结承接面，归档时按主规格正常演进；本轮不 `archive`。
- 重入前需重读本文与 09 的 §8/§10 阻塞登记，确认三条前提（`tests/integration/upgrade/` 存在、订阅面签名冻结、用户明示开工）。

## D3 三条实施前提（当前均不成立，实证登记）

1. `runtime/agent/stream.go` 不存在 → 3.2 无落点；先定 tee 的订阅接口（返回取消函数、有界缓冲、丢弃计数可查询），再动 CLI。
2. `assembly/host.go:421` 为 `http.Client{Timeout: doc.requestTimeout()}`，覆盖整个响应体读取 → 3.3 必须改为按空闲判定，否则长流被 10 分钟默认值截断。
3. `tests/integration/upgrade/` 目录不存在 → 3.1/3.3 的冻结验收面无落点；须与基线见证叶（见 `runtime-simplicity-features` 的 0.1）同批建立。

## D4 验收三锚（随任务行冻结，缩水即重开）

- 正锚：点名测试 + `-run '^TestName$'` 逐字符一致，可复跑 `-json`。
- 负锚：具名负向子测试作为勾选硬门（如 `slow_subscriber_does_not_block`、`idle_timeout_fires`、`partial_only_never_completed`、`final_body_printed_once`）。
- 消费者锚：经正式入口（`assembly.Boot`/`Assemble`、真实 CLI 子进程）的调用路径；测试手工补接线不算消费者成立。

## D5 被否方案

- 持久 chunk 回放 / 断线续流（否：需要新的持久 chunk 事实链与第二套读面，与"最终正文来自 provider 聚合包"的单一权威冲突）。
- 通用订阅平台（否：当前只有模型增量一个生产者，属结构自证）。
- 阻塞式分发（否：把慢订阅者的延迟变成模型背压，直接违背「增量不成为模型背压」）。

## 风险与回退

风险集中在 3.3 的超时改造：把整段超时改为空闲超时后，无响应的死连接需由空闲判定兜住，否则从"长流被截断"变成"死连接永不结束"。回退＝保留 `Stream: false` 的整段路径，流式面整体可关（订阅面在关闭时无生产者，零调用）。
