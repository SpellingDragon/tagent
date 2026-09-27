# 注意力三预算制：遥测通道分离 · 消费分级降级 · 行为审计（attention-budget-architecture）

## Why

远端生产实例（wechat-bot，eeae214）trajectory 实证：主循环上下文 338K↔409.6K 锯齿循环（单次回收仅 5.6%），其中 159 条 `[task settled]` 遥测占 user 内容 65%（483K chars），**注意力密度仅 0.09%**（一次回收 turn 的新增信息占 prompt 的 0.09%）。逐层实证后确认：压缩器、发送侧体积治理（`settleInlineCapChars=600`+spill）、settle 专属折叠路径（`foldSettleRuns`，设计意图"regardless of segment age"）全部存在且健康——**真正的根因是遥测的退出机制以错误的单位触发**：退出依赖「相邻连跑 ≥2」与「预算深度（L3 达标即停）」，而遥测的语义单位是**消费状态**（谁处理过、是否外显）。心跳式 Sx1 交错形态（实测 158 段中 150 段为单条，回收 turn 的 agent_output 恰为界段隔断）使专属折叠永不激活；浅锯齿动力学使 L3 永不深触骨架层。叠加后果：22K 历史债（17 条，旧 cap 时代构造）与增量中短通知无限期滞留，**存量与增量均无确定性退出时点**。

本变更将上下文组织从单一时间线改为**三预算制**：对话流按窗口保真（既有）、遥测流按消费状态降级（新）、反思流按周期重写（既有补强）——使常驻上下文从「时间的函数」变为「当前活跃度的函数」。

## What Changes

- **L1 消费分级降级（核心）**：settle 通知的退出单位从「相邻连跑」改为「消费状态」——已消费+已外显 → turn 收尾即时降级为票据行；已消费+内部性 → 保留一行摘要 N 轮后降级（**N=keepRecent 值同源引用，零新旋钮**）；未消费 → 保持完整（不可丢；积压有界性由任务层既有 TTL reaper + batch-retire N→1 汇总在源头保证，上下文层不设上限）。消费状态为客观可推导事实（settle→回收 turn→outputCh 投递记录），零 LLM 决策。
- **L2 通道身份显式化**：task_settled 不再以 external_input 身份进对话时间线——以「通知卡片」形态（≤300 chars，复用 settle_fold 票据行语义）参与回收 turn 装配；failed 极性卡片行打 ★（复用既有 ★ 渲染，内容级约定，零新代码路径）。
- **L3 看板为遥测唯一状态呈现**：任务状态查询不再依赖时间线；通知只承载「事件到达」。
- **L4 行为审计（自激检测）**：滚动窗口内自管遥测/环境事件占比超阈值触发分级动作——L1 告警事件 → L2 收敛自管 spawn 频率 → **L3 冻结自管 spawn：与 disk `degradation_disk_block_spawn` 完全同构（同一 spawnGate，新增触发源），自动执行、无人工审批**（宿主裁决 2026-09-27），附保护性任务豁免白名单（资源租约、mem_spill 重放、重试修复类不冻结）。
- **L5 召回闭环**：recall 暂存入投影管理，消费完同降级（递归闭环，防召回洪水重新膨胀）。
- **伴生不变量**：compaction 对**未消费遥测**的保护性豁免——L3 归档不得吞未消费 settle（防膨胀峰值触线时误归档）。
- 连跑折叠（foldSettleRuns）**保留**——风暴形态仍可能到来，与消费降级幂等共存（settle_fold 的 Synthetic 幂等设计已防重折）。
- 存量清债（22K×17 历史遗留）为前置运维动作（一次深度 L3 或调小 `recentFullCount`），不在本变更代码范围。

## Capabilities

### New Capabilities

- `telemetry-channel`：遥测通道的完整契约——通道身份（task_settled 不进对话时间线）、通知卡片形态与上限、消费状态的三级判定与降级时点、N 轮短期提醒（keepRecent 同源）、failed ★ 约定、compaction 豁免未消费、召回暂存生命周期、跨重启消费状态重建。
- `self-telemetry-audit`：行为审计契约——自管/环境事件占比指标、L1/L2/L3 动作阶梯、L3 与 disk block spawn 的同构接入（spawnGate 新触发源）、保护性任务豁免白名单、审计事件自身的遥测身份（L1 告警按未消费级完整投递）。

### Modified Capabilities

- `async-task-execution`：spawnGate 判定链增加审计触发源（Blocked 语义与 disk 退化一致——「已启动的工作不被冻结，新 spawn 被拒」）。
- `task-skeleton-compression`：compaction 的 L3 预算升级增加「未消费遥测豁免」约束。
- `event-sourced-projection`：投影增加通道分区（对话/遥测/反思），assemble 统一装配；「Compact 只改投影」红线不变，降级属投影操作。

## Impact

- **代码落点**：`agent/event_bus.go`（settle 事件通道身份）、`agent/compress/`（投影通道分区、消费降级、豁免）、`agent/event_loop.go`（turn 收尾降级挂点）、`agent/task/task_manager.go`（spawnGate 审计源）、`agent/agent.go`（审计器装配）、`wiring.go`/`build_agent.go`（通道接线）。
- **兼容性**：骨架红线（system 恒单条/摘要与卡片归 user/assistant 恒真实）全部保持；durable 四态与至少一次语义**强化**（未消费=新的不可丢级）；事实链/WAL/TTL 遗忘曲线不动；600 cap 发送侧不动；SmartCompressor 定级表不动（对话通道原样）。结构变更走执行代热更（drain-free），不兼容旧投影形态的部分在候选事务内完成迁移。
- **验收基线（实测对照）**：稳态水位 338K 锯齿 → ~75-80K（与运行时长解耦）；已外显 settle 驻留 ≈1 turn；存量 350K 历史债清零；空转可观测（占比指标+分级动作记录入事实链）。
- **依赖**：无新外部依赖；复用 settle_fold/★/spawnGate/output_spilled 既有机制，零新旋钮（宿主裁决：N 严格跟随 keepRecent，不增加配置项）。
