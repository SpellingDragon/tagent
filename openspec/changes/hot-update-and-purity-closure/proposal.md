# Proposal: hot-update-and-purity-closure

## Why

dev 分支评审（main...dev，483 文件）实证两类欠账：其一，运行中改 exec/memory/meditation/storage/governance/telemetry/宿主工具等 Boot 一次能力只能重启（`assembly/host_reload.go:790` restartRequiredChanges 具名 409），"事件指引整代装配"那一刀未落；其二，dev 撤自造状态后留下收敛残留尾——交付 owed 超驻留窗口被 trim 致重启漏投、retry 跨重启无界、读面 corruption 伪装 missing、影子只写字段、常量双源、gate 两洞、文档脱节。架构脊柱已纯（链单一真相、读/父/检索派生、owed 原子），但边缘漏，须清完残留并清热换缺口才称"足够纯粹"。

## What Changes

- **01 delivery-correctness**：owed 活动集驻留不被 trim、retry attempt 落链有界、交付读错具名不降级（锚 B）。
- **02 read-failure-honesty**：corruption≠missing、Reason/Found 语义对齐、barrier 失败不 serve 未落盘值。
- **03 retired-state-hygiene**：删影子只写副本/死参/独立写权（publicationRecords.records、configFact 双编码、budgetMB、SetRetrievalTuning、快照 Cursor）。
- **04 single-source-gates-docs**：常量单源机械连接、gate 两洞修复、preflight 覆盖、文档脱节清偿。
- **05 event-guided-hot-update**：变更事实指引 builder 每代装配整代 Boot 一次消费者 + 登记独占资源，复用整代切换/借还 drain/退役台账，清空 restartRequiredChanges 非根全清单（storage.type/root 根状态具名留重启档，锚 A）。

## Capabilities

spec 已下沉至二级 `changes/<NN-domain>/specs/`；一级仅留 `specs/orchestration/`。全部为既有能力 MODIFIED 扩展（delivery-closure / recall-protocol / config-state-twinship / architecture-guardrails / config-hot-reload / config-liveness），不新建重复能力。

## Impact

- 基线：dev HEAD `036c3500`。启动门：03/05 触碰 recovery.go/run.go/host_reload.go 簇，须在途 `restart-recovery-optimization` 归档后启动；01/02/04 可先行。
- 症状锚：A（热更 409）、B（重启漏投）第一天红着，F3 验红转绿+回滚必红。
- 规模：38 孙任务（>20），首波先跑最小可证伪探针 5.2 再扩规模（R34）。