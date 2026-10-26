## 1. 事件与任务
- [x] 1.1 实现事件消费壳，所有模型产出回队列、无等待门 —— 验证：`gofmt -l prototype/agent.go`。
- [x] 1.2 实现OwnerID任务生命周期、完成通知、恢复unknown及子调用适配 —— 验证：`gofmt -l prototype/task_manager.go`。
- [x] 1.3 逐函数核对一级签名并回报未集成状态；包含职责/未实现/接线 —— 验证：读取两份staging及关键词检索。
