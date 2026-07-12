## 1. 删除 PlanProgressTracker 及相关代码

- [x] 1.1 删除 `agent/plan_progress_tracker.go`
- [x] 1.2 删除 `agent/plan_progress_tracker_test.go`
- [x] 1.3 删除 `agent/context_manager.go` 中 PlanProgressTracker 回调注册和 `OpenSpecDir` 字段
- [x] 1.4 删除 `agent/tagent_agent.go` 中 `OpenSpecDir` 字段和传递
- [x] 1.5 删除 `config.go` 中 `OpenSpecDir` 字段
- [x] 1.6 删除 `tagent.go` 中 `OpenSpecDir` 传递
- [x] 1.7 删除 `examples/wechat-bot/tagent.yaml` 中 `openspec_dir` 配置
- [x] 1.8 `go build ./...` 通过

## 2. 更新 FrameworkPrompt

- [x] 2.1 删除 FrameworkPrompt 中的 `## 工作计划` 和 `## 框架注入的上下文` 段落
- [x] 2.2 FrameworkPrompt 仅保留异步工具、事件标识、上下文压缩三段
- [x] 2.3 `go build ./...` 通过

## 3. 实现 PlanAgent 自定义 Run

- [x] 3.1 新建 `tool/plan/plan_agent.go`，定义 `PlanAgent` 结构体（嵌入 `*agent.TagentAgent` + `openSpecDir` 路径）
- [x] 3.2 实现 `PlanAgent.Run` 方法：检查 invocation 中的 `action` 字段，`progress` 走工程直读，其他走 `TagentAgent.Run`
- [x] 3.3 实现 `runProgressQuery` 方法：扫描 `openspec/changes/`（排除 archive/），若恰好 1 个活跃 change 则读 tasks.md、解析 checkbox、构建进度摘要，作为 final response event 返回
- [x] 3.4 实现从 invocation 中提取 `action` 字段的逻辑（从 message content 或 RuntimeState 中解析）
- [x] 3.5 实现 `buildProgressEvent(summary string) *event.Event`：构造包含 final response 的 event，不调用 LLM
- [x] 3.6 `go build ./tool/plan/...` 通过

## 4. 更新 plan tool 描述和 agent prompt

- [x] 4.1 更新 `plan_tool_desc.md`：使用结构化 `action` 参数（enum: create/update/archive/progress）+ `request` 参数
- [x] 4.2 每种 action 提供调用示例
- [x] 4.3 更新 `plan_agent.md` system prompt：说明 `action` 参数和双模式执行逻辑

## 5. 更新 tagent.yaml 注册 PlanAgent

- [x] 5.1 在 `tagent.yaml` 中更新 plan agent 的工厂配置，使用 `PlanAgent` 而非标准 `TagentAgent`
- [x] 5.2 在 tagent agent tools 中注册 plan（已注册，确认 action 参数声明）

## 6. 测试与验证

- [x] 6.1 新增 `tool/plan/plan_agent_test.go`：测试自定义 Run 的 progress 直读路径（不过 model）、标准 ReAct 路径委托、未知 action 默认走 ReAct
- [x] 6.2 测试 `runProgressQuery`：0/1/多个活跃 change 时的行为
- [x] 6.3 `go build ./...` 通过
- [x] 6.4 `go test ./agent/... ./tool/...` 全部通过
## 1. 删除 PlanProgressTracker 及相关代码

- [x] 1.1 删除 `agent/plan_progress_tracker.go`
- [x] 1.2 删除 `agent/plan_progress_tracker_test.go`
- [x] 1.3 删除 `agent/context_manager.go` 中 Callback 2.5 PlanProgressTracker 注册代码（`tracker := NewPlanProgressTracker(...)` 和 `tracker.RegisterCallback(cb)`）
- [x] 1.4 删除 `agent/context_manager.go` 中 `ContextManagerConfig.OpenSpecDir` 字段
- [x] 1.5 删除 `agent/tagent_agent.go` 中 `TagentConfig.OpenSpecDir` 字段
- [x] 1.6 删除 `agent/tagent_agent.go` 中 `newContextManagerFromConfig` 传递 `OpenSpecDir` 的代码
- [x] 1.7 删除 `config.go` 中 `AgentConfig.OpenSpecDir` 字段
- [x] 1.8 删除 `tagent.go` 中 `OpenSpecDir: acfg.OpenSpecDir` 传递
- [x] 1.9 删除 `examples/wechat-bot/tagent.yaml` 中 `openspec_dir: "."` 配置
- [x] 1.10 `go build ./...` 通过

## 2. 更新 FrameworkPrompt

- [x] 2.1 删除 `agent/tagent_agent.go` FrameworkPrompt 中的 `## 工作计划` 段落和 `## 框架注入的上下文` 中的 `[active_plan]` 行
- [x] 2.2 FrameworkPrompt 恢复为仅描述框架机制（异步工具、事件标识、上下文压缩），不硬编码任何工具名
- [x] 2.3 `go build ./...` 通过

## 3. 更新 plan agent system prompt

- [x] 3.1 更新 `examples/wechat-bot/resources/prompts/plan_agent.md`：增加双模式说明——明确哪些操作需要 LLM 推理（创建/更新/归档），哪些是工程逻辑直读（查询进度）
- [x] 3.2 在查询进度段落中注明：理想情况下应直接读文件返回，不走 ReAct 循环（当前实现仍过 model，后续优化）

## 4. 更新 plan tool 描述

- [x] 4.1 更新 `examples/wechat-bot/resources/prompts/plan_tool_desc.md`：分"需要过 model 的操作"和"直接返回的操作"两个段落
- [x] 4.2 每个段落包含具体调用示例
- [x] 4.3 说明 tagent 何时应调用 plan（复杂多步骤任务创建计划、完成步骤后更新进度、需要了解进度时查询）

## 5. 验证

- [x] 5.1 `go build ./...` 通过
- [x] 5.2 `go test ./agent/... ./plugin/... ./event/...` 全部通过
- [x] 5.3 确认无 PlanProgressTracker / OpenSpecDir 残留引用
- [x] 5.4 确认 FrameworkPrompt 不包含 "plan" 工具名