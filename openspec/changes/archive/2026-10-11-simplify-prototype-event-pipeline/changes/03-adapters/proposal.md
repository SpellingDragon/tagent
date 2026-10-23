## Why
输入、结果和通知不是同一种呈现对象；代理监控训练数据职责不应回填到有状态循环。
## What Changes
实现plugins.go/memory.go/model.go：选择性前后变换、结果票据、无状态代理记录。
## 边界与依赖
- 依赖：一级design D4/D5签名常数，AgentState.Context/ReplaceContext集成时提供。
- 被依赖方：core、config与agent集成测试。
- 接口面：Plugin闭包、Memory、CallModel。
- 禁止：全事件默认标记、输入折叠、插件调度任务/模型、proxy暗读state。
