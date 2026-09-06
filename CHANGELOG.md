# Changelog

本项目所有显著变更记录于此。格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本遵循 [SemVer](https://semver.org/lang/zh-CN/)。首个 tag（v0.1.0）打出前的变更统一记于
`[Unreleased]`（按 roadmap 裁决 C11，tag 时点待定）。

## [Unreleased]

### Added

- **混合语义召回（T-A）**：Embedder + InMemoryEngine（hybrid RRF 融合 / 分区隔离 / 异步嵌入
  worker）+ engineBridge 解耦缝（契约 C6：IndexBuilder+Retriever+io.Closer，引擎仅在组合根出现）
  + 向量 KV 持久化跨重启重建 + recall hybrid 逐跳降级链（引擎错/零命中/分区全滤/全悬挂→关键词）
  + TracedEmbedder（GenAI semconv span）。
- **统一可观测（T-B）**：turn root span（`tagent.turn`，noop provider 安全）+ trace_id/span_id
  三投影互链（事件 Metadata + trajectory LLMCallRecord + OTel span 树）+ 异步任务 task span
  link（Origin trace 锚点经 task_settled 事件回流，跨 turn 关联不侵入 task 包）。
- **自进化（T-EVO/TC0）**：BundleStore（不可变内容寻址 + 原子 active 切换）+ VersionedSource
  （实现 prompt.Getter，回合边界生效）+ refine 工具（propose/diff/status/rollback，无 activate，
  agent 无直接激活权）+ ReleaseManager 风险分级发布道（DiffLaneRouter：模型/参数→慢道门后、
  提示词→快道后验；双回滚 = MetricGuardrail 确定性闸 + LLMJudgeEvaluator 模型决策回滚）+
  后验评估闭环（Evidence 从治理事件收集 canary 证据）。
- **治理（T-G）**：RiskClassifier（契约 C5：纯函数四级分级）+ GovernanceGate 决策管线（classify
  →critical 批准门→goal 检查→预算闸→记账）+ GovernanceTool 装饰器 + BudgetManager（滑动窗口
  epoch 防重启刷限）+ ApprovalManager（异步文件通道 args_digest 绑定）+ DenialLedger 治理账本
  （持久化）+ GoalRegistry。
- **可靠性（T-G）**：DegradationManager 五依赖退化状态机全 LIVE（memory/disk/rustviking 经
  ErrorTrackingStore 存储栈、model 经 event_loop、mcp 经 mcp_call）+ ErrorTrackingStore（契约
  C2 最外层，报告 D3 设计的挂点补齐）+ mem_spill 退化兜底（StoreEvent 失败事件落 JSONL，恢复
  按原 key 重放，at-least-once 延伸存储层）+ ReliableBus 磁盘溢出（channel 恒早于 spill 全序 +
  pending 背压）+ AnchorStore 冥想锚点持久化。
- **记忆策展（T-D）**：证据门控巩固（服务端 SHA1 指纹防伪造 + receipts 收据）+ MemoryDiagnostics
  维度锚定诊断 + `memory_consolidate`/`memory_health` agent 工具。
- **RL 轨迹（rl）**：TrajectoryRecorder JSONL 流水（含 trace 锚点、final sync on close）。
- **CI**：GitHub Actions（build + vet + 全量 short 测试，全 mock 无需 key）。

### Fixed

- F1：`FullEvent.Metadata` 在生产代码从未填充（归因地基双路径盖章修复）。
- F2：RunFlow 内 outputCh 发送无限阻塞（2s 宽限→落盘）。
- F3：replayWAL 对中间坏行直接报错导致启动失败（跳过坏行+计数上报）。
- F4：`DefaultConfig()` 的 `id:"action"` 与注册表 `"exec"` 不匹配（TestDefaultConfigBuildable
  永久守护配置-注册表漂移）。
- 四轮 gate-3 CodeReview 修复：事件时序倒置（回复路由错误会话）、critical 无批准通道绕过、
  judge 缺 score 零值误回滚、canary ctx 取消假通过、后验评估 Limit+asc 静默失效、file 后端
  同 path 多实例（跨 agent 因果链断链 + 双 Compactor 并发覆盖）、DegradationManager 计数
  语义塌缩/无恢复路径、knowledge 吞存储错误等（详见 `openspec/changes/LEDGER.md`）。
