## Context

event-driven-agent-loop 变更已完成，EventBus + AgentLoop + Preprocessor 架构已接通。架构 review 发现 5 个代码层面偏离需要修复。这些问题不影响数据流正确性，但违反设计意图，在进一步功能迭代前需清理。

当前状态：
- `handleResponse` 同时 publish tool_use 到 bus 和 dispatch，导致下一轮 Pull 拉到 tool_use 后浪费一次循环
- `Preprocessor.session` 字段被 SetSession 设置但 Process 从不读取（使用传入的 sess 参数）
- `AgentLoop.sessionSvc` 和 `AgentLoopConfig.SessionSvc`/`Session` 从未被调用方设置
- `Run()` 通过 `ta.preprocessor.maxTokens` 等私有字段访问父 Preprocessor 配置
- `Run()` 保留 `ta.preprocessor == nil` 的 legacy `runner.Run()` fallback 路径

## Goals / Non-Goals

**Goals:**
- tool_use 在消费时 dispatch，消除 handleResponse 中的双重执行噪音
- 清理所有死代码字段和方法
- Run() 不穿透 Preprocessor 封装访问私有字段
- 移除 legacy fallback 路径

**Non-Goals:**
- 不改变对外 API 签名（StartLoop/InjectMessage/StopLoop/Run）
- 不修改子 agent 隔离机制（工厂创建的 TagentAgent 已有独立实例）
- 不实现全 tmux 化（executeSync fallback 保留）
- 不修改 SummaryPlugin 的注册方式（保留在 Runner 上）

## Decisions

### 1. tool_use 消费时 dispatch

**决策**：将 tool dispatch 从 `handleResponse` 移到 `Run()` 主循环。

**当前流程**：
```
Run():
  Pull → onEvent → Process → callModel → handleResponse
    handleResponse:
      bus.Publish(tool_use)        ← 1. publish
      emitEvent(toolcall_evt)      ← 2. onEvent
      dispatchToolUse(tc)          ← 3. 立即 dispatch（不该在这里）
```

**修改后流程**：
```
Run():
  Pull → batch
    for evt in batch where type == tool_use:
      dispatchToolUse(evt.ToolCall)   ← 消费时 dispatch
    for evt in batch where type == external_input:
      onEvent(wrap(evt))              ← 持久化
    Process(batch, session)           ← 构建 messages
    if shouldCallModel:
      callModel → handleResponse
        handleResponse:
          bus.Publish(tool_use)        ← 只 publish，不 dispatch
          emitEvent(toolcall_evt)     ← onEvent
```

**理由**：符合 design.md 修正版的设计意图。当 batch 同时包含 tool_use + external_input 时，两者都能正确处理。dispatch 的 goroutine 异步执行，不阻塞主循环。

**风险**：handleResponse 中 maxToolIters 检查必须在 publish 前拦截（不变）。dispatch 从 handleResponse 移到 Run() 增加了极小延迟（主循环立即回到 Pull），可忽略。

### 2. Preprocessor.session 死字段删除

**决策**：删除 `Preprocessor.session` 字段和 `SetSession()` 方法。

**理由**：`Process()` 使用传入的 `sess` 参数，`p.session` 从未被读取。`AgentLoop.SetSession` 中对 `preprocessor.SetSession` 的调用也需删除。

### 3. AgentLoop 死字段删除

**决策**：
- 删除 `AgentLoop.sessionSvc` 字段
- 删除 `AgentLoopConfig.SessionSvc` 字段
- 删除 `AgentLoopConfig.Session` 字段（所有调用方使用 `SetSession()` 方法）
- 更新结构体注释（L18-33），反映实际流程

### 4. Run() 封装修复

**决策**：
- 提取 `newPreprocessorFromConfig(cfg *TagentConfig) *Preprocessor` 辅助方法
- `NewTagentAgent` 和 `Run()` 都调用此方法
- 删除 `ta.preprocessor.maxTokens`/`tokenCounter`/`thresholdPct` 私有字段访问

```go
func newPreprocessorFromConfig(cfg *TagentConfig) *Preprocessor {
    compressor := NewSmartCompressor(buildCompressorOpts(cfg)...)
    counter := NewDefaultTokenCounter()
    return NewPreprocessor(compressor, counter, cfg.MaxTokens, cfg.CompressThreshold)
}
```

### 5. Run() legacy fallback 删除

**决策**：移除 `tagent_agent.go` L357-362 的 fallback 路径：
```go
// 删除：
if ta.preprocessor == nil || ta.config == nil || ta.config.Model == nil {
    return ta.runner.Run(ctx, userID, sessionID, message)
}
```

**理由**：设计明确"不使用 runner.Run()"。仅测试中使用 fallback，需更新测试改用真正的 AgentLoop。`ta.preprocessor` 字段保留（用于 nil check 防止 panic），但不再 fallback 到 runner。

## Risks / Trade-offs

**[Risk] tool_use dispatch 时机变更影响测试** → 更新 `agent_loop_test.go` 和 `tool_dispatch_test.go` 中依赖 handleResponse 内 dispatch 的断言。

**[Risk] 删除 AgentLoopConfig.Session/SessionSvc 可能破坏测试** → 检查所有 `AgentLoopConfig{...}` 构造，移除这两个字段的设置。

**[Risk] Run() fallback 删除影响 mockRunner 测试** → `tool_agent_test.go` 中 `mockRunner` 测试需改用真正的 AgentLoop 或标记为 `//go:build poc`。

**[Trade-off] tool_use 在 bus 中等待消费** → design.md 接受"事件顺序不保证"。dispatch goroutine 异步执行，结果以 external_input 回写 bus，下一轮 Pull 消费。
