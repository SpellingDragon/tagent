## Why
核心回归纯事件消费，任务状态归任务管理器。
## What Changes
实现 agent.go 与 task_manager.go；移除收齐门与工具治理。
## 边界与依赖
- 依赖：一级design D4/D5为签名常数；state/插件实现仅在集成时依赖。
- 被依赖方：config装配与agent集成测试。
- 接口面：BaseTAgent、TaskManager、AgentTask。
- 禁止：修改其他域、批次等待、票据渲染、生产保护扩张。
