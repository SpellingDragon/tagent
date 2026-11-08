## ADDED Requirements

### Requirement: 调用期间提供增量输出
主路径 SHALL 以流式方式请求模型，并在调用进行中向订阅者提供增量内容。增量 MUST 标注为临时观察，MUST NOT 被当作最终正文。

#### Scenario: 首段内容先于完成可见
- **WHEN** 一次模型调用产生多个内容片段后正常结束
- **THEN** 订阅者 MUST 在调用结束前收到至少一个增量
- **AND** 调用结束后 MUST 能读到最终结果

#### Scenario: 增量与最终不一致以最终为准
- **WHEN** provider 在聚合阶段补齐或调整内容使增量拼接与最终正文不同
- **THEN** 读取面 MUST 以最终结果为准
- **AND** MUST NOT 将增量拼接声明为最终正文

### Requirement: 增量不成为模型背压
增量分发 MUST 非阻塞。订阅者处理过慢或缓冲已满时 MUST 丢弃该订阅者的增量并计数，MUST NOT 阻塞模型调用或其他订阅者。

#### Scenario: 慢订阅者被隔离
- **WHEN** 一个订阅者停止读取增量而模型继续产生片段
- **THEN** 模型调用 MUST 继续完成
- **AND** 该订阅者的丢弃 MUST 被计数并可见

#### Scenario: 取消订阅不取消调用
- **WHEN** 订阅者取消订阅
- **THEN** 模型调用 MUST 保持进行
- **AND** 完成后结果 MUST 仍可查询

### Requirement: 唯一终态判定不变
增量 MUST NOT 参与终态判定。终态 MUST 仍由既有单一归约入口产生。

#### Scenario: 只发片段不发聚合
- **WHEN** provider 只发送 partial 片段而未提供聚合终包
- **THEN** 终态 MUST 由既有归约按既有规则判定
- **AND** MUST NOT 因收到增量而判为完成

### Requirement: 断线后读取当前状态
订阅中断后，系统 MUST 允许读取当前状态或已完成结果；MUST NOT 承诺续接原增量位置。

#### Scenario: 重连只读结果
- **WHEN** 订阅者在调用中途断开后重新查询
- **THEN** MUST 返回调用当前状态或已完成结果
- **AND** MUST NOT 重放断开期间的增量序列

### Requirement: 长流不被整体超时截断
流式调用的超时边界 MUST 按空闲或无响应时长判定，MUST NOT 以覆盖整个响应体读取的固定总时长截断正常长流。

#### Scenario: 长回答完整结束
- **WHEN** 模型持续产生片段的总时长短于空闲上限但长于旧的固定总时长
- **THEN** 调用 MUST 完整结束并返回聚合结果
- **AND** MUST NOT 因固定总时长中途失败
