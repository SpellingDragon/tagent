## ADDED Requirements

### Requirement: 正式入口对贡献实例采用同一接线

`Assemble`、`Boot` 与 reload SHALL 对调用者贡献的插件实例与代际注册采用同一解析与挂载规则。正式 `Boot` MUST NOT 丢弃调用者经公开 option 提供的工厂；同一贡献在任一入口生效结果 SHALL 一致，并 SHALL 有正式入口证据。

#### Scenario: 贡献经 Boot 生效

- **WHEN** 宿主用公开 `Boot` 提供一个观察插件与一个有状态消费者
- **THEN** 插件 SHALL 在真实请求链路上被调用
- **AND** 消费者状态 SHALL 在重启后恢复
- **AND** 框架代码 SHALL NOT 为该贡献新增专用分支

### Requirement: 贡献工厂顺序固定且恰好执行一次

多个贡献工厂 SHALL 按声明顺序组合；每个工厂在每次装配中 SHALL 恰好执行一次。组合语义 MUST NOT 由后一个 option 覆盖前一个工厂。工厂失败时 SHALL 释放已完成部分并具名上返，MUST NOT 留下半挂载实例。

#### Scenario: 两个工厂

- **WHEN** 调用者提供两个工厂并启动
- **THEN** 两者 SHALL 依序各执行一次
- **AND** 后一个工厂 MUST NOT 覆盖前一个

#### Scenario: 第二个工厂失败

- **WHEN** 第二个工厂返回错误
- **THEN** boot SHALL 失败并具名
- **AND** 第一个工厂已挂载的实例 SHALL 被释放

### Requirement: 换代与作用域归属不因入口改变

贡献实例的代际注册、作用域判定与卸载归属 SHALL 与 builtin 一致；禁用与替换 SHALL 指向同一集合。未注册名与跨级名 SHALL 具名拒绝，MUST NOT 静默忽略。

#### Scenario: 跨级声明

- **WHEN** agent 声明点名一个 run/host 级插件
- **THEN** 解析 SHALL 具名拒绝并指出作用域
