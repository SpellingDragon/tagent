## ADDED Requirements

### Requirement: 配置热更维度矩阵

每个子 agent 配置维度 SHALL 归属且仅归属一条通道：fp 代际面（视图变更，下回合生效、在途钉定）或源拉取面（参数变更，下一次消费即生效）。维度归属与消费点 SHALL 以矩阵形式登记（`changes/01-hotupdate-matrix-audit/matrix.md`，随域归档迁入执行注记），每行 SHALL 携带代码行号或测试名证据；**源里携带而消费点未读=假热更**，SHALL 视为缺陷而非"部分支持"。

#### Scenario: 已登记维度的消费点有契约测

- **WHEN** 矩阵中任一维度标记为"源拉取面"
- **THEN** 该维度 SHALL 存在断言"消费点读到解析值"的契约测试，测试名登记于矩阵行

#### Scenario: 新维度接入有唯一路径

- **WHEN** 后续变更新增任一配置维度
- **THEN** 该维度 SHALL 经"源加字段+消费点读"或"入 fp 子集"之一接入，SHALL NOT 新增 push/订阅/广播通道，且矩阵 SHALL 增行登记
