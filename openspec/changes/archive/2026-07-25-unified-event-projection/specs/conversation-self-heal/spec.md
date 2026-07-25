## REMOVED Requirements

### Requirement: 发送模型前校验 tool 配对

**Reason**: 渲染规则改为配对自由（历史渲染不含 role=tool，见 event-timeline-rendering)，配对合法性问题从构造上消除，发送前校验不再有意义。

**Migration**: 删除 `message_validate.go` 的配对校验；以 event-timeline-rendering 的"渲染合法性不变量"测试替代。

### Requirement: 畸形消息保守修复（仅本次发送）

**Reason**: 配对概念消失后不存在"畸形配对"；此前修复的两类案例（重复/孤儿 tool 消息）分别由"投影写入统一（恰好一次）"与"配对自由渲染"根除。

**Migration**: 删除 `repairToolPairing`;e2e 以渲染合法性断言恒通过作为等价观测。
