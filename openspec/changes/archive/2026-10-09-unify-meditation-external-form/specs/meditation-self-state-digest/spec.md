# meditation-self-state-digest Specification（delta）

## MODIFIED Requirements

### Requirement: digest 覆盖任务层健康与空闲时长

反思 digest 覆盖面 SHALL 为单一形态：**观察面概况为主**——各观察分区自水位以来的分谱系事件计数（非自管/自管/未知）、引用页数与水合样本数、最近非自管活动（带可解析事件键 `[hex]` 与 trigger_source）；观察面仅含自身时，概况即"自体近况"（缺省自察形态的 digest）。**自身任务层为可选段**——agent 挂有任务层时按状态计数与需关注任务简摘渲染，无任务层时省略（优雅降级）。空闲时长 SHALL 恒含。digest 确定性生成、零 LLM、不阻塞、有界渲染，且 SHALL 复用判据那一次扫描的结果（MUST NOT 为渲染二次扫链）。

#### Scenario: 概况为主

- **WHEN** 反思触发构建 digest
- **THEN** 含观察面分谱系计数与带事件键的最近非自管活动行

#### Scenario: 自察形态的近况

- **WHEN** 缺省观察面（仅自身）触发反思
- **THEN** digest 呈现自身分区自水位以来的非自管近况（新输入/告警摘要）

#### Scenario: 自身无任务不报错

- **WHEN** agent 任务层为空或未接入
- **THEN** 省略任务段，概况照常渲染

#### Scenario: 单次扫描复用

- **WHEN** digest 渲染完成
- **THEN** 本轮对事实链的查询/水合次数与判据扫描一致（无二次扫链）
