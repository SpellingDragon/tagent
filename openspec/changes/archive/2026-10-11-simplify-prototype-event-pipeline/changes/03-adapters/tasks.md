## 1. 插件与代理
- [x] 1.1 实现按Kinds选择的前后闭包与结果memory插件、选择性Compact —— 验证：`gofmt -l prototype/plugins.go`。
- [x] 1.2 实现原文Memory及快照，CallModel无状态调用与全量成功/失败轨迹 —— 验证：`gofmt -l prototype/memory.go prototype/model.go`。
- [x] 1.3 核对输入不标记/折叠、首次结果原文可见、proxy不访问state，回报未集成状态 —— 验证：读取staging及关键词检索。
