# Design: 03 per-call 子 agent 覆盖层

## D1 覆盖栈形态（唯一新机制面）

调用作用域覆盖经 **invocation 携带、装配期解析、随调用消亡**实现，不引入任何全局状态：

```
委派工具 Call(args{目标请求, overrides{prompt, model, tools}})
  → 参数校验（tools ⊆ 最大工具域，越域 fail-closed）
  → overrides 序列化进 Declarative（冻结）
  → spawn/执行时 invocation 装配：overrides 压入本次调用解析上下文
  → Run 内视图装配读取：prompt/model/tools = overrides ?? generation 定义
  → 调用结束（settle/ack），invocation 消亡 = 覆盖自动弹出
```

- 实现落点：`Run` 的本地装配处（`agent/session.go` 装配链）读 invocation 携带的覆盖——与 `RuntimeState`/ExternalContextEntry 同一"调用期输入"家族，不新增传递通道类型。
- **防泄漏**：覆盖只存在于 invocation 结构内（栈=调用生命周期），无共享可写点 ⇒ 结构性防泄漏；fail-before 仍钉行为测（连续两次调用，第二次不带覆盖，断言其视图=generation 默认）。

## D2 三层解析与归属

| 面 | 解析层 | 时机 |
|---|---|---|
| system_prompt / 模型 ID / tools | overrides ?? generation 定义（fp 代际值） | 装配期一次（视图钉定语义保持——覆盖在调用期等效于"本次专用视图"） |
| 上下文预算/摘要/双 TTL（未来 knob） | 源拉取（本域不动） | 使用期 |
| 上下文内容 | 既有 RuntimeState/ExternalContextEntry | 装配期 |

覆盖只作用于**视图面**三件；参数面不提供 per-call 覆盖（防参数面爆炸——如需，未来另裁）。

## D3 Declarative 冻结与重放

- `Declarative` 增 `Overrides`（序列化结构：prompt/model/tools_subset）；relaunch/`RebuildTaskRegistry` 重放时还原同样覆盖。
- 与 `Params`（现有 kv）分工：Params 承载标量 knob（如 ttl），Overrides 承载视图三件——不混用。

## D4 治理（最大工具域）

- 空白 agent 的 org 定义声明 `max_tools`（显式清单）；调用 `tools_subset` 校验 ⊆ max_tools，越域返回结构化错误（fail-closed，不静默截断）。
- prompt 覆盖无内容审查（与文件 prompt 同信任级——调用方已是 agent 自身）；模型覆盖限定为已注册 provider/model 引用（不存在则错误）。

## 风险与回退

- 风险①：覆盖×代际热更交互——调用中文件热更发布新代：在途调用钉定其装配视图（含覆盖），与既有代际钉定语义一致，无需特判。
- 风险②：Declarative 膨胀（prompt 全文入账本）——允许引用式（文件路径/prompt_dir 引用）与内联两种，重放时同源解析。
- 回退：空白 agent 是新增 org 条目+新增参数，revert 即全撤，无存量迁移。

## 文件清单（预期）

`agent/tool_agent.go`（参数面+校验+overrides 透传）、`agent/session.go`（装配期覆盖读取）、`agent/task/task_manager.go`（Declarative.Overrides）、org 配置样例与 wiki 增补、对应测试文件 ×3。
