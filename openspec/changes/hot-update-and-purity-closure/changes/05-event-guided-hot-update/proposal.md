# Proposal: 05-event-guided-hot-update

父变更：hot-update-and-purity-closure（合并转移自 restart-recovery-optimization / runtime-simplicity-upgrade / event-driven-consumption）

## Why

运行中改 exec/memory/meditation/storage/timeout/governance/telemetry/宿主工具/skill_dirs/read_root/file 只能重启（host_reload.go:790 restartRequiredChanges 全清单 409）。根因：这些活实例 Boot 构造一次、reload 原样交还，既非每代 binding 也未登记候选独占资源进退役台账。dev 已铺声明上链 groundwork（Reload 落声明事实、boot 对账+Adopt），但"按代重建 Boot 一次消费者"未落。

## What Changes

变更事实指引通用代际作用域插件注册表（ADR 0007）每代重建绑定内消费者；Boot 一次消费者先插件化归位；登记独占资源进退役台账，复用整代切换/借还 drain/退役 sweep；清空 restartRequiredChanges 非根全清单（storage.type/root 根状态留重启档）；boot Adopt/对账覆盖新消费者；崩溃窗口退役收敛；applied/held 事实矛盾收敛。

## 边界与依赖

- 依赖：03（run.go 簇串行）、D6 冻结契约（接口常数：BuildGen 签名/登记键/gen-retire Key）；实现依赖=runtime/state 退役台账与整代切换（沿用不重写）。
- 被依赖方：无（终端域）。
- 接口面：assembly/run.go(装配缝,编排者单写)、assembly/host_reload.go、assembly/host_document.go、tool/exec、memory(plan)、features/evolution/meditation、storage/governance/telemetry 装配、宿主工具/skill/read/file 装配。
- 禁止事项：不让活消费者收事件就地自换（撕整代原子性）；不新造中心化插件卸载树；不重写退役台账/借还（复用）；storage.type/root 根状态具名留 restart 档（Q3 裁定）；其余非根消费者若探针证实不可安全热换须具名保留并上报，不静默扩 409。