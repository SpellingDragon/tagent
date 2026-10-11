## Why
state应持有agent id的完整消费现场，治理只约束架构定义改变。
## What Changes
实现state.go/config.go：JSON热加载、现场复用、快照恢复、孤儿回收。
## 边界与依赖
- 依赖：一级design D4/D5签名常数；TaskManager/Memory/NewAgent循环在集成时具备。
- 被依赖方：事件壳、任务适配、插件、恢复集成测试。
- 接口面：State/AgentState、Config解析与NewAgent装配。
- 禁止：普通工具治理、全局任务调度、函数序列化、生产WAL。
