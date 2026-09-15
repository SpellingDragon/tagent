## Purpose

为 tagent 助手提供邮件入站渠道：外部邮件送达专用 inbox 后，系统拉取、去重、格式化为任务消息并注入 tagent 持久事件循环消费；拉取器以常驻服务形态运行，异常自恢复、状态可持久化。

## ADDED Requirements

### Requirement: 邮件拉取与去重

系统 SHALL 以固定间隔轮询 AgentMail inbox（`GET /inboxes/{inbox_id}/messages`，API key 鉴权），对每个新出现的 `message_id`（本地未见过）触发取正文（`GET .../messages/{message_id}`）；已处理过的 `message_id` SHALL 被去重状态文件跳过，同一封邮件在拉取器重启后 SHALL NOT 被重复注入。

#### Scenario: 首次收到新邮件

- **WHEN** 专用 inbox 收到一封拉取器从未见过的邮件
- **THEN** 拉取器在下一轮询周期内取回正文并注入 tagent 消息循环（POST /task 返回 202）

#### Scenario: 已处理邮件重复出现

- **WHEN** 已注入过的邮件（message_id 已在去重状态中）再次出现在列表响应里
- **THEN** 拉取器跳过该邮件，不产生第二次注入

#### Scenario: 拉取器重启后

- **WHEN** 拉取器重启并轮询，列表中含重启前已处理的邮件
- **THEN** 这些邮件被去重状态文件（持久化于磁盘）跳过，无重复注入

### Requirement: 邮件注入格式

系统 SHALL 将邮件格式化为带完整上下文的用户消息（至少含：发件人、主题、正文、时间戳），并以 user 角色经 tagent HTTPAPI 提交；注入成功（202）SHALL 记录 message_id 与注入时间到日志；HTTPAPI 不可用（连接拒绝/503 loop 未激活）时 SHALL 保留该邮件为待投递并按退避重试，SHALL NOT 丢失或标记已处理。

#### Scenario: 正常注入

- **WHEN** 一封格式化后的邮件消息提交至 POST /task 且服务返回 202
- **THEN** 消息进入 tagent 持久事件循环，拉取器日志记录 message_id、投递时间与 202 确认

#### Scenario: tagent 暂不可用

- **WHEN** 提交时连接失败或健康检查显示 loop 未激活（503）
- **THEN** 该邮件保持待投递状态，按指数退避重试直到成功；期间不标记为已处理

### Requirement: 常驻运行与自恢复