# host-migration

## ADDED Requirements

### Requirement: 宿主按「提交即受理」判读

宿主 SHALL 把 `Host.Invoke` 返回的 `KindUnknown` 判读为「已受理、回答由交付面送达」，MUST NOT 判为失败或重复提交。

#### Scenario: KindUnknown 不重发
- **WHEN** 一次宿主调用返回 `KindUnknown`
- **THEN** 宿主 MUST NOT 重新提交同一输入，也 MUST NOT 判定为失败

#### Scenario: 就地消费仍即时投递
- **WHEN** 调用就地完成一次消费（`KindCompleted`）
- **THEN** 宿主 SHALL 按既有交付路径即时投递，且不重复投递

### Requirement: 端到端症状锚

宿主路径 SHALL 提供端到端锚：慢工具在途时第二条消息在工具放行前得到回答；第一条回答闭合后第二条不出现在已完成历史中。

#### Scenario: 慢工具期间的第二条消息
- **WHEN** 慢工具在途，用户经真实渠道发出第二条消息
- **THEN** 该消息 SHALL 在工具放行前得到回答

#### Scenario: 未消费输入不在已完成历史
- **WHEN** 第一条回答已闭合，第二条尚未被消费
- **THEN** 投影读面 SHALL NOT 把第二条列在已完成段中
