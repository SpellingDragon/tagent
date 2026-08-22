## Why

三个实证问题指向同一根因——**例外与抖动**：

1. **task_settled 构造时截断**是全库唯一违反"绝不截断"禁令（`GenerateEventSummary` 明文：信息损失只允许发生在设计好的定级点）的事件：截断烘进本体导致全量只在 TaskManager 内存（TTL 30m 后永久丢失）、截断文案广告未装配的 `get_task_result` 工具（提示-能力脱钩，模型转述承诺后当场穿帮，生产日志实证）。截断文案还会进滚动摘要卡片，永久污染上下文。
2. **压缩触发多维化（token 阈值 + 完整段超龄）**在稳态下轮数维度每轮触发整理路径：工具链折叠每轮改写投影、`recent_full_count` 全文窗口每轮滑动切换渲染方式——上下文前缀持续变化，LLM 前缀缓存持续失效。期望属性：**只有触发了上下文容量阈值才做整理**；`recent_full_count` 等参数指引的是**整理后的状态**，而非整理触发。
3. **框架文案引用工具名**（截断提示、去重提示、归档通知）与装配状态零耦合——文案承诺的能力模型未必有。

## What Changes

- **task_settled 全量保真**：结算结果全文入库（Content=全量），删除构造时截断与 `get_task_result` 提示；删除 `task_settled_max_inline` 配置项及全部穿线。全量随事件本体永在 MemoryStore，召回与 TTL 解耦。
- **BREAKING** 压缩触发收敛为单一 token 容量阈值：删除"完整段超龄"触发维度；工具链折叠、段定级、滚动摘要维护、卡片整理全部只在**整理轮**（触发后）执行，未触发轮 pass-through 不触碰投影。
- **渲染冻结（前缀稳定）**：全文窗口（`recent_full_count`）在整理时锚定为边界 key，整理间 append-only——新增事件全文追加到尾部，旧 refs 的渲染方式（全文/摘要）冻结不变。未触发轮的上下文前缀字节级稳定。
- **框架文案票据化**：同名去重提示收缩为事实陈述（task id 票据），不再教学 `resume_task`；归档通知不再列举工具名（`search_content` 等），找回指引收敛到票据与工具自身声明。
- **get_task_result 退役**：全量结果经事件本体 + `memory_recall`（票据=task_settled 的 evt key）召回，能力等价且与 TTL 解耦。`list_tasks`/`cancel_task`/`relaunch_task` 保留注册（能力无替代通道），框架文案不再引用。

## Capabilities

### New Capabilities

（无——全部为既有能力的需求修改）

### Modified Capabilities

- `async-task-execution`: task_settled 通知携带**全量**结果（删除构造时截断行为），文本级关联标识保留
- `task-skeleton-compression`: 压缩触发器从多维收敛为单一 token 容量阈值；工具链折叠移入整理路径（整理时才折叠）；新增"整理间渲染冻结"需求（全文窗口锚定整理点，append-only 稳定）
- `task-registry-and-board`: 任务操作工具组收缩——`get_task_result` 退役（全量召回由 recall 协议承接），其余工具保留注册
- `plan-agent`: 同名去重提示票据化（task id + 等待 settle 的事实陈述），删除 resume_task 操作教学
- `compress-event-enrichment`: 归档通知去工具名化——不再列举 `search_content` 等具体工具，指引收敛为"已归档 + 票据"事实

## Impact

- **代码**：
  - `agent/event_bus.go`：`newTaskSettledEvent` 全量化、删截断
  - `agent/compress/context_compressor.go`：触发判断单维化、`foldToolRuns` 移入触发后、`resolveRef` 按整理边界渲染
  - `agent/compress/projection.go`：投影记录整理边界 key
  - `agent/tool_agent.go`（去重提示）、`agent/compress/smart_compress.go`（归档通知）文案收缩
  - `tool/task/register.go`：`get_task_result` 退役；`config.go`/`tagent.go`/`agent` 构造穿线删除 `TaskSettledMaxInline`
- **配置**（BREAKING）：`task_settled_max_inline` 删除；`keep_recent_tasks`/`recent_full_count` 语义纯化为"整理后状态参数"（不再参与触发判断）
- **行为**：稳态下压缩从"每轮触发、多数幂等"变为"容量触发、一次整理"；整理轮前缀一次性重排，整理间前缀字节稳定（LLM 前缀缓存复用最大化）
- **测试/文档**：task_settled 截断断言改全量断言；轮数触发测试改容量触发；README 配置表、wiki `memory-architecture.md` §16 同步
