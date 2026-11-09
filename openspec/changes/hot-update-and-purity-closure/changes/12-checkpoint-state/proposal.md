# 12-checkpoint-state 提案

## Why

LongRun 校准（121 代进程重启）在 boot 挂死，阻断 PR#37 合并：每代 boot 停滞超线性增长（gen45≈30s→gen48≈3m→gen49+>15m 撞子超时）。代码审读钉死病灶＝boot 尾段 `Unsettled` 对每个 ActiveSession 全量读 durable 链（invoke.go:435/471 → run.go:2170 eachCommittedEvent），站点随委派永久累积、事件线性增 → O(站点×事件) 二次扫描；送达账恢复 `deliveryRecoveryFacts` 同样全史读、`RestoreOwed` 全量重投。投影/折叠恢复本身毫秒级（窗口有界），病在「义务账本无 checkpoint、每次重启从头重放」。

用户裁决（Flink 类比）：不做独立账本 checkpoint，由框架统一管理状态——插件/消费者如算子，把可恢复状态委托框架；checkpoint 载体统一为一种框架级 checkpoint 事实族，压缩事实回归纯投影、只作触发源之一。

## What Changes

- 新增框架级 `checkpoint` 事实族，承载全部可恢复状态切片（投影幸存者、宿主全局送达/结算账含开账、邮箱欠账、任务终态）；offset=分区 Seq 水位 + 开账清单。
- 框架 checkpoint 协调者：有界节奏（提交计数 + 折叠触发 + close/quiesce），提交锁下一致性切面聚合注册切片并落 checkpoint 事实。
- 状态接缝：消费者注册 snapshot/restore，框架聚合；恢复=读最近 checkpoint + 回放偏移后事实经现有 observe/commit 管线。
- 删除三条全史读者（deliveryRecoveryFacts 全扫、Unsettled 逐站点 durable 扫、RestoreOwed 全量重投）；Unsettled 改读恢复后内存态。
- 退役 RecoveryTicket 独立快照路线。
- 取代 domain 06 D3「快照装全状态否决」一条（用户显式裁决）；继承 D4 折叠锚一致性切面定理。

## Impact

- 能力：checkpoint-state（新增）；window-recovery（恢复读路径收口）；delivery-correctness（8.1 由构造消解）。
- 代码：api/event（TypeCheckpoint+载荷）、assembly/run（协调者+恢复重写+状态接缝）、assembly/host_delivery（删全史读）、runtime/state（邮箱快照）、退役 recovery.go 票据路线。
- 门：durable_truth / event_integrity_scenarios / fold_contract / FACT-VOCABULARY / gen_godoc / anchored_boot_parity / window_bounds。
- 合并门：LongRun 六分片全绿 + 每代 boot 成本平坦。