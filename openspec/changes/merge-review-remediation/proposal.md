## Why

dev（43 提交）合入 origin/main 前的分主题并行评审发现 2 项 P0（主规格与实现相反）与 14 项 P1（数据竞争、静默成功、租约泄漏、门禁绕过等）。这些问题一部分是实现违反既有 spec，一部分是 spec 未跟上已裁决的行为变更；不修复合入 main 会把数据完整性缺口与误导性规格一并固化。

## What Changes

**规格对齐（P0×2）**
- `event-sourced-projection` 主 spec 的"无锚恢复截断至 500 条/partial/truncated_events"条款按现行实现（fallbackCap=0，b871d30 裁决）重写为全量复原语义。
- `event-segment-store` 主 spec 移除两条已被归档 delta 声明 REMOVED 的条款（WAL 中间坏行容错、WAL-fsync 耐久），按现行实现落"KV Sync=原子快照屏障"真契约。

**实现修复（P1，评审证据见提交说明）**
- agent 核心运行时：SetAuditLine 死接线归位；execution gate 模型入口错误以失败响应呈现（不得静默零产出成功并 ack）；subagent 调用拒绝路径补 liveCM 注销。
- 常驻可靠性：退役谱系戳改挂结算信号（消除 Spec.Origin 并发写与 Resume 恢复轮谱系污染）；OnBatchRetire 补 per-invocation 结算路由与记账释放。
- compress：foldSettleRuns 批量折叠前按 dispositions 剔除 TelemActive 成员（兑现"未消费不可丢"契约）。
- memory：spill 重放的租约释放绑定"行已落盘移除"；RetentionLease.Release 注释与实现对齐。
- 工程化：check_comment_only wrapper 停止预过滤删除侧文件（让 codetools MISSING-HEAD 硬拒可达）；顺带两处一行级 P2（recovery.go 死残留删除、lint.sh 成功打印移到全部门之后）。

**文档修复（P1 文档面）**
- README"零必填配置"表述纠正（D1 依赖显式 working_dir）；rl-architecture"已知缺口"自相矛盾段修正；compression-and-telemetry 截断语义表述更新；storage-durability-positioning"码面事实"纠正；归档 evidence.md 个人绝对路径脱敏。

## Capabilities

### New Capabilities

（无）

### Modified Capabilities

- `event-sourced-projection`: 无锚恢复 Requirement 从"分页扫描后保留最新 500 条有效事件（partial/truncated_events）"改为"过滤后全量复原、无截断上限，truncated 仅在快照槽丢失/读失败时非零"。
- `event-segment-store`: REMOVED 两条 WAL 系条款（中间坏行容错、WAL-fsync 耐久）；ADDED 一条反映现行 LocalFileKV 的 KV Sync 原子快照屏障契约。
- `async-task-lifetime`: 批量退役 Requirement 显性化两条不变量——有 invocation 绑定的退役条目仍须完成结算路由与记账释放；Resume 恢复轮的外发信号按恢复语境取谱系（不继承 task-retired）。
- `persistent-event-loop`: ADDED 模型入口错误极性条款——迭代器/流创建失败、verify 拒绝必须以携带错误信息的失败响应呈现并归约 failed turn，MUST NOT 静默零产出且以 completed 冻结 ack。

## Impact

- 代码：`agent/agent.go`、`agent/execution_gate_model.go`、`agent/session.go`、`agent/recovery.go`、`agent/settle_routing.go`、`agent/task/task_manager.go`、`agent/compress/context_compressor.go`、`memory/mem_spill.go`、`memory/retention_lease.go`、`scripts/check_comment_only.sh`、`scripts/lint.sh`
- 文档：`README.md`、`docs/storage-durability-positioning.md`、`docs/wiki/rl/rl-architecture.md`、`docs/wiki/agent/compression-and-telemetry.md`、`openspec/changes/archive/**/evidence.md`（路径脱敏）
- 无 API/序列化/协议破坏；所有修复保持既有默认行为语义（修复方向均为"实现回归 spec"或"spec 对齐已裁决实现"）
- 评审全量证据与交叉验证记录：`.git/review-notes/01-findings.md`（P0×2/P1×14/P2×29 完整清单）
