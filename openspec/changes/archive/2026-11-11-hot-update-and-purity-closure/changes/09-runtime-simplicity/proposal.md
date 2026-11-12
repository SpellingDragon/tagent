# 运行时精简升级：追加事实日志、可重建请求、实时输出与可验证数据

## Why

tagent 的核心特点是单机长程 Agent：一条事实链、三个挂点、可回捞的上下文和可恢复的任务。当前实现有两类问题同时存在：

1. **存储与恢复的实际形态配不上声明**。localfile 是整分片快照覆盖，写入无 fsync；`TierFacts.Sync` 传空 batch；已失败写入可能被后续 flush 带出。这不是追加 WAL，也没有可证明的持久屏障。
2. **面向训练、流式和观测的证据链断裂**。请求快照只在内存、采样参数在快照之后才由宿主包装器补入、主路径未开流式、默认指标只有三个计数。数据与观测因此无法从正式入口可靠取得。

上一版规划试图用 SQLite 事务、自有模型协议、双 provider、统一 module 和九域 48 叶解决上述问题。该方案引入了与当前真实缺口不成比例的机制，已撤回。本变更以最小可验证切片修复证据链，保留现行 tRPC 模型协议、事件/插件结构和全部长程特性。

目标不是把 tagent 做成平台，而是让它作为独立 Go 运行时**更简单、更可信、更易扩展和观测**。

## What Changes

- **BREAKING**：持久层从整分片 JSON 快照改为追加日志。旧数据目录不做自动迁移或删除；新版本拒绝打开不认识的格式，需显式使用新目录。
- **BREAKING**：宿主默认采样参数（temperature/thinking/reasoning_effort）在请求快照固定前合并进 `model.Request`，使快照等于实际交由 provider 的请求参数。关闭 `Stream` 默认；主路径改为流式请求。
- 请求快照从仅内存改为沿现有有界 sink 异步落盘；新增封闭记录种类，导出读取真实的生效参数、可见工具与版本。
- 主路径开启 `Stream`，在既有归约旁增加**非阻塞**增量输出；最终内容仍来自 provider 聚合包，增量不是权威正文。提供嵌入式回调与 CLI 实时输出；不做持久 chunk 回放。
- 指标和 trace 从 proc-global 默认改为实例注入；补充请求终态、阶段耗时、积压与采集损失，移除高基数事件 key 标签。
- 训练导出沿用现有契约方向，但读取本轮落盘的快照；SFT/DPO 与真实 tokenizer/template/collator 消费保持原定范围。
- 删除已证明为死路径的整分片快照机件与冗余写入；不新建注册表、统一生命周期门面或第二套运行时。
- 不合并 Go module，不新增 provider，不自建模型协议，不引入 SQLite，不实现在线 RL 或权重训练。

## Capabilities

### New Capabilities
- `append-fact-log`：单机追加日志的完整记录、校验、刷盘、尾部恢复与损坏拒绝语义。
- `request-evidence`：生效请求快照的固定、异步落盘、记录种类与读取口径。
- `streaming-increments`：调用期间的非阻塞增量输出及其临时/最终边界。
- `runtime-observability`：实例注入日志/指标/trace 与低基数关键指标。

### Modified Capabilities
- `event-segment-store`：存储形态由分片快照改为追加记录，保留事实身份与重建投影语义。
- `decision-capture`：快照固定点移到宿主默认参数生效之后，并持久化保存。
- `offline-training-export`：读取落盘快照；不完整样本具名拒收。
- `runtime-inspection`：补齐采集损失、待处理义务与原因查询。
- `turn-tracing`：实例注入 tracer，保留异步 link 语义。
- `trajectory-recording`：默认记录承载收敛为执行事实与请求证据，移除平行采集。

## Impact

### 代码
- `memory/ledger/localfile.go`、`segment_store.go`、`tier_facts.go`：追加日志后端与冗余写入清理。
- `runtime/state/durable.go`：避免重复 Sync 语义，明确提交不确定。
- `assembly/facts.go`、`request.go`、`host_capture.go`、`host_model.go`、`host_delivery.go`：快照持久引用、参数合并、恢复路径。
- `runtime/agent/loop.go`：`Stream` 开启点与增量 tee；不改 `ReduceChannel` 契约。
- `api/model/*`：仅新增可选窄接口与记录种类，不改变 `Model` 接口。
- `features/rl/*`、`features/telemetry/*`、`assembly/host_face.go`：落盘 kind、实例化观测、指标。
- `tests/integration/{learning,host,inspection,history}`：新增或改造验收。

### 数据与兼容
- 新数据目录格式与旧格式不兼容；旧目录只读保全，不自动迁移。
- 请求快照 schema 与记录行格式增加版本；未知版本具名拒绝。
- 现有 OpenSpec 主规格按受影响条款迁移；`success-semantics` 的已落地代码作为基线保留，不替其勾选未完成收尾。
- `post-training-data-loop` 与 `online-rl-training` 的处理见 design D8：前者降格为继承输入，后者保持未实施。

### 运行参数
- 流式下 `http.Client.Timeout` 覆盖整个响应体读取，默认 10 分钟会截断长流；必须在验收中确认并显式调整或改用按空闲超时断开。
- openai-go 客户端默认重试 2 次且发生在 `model.Request` 边界之下；本变更不宣称隐藏重试可被 attempt 记录观测，选择显式关闭或记录该边界。

### 非目标
- 不自建模型/provider 协议，不新增 Anthropic 等第二 provider。
- 不引入 SQLite 或多表事务；不要求跨账本原子提交。
- 不做无缝断线续流、跨进程 chunk 回放或通用订阅平台。
- 不合并 module、不重排目录、不改写插件端口闭集。
- 不实现 SFT/DPO 训练、权重更新、在线 RL、多机协调或多租户。
