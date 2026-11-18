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
  - 不删/改已提交事实（`session-checkpoint-material` 的 Requirement「派生态有界且不删除已提交事实」）。
  - 本文档阶段**只规划不实施**。

## 本轮重整：R1–R3（2026-11-15 追加）

> 原 Why/What/Capabilities/边界原文保留不动：4.1–4.4 的四真量定义仍有效，原 S1/S2/S3 三条症状锚仍是验收矩阵第一行。本节只补本域此前未规划的三块工作，不取代原目标。

### 原计划的可复跑状态（复跑凭据，非汇报）

| 行 | 复跑命令 | 结果 |
|---|---|---|
| 4.1＋4.2 | `go test ./assembly -run '^(TestBootQuantityProbesReadTheRealBoot|TestBootFixtureGrowsTasksAcrossGenerations)$' -count=1` | exit 0（3.605s） |
| 4.3 S2 锚 | `go test ./assembly -run '^TestSymptomAnchorBootCostFlatAcrossGenerations$' -count=1` | exit 0（15.820s）——**已非原行所记「仍红」** |
| 4.4 负锚 | `grep -rn "TestSymptomAnchorBootFullHistoryReadRegression" --include=*.go .` | 零命中，锚未建立 |
| 探针出口 | `grep -n "JournalBytesOpen\|IndexEntries\|LoadStats\|ColdReadCost" memory/ledger/*.go runtime/state/durable.go` | 四量出口在位 |

结论：4.1/4.2 的交付仍成立；4.3 的正锚已绿（原行记「仍红」为历史时点，须校准）；4.4 未建立且其原设计被自证为不可判定（同一 `OpenTier` 下 boot 与全史对照三量恒等）。

### R1 原始 CI 失败清单固定

`/opsx:apply` 前必须先把 HEAD `9547faca` 的**原始**失败面固定成可复跑清单：sha、作业名、实际命令与选择器、退出码、具名失败测试与首条失败原文。已固定的清单与复跑命令见 [design.md](design.md) §八。约束：

- 只读取证（`gh run view` / `gh run download` / 本地复跑），**不触发 CI**（无写权限，`gh run rerun`、`gh workflow run` 一律不做）。
- 不得用「零键计数」「退出码非零」替代首个失败原因；不得把 256 窗口的超时与 32768 窗口的断言红并成一类。
- 「两 products」与「Scenario03 呈现未达」两处尚无根因判决，按缺陷列账，**不称 flaky**。

### R2 分阶段成本校准

原 4.1 只定义 boot 四量；本域后续的实际问题是**分阶段成本不可复算**：开档、提交、点读、退位、目录维护、重绑定、总耗时七段没有各自的区间与单位，候选实现里 `ColdReadCost` 内外计时重复又漏 cut 写，且锚把 S2 的时间面从「一次 assemble」改写成「整代」。R2 的工作是**纠正测量口径**，不是把阈值调到绿。**勘误（2026-11-16，退位机制整体删除）**：段数由七段降为六段（退位段及其 `ColdReadCost` 观测随机制删除）；「不重复计量、可复算、启动口径＝一次装配」三条判据不变。口径要求：

- 七段各自给区间、单位与是否含冷 cache；区间互不重叠，Σ 段 ≤ 总耗时。（**勘误**：退位段删除，其余六段各自给区间与单位。）
- S2 的时间面恢复为**一次 assemble**（原设计即宽松上界，不是缺时间）；整代耗时只作观测量与阶段总量，**不得替代 S2 判定**，不得把 boot 重定义为整代。
- 2x 无冻结来源（`golden/runtime/dod_predicates.json` P4 的 `bound.factor` 与候选变异记录自述的 8x 相互矛盾）；阈值由同条件校准与回归反例确定。
- 主干与分支同机、同镜像、同参数、同输入与同代序；实际工作量守恒；冷/热 cache 分记；bootstrap 测量必须包含待测 boot 本身，预热只可剥离编译等非目标成本。

细则见 [design.md](design.md) §九。

### R3 最终候选完整矩阵

最终候选须以**与 HEAD 同条件**的完整 CI 矩阵放行：全部作业（含 `event-integrity-long` 六分片与 `event-integrity-required`）绑定源树指纹、命令、退出码与合同摘要，逐项与 `9547faca` 的同一矩阵对照。跨指纹、跨镜像或跨输入的对照判无效；历史异机样本不得用于宣称百分比回归。CI 触发属外部写权限，执行前须获明示授权；本阶段只规划。

### 边界（本轮）

- 本轮只写 09 的四件套规划文档；不改生产代码、测试、golden、CI 配置、分支保护、用户规则与 skill。
- 不把 ci 失败清单当成本域的实现：呈现侧修复归 04，任务/状态修复归 05/07/12，本域只管「量什么、怎么复算、怎么放行」。
- **禁读范围不扩大**：用户真实 `examples/wechat-bot/.wechat-config/data` 与 `--source` 禁止访问；该限制**不**等于禁止在独立临时根跑 wechat-bot 合成验收——`event-integrity-long / short` 分片正是在合成临时根上跑（产物里可见 `/tmp/TestEventIntegrityLongRun…`），本域的分阶段成本与矩阵须照此取样，不得以「禁读」为由拒跑。