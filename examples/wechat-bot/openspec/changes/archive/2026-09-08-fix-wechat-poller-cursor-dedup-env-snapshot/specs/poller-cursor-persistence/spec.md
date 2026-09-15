## Purpose

让 WeChat 长轮询游标在进程重启后可恢复，使 SDK 在配置了游标存储时不再从零开始拉取消息；未配置时行为与现状完全一致。

## ADDED Requirements

### Requirement: Poller 游标持久化

系统 SHALL 提供游标存储接口，使长轮询游标（getUpdatesBuf）在配置了存储时随每次更新落盘、进程重启后自动恢复，从而避免重启后从头拉取导致的历史消息重放。

#### Scenario: 构造时恢复游标

- **WHEN** Poller 创建且游标存储中存在已保存的游标
- **THEN** Poller 以该游标发起首次 getupdates 请求，不从头拉取

#### Scenario: 游标更新时落盘

- **WHEN** Poller 收到响应且响应的 get_updates_buf 非空
- **THEN** 内存游标更新后，新游标 SHALL 被写入存储；写失败仅记录告警，不中断轮询

#### Scenario: 未配置存储时向后兼容

- **WHEN** 未注入任何游标存储
- **THEN** Poller 行为与改动前完全一致（游标仅存内存，重启即丢），既有调用方零改动

#### Scenario: 存储读取失败降级

- **WHEN** 构造时游标存储读取失败（如文件损坏）
- **THEN** Poller SHALL 以空游标启动并记录告警，不因存储故障拒绝启动

### Requirement: 文件游标存储

系统 SHALL 提供基于文件的游标存储实现，读写单个游标字符串。

#### Scenario: 保存后可读回

- **WHEN** Save 游标后立即 Get
- **THEN** 返回与保存值相同的游标

#### Scenario: 文件不存在时返回空游标

- **WHEN** Get 时存储文件不存在
- **THEN** 返回空字符串且无错误

#### Scenario: 并发安全

- **WHEN** 多个 goroutine 同时调用 Save
- **THEN** 不发生数据竞争（go test -race 通过），文件内容为某次完整写入的值
