## MODIFIED Requirements

### Requirement: 请求边界安全快照
采集 SHALL 在**请求开始前**固定本次实际模型、生成参数与 prompt/skill/tool 可见集合及其版本；固定点 MUST 位于宿主默认参数合并之后。MUST NOT 通过调用后重跑 provider options 反推或重建快照。`Headers` 与 URL 中的 secrets MUST NOT 落盘；正文 MUST 保持原样，MUST NOT 被通用脱敏器改写。

#### Scenario: 调用后重建被拒绝
- **WHEN** 尝试以调用后重跑 provider options 的方式构造快照
- **THEN** MUST 拒绝该路径并在验收中显式失败
- **AND** 快照 MUST 取请求前固定值

#### Scenario: 默认参数先于快照生效
- **WHEN** 宿主为该 agent 声明了 temperature 或 reasoning effort
- **THEN** 快照 MUST 记录合并后的生效值
- **AND** MUST NOT 记录为未声明或空值

#### Scenario: secrets 不落盘且正文保真
- **WHEN** 请求 Header 或 URL 携带 token/密钥
- **THEN** 采集产物 MUST NOT 含该 secret
- **AND** 投影后正文 MUST 与原始状态逐字节一致

#### Scenario: 并发请求各自快照
- **WHEN** 同一进程并发发起两个不同模型/参数的请求
- **THEN** 两条快照 MUST 分别绑定各自请求前的模型与参数
- **AND** MUST NOT 互相串写

#### Scenario: 重启后快照可读回
- **WHEN** 采集进程重启后按引用读取历史快照
- **THEN** 内容 MUST 与写入时一致
- **AND** 摘要不符时 MUST 具名拒绝
