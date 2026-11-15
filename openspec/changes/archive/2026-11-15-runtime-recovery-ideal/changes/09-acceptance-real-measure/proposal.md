# 04 验收真量（boot 路径簇合单域）

## Why

一级 [proposal.md](../../proposal.md) Why 4「验收锚量错面」与 [design.md](../../design.md) D6.6：既有 boot 锚量的是**代理量**，不是真量。

- `TestSymptomAnchorSessionBootCostFlatAcrossGenerations`（assembly/checkpoint_test.go:1231-1279）的 `costs` 取 `boot.CommittedFacts()`（checkpoint_test.go:1242 → assembly/run.go:2012），是**正文计数**；`indexed` 取 `chainIndexSize`（assembly/anchored_boot_parity_test.go:254）只作分母。journal 字节 / 索引条目 / durable load / 墙钟四量一个未量。（W0 已另立 S2 症状锚 `TestSymptomAnchorBootCostFlatAcrossGenerations` 于 assembly/symptom_anchor_recovery_test.go 并复跑红；本域 04 精化其断言面为四真量。既有 Session 锚保留不动——config-surface 占用 checkpoint_test.go。）
- 夹具单会话无任务（`commitSessionTurns`，checkpoint_test.go:1166/1244），durable 五前缀 keys 恒 0 → `DurableCommit.load` 全史扫（runtime/state/durable.go:305）在锚外；`openJournalLog` 整份文件重放（memory/ledger/journal.go:490）与 `FileSegmentStore.Load` 全量索引物化（memory/ledger/segment_store.go:281/373）同样在锚外——正文计数只反映 `seedAnchoredReplay`（run.go:2562）的解码面。
- 后果：一级 proposal Why 1-2 的违背全在锚外，锚绿而设计预期未达成，即「**全绿归档 ≠ 设计预期达成**」。P4 已明写有界恢复量的是真量而非正文计数代理量（design.md:16）。

## What

承 D6.6 与 §五冻结的四量名（design.md:86），本域**定义 S2 的度量面**：

- **四真量探针**：`journalBytes`（开档解码读入字节，journal.go:433/490）/ `indexEntries`（常驻索引规模 `len(s.index)`，segment_store.go:373；§五裁定**非 `EventCount` 全史标量**）/ `durableLoadCount`（`LoadStats` 五前缀 Keys 之和，durable.go:203-215/348-362）/ `wallClock`（真实 boot 入口两侧计时）。
- **多代＋多任务夹具**：沿用 5 代重启（checkpoint_test.go:1236-1254），每代追加任务提交（`settleTasks`，assembly/task_checkpoint_test.go:35），使 durable 五前缀真有正文；保留 `chainIndexSize` 分母与 `seedFullReplay`（run.go:2470）全史对照。
- **S2 症状直译锚**（§六 design.md:93）：四量不随历史线性增长；**W0 写就即红**，01/02 落地后转绿。
- **负锚**：boot 四量对全史走四量的对照断言，恢复全史读即红。
- 正文计数降级为观测量（`t.Logf`），**不作判据**。

## Capabilities

- `bounded-recovery`（ADDED）：boot 验收四真量、多代多任务夹具、S2 症状锚与全史读负锚。

## 边界与依赖

- **与 01/02 合单原子（W1）**：design D4（design.md:45）与一级 tasks.md:32——探针植根同一 boot 路径簇（run.go:617 `NewDurableCommit` / run.go:690 `restoreCheckpointState` / journal.go open / segment_store.go Load），且 01/02 落地需本域真量锚验证，不可并行派发。
- **四量是共享契约**（§五 design.md:86）：04 定义，01/02/03 消费转绿；量名与语义冻结，本域不得擅改，探针签名改动须报编排者核冲突。
- **依赖既有只读观测面**：`LoadStats`（durable.go:209）、`EventCount`（segment_store.go:584）、`Tier.Segments`/`Tier.Store`（memory/ledger/config.go:82/86）、`runCommit.inner`（assembly/commit.go:361-365，包内可达）。journal 开档读量今日无出口，需只读探针（见 design）。
- **被依赖方**：01（durableLoadCount 转绿）、02（journalBytes/indexEntries 转绿）、03（wallClock 与 governance 轮次）、一级 F3（S2 红转绿两次证据）。
- **governance 面边界**：DenialLedger/GoalRegistry 生产零调用（一级 proposal Why 3），其全扫（features/evolution/governance/denial.go:304/327）只落 `wallClock`；governance 夹具轮次待 03 接线后（W2）追加，本域 W1 不声称覆盖该层。
- **禁止事项**：
  - 不触外来未跟踪文件（runtime-architecture-convergence/、scripts/gates/check_event_semantics_symptoms.py、runtime_budgets.py、test_check_runtime_symptoms.py、skills/interview/agent-interview-helper/）、冻结归档（openspec/changes/archive/**）、config-surface-normalization 未提交变更。
  - 不读 examples/wechat-bot/.wechat-config/data。
  - 不改 `AtomicStore` 四方法（memory/ledger/atomic_store.go:50-61），不新增恢复旁路，不改生产恢复行为——本域只加度量面。
  - 不删/改已提交事实（session-checkpoint-material/spec.md:44 红线）。
  - 本文档阶段**只规划不实施**。