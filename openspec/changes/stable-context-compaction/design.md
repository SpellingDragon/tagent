# stable-context-compaction — 技术设计

## Context

生产实证（wechat-bot，2026-08-03 会话）暴露的完整生命周期事实：

- **库级禁令**：`GenerateEventSummary` 明文禁止内容截断——信息损失只允许发生在设计好的定级点（L1/L2/L3/卡片化），且本体必须保真。`task_settled` 是唯一例外：构造时把 2000 截断烘进 `Message.Content`（event_bus.go `newTaskSettledEvent`），全量只活在 TaskManager 内存（TTL 30m 后永久丢失），截断文案广告未装配的 `get_task_result`。
- **触发抖动**：`Compress` 触发条件为 `usedTokens > threshold || completeTurns > keepRecent`。稳态（~4×keepRecent 个 retained 回合）下轮数维度**每轮触发**；`foldToolRuns` 在触发判断之前无条件执行（新滑出 `recent_full_count` 窗口的工具对每轮折入链行）；全文窗口 `fullFrom = len(refs) - recentFullCount` 每轮右移，边界 ref 的渲染在全文↔摘要间切换。三者叠加使投影渲染每轮变化 → LLM 前缀缓存持续失效。
- **文案-装配脱钩**：框架注入文案（截断提示、[tool_agent.go 同名去重提示]、归档通知）硬编码引用工具名，与 agent 实际装配零耦合。
- **既有兜底**：`compressSkeleton` 的预算升级（escalation）在 `total > maxTokens` 时把老段逐级压 L2→L3（oldest-first）直到收敛——容量触发的整理有完整的预算兜底，进行中段与 keepRecent 段是其盲区（一般属性，用户消息同样无防线）。

## Goals / Non-Goals

**Goals:**

1. task_settled 与普通 external_input 同权：本体全量、视图有界化只发生在设计定级点、票据可召回。
2. 整理（compaction）只由 token 容量阈值触发；`keep_recent_tasks`/`recent_full_count`/定级边界全部纯化为"整理后状态"参数。
3. 整理间上下文前缀字节级稳定（append-only），最大化 LLM 前缀缓存复用。
4. 框架文案只发票据（task id / evt key），不广告工具名。

**Non-Goals:**

- 不改 memory 层的 Compaction scheduler（L1→L2→L3 日/周程压缩，另一子系统）。
- 不给巨型单事件（keepRecent/进行中段内）加渲染上限——escalation 已兜底，与用户消息同权的一般属性。
- 不动 `resume_task`（plan 生命周期真刚需）；不探"plan 状态文件化替代内存 rounds"（独立线程，另行提案）。
- 不优化看板"已运行 Xs"秒级精度的微小抖动（记录为后续可选）。

## Decisions

### D1: task_settled 全量保真（删构造时截断）

`newTaskSettledEvent` 直接使用 `sig.Output` 全文构造 Content；删除截断分支、`maxInline` 参数、`DefaultTaskSettledMaxInline`、`TaskSettledMaxInline` 配置穿线。

**拒绝的替代**：
- *保留宽上限（如 M/16）+ 全量另存*：FullEvent 无第二内容字段，"截断版本体 + 全量旁路"引入双表示复杂度，且违反"本体即真相"。
- *渲染层防暴上限*：escalation 已是预算兜底；巨型结算与巨型用户消息同权，单独设防反而制造新例外。

**成本账**：巨型结果全文驻留至 L3（窗口期 + L2 段龄 ≈ 4k=16 回合），每轮 token 增量受 escalation 管制；L3 后与现状完全相同（80 字符卡片）。

### D2: 触发收敛为单一 token 容量阈值（BREAKING）

`if usedTokens <= threshold { pass-through }`——删除 `completeTurns > cc.keepRecent` 维度及 `completeTurns` 统计。`threshold = compress_threshold × max_tokens` 不变。

**饿死问题重评**（原轮数维度的存在动机）：占位符渲染致 token 低估的场景已被"工具调用摘要"（调用 X）与工具链折叠根治；token 估算是渲染后实际内容的函数，不失真。小内容长会话（闲聊型）token 可能长期达不到阈值 → 投影 refs 无界增长——渲染 O(refs) 每轮一次、内存 trivial、无压力时保持原文恰是最高保真；token 终将随 L2 骨架驻留累积触达阈值（每完成一回合净增 ≥2 条全文 refs）。**接受该行为变化**，这是"容量是唯一整理信号"原则的直接推论。

### D3: 渲染冻结——整理边界锚定

投影（`SessionProjection`）新增整理边界 `compactBoundaryKey int64`：

- **整理轮**：`buildRetainedRefs` 产出 retained 后，取 `retained[max(0, len-retainedRecentFullCount)]` 的 EventKey 作为新边界（合成负 key ref 不计入计数）。`recent_full_count`（默认 4k）由此变成纯"整理后状态"参数。
- **整理间**：渲染 `full = ref.EventKey > 0 && ref.EventKey >= boundary`；Snowflake key 单调递增保证整理后新增事件天然全文（活跃前沿），旧 refs 的渲染方式冻结。负 key 合成 ref（滚动摘要/链行）走既有 EventSummary 分支不受影响。
- **初始态**（从未整理）：boundary=0 → 全部 refs 全文（等价于现状首整理前的小会话行为，可接受；或首事件 key）。

**拒绝的替代**：每轮滑动窗口（`len(refs) - recentFullCount`）——边界 ref 全文↔摘要切换是固有抖动源，正是本 change 要消除的。

### D4: 工具链折叠移入整理路径

`foldToolRuns` 从触发判断之前移到触发之后的整理路径内：

- **整理间**：窗口外工具对按 `EventSummary` 渲染（thinking_plan="调用 X"、action_command=机械工具结果行）——有界、确定性、字节稳定。
- **整理轮**：fold + 段定级 + L3 归档 + 滚动摘要维护 + 卡片整理一次性完成；幂等短路（changed=false）保留。
- 折叠语义纯化：折叠是"整理动作"的组成部分，不是持续维护。

### D5: 框架文案票据化

- 同名去重提示（tool_agent.go）：收缩为「同名计划任务已在运行 (task xxx)；请等待其 task_settled 结果，不要重复发起同名调用」——保留 task id 票据与等待指引，删除「用 get_task_result 查询」「结算后再用 resume_task(...) 续行」的操作教学（生命周期操作教学归 plan 工具描述，task-reentry 契约不变）。
- 归档通知（smart_compress.go `buildSegmentCompressNotice`）：保留「不要重复执行已被压缩的操作」的纪律句与 evt 票据清单；删除具体工具名列举（`recall(...)`/`search_content(...)`/`read_file` 参数示例）。

**原则**：框架文案只陈述"什么在哪儿"（where/what：票据），"怎么取"（how）归工具声明——后者天然与装配一致，提示-能力脱钩被结构性消灭。

### D6: get_task_result 退役，其余任务工具保留

- `get_task_result` 从 `RegisterSubTools` 注册表退役并删除实现与测试：能力等价替代为 `memory_recall(items=[{key}])`（票据=task_settled 事件的 evt key）→ FullEvent.Content 全量，且与 TTL 解耦。
- `list_tasks`/`cancel_task`/`relaunch_task` **保留注册**（看板之外的主动查询、取消卡死任务、重跑，均无替代通道），仅框架文案不再引用；`action_tool_desc.md` 删除对任务工具组的引用句。
- `resume_task` 保留（plan 生命周期契约）。

## Risks / Trade-offs

- [小内容长会话 refs 无界增长] → 接受（D2 论证）；渲染 O(refs) 每轮一次成本线性可测；后续如需兜底可加极宽 refs 上限（如 512），本 change 不做。
- [整理轮前缀一次性大重排] → 触发频率 = token 摊销周期（LSM 锯齿模型）；指数定级延长段驻留减少重排幅度；这是容量触发整理的固有代价，换来整理间的完全稳定。
- [巨型结算全文进一轮 LLM 调用] → escalation 预算兜底（L2→L3 oldest-first 收敛）；与巨型用户消息同权。
- [触发但无料可整（进行中段独大）] → changed=false 幂等短路原样返回，无行为回退。
- [BREAKING：轮数触发语义删除] → 既有依赖该行为的测试（skeleton_archive_test 收敛断言、hint 测试）迁移为容量触发等价断言；无部署配置依赖（`task_settled_max_inline` 在生产 yaml 中为注释态）。
- [历史截断版 task_settled 事件] → 不迁移（事件不可变原则）；新结算起全量。

## Migration Plan

1. 合入即生效，无持久化迁移；`task_settled_max_inline` 配置键删除后，yaml 残留键被忽略（现有解析容错）。
2. 回滚 = git revert（无数据格式变化；投影 boundary 为进程内存态，重启自然重建）。
3. 验证路径：单测（触发单维、渲染冻结字节断言、全量 settle）→ 全量 `go test ./...` → wechat-bot 实测观察 `[ContextCompressor]` 日志（预期：under budget 直通为主、compressing 稀疏化、SmartCompress 每次有效）。

## Open Questions

（无——核心决策已在探索阶段闭合；看板时间精度为记录在案的后续可选优化）
