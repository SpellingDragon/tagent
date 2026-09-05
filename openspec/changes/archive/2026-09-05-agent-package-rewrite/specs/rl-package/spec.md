## 能力: rl-package

将 TrajectoryRecorder、HTTPAPI、SwappableModel 移到独立的 rl/ 包。

## 需求

### 移动的类型

| 原位置 | 新位置 | 说明 |
|--------|--------|------|
| `agent/trajectory_recorder.go` | `rl/trajectory_recorder.go` | TrajectoryRecorder + NewTrajectoryRecorder + NewTrajectoryRecorderModelWrapper |
| `agent/http_api.go` | `rl/http_api.go` | HTTPAPI + NewHTTPAPI + handler 方法 |
| `agent/tagent_agent.go` 中 SwappableModel 部分 | `rl/swappable_model.go` | SwappableModel + NewSwappableModel + Swap + GenerateContent + Info |

### AgentLoop 接口

```go
// rl/agent_loop.go
package rl

import (
    "trpc.group/trpc-go/trpc-agent-go/event"
    "trpc.group/trpc-go/trpc-agent-go/model"
)

// AgentLoop is the interface that HTTPAPI uses to interact with TagentAgent.
// This decouples rl/ from agent/ — rl/ only depends on this interface.
type AgentLoop interface {
    InjectMessage(msg model.Message)
    InjectMessageWithSource(source string, msg model.Message)
    StartLoop(userID, sessionID string) (<-chan *event.Event, error)
    StopLoop()
}
```

### 外部引用更新

- `tagent.go`：`agent.TrajectoryRecorder` → `rl.TrajectoryRecorder`
- `tagent.go`：`agent.NewTrajectoryRecorder` → `rl.NewTrajectoryRecorder`
- `examples/wechat-bot/main.go`：`agent.NewSwappableModel` → `rl.NewSwappableModel`
- `examples/wechat-bot/main.go`：`agent.NewHTTPAPI` → `rl.NewHTTPAPI`

### 约束

- rl/ 包不得 import agent/ 包（通过接口注入解耦）
- TrajectoryRecorder 实现 model.Model 接口不变
- HTTPAPI 通过 AgentLoop 接口访问 TagentAgent 能力
- 所有测试随文件移动，包名改为 `package rl`
