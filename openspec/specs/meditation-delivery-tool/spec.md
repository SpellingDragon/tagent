# meditation-delivery-tool Specification

## Purpose
冥想会话的跨会话写经经唯一通信工具 `send` 表达；deliver_to 白名单、内容上界与身份不可伪造语义保留。

归档溯源：本能力由 `complete-meditation-loop` 归档建立；`send` 归并语义由 `send-cross-task-session`（03-cross-session-send）手工并入。
## Requirements
### Requirement: 投递内容有界

`content` SHALL 有上界（8KiB）；超限具名拒绝且回合继续。每次成功/被拒 SHALL 有 run 级日志可观测，MUST NOT 为此新建第二账本。

#### Scenario: 超限拒绝

- **WHEN** content 超过 8KiB
- **THEN** 工具返回具名拒绝文本，无投递发生

### Requirement: 冥想跨会话写经 send

冥想会话 SHALL 经 `send{target:{agent,session}, content}` 向 deliver_to 目标写事件；`deliver_summary` MUST NOT 出现在任何工具面。deliver_to 白名单门、盲投拒绝、目标未运行拒绝语义 SHALL 保留（复用 cross-session-delivery 四门）。

#### Scenario: 冥想经 send 投递

- **WHEN** 冥想回合调 `send{target:{agent,session}}`
- **THEN** 目标会话入链并驱动一轮；工具面无 `deliver_summary`

#### Scenario: 白名单外拒绝

- **WHEN** 冥想向 deliver_to 外目标 send
- **THEN** 具名拒绝，目标零感知

### Requirement: 身份不可伪造

投递方身份 SHALL 由装配期闭包固定（调用帧会址），MUST NOT 可被 LLM 参数指定或覆写；参数出现"来源"类字段 SHALL 按未知字段具名拒绝。

#### Scenario: 来源字段被拒

- **WHEN** LLM 在参数夹带来源字段
- **THEN** 未知字段具名拒绝，实际投递方恒为闭包捕获的身份
