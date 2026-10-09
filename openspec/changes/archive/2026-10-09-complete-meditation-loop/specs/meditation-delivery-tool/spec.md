# meditation-delivery-tool Specification

## ADDED Requirements

### Requirement: deliver 工具仅授予有回流授权的冥想 agent

`deliver` 工具 SHALL 仅在 agent 配置 `meditation.enabled` 且 `deliver_to` 非空时挂进其工具面（装配期决定）；未授予的 agent 的模型请求 MUST NOT 出现该工具声明。工具参数 SHALL 仅含 `{target, content}`——投递方身份由装配期闭包固定，MUST NOT 可被 LLM 指定或覆写。

#### Scenario: 未配白名单不见工具

- **WHEN** agent 配了 meditation 但 `deliver_to` 为空
- **THEN** 其请求的工具声明列表不含 deliver

#### Scenario: 身份不可伪造

- **WHEN** LLM 在参数里夹带任何"来源"字段
- **THEN** 参数校验按未知字段具名拒绝或忽略声明外字段，实际投递方恒为闭包捕获的宿主 agent

### Requirement: 调用时过投递缝全部安全门且拒绝以结果文本回模型

`deliver` 执行 SHALL 复用进程内投递缝的四道门（白名单/盲投/未知目标/未运行，具名错误语义不变），MUST NOT 新开注入路径；成功 SHALL 经 `InjectMessageWithSource("meditation", …)` 进入目标 mailbox（谱系/遥测自管/同批让位/不 re-arm 目标 novelty 全部继承）。一切拒绝与失败 SHALL 以工具结果文本返回（`[delivery_denied] <具名原因>` + 白名单现状提示），MUST NOT 抛错中断反思回合。

#### Scenario: 自主投递闭环

- **WHEN** 冥想回合的模型发起 deliver 调用、target 在其白名单且目标 loop 活跃
- **THEN** 卡片进入目标 mailbox，目标下一回合可见，成功文本回执目标名

#### Scenario: 白名单外目标被拒且模型可读

- **WHEN** LLM 投出白名单外的 target
- **THEN** 回合继续，工具结果为 `[delivery_denied]` 具名文本（含可用白名单），无跨 agent 写入发生

### Requirement: 投递内容有界

`content` SHALL 有上界（8KiB）；超限具名拒绝且回合继续。每次成功/被拒 SHALL 有 run 级日志可观测，MUST NOT 为此新建第二账本。

#### Scenario: 超限拒绝

- **WHEN** content 超过 8KiB
- **THEN** 工具返回具名拒绝文本，无投递发生
