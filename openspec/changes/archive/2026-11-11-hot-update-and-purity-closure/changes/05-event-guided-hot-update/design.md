# Design: 05-event-guided-hot-update

## 技术要点

- 装配单元=代际绑定：通用代际作用域插件注册表（ADR 0007）读变更声明事实 → 重建该绑定内消费者实例 → 登记独占资源（gen:<id>:<consumer>）→ 整代指针切换（复用）→ 借还 drain（复用）→ 退役 sweep 释放（复用）。消费者以插件/挂点自注册，装配根只认注册表不认特性名。事件只当输入，drain/release/原子性来自现有代际机制。
- 装载时序：声明事实先落链→构整代→切换；观察滞后窗口以按代 ID 惰性幂等装载兜底；候选构建失败 fail-closed 保旧代并 Close 候选。
- retired exactly-once：确定性 Key gen-retire:<id> 走 commitFactChecked 按键去重；boot 重放对"被取代且无后续借用"的代补退役。
- 代私有状态审计：meditation lastFired/holdUntil 改链派生；governance 待审批态落账本；AgeFold 产物本在存储（合格）。
- 回滚=旧绑定作为新代发布（事实不可变）。

## 文件清单

assembly/run.go(缝,编排者)、host_reload.go、host_document.go、tool/exec/**、memory plan 装配、features/evolution/meditation/**、storage/governance/telemetry 装配、宿主工具/skill/read/file 装配、docs/wiki/platform/org-hot-reload.md。

## 风险与回退

- 最大未知 storage 根变更在途安全（5.7）：探针 5.2 先 exec 证明机制；storage 不可安全热换则具名保留 restart 档+回写 proposal（D7）。
- 回退：fail-closed 保旧代；单消费者域可独立回滚；整代切换保证无半新半旧。