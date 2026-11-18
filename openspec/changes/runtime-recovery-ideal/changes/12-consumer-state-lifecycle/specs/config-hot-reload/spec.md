## MODIFIED Requirements

### Requirement: 执行代绑定完整性

结构热更换入新 executor 时，新代对象 MUST 完成对常驻状态的绑定后才视为成功：工具 wrapper 的 parentProjection MUST 指向常驻投影（非新壳空投影）；system prompt getter MUST 按执行代不可变快照读取；常驻 cm 的 execCfg MUST 更新为生效代配置。

被钉委派（继承发起调用租约的子跳）的验收 MUST 按**回合身份**取该跳的答案，MUST NOT 以"发布后第一条新答案"之类的顺序下标代表被测跳：发布抬起的通知回合会在新代面上产生同类答案，其到达顺序不是被测属性。

一次结构热更改变已挂载的消费者集合时，新代 SHALL 绑定同一切片名与同一状态权威：既存切片与已陈述态 MUST NOT 随代际重建，新实例 SHALL 从该权威取得它继续所需的态。移除或替换一个仍持未完义务的消费者时，该义务 SHALL 在候选生效前具名交接；找不到承接者时 MUST NOT 发布该候选，生效绑定与切片 SHALL 保持旧代。声明面 SHALL 只有一条（builtin 目录与 `plugins:` 声明同一份），MUST NOT 为不同来源的消费者建立第二套生命周语义。

#### Scenario: 热更后子 agent 自动上下文注入

- **WHEN** 结构热更成功后调用支持 event_keys 的子 agent 且未显式提供 event_keys
- **THEN** 自动上下文注入 MUST 与热更前等价（读取常驻投影而非空投影）

#### Scenario: 被钉跳的验收锚定被钉跳本身

- **WHEN** 一轮子调用在发布前被挂起，发布后通知回合与它各自产生一条同类答案
- **THEN** 验收 MUST 只取被挂起那一轮的答案来断言代际，且 MUST 在断言前确认该答案已出现；通知回合的新代答案 MUST NOT 被当作被测跳的答案，也 MUST NOT 替它达标

#### Scenario: 新代绑定同一切片权威

- **WHEN** 一次结构热更换入同一消费者的新代实例
- **THEN** 新实例 SHALL 读同一片名下的已陈述态，切片 MUST NOT 被重建或清空
- **AND** 恢复与运行期仍 SHALL 走同一派生路径，热更前后同输入的可观察结果 SHALL 相等

#### Scenario: 无承接者的移除拒绝候选

- **WHEN** 候选配置移除了一个仍持未完义务的消费者，且义务没有承接者
- **THEN** 发布 SHALL 具名失败，生效绑定、切片与旧代实例 SHALL 保持不动
- **AND** 回执 SHALL 指出未完成的义务规模与失败阶段，MUST NOT 记为已生效
