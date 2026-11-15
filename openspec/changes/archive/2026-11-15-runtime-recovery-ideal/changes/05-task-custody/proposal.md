# 01 任务托管收编（W1 boot 路径簇合单）

## Why

一级 design D1 的 P2（单一 g(x)）与 P4（有界恢复）在任务恢复路径上未兑现：

- **全史扫旁路**：`DurableCommit.load`（runtime/state/durable.go:258）无条件全扫五前缀 `tagent/task/v1/{spec,consume,attempt,reject,fact}:`（:259/:270/:282/:294/:305，前缀常量 :53-57），解码全部历史任务正文，并以 load 自己的循环重建 revisions/mutations/owed（:317-341）——与运行期 `Commit` 的入表路径（:600-609）是两套派生。
- **时序倒置**：`NewDurableCommit` 在 assembly/run.go:617 构造即 load，早于 `restoreCheckpointState`（run.go:690）。`restoreChains`（run.go:1073-1097）用 `CheckpointCoverage()` 只跳过覆盖链的 **fold**，读的仍是已全扫入内存的表（`Records` 读 `d.facts`，durable.go:749）——省 fold 不省 load。
- **违背条款**：checkpoint-state「任务终态有界恢复」:39「boot SHALL 经 checkpoint 恢复任务态而非全史重读」、:43「boot 任务恢复成本 SHALL 不随历史任务总数线性增长」。12.4b 残留栏书面自认「body 已由 durable load 全量读入，非本单可消」。
  - ⚠️ 实测：`openspec/specs/` 无 `checkpoint-state` 主 spec 目录（151 个能力中只有 `session-checkpoint-material`）；该条款权威文本现存于冻结归档 `openspec/changes/archive/2026-11-11-hot-update-and-purity-closure/changes/12-checkpoint-state/specs/checkpoint-state/spec.md:37-47`。主 spec 转正归 05 域，本域 delta 以该 Requirement 名与文本为基准。
- **用户终裁**：「把所有已知问题修干净，达架构理想预期状态」「不考虑开发成本」，全修到干净终点、一笔做完（一级 proposal What）。症状锚 **S2**（boot 真量有界，一级 design §六，共享锚由 04 定义）今天红。

## What

承一级 **D6.4**（读切片非全扫；否「保留全扫＋加索引」，仍 O(全史)）：

1. **切片扩携**：tasks 切片（runtime/task/checkpoint.go:40-89 `StatedTask`，已携 Spec/Status/Revision/Mutations/Obligations）扩携每任务的 **consumed / attempts / rejected** 三族收据——settle 幂等所需（缺之则重启把已消费通知重新欠账、再驱动第二次结算轮）。
2. **load 改读切片**：覆盖段零任务事实族前缀读，由切片入表；尾段按任务身份读（成本＝存活链＋尾）；五前缀无条件全扫退役为「无 checkpoint（Offset=0）」与「旧切片未携收据键」的兜底。
3. **时序后移**：`NewDurableCommit` 只构造空表；账户入表移到读最新 checkpoint 之后、消费该账户的切片（mailbox）恢复之前。
4. **单一 g(x)**：删 load 内独立派生循环（:317-341），覆盖段与尾段与运行期 `Commit` 共用同一记录应用路径；欠账恒等于 outbox − consumed − rejected，由同一组函数维护（:466/:508/:603-609）。

## Capabilities

- `checkpoint-state`（**MODIFIED**）：「任务终态有界恢复」——任务提交账户读切片非全扫、load 真量计数不随历史任务数增长、年轻链兜底仍一致、settle 幂等与尾段欠账不因时序后移丢失。

## 边界与依赖

- **依赖**：Stateful 接缝（runtime/state/checkpoint.go:36，签名冻结，一级 §五）；tasks 切片与其生产者（assembly/task_checkpoint.go:21-44）；最新 checkpoint 读取（assembly/checkpoint.go:963 `restoreNewestCheckpoint` / :780 `restoreCheckpoint`）；**02 的尾增量键集**（年轻链发现：任务事实是 KV 族而非事件链，`journalKV.Commit` 一帧携 Puts 键值，memory/ledger/journal_kv.go:101-118 ✅ 实测；事件索引只收 `<partition>:index:` 键，segment_store.go:301-336，故 `eachFactPastCut` 不足以发现年轻任务链）；**04 的真量探针**（`durableLoadCount` 消费既有 `LoadStats`，durable.go:194-215）。
- **被依赖方**：boot 路径全部消费者（run.go:617/690/693/704）；mailbox 切片（`owedOfWork` 读 durable 欠账，runtime/state/mailbox_checkpoint.go:89）；settle 泵（runtime/state/memory.go:323/383 读 attempts/rejections）；02/04 同簇合单；06 退位的 存活 权威（切片点名＝存活键集来源之一，一级 §五）。
- **接口面**：Stateful 三方法（StateName/SnapshotState/RestoreState）不变；`StatedTask` 追加三族字段（**不用 omitempty**，空集显式落 `[]`，使「旧切片无键」与「新切片无收据」可区分）；`DurableCommit` 增「按切片入表」与「按任务陈述三族收据」两个读面（第二使用方＝切片生产者，独立于该结构存在）；`FactLog` 四方法不变，**不新增端口**。
- **禁止事项**：
  - **SHALL NOT 删除/覆盖/改写已提交事实**（session-checkpoint-material/spec.md:47 红线）；有界化只改读法，不动链。
  - 不触外来未跟踪文件（runtime-architecture-convergence/、scripts/gates/check_event_semantics_symptoms.py、runtime_budgets.py、test_check_runtime_symptoms.py、skills/interview/agent-interview-helper/）；不触 config-surface-normalization 未提交变更（同 dev 分支，互不触碰）。
  - 不改写冻结归档（openspec/changes/archive/** 零 diff，只附勘误）。
  - 不另立第二派生实现或第二真相；不为单使用方抽象导出；不新增 model 级/任务级容量注册表。
  - **与 02/04 合单原子（W1 boot 路径簇）**，不并行派发（一级 D4）。
  - 本文档阶段**只规划不实施**；实施待用户审过一级 design 后另行编排。