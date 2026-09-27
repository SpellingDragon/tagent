# 任务：注意力三预算制（attention-budget-architecture）

> **验收基准（锚定远端实测 + 真实轨迹回放）**：真实 trajectory 回放（batch 123，159 条 settle）disposition 命中 ≥98%（156/159）、单次 act 可回收 ~481K chars（→~14K 卡行）；稳态水位目标**区间化**（见 design Risks）：审计 L2 收敛生效 → 75-80K；未生效 → 锯齿态（act 间积累 + act 清空，均值随 spawn 频率）；已外显 settle 驻留 ≈1 turn；稳定前缀区字节稳定；冻结豁免测试先行；审计迟滞（<20% 解除）与动作记录可 recall。
> **执行纪律**：每组自带「改什么 / 禁止 / 判据」三段；开工前重读 design.md「被推翻论断清单」与「实现红线（anti-drift）」。**零新旋钮为宿主明令**（R3）——diff 中出现新 YAML 键/新配置常量即跑偏。**归档阻塞（R8）：4.3 远端回执未收到前不得归档**（前序大变更 34/34 归档后效果未验的结构病，见修订记录）。
>
> **修订记录（2026-09-27 第二版，整体 review 后）**：review 以真实轨迹回放升级为实证（156/159 命中、97.2% 降幅）并回溯了前序大变更失效的五因（合成形态验收/因果断裂/DoD 无效果门禁/观测窗节奏错配/度量债不回灌）。本版吸收整改：①新增组 5「效果固化」（轨迹回放固化为 Go 回归 + 两个缺口测试）；②**4.3 升格为归档阻塞项**并补主动触发/键形抽验/回执超时追踪；③稳态预期区间化；④审计迟滞语义回写 spec。首版实施中的三处自纠（0 值豁免/L2 语义/metadata 类型）已修复并有测试。

## 0. 前置与基线

**改什么**：无产品代码。**禁止**：不在本变更内做存量清债（运维动作另行的记录）。
- [x] 0.1 通读 design.md 红线章与被推翻论断清单；确认三分歧裁决（A+TTL / 2a 自动冻结+豁免 / N=keepRecent 同源）已内化
- [x] 0.2 基线快照：`go test ./agent/... ./memory/ -count=1` 全绿记录（deep-review-fixes 应已合入或独立在途，两者 diff 不交叉）
- [x] 0.3 对照设计确认消费状态三判定的推导材料齐全（settle 事件→回收 turn output→outputCh 投递记录三者在事实链/溢出落盘中的可得性走查，形成书面清单）

## 1. 遥测通道核心（第一结构代：L1+L2）

**改什么**：投影通道分区（event-sourced-projection delta）、settle 通道身份与卡片、消费分级降级、compaction 豁免。**禁止**：不动 SmartCompressor 定级表与骨架红线；不新增任何配置项（N 引用 keepRecent 解析值）；不动 600 cap 与发送侧；降级只走投影 Replace 族。

- [x] 1.1 投影通道分区：SessionProjection 增加遥测分区与 ref 来源标记（recalled/settled/对话），装配次序按 spec（system+反思+对话+遥测卡片+看板+新事件）；assembleRequest 唯一装配源不变
- [x] 1.2 settle 通道身份：task_settled 不再以 external_input 时间线成员进常驻投影——回收 turn 装配注入 ≤300 chars 卡片（复用 settleFoldRowMaxChars 票据行语义），failed 卡片带 ★
- [x] 1.3 消费状态推导器：从事实链+outputCh 投递记录导出三态（已外显/内部性/未消费），纯函数、零 LLM；含跨重启重建路径（WAL 回放序）
- [x] 1.4 分级降级挂点：processTurn 收尾纪律点（endTurn 族）按消费状态执行投影降级——已外显即时票据化、内部性保一行（N=keepRecent 现读）、未消费不降级；与 settle_fold 幂等共存测试
- [x] 1.5 compaction 豁免：L3 预算升级跳过含未消费遥测的段；全部候选被豁免时如实报告
- [x] 1.6 召回闭环：recall 暂存 ref 入投影+来源标记，消费后按遥测规则退出
- [x] 1.7 判据：新增回归族全绿——①已外显 settle 下轮装配不含全文 ②内部性 N 轮（keepRecent 热更跟随）③未消费经重启重建后仍完整 ④豁免段不被 L3 ⑤稳定前缀字节稳定（对话+反思区在遥测降级前后逐字节一致）⑥骨架红线既有测试零回归

## 2. 行为审计（第二结构代：L4）

**改什么**：审计器、占比指标、L1/L2/L3 阶梯、spawnGate 审计源、豁免白名单。**禁止**：豁免不可由模型运行时标注（构造时声明）；不引入人工审批路径（宿主裁决 2a）。

- [x] 2.1 豁免白名单先行（R4 红线：先建保护再建冻结）：TaskSpec 声明性保护类别（retention guard / mem_spill 重放 / 重试修复），冻结期放行测试先行落绿
- [x] 2.2 占比指标与事实链记录：滚动窗口自管/环境事件占比，指标与样本计数入事实链
- [x] 2.3 分级动作：L1 告警（未消费级完整投递）→ L2 收敛自管 spawn 频率 → L3 冻结
- [x] 2.4 spawnGate 审计源：与 disk block 同构接入，Blocked reason 区分；占比回落自动解除；双源并存语义测试
- [x] 2.5 判据：①空转注入测试（构造自管风暴）触发 L1→L2→L3 全链 ②冻结期保护性任务可 spawn ③解除后恢复 ④动作记录可 recall

## 3. 看板职责与收尾（L3 层+L5 检核）

**改什么**：看板成为任务状态唯一呈现的文档与提示词侧对接（工具描述/召回指引提及票据卡片用法）。**禁止**：不改看板注入位置与渲染红线。

- [x] 3.1 看板/卡片/票据的职责分工写入 wiki（agent-architecture §2.10 与 event-flow）与提示词工具描述（TOOLS.md 侧由远端宿主同步）
- [x] 3.2 轨迹统计口径补入冥想 digest（时序模式分析归反思层，非新增常驻）

## 4. 端到端验证与收口

- [x] 4.1 端到端场景回归（按设计文档场景）：三天剧本关键断言——心跳 172 条形态下水位稳态、recall 往返后暂存退出、审计全链、重启重建一致性
- [x] 4.2 与既有测试族全量共存：`go test ./...` 全绿（agent/memory/rl/根包）
- [ ] **4.3 【BLOCKER·归档阻塞】实测验收（远端配合）**：换装含本变更的 dev 后——
  - **主动触发**：热调压缩阈值（五个数值热参内）至当前水位之下，**立即触发首个 compaction act**（不等自然触线——观测窗节奏对齐，教训 ④）；采集 before→after 与 settle_fold 卡计数
  - **24h 基线对照**：水位曲线（区间判定见 design）、回收率、注意力密度、审计指标可见性
  - **键形抽验**：远端 FullEvent.Metadata 的 `meta_trigger_source`/`lineage_absent` 存在率与取值分布 ≥20 条（externalization 判定的真实数据前提）
  - **存量清债核查**：17 条 22K 历史债消化进度（回执滞留 key）
  - **回执超时追踪（度量债回灌）**：发出后 48h 未回执 → 升级为对宿主的显式提醒任务，不得静默沉淀（教训 ⑤）
- [ ] 4.4 提交纪律：结构代一（1.x）/结构代二（2.x）/收尾（3.x-5.x）各一组 commit，message 前缀 `feat(telemetry-channel|self-telemetry-audit)`；**归档顺序：4.3 回执落袋 → check-openspec → openspec archive（用户确认后执行）**

## 5. 效果固化（review 整改·第二版新增）

**改什么**：把 review 阶段的真实轨迹回放从 Python 脚本固化为 Go 回归；补两个 spec 语义缺口测试。**禁止**：fixture 手造 settle 文案（必须从真实 trajectory 提取——教训 ①）；不引入新配置。

- [x] 5.1 **轨迹回放回归**：从 `traj-30m-raw.jsonl`（batch 123）提取 159 条真实 settle refs（summary 原文、位置序、长度分布含 17 条 22K 债）生成本地 fixture；Go 测试断言 disposition 分布（Demote≥156/Active≤2/Internal≤2）与 fold 后 chars 降幅 ≥97%
- [x] 5.2 **召回暂存不常驻**（spec：召回闭环的钉子）：构造 recall 产生的 tool 消息 + 骨架压缩路径，断言其随 L1 丢弃退出、不因任何骨架保全常驻
- [x] 5.3 **重启重建一致性**：投影 WAL 重建后 disposition 重算与停机前等价（已消费→降级形态、未消费→完整），集成级断言
- [x] 5.4 spec 回写验证：self-telemetry-audit 迟滞条款（<20% 解除）与实现一致（实现已含迟滞，spec 已在第二版回写——此项跑 openspec validate 确认）
