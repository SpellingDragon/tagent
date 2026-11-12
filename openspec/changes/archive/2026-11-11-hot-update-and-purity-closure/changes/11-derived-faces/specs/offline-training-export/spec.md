# offline-training-export（配对判据增补）

## ADDED Requirements

### Requirement: 训练导出按消费关系配对

训练样本的输入与输出 SHALL 按「输出的消费键集合」配对，MUST NOT 按 `InvocationID` 分组；未被任何输出携带的输入 MUST NOT 产出样本。

#### Scenario: 跨来源合批样本配对
- **WHEN** 一次回答携带了两个不同来源的输入
- **THEN** 该样本 SHALL 含这两条输入，且各自的来源字段保留

#### Scenario: 未消费输入不出样本
- **WHEN** 一条输入未被任何输出携带
- **THEN** 导出 MUST NOT 为它产出样本；它仍留在进行中段

### Requirement: 一次装配一次快照

同一请求装配中的工具表、技能集与环境清单 SHALL 取自同一次快照，并复用给 `Request.Tools`、`ToolOrder`、环境块与 capture 视图。

#### Scenario: 热更窗口内一致
- **WHEN** 装配期间发生发布（工具/技能变更）
- **THEN** 本次请求的工具表、环境清单与 capture 视图 SHALL 全部来自装配开始时的那一代

#### Scenario: 不跨请求缓存
- **WHEN** 下一次请求开始装配
- **THEN** 它 SHALL 重新取一次快照，不得复用上一请求的快照
