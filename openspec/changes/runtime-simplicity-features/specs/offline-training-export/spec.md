## ADDED Requirements

### Requirement: 导出读取落盘证据
训练导出 MUST 从持久化的请求证据与执行事实读取样本，MUST NOT 依赖进程内快照。样本 MUST 携带来源 manifest。

#### Scenario: 重启后仍可导出
- **WHEN** 完成若干调用后进程重启，再执行导出
- **THEN** 导出 MUST 产出与调用对应的样本
- **AND** 每条样本 MUST 可回溯到其请求证据与调用身份

### Requirement: 不完整样本具名拒收
样本缺少请求证据、摘要不一致、终态未知或未获授权时 MUST 被拒收并记入拒收清单，MUST NOT 以零值或推断值填补。

#### Scenario: 缺请求证据被拒收
- **WHEN** 某次调用只有结果事实而无请求证据
- **THEN** 该样本 MUST 进入拒收清单并给出具名原因
- **AND** MUST NOT 出现在导出制品中

#### Scenario: 未授权分区不读取
- **WHEN** 导出请求的范围包含未授权分区
- **THEN** 该分区 MUST NOT 被读取
- **AND** MUST 返回具名拒绝而非空结果

### Requirement: 训练数据形态
SFT 样本 MUST 按模型决策导出 prompt、completion 与工具声明，并保留仅有工具调用的 assistant 目标。DPO 样本 MUST 使用相同输入前缀与相同工具集合的候选，并携带偏好来源与版本。

#### Scenario: 工具调用目标保留
- **WHEN** 目标 assistant 消息只含工具调用而无正文
- **THEN** 导出 MUST 保留该目标
- **AND** 训练掩码 MUST 覆盖该目标

#### Scenario: 前缀不同不构成偏好对
- **WHEN** 两个候选的输入前缀或工具集合不同
- **THEN** 它们 MUST NOT 组成偏好对
- **AND** 该对 MUST 被拒收并说明原因

### Requirement: 数据可被真实消费者读取
导出的 SFT 与 DPO 数据 MUST 能被真实 tokenizer、chat template 与训练数据整理器读取，历史部分 MUST 不被计入训练目标。

#### Scenario: 历史掩码为零
- **WHEN** 使用真实 tokenizer 与 chat template 读取一条含工具往返的样本
- **THEN** 历史与工具结果位置的标签 MUST 被忽略
- **AND** 目标 assistant 位置的标签 MUST 非空

#### Scenario: 模板不支持即失败
- **WHEN** chat template 不支持目标 assistant 掩码
- **THEN** 验收 MUST 失败
- **AND** MUST NOT 以字符串位置猜测替代
