## 为什么

tagent 项目的 wiki 文档与实际代码实现之间存在显著差距，多个模块的文档描述的数据结构、API 签名、架构状态与代码完全不符。这导致：

1. **误导新开发者**：文档中的 EventKey 格式（`"evt_{ts}_{seq}"` 字符串）与实际（Snowflake int64）完全不同
2. **架构认知混乱**：agent wiki 将已实现的功能标注为 "未来设计"，tool wiki 将已落地特性标注为 "改进计划"
3. **信息不完整**：agent wiki 缺少 Phase 1 事件视图转换的完整描述，plugin wiki 仍描述已废弃的全局单例设计
4. **回顾依赖**：团队进行 code review 和架构决策时无法信任文档

**设计原则**：wiki 文档是 "single source of truth"，必须如实反映当前实现状态。

## 变更范围

**纯文档修正**，零代码变更。

### 涉及文件

| 文件 | 严重度 | 变更量 | 主要原因 |
|------|--------|--------|---------|
| `docs/wiki/memory/memory-architecture.md` | ★★★ | ~200行重写 | EventKey string→int64、PartitionID 缺失、存储结构二层级 |
| `docs/wiki/agent/agent-architecture.md` | ★★ | ~50行修正 | Compress 签名、Snowflake "未来设计"→"已实现"、Phase 1 补充 |
| `docs/wiki/plugin/plugin-architecture.md` | ★★ | ~60行修正 | lastEventKey 单例→分区 map、EventKey 生成方式、StateDelta 字段 |
| `docs/wiki/tool/tool-architecture.md` | ★ | ~40行修正 | Snowflake/PartitionID "改进计划"→"已实现"、RecallAgent 架构更新 |

### 不涉及

- 任何 `.go` 源代码
- 测试文件
- prompt-architecture.md（基本准确）、event-architecture.md（已修正）

## 能力

### wiki-memory-sync
将 `memory-architecture.md` 与实际代码对齐，覆盖：
- EventKey: string → Snowflake int64
- FullEvent/EventReference: 新增 PartitionID 字段，EventKey 类型变更
- MemoryStore 实现: 单层 map → 二层 map（按 partition 分区）
- EventKey 生成: `NewEventKey` → `NewSnowflakeEventKey`
- 文件存储: 单层目录 → 分区子目录
- StateDelta: 补充 `partition_id` 字段
- 写入操作: AddEvent + atomic 写（`file_backend.go`）

### wiki-agent-sync
将 `agent-architecture.md` 与实际代码对齐，覆盖：
- Compress 签名: 2 参数 → 3 参数（含 `inv *agent.Invocation`）
- §12.5 Snowflake EventKey: "未来设计" → "已实现"
- Phase 1 事件视图转换: 补充 `applyEventView` / `extractEventInfo` 描述
- 文件行数表: 更新 `context_intervention.go` 行数

### wiki-plugin-sync
将 `plugin-architecture.md` 与实际代码对齐，覆盖：
- 前驱跟踪: `lastEventKey string` → `lastEventKeys map[int]int64`
- EventKey 生成: `NewEventKey` → `NewSnowflakeEventKey`
- StateDelta 写入: 补充 `partition_id`
- 并发安全描述: 更新为分区级别

### wiki-tool-sync
将 `tool-architecture.md` 与实际代码对齐，覆盖：
- Snowflake/PartitionID: "改进计划" → "已实现"
- RecallAgent: "普通 CallableTool" → "完整 TagentAgent + 内部 React"
- event_key 注入: "框架级自动注入" → "AgentToolWrapper 自解析"

## 影响

- **零代码变更**：纯文档修正，不产生编译/测试影响
- **文档一致性**：修复后 6 个 wiki 文件全部与实际代码一致
- **长期价值**：文档恢复 "可信源" 地位，降低 onboarding 成本
