# per-call-subagent-overrides Specification

## Purpose

空白委派 agent 的调用作用域覆盖层：三层解析（generation 默认 → 热参记录 → 调用覆盖）、最大工具域硬上界、Declarative 冻结重放、覆盖不跨调用泄漏。文件真源不动，动态性全在调用参数层。

## Requirements

### Requirement: 调用时覆盖子 agent 视图

空白委派 agent 的调用参数 SHALL 支持三覆盖：`system_prompt_override`、`model_override`（已注册模型引用）、`tools_subset`。覆盖 SHALL 仅存在于 invocation 作用域（装配期解析、随调用消亡），SHALL NOT 泄漏到后续调用；无覆盖的调用 SHALL 使用 generation 定义视图。调用参数 SHALL 另暴露 `context_refs`（调用期上下文输入），并 SHALL 经既有 `RuntimeState`/ExternalContextEntry 通道注入，SHALL NOT 新建传递面。

#### Scenario: 覆盖生效与不泄漏

- **WHEN** 调用 A 携带三覆盖、紧随的调用 B 不携带
- **THEN** A 的执行视图为覆盖值，B 的执行视图为 generation 定义值

#### Scenario: 在途覆盖跨代际钉定

- **WHEN** 覆盖调用在途时文件热更发布新代
- **THEN** 在途调用保持其装配视图（覆盖不被新代冲掉），下一次无覆盖调用使用新代定义

### Requirement: 最大工具域硬上界

空白 agent 的 org 定义 SHALL 声明显式最大工具域；调用 `tools_subset` SHALL 是该域的子集，越域请求 SHALL 收到结构化错误（fail-closed），SHALL NOT 被静默截断或放行；模型覆盖 SHALL 限定为已注册引用。

#### Scenario: 越域拒绝

- **WHEN** `tools_subset` 含最大工具域之外的条目
- **THEN** 调用返回结构化错误且不产生任何执行

### Requirement: 覆盖冻结与重放

调用覆盖 SHALL 序列化入 `Declarative.Overrides`（与承载标量 knob 的 `Declarative.Params` 分工不混）；relaunch 与跨重启 `RebuildTaskRegistry` 重放 SHALL 以该记录为唯一视图来源还原同样的覆盖（重放一致）；覆盖支持内联与引用两种形态，重放时同源解析。

#### Scenario: 跨重启重放一致

- **WHEN** 携带覆盖的任务跨重启后 relaunch
- **THEN** 重放调用的执行视图与原调用逐位一致
