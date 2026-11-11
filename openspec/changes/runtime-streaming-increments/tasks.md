# 任务：runtime-streaming-increments（deferred，本轮不实施）

> **deferred**：本文件全部为 `- [ ]`，勾选即开工。本轮（hot-update-and-purity-closure 纯核心收口）不派发，不计入任何一级变更的完成度。
> 来源：`hot-update-and-purity-closure/changes/09-runtime-simplicity/tasks.md` 的 3.1–3.4，按用户 2026-11-08 裁定剥离。
> 验证约定：每条叶任务的验证行按原 09 冻结文本原样保留（正锚点名 + 负锚具名 + 消费者锚），实施时不得追认缩水；`tests/integration/upgrade/` 与 `runtime/agent/stream.go` 是现存缺口，见 design D3。

## 前置（三条，均未成立）

- [ ] P1 建立 `tests/integration/upgrade/` 验收目录，落 3.1/3.3 的冻结测试名 —— 验证：`go test ./tests/integration/upgrade -run '^$' -count=1` EXIT=0（目录可编译、零匹配不报错）
- [ ] P2 冻结 `runtime/agent/stream.go` 订阅面签名（订阅返回取消函数、有界缓冲、丢弃计数可查询），并以签名级契约固定后独立派发下游 —— 验证：`go doc ./runtime/agent` 列出冻结签名且 `go build ./...` EXIT=0
- [ ] P3 确认 `assembly/host.go` 超时面现状（`http.Client{Timeout: doc.requestTimeout()}`，覆盖整个响应体读取）并裁定空闲判定的默认上限来源 —— 验证：`grep -n 'http.Client{Timeout' assembly/host.go` 命中 1 处，裁定记录入 design

## 3. 流式增量（capability: streaming-increments）

- [ ] 3.1 主路径开启 `Stream: true`，确认既有归约仍产出同一终态 —— 验证：`go test ./tests/integration/upgrade -run '^TestStreamingTerminalParity$' -count=1 -json`；负锚 `partial_only_never_completed`
- [ ] 3.2 在 `runtime/agent/stream.go` 实现 tee 与有界非阻塞 fan-out，慢订阅者丢弃并计数，不阻塞模型 —— 验证：`go test ./runtime/agent -run '^TestIncrementFanout$' -count=1 -json`；负锚 `slow_subscriber_does_not_block`、`cancel_subscription_keeps_call`
- [ ] 3.3 流式超时改为空闲判定，消除 `http.Client.Timeout` 对长流的整体截断；显式处理 openai-go 默认重试 —— 验证：`go test ./tests/integration/upgrade -run '^TestLongStreamCompletes$' -count=1 -json`；负锚 `idle_timeout_fires`、`retry_disabled_or_recorded`
- [ ] 3.4 CLI 实时输出：`examples/cli-host/main.go` 经公开面订阅增量并在完成前写出首段 —— 验证：`go test ./examples/cli-host -run '^TestCLIStreamsIncrementally$' -count=1 -json`；负锚 `final_body_printed_once`；消费者：真实 CLI 子进程

## 依赖与派发提示

- 顺序：P1 → 3.1 → 3.2 → 3.3 → 3.4；P2 必须先于 3.4，否则 CLI 叶只能测试补接线。
- 同文件簇：`runtime/agent/**`（3.1/3.2）与 `assembly/host.go`（3.1/3.3）串行，不得并行派发。
- 3.3 与 3.1 共用 `assembly/host.go`，合单或严格串行。
