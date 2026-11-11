# Proposal: 03-structural-golden

## Why

词汇审计第三层：结构/golden 残留——checkpointSliceMailbox 常量与 StateSliceMailbox 重复（双名一义，违 session-checkpoint-material「只注册一个切片」精神）；golden window_bounds 检查点义「载体」未对齐 checkpoint；SnapshotCheckpoint/RestoreCheckpoint 方法名与接缝 SnapshotState/RestoreState 关系待裁定。

## What Changes

删除 checkpointSliceMailbox 重复常量（级联验证零生产消费者）；golden window_bounds 检查点义载体→checkpoint；评估 SnapshotCheckpoint/RestoreCheckpoint 去留。

## 边界与依赖

- **依赖**：与 02 触碰集不相交（立案即并发）；golden 载体→checkpoint 的词汇门复核待 02 入 main（**实现依赖**，非阻塞结构删除）。session-checkpoint-material／request-budget-accounting 为**接口常数**（既有契约）。
- **被依赖方**：01（共享 runtime/task/checkpoint.go——03 先定方法名去留，01 后改注释，**串行**）；F0 godoc（若 03.1 改名）。
- **接口面**：StateSliceMailbox（runtime/state/mailbox_checkpoint.go:18）为 mailbox 切片单一权威名；checkpointSliceMailbox 删除后消费者改用 StateSliceMailbox。
- **禁止事项**：MUST NOT 动 02 白名单（lint.sh/vocabulary_alignment.json）；MUST NOT 改 mailbox 切片语义（只去重复名，行为不变）；MUST NOT 在 01 改注释前抢占 runtime/task/checkpoint.go 注释面；方法名去留裁定上报编排者（不自行改名扩面）。

## Impact

- 验收：删除后 build/vet/测试绿＋零生产消费者实证＋golden 门绿。
- 共享面：runtime/task/checkpoint.go 方法名评估先于 01 注释改名。