# Proposal: 03-retired-state-hygiene

父变更：hot-update-and-purity-closure（合并转移自 restart-recovery-optimization / runtime-simplicity-upgrade / event-driven-consumption）

## Why

评审实证残留独立写权/影子/死字段：publicationRecords.records 只写不读影子副本（commit.go:55）；configFact 顶层字段与嵌套 Publication wire 重复编码同一数据（commit.go:119-151）；撤 capture_budget_mb 后留死参 budgetMB+死分支（host.go:449、host_capture.go:59）；SetRetrievalTuning 独立写权唯一生产调用传常量（run.go:536）；快照 Cursor 只写不读、pinnedFacts 未 boot 重播种（recovery.go）。违反反事实检验与单一真相源。

## What Changes

删影子只写副本与死参死分支；configFact 收敛单一 wire 编码；SetRetrievalTuning 改派生/常量撤独立写权；快照 Cursor 清理、pinnedFacts boot 重播种。

## 边界与依赖

- 依赖：restart-recovery-optimization 归档（recovery.go/run.go 簇）；接口常数=Publication wire 字段集（沿用）。
- 被依赖方：05（run.go 簇串行：03 先清小改）。
- 接口面：runtime/state/commit.go、assembly/host.go、assembly/host_capture.go、assembly/run.go(SetRetrievalTuning)、memory/search/store_searcher.go、runtime/state/recovery.go。
- 禁止事项：不改 host_delivery.go（01）、host_reload.go（05）、read.go（02）；不复活已撤 tuning key。