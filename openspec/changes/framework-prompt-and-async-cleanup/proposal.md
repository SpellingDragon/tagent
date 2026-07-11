## Why

LLM 不理解 tagent 的事件驱动机制——异步工具的返回值、`[action_tool_result]` 事件、`[evt_KEY|type]` 前缀、上下文压缩等——导致 LLM 看到中间状态后退化循环。同时，`AsyncTaskChecker` 机制（让 `Run()` 阻塞等待异步完成）与设计意图不符——设计预期是非阻塞的，action 子 agent 正常返回后 tmux 完成事件注入到 tagent 主 agent。

## What Changes

- **框架 system prompt 注入**：在用户配置的 system prompt 之前，自动注入 tagent 框架运行时说明，告知 LLM 异步工具机制、事件标识、上下文压缩等。
- **删除 AsyncTaskChecker**：`Run()` 不再阻塞等待异步完成。action 子 agent 正常返回，tmux 完成事件通过 `handleStateChange → InjectMessage` 注入到正确的 EventBus（当前是 action 子 agent 的 invBus，修复后是 tagent 主 agent 的 persistentBus）。
- **`handleStateChange` 注入目标修正**：tmux 完成事件应注入到发起调用的 agent 的 EventBus。当前注入到 action 子 agent（已退出的 invBus），应注入到 tagent 主 agent 的 persistentBus。
- **`waiting_async_response` 返回值简化**：tool result 不再包含冗长的 message 字段，改为简洁的状态标识。

## Capabilities

### New Capabilities
- `framework-prompt`: tagent 框架自动在 system prompt 前面注入运行时说明

### Modified Capabilities
- `view-transform-stateless`: 删除 AsyncTaskChecker 机制；Run() 不再阻塞等待异步完成

## Impact

- `agent/tagent_agent.go`: 删除 AsyncTaskChecker 相关代码；system prompt 注入框架说明
- `agent/context_manager.go`: system prompt 构建时前置框架说明
- `tool/action/action_tool.go`: handleStateChange 的 injector 指向 tagent 主 agent（通过 SetMessageInjector 传入）；简化 TmuxExecResponse
- `tagent.go`: 调整 ActionTool 的 injector 设置目标
- `examples/wechat-bot/tagent.yaml`: 无需修改
