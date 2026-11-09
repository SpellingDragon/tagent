# Proposal: 12-checkpoint-state

父变更：hot-update-and-purity-closure（合并阻断项；CI 长分片 TestEventIntegrityLongRun 挂死）

## Why

重启恢复成本必须如设计所言由窗口/增量界定，而非随历史超线性增长。实测：boot 尾段 `Unsettled` 对每个 ActiveSession 全量读 durable 链（assembly/invoke.go:435 → run.go:2203 eachCommittedEvent 逐 key 读全部事件体），站点随委派永久累积、事件线性增 → O(站点×事件) 二次扫描，静默分钟级，gen49+ 撞爆 15m 子超时；送达账恢复 deliveryRecoveryFacts 同样全史扫（host_delivery.go:1608，注释明写 "never limited to the hot fact window"）。链/投影恢复本已有 checkpoint（压缩产物，毫秒级），但账本/任务/邮箱层没有等价物，每次重启从头重放。

裁决（用户）：不做独立账本 checkpoint，也不往压缩事实临时塞状态——引入**框架级统一 checkpoint 事实族**，类比 Flink：框架（assembly/run）做 checkpoint 协调者与状态后端，插件/消费者如算子注册状态切片、把状态管理委托框架；压缩事实回归纯投影职责，只作 checkpoint 触发源之一。恢复=最近 checkpoint + 回放偏移后事实经现有 observe/commit 管线。

## 边界与依赖

- 依赖：05 代际注册表缝（GenRegistry 模式可借鉴）、06 窗口恢复（投影幸存者锚）、01 送达账事实族。
- 接口面：api/event（新 TypeCheckpoint + Meta + 载荷类型）、assembly（协调者/恢复重写/run.go 装配缝归编排者单写）、runtime/state（状态接缝）、memory/replay（压缩触发源）。
- 退役面：RecoveryTicket 独立快照路线（assembly/recovery.go WriteRecoveryTicket、run.go:948 fileBootRecoveryTicket、recoveryTicketWriter、blob+ticket KV、golden durable_truth 钉条目）。
- 禁止事项：不改 01-11 已绿语义；checkpoint 偏移用分区 Seq（landedSeq, run.go:324），禁用进程局部 factSeq（facts.go:1045，随机后缀不全局单调）；义务无限期语义不变（开账随 checkpoint 存活，折叠不得吃掉未清承诺）。
- 连带：runtime-simplicity-features 8.1（应答被折叠收走致永不送达）由本域构造消解——开账嵌入 checkpoint 后折叠不再丢债，该卡标记 superseded。