## 1. TaskSegmenter 抽取（与框架迁移解耦，先行落地）

- [x] 1.1 新建 `agent/task_segmenter.go`，提供 `SegmentMessages([]model.Message) []*TaskSegment` 和 `SegmentReferences([]memory.EventReference) [][]memory.EventReference`
- [x] 1.2 统一 `isTaskBoundary` 判定函数，兼容 messages（`RoleAssistant && len(ToolCalls)==0`）和 references（`EventType == agent_output`）两种输入
- [x] 1.3 将 `SmartCompressor.splitByTaskBoundary` 改为委托 `TaskSegmenter.SegmentMessages`
- [x] 1.4 将 `Compactor.splitTasks` 改为委托 `TaskSegmenter.SegmentReferences`
- [x] 1.5 新增 `agent/task_segmenter_test.go`：验证 messages 和 references 分段结果一致
- [x] 1.6 运行 `go test ./agent/...` 验证无回归

## 2. Research & PoC

- [x] 2.1 阅读 `trpc-agent-go` 源码，确认 `Flow.Run` / `LLMAgent.Run` 的 `BeforeModel` 回调语义、processor 链顺序和 event channel 输出
- [x] 2.2 在 `agent/poc_test.go` 风格下完成 PoC：用 `LLMAgent.Run` + `BeforeModel` 实现一次 tool_call → final response 的完整循环
- [x] 2.3 验证 `BeforeModel` 修改 `Request.Messages` 后框架不会再次覆盖
- [x] 2.4 验证多个 `BeforeModel` 回调按注册顺序执行

## 3. FrameworkFlowAdapter 基础结构

- [x] 3.1 新建 `agent/framework_flow_adapter.go`，定义 `FrameworkFlowAdapter` 结构体与构造函数
- [x] 3.2 实现 `BuildInvocation(batch []*AgentEvent, session *session.Session) *agent.Invocation`，包含 mergeBatch 逻辑和 `Source == "agent_output"` 过滤
- [x] 3.3 实现 `RunFlow(ctx context.Context, inv *agent.Invocation) error`，消费框架 event channel 并转发到 `outputCh`
- [x] 3.4 在 `RunFlow` 中识别 final response（无 tool_calls）并写回 `EventBus`（`Source == "agent_output"`）

## 4. 回调注册

- [x] 4.1 实现 `SmartCompressor` 的 `BeforeModel` 包装函数：计算 token → 压缩 → 恢复 `KeepRecentTasks`
- [x] 4.2 实现 `Compactor` 的 `BeforeModel` 包装函数：SmartCompressor 后仍超 → compact projection → 重建 messages → 重新注入前缀
- [x] 4.3 确认回调注册顺序：先 SmartCompressor，后 Compactor
- [x] 4.4 在 `FrameworkFlowAdapter` 构造 `LLMAgent` 时传入 callbacks 和 model/tools/systemPrompt
- [x] 4.5 验证 SmartCompressor 回调不修改 `SessionProjection`
- [x] 4.6 验证 Compactor 回调在 SmartCompressor 未触发时跳过

## 5. AgentLoop 改造

- [x] 5.1 移除 `AgentLoop.Run` 中自建的 ReAct 循环（callModel/handleResponse/dispatchToolUse）
- [x] 5.2 让 `AgentLoop.Run` 退化为事件总线消费者：Pull → onEvent → mergeBatch → `FrameworkFlowAdapter.RunFlow`
- [x] 5.3 更新 `AgentLoop` 顶部注释，反映新的执行流程
- [x] 5.4 保留 `TypeToolUse` 的处理：仅记录日志，不再 dispatch（dispatch 已移入框架 Flow）

## 6. TagentAgent 集成

- [x] 6.1 在 `NewTagentAgent` 中创建 `FrameworkFlowAdapter` 并注入到 `AgentLoop`
- [x] 6.2 在 `TagentAgent.Run` 子 agent 路径中使用临时 bus/projection + `FrameworkFlowAdapter`
- [x] 6.3 添加 `TagentConfig.UseFrameworkFlow` feature flag（默认 true），保留旧 `AgentLoop` 路径作为 fallback
- [x] 6.4 当 `UseFrameworkFlow == false` 时，`Preprocessor.Process` 保留内联 SmartCompress + Compact 逻辑
- [x] 6.5 当 `UseFrameworkFlow == true` 时，`Preprocessor.Process` 跳过内联压缩，仅做 shouldCallModel 判断和 messages 初始构建
- [x] 6.6 确保 `StartLoop/InjectMessage/StopLoop` 对外签名不变

## 7. 测试

- [x] 7.1 新增 `agent/framework_flow_adapter_test.go`：验证 Invocation 构造、输出转发、agent_output 回写 bus
- [x] 7.2 新增/更新 `agent/agent_loop_test.go`：验证持久循环仍持续运行、StopLoop 正常关闭
- [x] 7.3 新增框架 mock：`MockLLMAgent` / `MockFlow`，支持 scripted response 序列
- [x] 7.4 新增 `BeforeModel` 回调单元测试：验证 SmartCompressor → Compactor 顺序、token 阈值跳过、projection 重建
- [x] 7.5 运行 `go test ./agent/...` 并修复失败
- [x] 7.6 运行长会话集成测试：验证 projection 不溢出、Compact 生效
- [x] 7.7 运行 tmux 异步集成测试：验证异步命令完成后触发下一轮 Flow.Run
- [x] 7.8 运行 A2A 远程子 agent 测试：验证上下文传递不受框架迁移影响

## 8. 文档与清理

- [x] 8.1 更新 `README.md` 和 `docs/wiki/agent/agent-architecture.md` 中关于 AgentLoop 的描述
- [ ] 8.2 删除已废弃的 `AgentLoop.callModel`、`handleResponse`、`dispatchToolUse` 代码（在 feature flag 稳定后 — deferred，保留 legacy fallback）
- [x] 8.3 删除 `SmartCompressor.splitByTaskBoundary` 和 `Compactor.splitTasks` 的旧实现（在 TaskSegmenter 稳定后）
- [x] 8.4 运行 `go build ./...` 和 `go test ./agent/...` 最终验证
