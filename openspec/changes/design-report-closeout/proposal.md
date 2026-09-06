## Why

docs/.dev/tagent下一步迭代设计报告.md（五方向 D1-D5）与 2026-09 大迭代交付的代码存在系统性差距：经五路逐项核对，D1 ~60%、D3 ~55% 有核心承诺未闭合，其中**报告 §4.3 明标 Wave 0 必修的两个先行修复项（F2 慢消费者卡死主循环、F3 单比特翻转致记忆库启动失败）至今未修**（正确性级欠账）；D1 的 bundle_id 归因根未盖章导致 feedback 闭环（M3=0%）与 Evidence 归因全部悬空；慢道人工批准通道缺失使高风险自进化改动事实不可用；D2 巩固纯 manual 无触发体系；D3 goal 门有管理器无工具入口=事实关闭。本变更落地**用户已确认**的 P0（先行修复四项）+ P1（核心承诺收尾五项），全部为设计内项，不做设计外变更，但设计内项须保障实现成熟度（含回归测试与门禁）。

## What Changes

**P0 先行修复（正确性级）**
- F3：LocalFileKV.replayWAL 遇中间坏行从容错（跳过坏行 + quarantine 计数上报）替代启动即失败
- F2：RunFlow outputCh 发送加 2s 宽限→超限落盘+票据（对齐 task_settled 大结果转储语义，不吞信号）；SessionHook default 丢弃路径同步加计数
- bundle_id 归因盖章：Attribution 载体补 bundle_id 字段，双持久化路径（插件管线+persistBusEvent）盖章，RunFlow 从 evoStore 取当前 active bundle 版本
- F5：API key env 双名并存（ZAI_API_KEY / TENCENT_HY_API_KEY）分层标注（README/CI 声明），不强行统一（hy3 测试本质用混元）

**P1 核心承诺收尾**
- D1 feedback 闭环：`feedback` 事件类型经 EventTypeSpec 注册（TTL 可配置、正 key、不进 LowValue）；FeedbackBinder 经 RelationStore 因果边绑定回执；两来源先行（OnSettle 任务成败自动 + HTTP API）；MetricGuardrail 补 negative_feedback 判据
- D1 approve 通道（设计统一为「审批消息流」抽象：审批请求→渗透消息→人工响应→落盘生效；实现优先微信消息注入 + `tagent approve` CLI 两个入口）
- D2 巩固触发体系：容量阈值（默认 200，配置化 `memory.engine.consolidation`）+ 冥想 hint 两路触发先行；SNOOZED 退避状态机；`min_source_events` 硬门控；novelty 判据明确不做（依赖 agent 篇缺口 4 未成熟）
- D3 goal 工具五件套（goal_declare/goal_list/goal_resolve/denial_query/approval_list，entry only）+ GoalRegistry 事件持久化（重启不丢 goal 声明）
- D3 降级行为层：model 依赖退化时 turn 间退避停顿、mcp 依赖退化时 mcp_call 熔断快失败+半开恢复、disk 依赖退化时禁 spawn 新任务；均为「闸不是墙」语义（警告级降级，可配置关闭）
- D3 mem_spill 重放补 projection.Append（设计的双写语义，当前只回灌 StoreEvent）
- governance 事件 EventTypeSpec 的 Skeleton 标记改为 false（设计要求非 skeleton，该类事件不进骨架压缩）

**配套**
- 五个设计分歧裁定表固化进 LEDGER（engineBridge/快慢道/同步删除/时间窗/OTel 均保持实际实现；报告以勘误附录标注 2 处过期 mock 表述——勘误随 D5 M2 落，不在本变更）
- 明确非目标：D4 三支柱（obs/evals/回归）、D5 M1-M4、cassette+replay 门、故障注入矩阵——留 roadmap 后续变更

## Capabilities

### New Capabilities
- `feedback-binding`: 回执-反馈绑定——feedback 事件类型、FeedbackBinder 因果边、OnSettle/API 双来源、guardrail negative_feedback 判据
- `approval-channels`: 慢道人工批准通道——审批消息流抽象 + 微信消息注入 + CLI 入口
- `consolidation-triggers`: 巩固建议式触发——容量/冥想 hint 双路 + SNOOZED 退避 + 硬门控
- `goal-tools`: goal 登记与查询工具族（entry only）+ GoalRegistry 持久化
- `degradation-behaviors`: 依赖退化的行为响应层（退避/熔断/禁 spawn，警告级可配置）

### Modified Capabilities
- `event-segment-store`:F3——replayWAL 坏行容错（跳过+quarantine 计数，不再启动失败）
- `persistent-event-loop`:F2——outputCh 2s 宽限→落盘+票据，不吞信号
- `event-metadata-contract`:bundle_id 归因键补章（双持久化路径）+ feedback 事件归因
- `event-type-unification`:feedback 类型注册（EventTypeSpec）+ governance 类型 Skeleton→false
