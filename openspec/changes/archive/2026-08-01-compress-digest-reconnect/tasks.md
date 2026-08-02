# compress-digest-reconnect — 实现任务

## 1. 触发器多维化（P0）

- [ ] 1.1 `ContextCompressor.Compress`：在现有 ref 遍历循环顺带统计 `agent_output` 段边界数（完整任务回合数），触发条件从 `usedTokens > threshold` 扩展为 `usedTokens > threshold || completeTurns > keepRecent`（零额外扫描成本）
- [ ] 1.2 回归测试：5 个完整回合（keepRecent=2）+ under token → SmartCompressor 被调用（fail-before 验证此前不被调用）；1 个回合 + under token → 不调用；over threshold → 仍触发

## 2. LLM 摘要接线对齐

> 机制核实：骨架管线纯工程零 LLM（L3=整段离场→滚动摘要卡片）；其 LLM 文摘是 `condenseCardLines`（卡片超限浓缩旧卡）。LLM 段摘要+归档缓存是 `compressLegacy` 独有，本 change 不动。

- [x] 2.1 核实骨架管线文摘=condenseCardLines（`curateCards` 内，卡片超 cardMaxChars 时调 summaryModel 浓缩旧卡、保留新卡）；触发器修复后随压缩真实运行
- [x] 2.2 现有测试已覆盖：`TestCurateCards_MultiLineCondensation`（带模型浓缩+票据保留）、`TestCurateCards_SinkWithoutModel`（无模型沉底）；legacy L3 归档缓存素材律 `TestArchiveCache_SameSegmentNotResummarized`
- [x] 2.3 condenseCards 浓缩旧卡且保留新卡票据（已有逻辑与测试，核实通过）

## 3. memory_turn 链召回工具

- [x] 3.1 新增 `memory_turn` plain tool：沿 `GetParent` 回走到 `external_input`（含）为止，返回该区间全部事件（含被丢弃 tool 步骤），按时间排序；注册到工具注册表与 RecallAgent 子工具集
- [x] 3.2 测试：给定 agent_output key → 返回整轮事件且在 external_input 处停止；丢弃的 thinking_plan/action_command 在返回中（`TestMemoryTurn_ReconstructsWholeTurn` / `TestMemoryTurn_StopsAtExternalInput`）
- [x] 3.3 `buildRetainedRefs` 压缩时统计每轮丢弃 tool 步数，agent_output 卡片追加“含 N 步工具调用，可用 memory_turn 追溯”提示（external_input 重置计数）
- [x] 3.4 测试：含 tool 步骤的轮被 L3 丢弃后卡片带追溯提示（`TestContextCompressor_CardCarriesMemoryTurnHint`）

## 4. 死代码清理（保留 compressLegacy）

- [x] 4.1 核实：本次改动未引入死代码（新增函数均被引用）；`compressLegacy` 为 `skeleton_segmentation:false` 有意回退路径，保留；legacy 独有的 LLM 段摘要/归档缓存随之保留
- [x] 4.2 （可选增强）纯工具调用 thinking_plan 摘要——评估后**不做**：触发器修复后 tool 事件在 L1 即被丢弃、且 thinking_plan 不生成卡片，空摘要占位符仅为短暂过渡态（票据保留），增强收益低

## 5. 端到端与收尾

- [x] 5.1 端到端行为由分片测试组合覆盖：压缩折叠（`TestContextCompressor_TriggersOnExcessTurns`）+ 滚动摘要含边界卡片与追溯提示（`TestContextCompressor_CardCarriesMemoryTurnHint`）+ memory_turn 从 store 召回被丢弃 tool 事件（`TestMemoryTurn_*`）；store 保留被压缩事件为架构保证（压缩只移出投影不删存储）
- [x] 5.2 验证：`go build ./...`、`go vet ./...`、`gofmt` 干净；本 change 相关包（agent/compress、tool/recall、memory）测试全绿。注：`TestPlanPromptContract` 失败为**本 change 之外**的既有漂移（example plan_agent.md 工作区未提交修改与主树不一致），不属本 change
- [x] 5.3 `openspec validate compress-digest-reconnect --strict` 通过

## 6. Code Review 修复（零基审查后补充）

> 来源：实现完成后的零基审查（1 Blocker / 2 Major / 3 Minor-Nit）。

- [x] 6.1 **Blocker：memory_turn 未接入 agent 工具表**——注册进工厂表但 config 驱动的 yaml 无此工具（顶层 tagent 与 recall 子 agent 都拿不到，卡片提示会诱导调用不存在的工具）。已接入 `tagent.yaml` 顶层 tagent 工具表与 recall 子 agent 工具表
- [x] 6.2 **Major：toolSteps 被回合内 bus 注入的 external_input 错误重置**——persistBusEvent 把 task_settled 等回合中途事件也以 external_input 持久化，重置逻辑误清零致低估。改为**只在 agent_output（回合结束）重置**，删除 external_input 重置分支；`TestContextCompressor_HintIgnoresBusInjection` 实证
- [x] 6.3 **Minor：触发器"回落到 keepRecent 停止"论断错误**——实测收敛到 ~3×keepRecent 并持续触发（LSM 连续维护滚动摘要），非 bug 但注释/design 描述失真，已修正
- [x] 6.4 补测试：memory_turn Capped（maxSteps 打满）、断链诚实降级；ParseEventKey evt_/方括号前缀容忍（共享函数，惠及所有 recall 工具）+ Summary 截断 + RegisterSubTools 文档注释
- [x] 6.5 全量验证：build/vet/gofmt 干净；相关包测试全绿（仅 TestPlanPromptContract 既有漂移失败，与本 change 无关）
