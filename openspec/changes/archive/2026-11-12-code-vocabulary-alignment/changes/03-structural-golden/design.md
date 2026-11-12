# Design: 03-structural-golden

决策权威见一级 `design.md` D2/D4；验收锚冻结于一级 `tasks.md`。本域只落结构裁定细节。

## 03.1 方法名评估判据（先裁定，后动手）

`runtime/task/checkpoint.go` SnapshotCheckpoint(:93)/RestoreCheckpoint(:118) vs 状态接缝 SnapshotState/RestoreState：
- 唯一生产调用方 `assembly/task_checkpoint.go:34,43`（grep 实证全清单）。
- **裁定判据**：若 SnapshotCheckpoint/RestoreCheckpoint 是接缝 SnapshotState/RestoreState 的**任务域具体实现**（distinct role：任务检查点快照 vs 通用状态接缝），则**保留**（合法分层，非冗余别名）；若是**同义冗余别名**（同一操作两个名），则改名对齐接缝并登记旧→新映射。
- 倾向：审计标注「borderline，likely legitimate distinct roles」——默认保留，除非读码证实冗余。裁定产出（保留理由/改名映射）上报编排者，不自行扩面。

### 03.1 裁定结果（D-03 实施期产出）：**保留**（不改名）

读码四条证据，均指向「distinct role，非冗余别名」：

1. **结构上不可能实现接缝**：`runtime/state` 依赖 `runtime/task`（`runtime/state/mailbox.go:10`、`store.go:18`、`memory.go:11`、`migrate.go:19`、`session.go:5`、`durable.go:16`、`skeleton_assert.go:10`），`runtime/task` 零处依赖 `runtime/state`（`rg -n "runtime/state" runtime/task/` 无匹配）。`MemoryRegistry` 若实现 `state.Stateful` 即成导入环，故 `SnapshotCheckpoint/RestoreCheckpoint` 在语法上不可能是接缝实现——接缝实现是 `assembly/task_checkpoint.go` 的 `tasksStateSlice`（提供 `StateName`，映射到 registry 方法）。
2. **契约不对等**：接缝 `RestoreState(ctx, payload) error` 只回错误；`RestoreCheckpoint` 除落 `r.restored` 外还登记去重键（`r.admission.AdmitKey`）、观测通知序号（`r.seq.Observe`），并经 `CheckpointCoverage()` 交出「各任务恢复所站修订号」这一 registry 域水位，boot 据此只折水位之后的链——接缝契约无此槽位。
3. **载荷归属不同**：载荷是 registry 自己的 `RegistryCheckpoint`（`StatedTask` 含 spec/status/revision/round/handoff/mutations/obligations），不是接缝的切片信封；接缝文档亦明写「其状态跟随本接缝不读的账户」的那类持有者（任务账、邮箱欠账）不实现 reducer 面。
4. **名词语义不同层**：「Checkpoint」在仓库里指事实族（`event.TypeCheckpoint`，GLOSSARY 行 31「框架检查点」），「State」在接缝里指持有者的可恢复状态按切片键声明。改名 `SnapshotState` 会让该 registry 方法看起来实现了一个它物理上实现不了的接缝，且误导读者以为存在 `StateName()`（实际不存在）。

调用方全清单（`rg -n "SnapshotCheckpoint|RestoreCheckpoint" --glob '*.go' .`）：定义 `runtime/task/checkpoint.go:93,118`；生产 `assembly/task_checkpoint.go:34,43`；测试 `runtime/task/checkpoint_test.go:44,55,64,101,121,141,193,208,241,246`、`assembly/task_checkpoint_test.go:77,184`。改名不触发电机，故本域零 .go 方法改动，`runtime/task/checkpoint.go` 释放给 01 改注释。

## 03.2 常量去重级联

`assembly/checkpoint.go:71` checkpointSliceMailbox="mailbox" 与 `runtime/state/mailbox_checkpoint.go:18` StateSliceMailbox 同值同义。
- 删除前 grep checkpointSliceMailbox 全仓→实证唯一消费者 `assembly/checkpoint_test.go:1151`（负锚）。
- 删除常量，消费者改用 StateSliceMailbox（单一权威名，session-checkpoint-material「只注册一个切片」）。
- checkpointSlice* 系列（Projection/Delivery/Mailbox/Tasks/Inputs/Compress）：只删 Mailbox 重复项，其余若各有独立值则不动（逐一核值，非整系列删）。
- 行为不变：mailbox 切片键仍 "mailbox"，只去重复名。

## 03.3 golden 载体→checkpoint

`golden/runtime/window_bounds.json:218,227` 检查点义「载体」→checkpoint（与 GLOSSARY 行 31 对齐）。泛义载体不动。check_window_bounds.py 绿＋词汇门复核（02 入 main 后）。

## 风险与回退

- 误删非重复常量：逐一核 checkpointSlice* 值，只删与 StateSliceMailbox 同值同义者。
- 方法名误改：默认保留，裁定上报；改名须级联调用方全清单＋build/vet/测试绿。
- 回退：常量删除/字面量改名，git revert 即恢复。