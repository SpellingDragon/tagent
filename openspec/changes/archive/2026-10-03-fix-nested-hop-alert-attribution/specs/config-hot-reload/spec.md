## MODIFIED Requirements

### Requirement: 执行代绑定完整性
结构热更换入新 executor 时，新代对象 MUST 完成对常驻状态的绑定后才视为成功：工具 wrapper 的 parentProjection MUST 指向常驻投影（非新壳空投影）；system prompt getter MUST 按执行代不可变快照读取；常驻 cm 的 execCfg MUST 更新为生效代配置。

被钉委派（继承发起调用租约的子跳）的验收 MUST 按**回合身份**取该跳的答案，MUST NOT 以"发布后第一条新答案"之类的顺序下标代表被测跳：发布抬起的通知回合会在新代面上产生同类答案，其到达顺序不是被测属性。

#### Scenario: 热更后子 agent 自动上下文注入
- **WHEN** 结构热更成功后调用支持 event_keys 的子 agent 且未显式提供 event_keys
- **THEN** 自动上下文注入 MUST 与热更前等价（读取常驻投影而非空投影）

#### Scenario: 被钉跳的验收锚定被钉跳本身
- **WHEN** 一轮子调用在发布前被挂起，发布后通知回合与它各自产生一条同类答案
- **THEN** 验收 MUST 只取被挂起那一轮的答案来断言代际，且 MUST 在断言前确认该答案已出现；通知回合的新代答案 MUST NOT 被当作被测跳的答案，也 MUST NOT 替它达标
