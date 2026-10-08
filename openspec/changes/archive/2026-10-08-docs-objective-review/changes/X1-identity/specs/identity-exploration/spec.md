# identity-exploration Specification

## ADDED Requirements

### Requirement: 标识归属表交付

报告 SHALL 交付标识归属表：event_key / trace_id / span_id / turn / invocation / task_id / generation 每个标识一行，列明定义点、写入点、读取点、跨层贯穿情况（存储/路由/模型调用/训练记录四层）与断点。

#### Scenario: 归属表可稽

- **WHEN** 编排者四查本报告
- **THEN** 归属表 ≥7 行且每行有 文件:符号 级定义点

### Requirement: P0 假设逐条核验

H1-H5（trace_id 互链、轨迹无 event_key、到达序、幽灵前驱、三跳 join 可行性）SHALL 各有独立核验行与三元判定；三跳 join 核验 SHALL 落到"中间键查询能力是否存在"的代码事实（索引/扫描能力面），不得停留在理论推断。

#### Scenario: 推翻可溯源

- **WHEN** 任一假设被判不成立
- **THEN** 核验行含推翻证据与第一阶段原结论出处
