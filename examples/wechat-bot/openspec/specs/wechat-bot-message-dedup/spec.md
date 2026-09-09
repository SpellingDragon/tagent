# wechat-bot-message-dedup Specification

## Purpose
为 wechat-bot 消息入口提供幂等防御：同一消息（含网关重放）只被处理一次，已见消息集合跨进程重启可恢复。

## Requirements

### Requirement: 入站消息去重

wechat-bot 消息入口 SHALL 在处理任何入站消息前检查幂等键；键已存在时 SHALL 丢弃该消息（返回成功，不注入 agent、不产生回复）。

#### Scenario: 网关重放同一消息

- **WHEN** 同一消息（相同幂等键）在短窗口内到达两次
- **THEN** 仅第一次被处理，第二次被静默丢弃

#### Scenario: 进程重启后重放

- **WHEN** bot 重启后网关重放重启前的消息（游标丢失场景）
- **THEN** 因 seen 集合已持久化，重放消息被丢弃

#### Scenario: 不同消息不受影响

- **WHEN** 两条不同幂等键的消息先后到达
- **THEN** 两条均被正常处理

#### Scenario: 去重失败不阻断主链路

- **WHEN** seen 集合持久化文件读写失败
- **THEN** 降级为内存去重并记录告警，消息处理不被阻断

### Requirement: seen 集合有界

seen 集合 SHALL 有容量上限；超过上限时按淘汰策略（如插入序最旧优先）淘汰旧键，防止文件与内存无限增长。

#### Scenario: 超出容量淘汰最旧

- **WHEN** 集合达到容量上限且插入新键
- **THEN** 最旧的键被淘汰，新键加入，集合大小不超过上限

### Requirement: 使用本地 SDK 源码编译

wechat-bot example 的 go.mod SHALL 通过 replace 指令将 wechat-robot-go 依赖指向本地源码路径（/home/lighthouse/src/wechat-robot-go），确保新 SDK 能力在 example 中编译生效。

#### Scenario: replace 生效

- **WHEN** 执行 go build
- **THEN** 构建使用本地 SDK 源码，SDK 新增的 CursorStore 等能力可被 example 引用
