# per-call-subagent-overrides delta（01-state-binding）

> 复用登记（非规范，不改本条语义）：覆盖解析出的模型/工具为 state 借出的**同代**原生 `model.Model`/`tool.Tool`；不新建传递面、不建影子协议。
> 本 delta 只覆盖「调用时覆盖子 agent 视图」；本能力其余义务（最大工具域硬上界、覆盖冻结与重放）由主 spec `openspec/specs/per-call-subagent-overrides/spec.md` 继承（**证据映射，不改**）。

## MODIFIED Requirements

### Requirement: 调用时覆盖子 agent 视图

空白委派 agent 的调用参数 SHALL 支持三覆盖：`system_prompt_override`、`model_override`（已注册模型引用）、`tools_subset`。覆盖 SHALL 仅存在于 invocation 作用域（装配期解析、随调用消亡），SHALL NOT 泄漏到后续调用；无覆盖的调用 SHALL 使用 generation 定义视图。调用参数 SHALL 另暴露 `context_refs`（调用期上下文输入），并 SHALL 经既有 `RuntimeState`/ExternalContextEntry 通道注入，SHALL NOT 新建传递面。

覆盖 SHALL 在**发起调用所借出的同代执行绑定**上解析（state 按代借出，见 `config-state-twinship`「候选活绑定准备与同代借用」）；`model_override` SHALL 限定为该代已注册模型引用，`tools_subset` SHALL 限定在该代 agent 的声明工具域内。覆盖产生的执行视图 MUST NOT 直接从全局最新绑定解析，也 MUST NOT 泄漏到同一 session 的其他 invocation。

#### Scenario: 覆盖生效与不泄漏

- **WHEN** 调用 A 携带三覆盖、紧随的调用 B 不携带
- **THEN** A 的执行视图为覆盖值，B 的执行视图为 generation 定义值

#### Scenario: 在途覆盖跨代际钉定

- **WHEN** 覆盖调用在途时文件热更发布新代
- **THEN** 在途调用保持其装配视图（覆盖不被新代冲掉），下一次无覆盖调用使用新代定义

#### Scenario: 覆盖在同代绑定上解析

- **WHEN** 调用携带 `model_override`/`tools_subset`，且期间发生了新代发布
- **THEN** 覆盖 SHALL 在发起时借出的同代绑定上解析；MUST NOT 改为在最新代上解析或跨代取模型/工具

#### Scenario: 覆盖不跨 invocation 泄漏

- **WHEN** 同一 session 内两个不同 invocation 并发，其中一个携带覆盖
- **THEN** 覆盖 SHALL 只作用于该 invocation，另一 invocation 的视图 SHALL 不受影响
