# plugin-mounting Specification

## Purpose

内置特性经插件目录挂载并由默认启动流程注册，声明面可点亮或禁用 agent 级内置，未注册名与越域名在装配期具名拒绝；装配根不出现任何特性专属构造，新增特性只登记目录一行。

## Requirements

### Requirement: 特性经 builtin 插件目录挂载，默认启动流程注册

框架内置特性 SHALL 一律经**插件目录**（一行一插件）挂载：`compress-consumer`、`final-round`、`reflection-driver`、`settle-driver`、`lease-observer`、`age-fold`、`telemetry-observer`。每项 SHALL 声明其作用域（run 级／host 级／agent 级）与构造；插件的具体类型构造 SHALL 只出现在该插件自己的挂载文件与目录一行。默认启动流程 SHALL 遍历目录完成注册（run 级装 run 实例集、host 级装宿主实例集、agent 级按声明解析），SHALL 使框架在无任何 `plugins:` 声明时开箱可用，SHALL NOT 要求部署方为内置特性补声明才生效。同名插件在目录中重复登记 SHALL 被具名拒绝。

#### Scenario: 缺省即全挂

- **WHEN** 一份文档不含任何 `plugins:` 声明，装配一次运行
- **THEN** 七项内置特性 SHALL 按目录注册齐备，run 级实例 SHALL 在 run 实例集中
- **AND** 既有部署文档 SHALL 无需改动即保持原行为（折叠、终轮、反思、结算、遥测观察照跑）

#### Scenario: 特性构造只在册内

- **WHEN** 检查装配根（`run.go`／`tools.go`／`host.go` 的装配路径）是否出现任一内置特性的具体类型构造
- **THEN** 守护门 SHALL 判红并指名该构造
- **AND** 把同一构造搬入该插件自己的挂载文件或目录一行后，守护门 SHALL 转绿

#### Scenario: 目录重名拒绝

- **WHEN** 目录登记了两个同名插件
- **THEN** 装配 SHALL 以该名字具名拒绝本次构造
- **AND** SHALL NOT 静默取其一

### Requirement: `plugins:` 声明定制内置并响亮失败

agent 声明 SHALL 经通用 `plugins:` 序列定制其插件集：缺省（无该键或空序列）SHALL 挂载全部 agent 级内置；声明项 `name` SHALL 点亮该内置，`-name` SHALL 禁用该内置；声明 SHALL NOT 具备白名单语义（一行无关声明 SHALL NOT 关掉其余内置）。宿主方言 SHALL 声明该键并把它写入 agent 规格，生产代码 SHALL 逐项迭代声明名集求值（配置包自身的编解码、克隆与指纹映射 SHALL NOT 计为消费方）。空名、目录未注册名、以及作用域属 run/host 级的内置名出现在 agent 声明中 SHALL 在装配期以该名字具名拒绝并列出可选 agent 级内置；`plugins:` 的值 SHALL 是序列，SHALL NOT 把标量当单元素接纳。

#### Scenario: 禁用内置

- **WHEN** 某 agent 声明 `plugins: ["-age-fold"]` 且其 `replay_window` 大于零
- **THEN** 该 agent 的插件管线 SHALL 不含 age fold，SHALL NOT 因缺省而继续挂载
- **AND** 其余 agent 级内置 SHALL 照缺省挂载

#### Scenario: 点亮幂等

- **WHEN** 某 agent 声明 `plugins: [age-fold]`
- **THEN** 该 agent 的插件管线 SHALL 恰含一个 age fold 实例（SHALL NOT 因声明叠加出两个）

#### Scenario: 未注册名与越域名响亮失败

- **WHEN** 声明列出一个没有任何插件注册的名字，或列出 run/host 级内置名
- **THEN** 装配 SHALL 以该名字具名拒绝本次构造，并说明其作用域或列出可选内置
- **AND** SHALL NOT 静默丢弃该项、SHALL NOT 以空实例顶替

#### Scenario: 标量声明被拒绝

- **WHEN** 文档写下 `plugins: age-fold`（标量而非序列）
- **THEN** 配置解码 SHALL 具名拒绝
- **AND** SHALL NOT 把标量当作单元素序列接纳

### Requirement: 装配根无特性硬编码

装配根 SHALL NOT 构造具体特性类型的实例、SHALL NOT 无条件把某特性实例追加进 run 级实例集或 agent 管线。装配根对插件的认识 SHALL 只经插件目录与其解析函数（转发已解析集合）表达。该不变量 SHALL 由机械门禁守护（`scripts/gates/check_plugin_mounting.py`），违反即红；判据口径 SHALL NOT 削弱以满足实现。

#### Scenario: 管线只转发已解析集合

- **WHEN** 装配根把一个特性实例挂进插件管线
- **THEN** 该成员 SHALL 来自目录解析结果（展开转发），SHALL NOT 是现场构造的单个具体成员或复合字面量
- **AND** 门禁 SHALL 对该形态 exit 0

#### Scenario: 新增特性不动装配根

- **WHEN** 新增一个实现标准端口的插件
- **THEN** 落地 SHALL 只含「插件实现」「目录一行登记」，agent 级另需「目标 agent 声明一行」
- **AND** 装配根 SHALL 零改动，SHALL NOT 需要新增特性专属解析函数或专属配置节

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
