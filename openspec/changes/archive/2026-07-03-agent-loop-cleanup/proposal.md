## Why

event-driven-agent-loop 变更接通了数据流链路，但架构 review 发现 5 个代码层面偏离：tool_use 被 publish 到 bus 后立即 dispatch（双重执行噪音）、Preprocessor.session 死字段、AgentLoop 死字段/误导注释、Run() 通过私有字段访问父 Preprocessor（封装泄漏）、Run() 保留 legacy runner.Run() fallback 路径。这些问题不影响数据流正确性，但违反设计意图，需在进一步迭代前清理。

## What Changes

- **tool_use 消费时 dispatch**：将 `handleResponse` 中的 `dispatchToolUse` 移到主循环 `Run()`，在 `Preprocessor.Process` 之前扫描 batch 中的 tool_use 事件并 dispatch。`handleResponse` 只 publish tool_use 到 bus，不再 dispatch。
- **Preprocessor.session 死字段清理**：删除 `Preprocessor.session` 字段和 `SetSession()` 方法。`Process()` 已使用传入的 `sess` 参数，该字段从未被读取。
- **AgentLoop 死字段/注释修正**：删除 `AgentLoop.sessionSvc` 字段和 `AgentLoopConfig.SessionSvc`/`Session` 字段（从未被调用方设置）。更新结构体注释，反映实际流程（dispatch on consumption, not in handleResponse）。
- **Run() 封装修复**：将 `ta.preprocessor.maxTokens`/`tokenCounter`/`thresholdPct` 私有字段访问替换为 `ta.config.MaxTokens`/`NewDefaultTokenCounter()`/`ta.config.CompressThreshold`。提取 `newPreprocessorFromConfig()` 辅助方法，NewTagentAgent 和 Run() 共用。
- **Run() legacy fallback 清理**：移除 `ta.preprocessor == nil || ta.config == nil` 的 `runner.Run()` fallback 路径。更新依赖该路径的测试改用真正的 AgentLoop。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

（无 — 这些是内部实现清理，不改变对外 API 的 spec 级行为）

## Impact

- **agent/agent_loop.go**：Run() 主循环结构变更（增加 tool_use dispatch 步骤）、handleResponse 简化、死字段删除、注释更新
- **agent/preprocessor.go**：删除 session 字段和 SetSession 方法
- **agent/tagent_agent.go**：Run() 中 Preprocessor 创建方式变更、legacy fallback 删除、newPreprocessorFromConfig 提取
- **agent/*_test.go**：可能需要更新依赖 fallback 路径或 AgentLoopConfig.Session/SessionSvc 的测试
- **无 breaking change**：对外 API（StartLoop/InjectMessage/StopLoop/Run）签名不变
