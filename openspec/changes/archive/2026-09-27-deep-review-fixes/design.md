# 设计：深度评审修复（deep-review-fixes）

## Context

三轮评审（架构走查 → 分区精读 → 实据取证）确认 7 项缺陷 + 1 项可观测性备注，分布在四个既有 capability 的**故障路径**上。共同根因有二：

1. **对称性修复未扩散完全**——同一 gap 的修复只落在一个调用点（received 口径只改了 finishDurableBatch 三处、sealed 降级只在 StoreEvent、voidSpawn 只覆盖 Settled）；
2. **fail-open 的错误吞没**——mergeEvents/checkTTL 对 KVScan `continue`，违背同仓 `locateOrphanCommit`/`recoverWindowSeqLocked` 已确立的 fail-loud 纪律（"an incomplete scan cannot prove absence"）。

取证资产已就位：`memory/zz_review_evidence_test.go`（P1/P3-3）、`agent/zz_review_evidence_test.go`（P2-3 两形态 + inline 对照）——当前红，是本变更的验收基准。

## Goals / Non-Goals

**Goals:**

- 消除 P1 静默数据丢失路径（生产 rustviking 后端可触发，warm LRU 遮蔽使其潜伏到重启/淘汰后暴露）
- 恢复 durable 协议边角的对称性（P2-1/P2-2/P2-3），使子调用环与 durable inbox 在异常路径上保持活性与至少一次语义
- 取证测试全部转绿并保留为永久回归

**Non-Goals:**

- 不重构 compaction/inbox/执行代的结构（主干经三轮验证无恙）
- 不引入 compaction 重试策略配置（沿用 scheduler 周期重试的既有机制）
- 不处理 localfile 后端的耐久性（已有裁决：生产耐久推迟 rustviking）

## Decisions

### 决策 1：P1 mergeEvents 读失败 = fail-loud 中止本轮（而非跳过坏窗继续）

**选择**：`KVScan` 失败返回 `fmt.Errorf("merge scan failed pid=%d window=%d: %w", ...)`，让 `CompactL1ToL2/L2ToL3` 的既有错误路径中止（mutationMu 释放、源段不动，scheduler 下轮重试）。

**理由**：compaction 天然幂等且由周期 scheduler 驱动——中止本轮零成本、源段完好；跳过坏窗继续则重蹈「merge 与 delete 的可见性不一致」覆辙。与 `recoverWindowSeqLocked`（M3）的先例一致。

**替代方案（否决）**：跳过失败窗口、merge 其余、不删除失败窗口——仍需在 delete 侧区分「读失败」与「读成功」，且 L2 边界将长期缺失该窗口内容，恢复语义不完整。

### 决策 2：P2-2 undecodable slot = 整封隔离（而非按 slot 隔离）

**选择**：`decodeSourceEvent` 失败时对整个 envelope 调 `QuarantineEnvelope(path, reason)`，放弃本 envelope 已收集事件（尚未提交事实，安全），`claimDurable` 返回已收集的其余 envelope。

**理由**：envelope 是投递原子单元（两阶段完成协议以 envelope 为 completion 边界）；按 slot 隔离会引入「部分消费的 envelope」状态，completion 的逐槽处置（slotProcessed/slotSkipped）没有「永久坏 slot」的第四种 disposition，扩展它比隔离整封昂贵得多。与叶子层 unreadable envelope 的既有处置完全同构。

**替代方案（否决）**：给 completion 增加 `slotCorrupted` disposition——协议面扩张，且「半坏随 Ack 销毁」的窗口在修复期间仍然敞开。

### 决策 3：P2-1 重排口径 = 冻结核（received），单行统一

`releaseBatchClaims(received)`——`releaseBatchClaims` 按 path 去重、volatile 事件无 claim，扩到全集无副作用；这是 §4.1 注释已声明的意图（"Receipt/ack provenance always runs over the full received set"）在 release 侧的补齐，无设计分歧。

### 决策 4：P2-3 voidSpawn 条件扩展（booking 保持前置）

**选择**：`if res.Settled || res.Deduped || res.Blocked != ""` 时 `voidSpawn`。booking 保持在 `inner.Spawn` **之前**（原注释明确的防 race 设计：期望先于任务可能 settle 而登记），不可后移；三个「本次调用不再拥有 settle 期望」的形态在 `inner.Spawn` 返回时即可判定。

**理由**：Deduped 时现有任务 settle 消耗的是**原发起调用**的 booking；Blocked 时无任务无 settle——两者对本次 invID 都是不配对的 +1。

**替代方案（否决）**：noteSpawn 移到 Spawn 之后——破坏「booking 先于可能极快的 settle」次序不变量。

### 决策 5：P3-3 sealed 降级提取共用函数

`demoteSealedWindowLocked(pid, windowTS)`（或等价私有 helper）供 StoreEvent 与 ReplayEvent 的 `seqCounter==0` 分支共用——两处行为逐字节一致，杜绝下一次再发散。

### 决策 6：测试策略 = 取证转绿 + 三条新集成回归

- 取证测试（5 例）转绿后改命保留（去 `Review` 前缀，并入各包规范命名）；
- 新增：① 混批×transient 熬尽 → 让位 meditation 的 claim 回 pending（P2-1）；② undecodable 全坏/半坏两分支 → 隔离可见、无 Ack（P2-2）；③ ReplayEvent→sealed 窗口降级后 bounds 覆盖新事实（P3-3，与取证互补的正面形态）。

## 实现红线（anti-drift）

长执行会话中实现漂移的主要形态与本变更的对应防线——**apply 阶段每个任务组开工前重读本章**：

| # | 漂移形态 | 防线 |
|---|---------|------|
| R1 | **为绿改测**：修不动实现，转而放宽/反转取证测试断言 | 取证测试断言方向**冻结**（只许改名与注释口吻，tasks 6.1 的机械映射表是唯一允许的改动）；实现必须满足测试，不是相反 |
| R2 | **顺手重构**：借修复之机重命名/格式化/抽象无关键代码 | 每个任务组的「禁止」段划定最小 diff 边界；commit 前 `git diff --stat` 复核只触及「改什么」列出的文件与位置 |
| R3 | **协议面扩张**：给 completion 加第四种 disposition、给 compaction 加重试配置、改 SpawnResult 结构 | 设计决策 1/2/4 已否决的替代方案不得复活；无新配置项、无新 API、无新序列化字段 |
| R4 | **正常路径回归**：修复波及健康路径行为 | 对照测试（inline settle）+ 既有测试族是护栏；0.2 基线快照后**任何新增红 = 立即停下回溯本组 diff**，不得带红推进 |
| R5 | **语义偷换**：把「中止本轮」实现成「跳过坏窗继续」、把「整封隔离」实现成「跳过坏 slot」、把「降级」实现成「无条件重写 meta」 | 每项修复在 tasks 中写明精确的 before→after；偏离设计决策文字即跑偏，即使测试碰巧全绿 |
| R6 | **验证造假**：新回归测试只执行不断言、用真等 600s/真实外部依赖 | 新测试必须有可失败的断据（Outstanding 状态/隔离区可见/关闭时限）；2.3 明令复用快进基建禁止真等超时 |
| R7 | **混线提交**：多项修复挤一个 commit、夹带无关文件 | 每任务组一 commit（tasks 6.5）；单项独立可 revert/cherry-pick 是本变更的回滚策略 |
| R8 | **遇阻绕行**：现状与设计冲突时自行发明第三条路 | 停下、在 change 目录记录冲突证据、上报裁决——禁止忠实执行一个与现状矛盾的设计，也禁止静默改道 |

## Risks / Trade-offs

- **[P1 中止使 compaction 在持续 I/O 故障下停滞]** → scheduler 周期重试 + `CompactL1ToL2` 失败已有 `log.Errorf`；持续故障本就是 disk 依赖退化状态机（DegradationManager）的管辖面。
- **[P2-2 整封隔离丢弃同封好 slot]** → 隔离区保留原始字节（运维可检查/回灌）；相比「半坏随 Ack 静默销毁」，隔离是可观测、可逆的选择；触发前提本身是"anomaly"级（enqueue 校验通过但完整反序列化失败）。
- **[P2-3 行为变化：dedup 后环静默退出提前]** → 这正是修复目标（对照测试锁定 inline 形态不变）；越窗真 settle 仍持有正确 booking，无提前退出风险。
- **[取证测试改命]** → 机械重命名 + 全量 `go test ./memory/ ./agent/ ./rl/` 验证。

## Migration Plan

单分支落地，无部署迁移：修复 → 取证测试转绿 → 改命保留 → 全量测试 → 按归档纪律 `scripts/check-openspec.sh` → `openspec archive deep-review-fixes`。回滚 = revert 单 commit（每项修复彼此独立，可拆分 cherry-pick）。

## Open Questions

（无——八项修复的方案在评审中均已与代码现状逐点对齐，取证测试即验收基准。）
