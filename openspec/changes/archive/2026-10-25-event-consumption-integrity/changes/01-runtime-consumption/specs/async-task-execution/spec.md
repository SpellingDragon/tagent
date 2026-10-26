## ADDED Requirements

### Requirement: 通知义务存活至逻辑消费确认

后台任务的通知义务 SHALL 存活到该通知被逻辑消费确认，MUST NOT 因下列任一状态提前结清：运输层接收（EventBus、邮箱、claim）、发送动作成功、任务终态（settled/completed/failed）、宿主 `Close` 返回 nil。义务是否结清 SHALL 以消费方记录的已提交事实键与消费位置为准（见 `committed-event-attribution`、`persistent-event-loop`「通知消费位置连续且不跨空洞」）。重启恢复 SHALL 重新暴露未结义务，MUST NOT 把“已发出”当作“已消费”。显式 Handoff 的改投边界沿用 `persistent-event-loop`「Handoff 未来通知的目标、实例与稳定序号」，本要求不改写。

#### Scenario: 运输接收不等于消费

- **WHEN** 通知已进入 EventBus 或邮箱并被 claim，但消费方尚未记录逻辑消费确认
- **THEN** 该义务 SHALL 保持未结清
- **AND** MUST NOT 从待办中移除或计为已完成

#### Scenario: 发送成功不等于消费

- **WHEN** 通知内容已由发送动作送达外部渠道
- **THEN** 该通知义务 SHALL 仍保持未结清直到逻辑消费确认
- **AND** MUST NOT 以发送成功作为消费完成

#### Scenario: 重启恢复未消费义务

- **WHEN** 进程重启时存在已发出但未逻辑消费的通知
- **THEN** 该义务 SHALL 在恢复后重新可见并可被消费
- **AND** MUST NOT 被当作已完成或静默丢弃
