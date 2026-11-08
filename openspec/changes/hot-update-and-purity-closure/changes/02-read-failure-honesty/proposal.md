# Proposal: 02-read-failure-honesty

父变更：hot-update-and-purity-closure（合并转移自 restart-recovery-optimization / runtime-simplicity-upgrade / event-driven-consumption）

## Why

评审实证：read.go Event/Reference/Text 吞 GetEvent 错误为 not held，get.go 遂答 missing，而 QueryEvents 同况如实报 ErrSegmentBodyMissing——同一下层 named failure 读面两副答案（corruption 伪装 missing）；get.go Reason 注释"empty when Found true"与实现 Found=true+Reason=relation_error 矛盾；kvCommit put 先于 flush，flush 失败后 resident 返回未落盘值（短暂第二真源）。

## What Changes

GetEvent 错误具名传播不吞；Reason/Found 语义对齐；barrier 失败 resident 不 serve 未落盘值。

## 边界与依赖

- 依赖：无（接口常数：recall_get 的 missing/expired/relation_error 具名原因族，沿用）。
- 被依赖方：无。
- 接口面：assembly/read.go、tool/recall/get.go、memory/ledger/localfile.go。
- 禁止事项：不改 host_delivery/host_tools（01 域）；不改 fuse/request_assembly（04 域）；不新增静默 fallback。