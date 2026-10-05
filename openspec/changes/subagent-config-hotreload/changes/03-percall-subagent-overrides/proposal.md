# Proposal: 03 per-call 子 agent 覆盖层（percall-subagent-overrides）

## Why

用户已裁方案（上轮对话）：临时子 agent = **特殊 tool agent + 调用时指定参数**，文件真源不动、动态性全在调用参数层。现状缺口四件：system_prompt 无 per-call 覆盖、模型无 per-call 覆盖、工具子集无收窄参数、覆盖参数不入 Declarative（重放断裂）。执行通道（`Run` 本地装配、`RuntimeState`/ExternalContextEntry 上下文注入、OriginSpawner 任务化、dense/ack 双返回）全部已在——本域只加**调用作用域覆盖栈**这一个新机制面。

## What Changes

- org 定义一个**空白委派 agent**（文件真源✓）：system_prompt 为空壳模板、绑定最大工具域、无自有模型强绑定。
- 调用参数面扩三覆盖：`system_prompt_override`、`model_override`（模型 ID/引用）、`tools_subset`（只能从空白 agent 的最大工具域**收窄**——硬上界）。
- **覆盖栈**：invocation 装配时压入覆盖 → 解析链三层（generation 默认 → 热参记录 → 调用覆盖）→ 调用结束弹出；防泄漏为红线。
- Declarative 冻结：覆盖参数序列化入 `Declarative.Params`/新字段，relaunch/跨重启重放重建同样调用。
- 治理：覆盖不越过最大工具域；拒绝越域（fail-closed 400/工具错误，不静默截断）。

## 边界与依赖

- **依赖**：无（与域 01/02 正交；覆盖参数键名与摘要 knob 无交集——接口常数本域自持）。
- **被依赖方**：无。未来"冥想独立 session"（特性 2 族）可能消费覆盖栈，但非本域范围。
- **接口面**：空白 agent 的 org 配置条目；委派工具参数 schema 三新字段；`Declarative` 附加字段（向后兼容）。
- **禁止事项**：不触碰 owner 源与三禁区；覆盖栈不得实现为全局/owner 级状态（只许 invocation 作用域）；不注册任何运行时 agent 定义（文件真源不动）。

## Capabilities

### New Capabilities

- `per-call-subagent-overrides`: 见本域 specs。
