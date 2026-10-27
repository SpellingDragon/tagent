# 保真与恢复：第三方评审 P1 修复

## 症状（验收矩阵第一行——第三方复现直译，逐条对应 P1）

| # | 复现原话（直译测试 G-n） | 根因（已抽查核实） |
|---|---|---|
| G1 | 工具调用后发起下一轮对话，请求出现缺 `tool_call_id` 的 `role=tool` 消息，协议校验 400，回合 failed | `request.go:633 projectedSegment` 只保留 Role/Content，丢 ToolCalls/ToolCallID |
| G2 | 同一存储重新装配：InvocationID/TaskID/DecisionID/EpisodeID 消失、exec 审计 meta 消失、纯 ToolCalls 消失；同键先 original 后 changed 两次均成功，进程内读 changed、重开读 original | `memory/event_body.go:31` 轻字段；`facts.go` 重复键跳写盘仍更新内存 |
| G3 | 阻断存储屏障后 `terminal=completed`、`Invoke error=nil`、assistant fact 不存在——调用成功而用户没收到回答 | `facts.go:46 commitFact` 吞错返空身份；`loop.go:298` 调用方仍返回成功 |
| G4 | 两个已声明 Agent 用相同 SessionID，第二个 Agent 的请求带入第一个的历史标记 | `request.go:341 requestBoundary` 只按 SessionID 索引 replays |
| G5 | 重启前待审批 1 条，重新 Boot 后 0 条，批准原身份 `ErrDeliveryNotFound` | `host.go:1174 holdDelivery` 内存表 |
| G6 | 执行器首次调用只拿到部分输出；进程退出后最终输出捕获次数 0，再读资源 `ErrResourceUnknown` | `tool/exec/state.go:241` 退出释放前不保存最终输出 |
| G7 | `max_tokens=0` 时事实已落盘，重新装配后请求没有旧历史；设 32000 后恢复 | 历史恢复依赖预算投影启用 |
| G8 | 不限候选命中旧相关记录；`vector_top_k=1` 后零命中 | `memory/search/store_searcher.go:189 vectorLeg` 先限制最近候选 |
| G9 | 呈现决定无条件重用；200 字节内容在预算缩至 4 字节后仍完整保留 | `memory/replay/replay_budget.go:172` |
| G10 | `TestLocalSmokeSymptomNoSendTool` 单跑 10 次 1 失败——`requests[0]` 被当作入口请求，异步冥想可能先到 | 测试的调度顺序假设（本变更顺带根治上轮 CI flaky 讨论） |

## Why

事件链已给出可靠的数量层保证（不重不漏不重做），但第三方复现证明**保真层与恢复层仍缺四块**：协议消息不完整（G1）、事实不完整且键语义双状态（G2）、失败被吞成成功（G3）、作用域漏 Agent（G4）；以及交付/执行的跨重启恢复（G5/G6）。这四块直接决定"长期无人值守"是否成立——一次工具调用后的 400 就能打断对话。

## What Changes

- **请求协议保真**（G1）：投影/呈现层恢复完整消息（ToolCalls/ToolCallID/工具名），调用与结果配对校验进测试（工具调用后下一轮、压缩后、冷启动后）。
- **完整事实一致性**（G2）：EventBody 全字段（ID 四元组+meta+ToolCalls）；同键重复提交按内容一致=幂等、不一致=具名冲突（不得双成功）；进程内外读值一致。
- **提交错误传播**（G3）：commit 失败进入正式返回（Invoke error 或明确的提交义务）；不得"completed 但事实不存在"。
- **地址作用域**（G4）：投影索引按 AgentID+SessionID 完整地址；跨 Agent 同名会话隔离测试。
- **交付恢复**（G5）：待审批投递持久化（身份/内容/血统/目标），跨重启可批准/拒绝；发送失败转为持久重试义务。
- **执行最终输出**（G6）：退出释放前保存最终输出（可读、可确认消费）；重启测试升级为"自动接管"（不只"具名报告未知"）。
- **恢复与检索预算**（G7/G8/G9）：历史恢复不依赖预算投影；向量候选不按 recent 预截断；呈现决定条件重用+完整请求硬约束。
- **测试调度去假设**（G10）：按模型身份+本轮标记定位请求，消除 `requests[0]` 顺序假设。

## Capabilities

### Modified Capabilities

- `persistent-event-loop`：事实完整性与键语义（幂等/冲突）、提交错误传播。
- `context-assembly`（或主规格投影面）：协议保真、Agent 作用域、历史恢复独立于预算。
- `delivery-plugin`：待审批投递持久化与发送重试义务。
- `execution-resource-lifecycle`：最终输出保存、自动接管。
- `memory-retrieval`：向量候选完整检索、完整请求预算。

## Impact

- 代码：`request.go`、`facts.go`、`memory/event_body.go`、`memory/search/store_searcher.go`、`memory/replay/replay_budget.go`、`host.go`（pendingDeliveries 持久化）、`tool/exec/state.go`、`settle_turn.go`（retained 持久待办）、`runtime/agent/loop.go`、smoke 测试。
- 合同：主规格相应 requirement 修订；症状锚 G1–G10 进症状 CI。
- 建议顺序（用户裁定采纳）：W1 协议保真/事实一致性/错误传播/作用域 → W2 交付恢复/执行最终结果/唤醒持久化 → W3 检索与预算。

## 必答一问

"本案完成后，第三方那 10 条复现还会发生吗？"——不会：G1–G10 各有直译测试（第一天红着），修复后红转绿，F3 回滚即红双证据。诚实边界：G6 的真实 tmux 接管在本地受 `Operation not permitted` 限制，用执行器替身验证，真实 tmux 需 CI/授权环境补验。
