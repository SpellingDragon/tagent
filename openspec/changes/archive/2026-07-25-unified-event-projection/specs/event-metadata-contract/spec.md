## ADDED Requirements

### Requirement: 元数据 key 统一定义

事件元数据（存储标识 `event_key`/`partition_id`/`event_type`/`event_summary`、路由来源 `trigger_source`、透传 `meta_` 前缀）的 key SHALL 在框架（agent 包）中以常量单点定义；所有注入点与解析点 SHALL 引用该唯一来源，SHALL NOT 各自硬编码字符串。

#### Scenario: 全仓键引用同源

- **WHEN** 审计框架内所有写入或读取上述元数据 key 的位置
- **THEN** 它们 SHALL 全部引用统一定义的常量

### Requirement: 注入点职责归一

框架 SHALL 在固定注入点写入元数据：存储标识由事件插件管线在存储时写入；`trigger_source` 由 RunFlow 入口按 invocation 设置并传播到该 invocation 的全部派生事件；`meta_*` 透传元数据由框架在事件投递时统一传播。每个投递到消费端的事件 SHALL 携带完整的存储标识与路由来源。

#### Scenario: 投递事件元数据完整

- **WHEN** 消费端从 outputCh 收到任意携带 Response 的事件
- **THEN** 该事件 SHALL 携带 `trigger_source`
- **AND** 若该事件已被存储，则 SHALL 同时携带 `event_key`/`partition_id`/`event_type`

### Requirement: 元数据解析 API

框架 SHALL 提供类型化解析 API（如 `ParseEventMeta` 与路由助手），消费端 SHALL 通过该 API 解析元数据，SHALL NOT 依赖未在契约中定义的字符串键。

#### Scenario: 消费端经 API 取路由与标识

- **WHEN** example/消费端需要获取事件的 trigger_source、chat_id、event_key
- **THEN** 其 SHALL 通过框架解析 API 获得，且解析结果与注入值一致
