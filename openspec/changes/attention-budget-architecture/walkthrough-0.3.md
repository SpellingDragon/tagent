# 0.3 消费状态推导材料走查（决策 7 验证，实施前置交付）

## 三态判定 → 事实链可得性对照

| 判定 | 所需材料 | 事实链落点 | 可得性 |
|------|---------|-----------|--------|
| settle 身份 | task_id（全量 UUID）、settle_status、source=task | settle 事件 Metadata（event_bus.go 构造时携带） | ✓ |
| 已消费 | 该 settle 之后存在以它为输入的回收 turn（agent_output） | 单消费者 strict order：settle external_input → 其后最近的 agent_output 即其消费 turn（批量合并时一个 turn 消费多条，按序推导） | ✓ |
| 已外显 | 回收 turn 的 lineage 是否用户派生 | settle 事件 meta_*（Origin 信使携带 spawn turn 的 chat_id/trigger_source）；task-retired/task-unstamped/unknown → 门禁扣留 → 内部性 | ✓ |
| 内部性 | 同上取反 | 同上（lineage 缺失/内部来源 = 门禁按 fail-closed 扣留，即不外显） | ✓ |

**结论**：消费状态 = f(事实链 metadata + 事件序)，确定性推导，零新增持久化面——决策 7 成立。投影重建回放本推导即恢复降级形态。

## 实施捷径（走查发现）

「票据卡片」与 `buildSettleFoldRef` 产出的 settle_fold 卡片**同物**（≤80 chars 票据行、Synthetic 幂等、recall 票据既有）。降级操作实现为**单条 settle 走既有折叠卡机制** → Replace 投影（红线 4 合规）。L5 召回暂存：recall 结果是 tool 类消息，L1 起即被丢弃（既有骨架定级天然短命）——L5 按「不违反既有机制」验证性落地，无需新结构。
