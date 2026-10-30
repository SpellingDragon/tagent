# config-state-twinship（受理与占用语义增补）

## ADDED Requirements

### Requirement: 提交即受理

`Host.Invoke` SHALL 先提交输入事实并即受理；受理 MUST NOT 依赖「本会话是否有在途消费」。

#### Scenario: 在途时受理
- **WHEN** 本会话已有一次消费在进行，新输入到达
- **THEN** 调用 SHALL 返回 `Turn{InvocationID: 本次身份, Terminal: KindUnknown}` 且无错误，输入已在链上

#### Scenario: 空闲时就地消费
- **WHEN** 本会话没有消费在进行
- **THEN** 本次调用 SHALL 驱动一次消费并返回其 `Turn`

### Requirement: 同会话单消费者

同一会话 SHALL 至多一个消费者推进；跨会话 SHALL 可并行。占用只是并发原语，MUST NOT 决定是否受理。

#### Scenario: 占用不拒绝受理
- **WHEN** 占用被另一消费者持有
- **THEN** 新输入仍 SHALL 被受理，只不就地驱动

### Requirement: 不移写来源身份

输入事实的 `InvocationID`、`Source`、`Owner` SHALL 保持调用者所铸；MUST NOT 因在途消费被改写成在途 range。

#### Scenario: 来源保持调用者
- **WHEN** 输入在在途消费期间提交
- **THEN** 该事实的来源身份 SHALL 等于调用者身份

### Requirement: 同步返回语义不变

`Host.Invoke` 的对外形态 SHALL 保持「提交 + 返回 Turn」；`KindUnknown` 只表示已受理、结果由后续消费产生。

#### Scenario: KindUnknown 不表示失败
- **WHEN** 调用返回 `KindUnknown`
- **THEN** 宿主 MUST NOT 将其判为失败；实际回答由消费方交付
