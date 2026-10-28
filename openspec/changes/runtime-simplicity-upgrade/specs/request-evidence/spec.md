## ADDED Requirements

### Requirement: 请求证据覆盖生效参数
请求证据 SHALL 记录宿主默认参数合并之后交给 provider 的请求口径：模型、生成参数、消息、工具声明、可见 prompt/skill 版本。证据 MUST 在请求发起前固定。

#### Scenario: 默认参数进入证据
- **WHEN** 宿主声明 temperature、thinking 与 reasoning effort 而请求自身未命名
- **THEN** 请求证据 MUST 记录这三个生效值
- **AND** provider 收到的请求 MUST 与证据一致

#### Scenario: 未声明参数不虚构
- **WHEN** 宿主与请求都未声明某个生成参数
- **THEN** 请求证据 MUST NOT 为该参数写入默认值
- **AND** MUST 记录该参数未声明的事实

#### Scenario: 快照后修改不影响证据
- **WHEN** 请求证据固定后调用方继续修改原请求对象
- **THEN** 已固定证据 MUST 保持不变

### Requirement: 请求证据持久可读
请求证据 SHALL 持久保存并可按引用读回；重启后读取 MUST 校验内容摘要。

#### Scenario: 重启后读回
- **WHEN** 进程在提交请求证据后重启
- **THEN** 按引用读取 MUST 返回与写入时一致的内容
- **AND** 摘要不一致时 MUST 具名拒绝

#### Scenario: 采集不阻断模型
- **WHEN** 证据落盘队列已满或写入失败
- **THEN** 模型调用 MUST 继续
- **AND** 该次损失 MUST 进入可查询的损失账目

### Requirement: 记录口径不冒充
请求证据 MUST NOT 声明自己等同于 provider 序列化请求体或服务端实际采样。

#### Scenario: 拒绝 wire 级声明
- **WHEN** 读取方将请求证据标注为 provider wire 请求体
- **THEN** 系统 MUST 在读取面标明其口径为运行时请求快照
- **AND** MUST NOT 提供声称可验证 provider 内部改写的字段

