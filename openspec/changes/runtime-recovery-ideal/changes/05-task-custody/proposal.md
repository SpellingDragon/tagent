> **历史输入（2026-11-20）**：本目录不再作为当前派发或自动规格同步输入；下文旧勾选、方案与限制按原时点保留，不授权后续实施。当前只按[编排入口](../../ORCHESTRATION.md)派发三个语义单元，全部旧要求由[承接表](../../CARRYOVER.md)逐项接收，不因转历史而取消。

# 01 任务托管收编（W1 boot 路径簇合单）

> **阶段状态（本轮重整）**：旧编号 1.1–1.4 与其凭据**保留为历史时点证据**（1.1–1.3 曾勾选、1.4 未建立），不因目录回到进行中而撤回，也不推导当前完成。受影响条目在 `tasks.md` 标「待复验」。本轮新增叶 **05.R1**（设计门 G2）与 **05.R2**（清减与恢复一致），二者只规划、不实施。
> **当前生效条款**：`specs/session-checkpoint-material/spec.md`（本轮新增，条款名对齐主规格）；旧 `specs/checkpoint-state/spec.md` 的 MODIFIED 块**降级为历史时点证据**（主 `openspec/specs/` 中无 `checkpoint-state` 能力，该块无法在主规格上归档生效，取代关系已在该文件头注明）。主规格转正仍归 10 域。

## Why

一级 design D1 的 P2（单一 g(x)）与 P4（有界恢复）在任务恢复路径上未兑现：

- **全史扫旁路**：`DurableCommit.load`（runtime/state/durable.go:258）无条件全扫五前缀 `tagent/task/v1/{spec,consume,attempt,reject,fact}:`（:259/:270/:282/:294/:305，前缀常量 :53-57），解码全部历史任务正文，并以 load 自己的循环重建 revisions/mutations/owed（:317-341）——与运行期 `Commit` 的入表路径（:600-609）是两套派生。
- **时序倒置**：`NewDurableCommit` 在 assembly/run.go:617 构造即 load，早于 `restoreCheckpointState`（run.go:690）。`restoreChains`（run.go:1073-1097）用 `CheckpointCoverage()` 只跳过覆盖链的 **fold**，读的仍是已全扫入内存的表（`Records` 读 `d.facts`，durable.go:749）——省 fold 不省 load。
- **违背条款**：checkpoint-state 的「任务终态有界恢复」条款——「boot SHALL 经 checkpoint 恢复任务态而非全史重读」「boot 任务恢复成本 SHALL 不随历史任务总数线性增长」。12.4b 残留栏书面自认「body 已由 durable load 全量读入，非本单可消」。
  - ⚠️ 实测：`openspec/specs/` 无 `checkpoint-state` 主 spec 目录（151 个能力中只有 `session-checkpoint-material`）；该条款权威文本现存于冻结归档 `openspec/changes/archive/2026-11-11-hot-update-and-purity-closure/changes/12-checkpoint-state/specs/checkpoint-state/spec.md` 的「任务终态有界恢复」条款。主 spec 转正归 05 域，本域 delta 以该 Requirement 名与文本为基准。
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
- **被依赖方**：boot 路径全部消费者（run.go:617/690/693/704）；mailbox 切片（`owedOfWork` 读 durable 欠账，runtime/state/mailbox_checkpoint.go:89）；settle 泵（runtime/state/memory.go:323/383 读 attempts/rejections）；02/04 同簇合单；06 退位的 存活 权威（切片点名＝存活键集来源之一，一级 §五）。**勘误（2026-11-16，退位机制整体删除）**：末项作废——退位不存在，切片点名不再是任何搬迁的存活输入；切片点名的其余消费者（恢复与封顶陈述）不变。
- **接口面**：Stateful 三方法（StateName/SnapshotState/RestoreState）不变；`StatedTask` 追加三族字段（**不用 omitempty**，空集显式落 `[]`，使「旧切片无键」与「新切片无收据」可区分）；`DurableCommit` 增「按切片入表」与「按任务陈述三族收据」两个读面（第二使用方＝切片生产者，独立于该结构存在）；`FactLog` 四方法不变，**不新增端口**。
- **禁止事项**：
  - **SHALL NOT 删除/覆盖/改写已提交事实**（`session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」）；有界化只改读法，不动链。
  - 不触外来未跟踪文件（runtime-architecture-convergence/、scripts/gates/check_event_semantics_symptoms.py、runtime_budgets.py、test_check_runtime_symptoms.py、skills/interview/agent-interview-helper/）；不触 config-surface-normalization 未提交变更（同 dev 分支，互不触碰）。
  - 不改写冻结归档（openspec/changes/archive/** 零 diff，只附勘误）。
  - 不另立第二派生实现或第二真相；不为单使用方抽象导出；不新增 model 级/任务级容量注册表。
  - **与 02/04 合单原子（W1 boot 路径簇）**，不并行派发（一级 D4）。
  - 本文档阶段**只规划不实施**；实施待用户审过一级 design 后另行编排。

## 本轮重整：托管已达成，清减与一致未达成

**已核事实（读码，本轮）**：托管面（1.1/1.2/1.3）在树内；但**「当前有效态按有效工作规模计费」未达成**，且有三处只在他域/后续才能回答的边界：

- `StatedTask` 逐任务陈述 `Consumed/Attempts/Rejected`（runtime/task/checkpoint.go:82-99），生产者 `StateSettleAccount` 对每个被点名任务调一次 `ReceiptsOf`（runtime/task/checkpoint.go:160-181）；而 `ReceiptsOf`（runtime/state/durable.go:1089-1155）对**每个任务**遍历 `consumed/attempts/rejected` 三张**全库 map**。故一次切面的收据陈述成本≈O(任务数 × 全库收据数)，已结历史越多越贵——切面本身成了随历史增长的消费者。
- 任务账是 **registry 全局账**：`tasksStateSlice` 只实现三方法，无 `SnapshotSessionState`（assembly/task_checkpoint.go:32-50）。会话切面不陈述任务账，这是**有意设计**（一级 D6.4 只要求「读切片非全扫」），本轮不推翻；任何把任务账塞进会话切面的方案须先证伪「全局账」这一身份源。
- 收据的删除边界今天不存在：`PruneTerminal` 清扫任务记录后，其收据不再被陈述，而「旧键重试/跨进程重投」的判重身份随之消失。是否安全取决于事实提交语义（11.R2）与判重身份保留判据（G2），**不能靠 TTL 删除未完义务**，也不能为省成本删掉 `attempts/rejected`（删三族即破幂等，见旧 design 风险节）。

**新增叶（叶号即依赖序）**：

- **05.R1 设计门 G2（任务域）**：证明每族状态的处理水位、未完集合、有效历史状态与按身份查询入口；回答四个具体问题——①任务账的有效集是「registry 现存任务」还是别的；②收据的判重身份要保留到什么时候（按身份重放不再可达该身份为止，还是任务离开账为止）；③乱序完成（后序先行、前序留洞）会不会让「最长连续已消费前缀」与欠账恒等式分叉；④旧键/跨进程重试与 11 的事实身份等价如何衔接。产出：判据表（与 07.R1、12.R1 **共用一张**）+ 红着的锚。
- **05.R2 清减与恢复一致实现（依 G2 + 11 提交结果）**：按 G2 结论落地，**重用现有按 task 索引**（三张收据 map 的键已含 task 身份，候选做法是补一份按任务的键索引，使 `ReceiptsOf` 与切面陈述成本＝O(该任务收据)，**不删历史、不改恒等式**）；清减判据写成「按身份判重仍可答」，MUST NOT 用 TTL 删除未完义务；恢复侧（`Seat`/`applyRecordLocked`/尾段读）与运行期 `Commit` 继续同一记录应用路径。

**本轮不做的**（显式登记，避免被读成遗漏）：不把任务账改成会话切面；不清减压缩账（03）；不改治理账（07）；不重写 `LoadStats` 口径（04/09 的真量锚沿用）；不承诺 `ReceiptsOf` 变成 O(1)。

### 边界与依赖（本轮新增叶）

- **依赖**：G2 判据表（05.R1 与 07.R1 协同产出，12.R1 消费）；11.R2 的事实提交结果（收据/事实的「已存在 vs 新增 vs 冲突」语义，决定判重身份能否随清减消失）；`Stateful` 接缝（runtime/state/checkpoint.go:36-45，签名冻结）；现有按 task 索引面（`durable.facts/specs/consumed/attempts/rejected` 与 `unowedLocked`，runtime/state/durable.go:1158 起）；`LoadStats`（:235）作为真量口径（04 定义）。
- **被依赖方**：12.R2（「已提交未消费」边界必须以任务账的`Seat`/尾段读行为为样本）；08 退位的存活权威（切片点名是存活键集来源之一）；09 的 S2 真量（`durableLoadCount`）；F3 回滚证据（清减回退后症状必红）。**勘误（2026-11-16，退位机制整体删除）**：第二项作废——08 域不再有退位，切片点名不服务任何搬迁判据；09 的 S2 真量与本域清减判据的依赖不变。
- **接口面（候选、未冻结）**：`StatedTask` 三族的**显式空集语义不变**（不用 `omitempty`）；可能在 `RegistryCheckpoint` 追加一个「收据判重保留水位」字段（候选名未定），使「未陈述」与「陈述空」仍可区分；`SettleAccount` 接口（runtime/task/checkpoint.go:143-146）签名不变；不新增 `FactLog` 端口。
- **禁止事项**：不改写已提交事实；不以 TTL 删除未完义务；不为单使用方抽象导出；不另立第二派生实现；不触 `openspec/specs/**`、`scripts/gates/**`、`golden/**`、外来未跟踪文件与 `config-surface-normalization` 未提交变更；不改写冻结归档；本轮不提交/推送/归档。