## Why

tagent 的 InjectMessage 启动全新 Run 处理 Tmux 通知，事件被 drain 丢弃；同一 Session 上的并发 Run 导致因果链竞争。根因不是 Flow 的 `IsFinalResponse → break`（这是正确的"这批处理完了"语义），而是**没有持久循环在 Run 结束后继续 drain 下一批事件**。需要一个单消费者事件邮箱 + 持久 Loop，让 tagent 成为不间断运行的 Agent 运行时。

## What Changes

- **新增 StartLoop/StopLoop**：TagentAgent 新增两个方法。StartLoop 启动持久 Loop goroutine，返回持久 output channel；StopLoop 终止 Loop
- **新增内部 mailbox**：`chan model.Message`（cap=256），并发写入、单 goroutine 消费。不引入新事件类型——mailbox 就是 `model.Message` 的 channel
- **修改 InjectMessage 内部实现**：Loop 运行时写入 mailbox（而非启动新 Run）；Loop 未运行时保持现有行为（向后兼容）。**签名不变**
- **修改 Close()**：先 StopLoop 再关闭现有资源
- **不改动**：RunSimple（one-shot 模式不变）、CommandTool（仍调 InjectMessage，接口不变）、AgentToolWrapper（Call 保持同步）、Plugin/SmartCompressor/BeforeModel（Runner 内部调用不变）、trpc-agent-go 框架（零修改）

## Capabilities

### New Capabilities
- `persistent-event-loop`: 持久 Loop + mailbox——StartLoop 启动后台 goroutine 循环执行 `drain mailbox → runner.Run() → 转发事件 → 回到 drain`，不退出。InjectMessage 在 Loop 模式下写入 mailbox。复用 Runner/Flow/Plugin/BeforeModel 全部管道

### Modified Capabilities
- `production-wiring-fix`: Close() 调用顺序变更（先 StopLoop 再关 closers + memStore + runner）；InjectMessage 行为变更（Loop 模式下写 mailbox）

## Impact

- **agent/tagent_agent.go**: 新增 mailbox/outputCh/loopCtx/loopCancel/loopActive 字段；新增 StartLoop/StopLoop 方法；新增 loop goroutine + mergeBatch 辅助函数；修改 InjectMessage 内部实现；修改 Close() 增加 StopLoop 调用
- **无其他文件改动**
