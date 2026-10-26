# failure-polarity delta（02-execution）

> 复用登记（非规范，不改本条语义）：失败极性透传；模型与 RL 的单终局规则与上游框架 callbacks 同构，不另立第二终局来源。
> 本 delta 只新增「模型与 RL 单终局规则、上游 callbacks 优先」；本能力既有 Requirement（进程死亡按退出码分流极性、探测不可辨超限报失败极性、kill 失败强拆报失败极性并提示逃逸、结算信号与通知携带 exit_code、纯空白载荷通知降级、工具描述引导退出码透传）由主 spec `openspec/specs/failure-polarity/spec.md` 继承（**证据映射，不改**）。

## ADDED Requirements

### Requirement: 模型与 RL 单终局规则、上游 callbacks 优先

模型入口与 RL 采集 SHALL 遵守**单一终局规则**：一次请求/回合只归约出唯一终局（`completed` / `failed` / `cancelled` / `partial-only` / `unknown` 之一）。归约 SHALL 优先采纳上游框架 callbacks 给出的终局语义（迭代器创建失败可经 error 返回、流内错误编码进 `Response.Error`），MUST NOT 在包装层另立一套与上游冲突的终局判定。`unknown` MUST 告警，MUST NOT 默认降级为 `completed`。

模型入口的任何失败（凭据校验拒绝、迭代器创建失败、流通道创建失败、返回 nil 流）SHALL 以携带错误信息的失败 `Response` 呈现于事件流，并使回合归约为 failed；MUST NOT 静默零产出而把回合归约为 completed。RL 采集消费终局时 SHALL 与模型侧取同一终局，MUST NOT 产生第二个终局来源。

#### Scenario: 单终局归约

- **WHEN** 一次请求同时出现错误响应与部分输出
- **THEN** 终局 SHALL 唯一（此处为 `failed`），不产生第二个终局事实

#### Scenario: 上游 callbacks 优先

- **WHEN** 上游框架回调已给出 failed 语义，而包装层另有宽松判定
- **THEN** 归约 SHALL 采纳上游 failed 语义，MUST NOT 覆盖为 completed

#### Scenario: unknown 不默认 completed

- **WHEN** 终局无法判定
- **THEN** 归约 SHALL 为 `unknown` 并告警，MUST NOT 记为 completed

#### Scenario: RL 与模型同终局

- **WHEN** RL 采集消费一次模型回合结果
- **THEN** 其终局 SHALL 与模型侧归约一致，不引入第二终局来源
