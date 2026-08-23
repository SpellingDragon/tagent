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
- 不给巨型单事件加**条件式**渲染上限（随预算状态变化的截断会破坏 D3 渲染冻结的字节确定性）；超大输出的防线是产生时转储（见 D1 修订）。
- 不动 `resume_task`（plan 生命周期真刚需）；不探"plan 状态文件化替代内存 rounds"（独立线程，另行提案）。
- 不优化看板"已运行 Xs"秒级精度的微小抖动（记录为后续可选）。

## Decisions

### D1: task_settled 输出转储（评审修订版：全文→文件）

**初版（已废弃）**：直接用 `sig.Output` 全文构造 Content，"全量随事件本体永在 MemoryStore"。评审澄清后推翻，两个致命缺陷：

1. **不可压区硬上限死区**：巨型 settle 落入进行中段（agent_output 前恒 L0）或 keepRecent 窗口，escalation 够不到这两区（它只压 `IsComplete && age≥k` 的老段）。若不可压区合计物理超过 provider 硬上限（maxTokens）→ 请求 400 超限 → 空响应不 echo agent_output（既有防退化设计）→ 段永不闭合 → 会话卡死，不可自愈。M=128k 时单条约 >9 万字（中文）可触发——tmux 超宽行/中文密集输出可达。
2. **召回复发**：`memory_recall` 是整事件返回、无分页——模型凭票据召回巨型全文，上下文当场再爆。事件持全文的方案只是把爆炸从"自动进"推迟到"召回进"。

**修订版（实施目标）**：对齐同步路径已有的转储三件套（`OutputLimitTool` 超限全文存文件+路径票据；`ActionTool` >2000 字符写 `output_<session>.txt`+尾部；`workspace.Cleaner` 周期清理 tool-output/，1h 扫描/24h 过期/200 文件封顶）——异步 settle 是唯一没接上这套模式的路径，补齐它：

- 超过转储阈值（与 `OutputLimitTool` 同公式 `maxChars = MaxTokens/2×4` 字符，同步异步一个"超大"定义）→ 全文写 `workspace tool-output/task-<id8>-<ts>.txt`（Cleaner 自动管清理）；
- 事件 Content = 尾部（对齐 ActionTool 的 2000 字符）+ 路径票据；**事件本体有界**，`memory_recall` 召回此事件返回有界版+票据，永不复发大结果；
- 全文消费唯一通道：`read_file(path, start_line, num_lines)` 行级分页（上游 file 工具集已支持：返回带行号范围、大文件读全量报错强制分块）；
- 小结果（≤阈值）全文内联，与现状一致。

**确立的分层原则**：**超大内容的本体是文件，不是事件**——产生时转储（防进上下文）、记忆只持有界内容+路径票据（防召回复发）、read_file 分页消费（防读回爆炸）。三层各司其职，任何一层都不单独承担全文。

**与初版的权衡差异**：全文不再永久随事件持久（文件受 24h/200 文件清理约束）——与同步工具输出同等待遇，对称即可接受；旧卖点"与 TTL 解耦永久召回"退役，由"24h 内 read_file 分页可读"替代。

**拒绝的替代**（评审澄清过程淘汰）：
- *渲染层条件式上限（预算升级后仍超限才截）*：截断随预算状态变化 → 同一事件各轮渲染不同 → 破坏 D3 字节稳定性，自相矛盾。
- *失败时强制闭合段*：闭合后巨型内容仍在 keepRecent 窗口，需再等 4 回合老化+破例压缩才自愈，修不彻底。
- *纯接受死区（零代码）*：死区触发条件极端（单条 >70% 预算）且卡死可重启恢复，但卡死表现（会话静默）对使用者不友好；转储方案复用既有三件套，增量极小，性价比高于接受。

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

- `get_task_result` 从 `RegisterSubTools` 注册表退役并删除实现与测试：能力替代为双层——小结果随事件内联直接可见；超大结果经转储文件 + `read_file` 分页读取（D1），票据（evt key / 文件路径）均随通知可达。
- `list_tasks`/`cancel_task`/`relaunch_task` **保留注册**（看板之外的主动查询、取消卡死任务、重跑，均无替代通道），仅框架文案不再引用；`action_tool_desc.md` 删除对任务工具组的引用句。
- `resume_task` 保留（plan 生命周期契约）。

### D7: recall 统一单入口（参数路由，确定性优先）

**现状三张脸**（模型侧认知负担 + 误调用面）：`memory_recall`（纯函数，items/query 已有分流雏形）、`memory_turn`（因果链回走）、`recall` 子 agent（LLM 多跳编排，自带 recall_query/get/recent/trace/memory_turn 五个子工具）。用户裁决：模型侧不应当区分，工具自行按参数路由纯工程或 LLM。

**统一形态**：单一 `recall` 工具，参数即路由——

| 参数形态 | 路由 | 原工具 |
|---|---|---|
| `items=[{key,hint?}]` | 纯函数票据直达（批量 GetEvent，零 LLM） | memory_recall items |
| `query`(+time/type/keyword filters) | 工程检索（QueryOptions，可演进向量） | memory_recall query |
| `turn_key` | 因果链回走（external_input 边界停） | memory_turn |
| `orchestrate: true`（显式） | 升级 RecallAgent LLM 多跳编排 | recall 子 agent |

- **确定性优先**：LLM 编排为**显式 opt-in**（`orchestrate` 参数），不做自动升级——同一次调用在不同状态下走不同路径不可预测，与 D3 确定性精神一致。
- **退役面**：`memory_recall`/`memory_turn` 工具名合并退役；recall 子 agent 的五子工具不再对主 agent 直接暴露，收编为编排分支的内部实现；RecallAgent 保留为编排引擎（prompt/定位不变，入口收窄）。
- **装配**：yaml 从 `memory_recall`+`memory_turn`+`- agent: recall` 三条挂载收敛为单条 `- kind: tool, id: recall`。
- **输出协议不变**：统一条目 `{key(hex), type, summary, content, time}`（含诚实截断提示）；大结果防复发由 D1 转储保证（事件本体有界，recall 任何路径都不会回流超大全文）。

**拒绝的替代**：*保留多工具*——三张脸要求模型理解工具职责边界（生产日志已见误用与自报工具清单幻觉）；*自动判断升级 LLM*——不可预测路径。

## Risks / Trade-offs

- [小内容长会话 refs 无界增长] → 接受（D2 论证）；渲染 O(refs) 每轮一次成本线性可测；后续如需兜底可加极宽 refs 上限（如 512），本 change 不做。
- [整理轮前缀一次性大重排] → 触发频率 = token 摊销周期（LSM 锯齿模型）；指数定级延长段驻留减少重排幅度；这是容量触发整理的固有代价，换来整理间的完全稳定。
- [转储文件被 Cleaner 清理后票据失效] → read_file 报 not found，模型可感知并停止依赖；与事件归档后的召回退化同级；24h 窗口内可读已覆盖绝大多数消费时机。
- [转储阈值公式（M/2×4 chars）对中文偏松] → 与同步路径同公式保持一致性优先；即便单条巨型，其宿主段闭合后交由定级/escalation 处理，死区仅在"不可压区自身物理超限"触发，条件极端。
- [触发但无料可整（进行中段独大）] → changed=false 幂等短路原样返回，无行为回退。
- [BREAKING：轮数触发语义删除] → 既有依赖该行为的测试迁移为容量触发等价断言；无部署配置依赖。
- [历史截断版 task_settled 事件] → 不迁移（事件不可变原则）；新结算起转储。

## Migration Plan

1. 合入即生效，无持久化迁移；`task_settled_max_inline` 配置键删除后，yaml 残留键被忽略（现有解析容错）。
2. 回滚 = git revert（无数据格式变化；投影 boundary 为进程内存态，重启自然重建；转储文件随 Cleaner 自然回收）。
3. 验证路径：单测（触发单维、渲染冻结字节断言、settle 转储/内联分界）→ 全量 `go test ./...` → wechat-bot 实测观察 `[ContextCompressor]` 日志（预期：under budget 直通为主、compressing 稀疏化、SmartCompress 每次有效）与巨型后台命令的 settle 转储行为。

## Open Questions

（recall 统一已纳入 D7，随本 change 实施）
- 看板"已运行 Xs"秒级精度的微小抖动为记录在案的后续可选优化。
