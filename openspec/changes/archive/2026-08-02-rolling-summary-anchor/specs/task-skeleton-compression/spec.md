# task-skeleton-compression Delta

## ADDED Requirements

### Requirement: 滚动摘要消息豁免 L3 压缩（常驻可见）

骨架压缩 SHALL 把领先的 `context_compress` 滚动摘要消息从段结构中摘出（类比 `SplitSystemMessage`），使其不参与分段与定级，并在压缩后无条件回填到结果最前（紧随系统消息）。由此滚动摘要消息 SHALL 在任何段龄/级别下都保留在模型上下文中，SHALL NOT 被 L3 整段丢弃。

#### Scenario: K≥7 段0 升 L3 时摘要仍可见

- **GIVEN** 投影含滚动摘要 + 8 个完整任务回合（段0 段龄达 L3 阈值）
- **WHEN** 执行骨架压缩
- **THEN** `result.Messages` SHALL 仍含 `context_compress` 滚动摘要消息（位于系统消息之后）
- **AND** 段0 SHALL NOT 包含该滚动摘要消息（它已被摘出，不参与定级）

#### Scenario: 摘要 ref 仍被投影携带

- **GIVEN** 骨架压缩保护了滚动摘要消息
- **WHEN** `buildRetainedRefs` 构建 RetainedRefs
- **THEN** SHALL 仍吸收并重建滚动摘要 ref（负 key），投影照常携带到下一轮

### Requirement: 段定级采用指数段龄边界

`deterministicLevel` 的段龄阈值 SHALL 为指数边界 `keepRecent × 2^level`（即 L0 < k、L1 < 2k、L2 < 4k、L3 ≥ 4k），而非线性 `{k, 2k, 3k}`，以使段在每个级别驻留更久、被折叠进滚动摘要的频率降低（前缀缓存更稳定）。指数底数 SHALL 固定为 2。

#### Scenario: 指数边界定级

- **GIVEN** keepRecent=2
- **WHEN** 计算段龄 age 的级别
- **THEN** age=3 SHALL 为 L1、age=5 SHALL 为 L2、age=7 SHALL 为 L2、age=8 SHALL 为 L3（线性下 age=5/6 已为 L3）

### Requirement: 压缩配置参数公式化默认值

压缩相关配置 SHALL 以 `max_tokens`（M）与 `keep_recent_tasks`（k）为主变量，未显式设置时其余参数按简单公式派生默认值：token 阈值 = `compress_threshold × M`；`recent_full_count` = `k × 每轮引用数`；`card_max_chars` = `M / 20`；`compact_keys_listed` = `card_max_chars / 200`。显式设置的值 SHALL 优先于公式默认值（向后兼容）。

#### Scenario: 未设置时按公式派生

- **GIVEN** M=128000、k=2，且 card_max_chars / compact_keys_listed 未显式设置
- **WHEN** 初始化压缩器
- **THEN** card_max_chars SHALL 默认 6400（M/20）、compact_keys_listed SHALL 默认 32（6400/200）

#### Scenario: 显式设置优先

- **GIVEN** 显式设置 card_max_chars=6000
- **WHEN** 初始化压缩器
- **THEN** SHALL 使用 6000 而非公式默认值
