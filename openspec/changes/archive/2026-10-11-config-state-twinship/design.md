## Context

上一轮 simplify-prototype-event-pipeline 已交付事件驱动原型（State/AgentState/Apply/ReloadConfig，govern 为单回调）。用户指出两点缺口：state 不保存生效 system prompt；config 与 state 应一体双生，变更经 govern(eval/benchmark/apply-gate) 管道，机制恒真 mock。本轮不重写架构，只把"声明→生效"的派生关系收进 state，并把治理从单回调升级为显式三段管道。

## Decisions

### D1 生效 system prompt 归 state

- AgentState 增私有字段 `prompt string` 与读取方法 `SystemPrompt() string`。
- Apply 时对每个 Adds/Updates 的定义计算下一版 prompt：`resolve func(AgentConfig) string`；resolve 为 nil 时取 `def.SystemPrompt` 原文（State 可脱离 registry 使用，LoadState 后直接 Apply 不炸）。ReloadConfig 注入 `func(def) string { return buildSystem(def, registry) }`（skills 内容与 tools 行在此时固化）。
- NewAgent 的 ModelCompletion 改读 `owner.SystemPrompt()`，删除请求期 buildSystem 调用；registry.Skills 不再进入请求路径。
- agentSnapshot 增 `Prompt string`；restoreFrom 回填。恢复后的旧 prompt 在随后 ReloadConfig 时被 resolve 重算覆盖——快照保存的是"当时的生效值"，与现场一致。

### D2 StatePlan 与三段治理

```go
type StatePlan struct {
    Change  Change            // 声明面 diff（Before=当前生效定义重建，After=候选）
    Adds    []AgentConfig     // 将新建的定义（按 id 升序）
    Updates []AgentConfig     // 将更新的定义（按 id 升序）
    Removes []string          // 将回收的 owner id（升序）
    Prompts map[string]string // Adds+Updates 的下一版生效 prompt
}
type Governance struct {
    Eval      func(StatePlan) error // 评估
    Benchmark func(StatePlan) error // 基准
    ApplyGate func(StatePlan) error // 应用门
}
```

- `Apply(cfg Config, resolve func(AgentConfig) string, gov Governance) ([]string, error)`：校验结构 → 由 currentConfig 与候选 diff 构建 StatePlan → 依序 Eval→Benchmark→ApplyGate（nil 段跳过即恒真）→ 全过后提交（新建/更新 def+prompt、移除回收）。任一段 err 即返回，state 零改动。
- `ReloadConfig(path, state, registry, gov Governance)`：解析、validateRegistry、构造 resolve、调用 Apply。旧 `govern func(Change) error` 参数删除。
- 治理段收到的是同一份 StatePlan（值拷贝），拒绝理由用 error 返回；原型不实现审批等待、重试或灰度——三段同步返回。
- 被 D1/D2 取代的方案：请求期现拼 prompt（状态真源分裂）、govern 只看 Change 看不到生效面 diff（无法评估 prompt 变化）——均否决。

### D3 一体双生边界

- config 是声明面（文件里的 Config），state 是生效面（定义+派生 prompt+队列+上下文+任务+记忆）。每次显式 Reload 驱动 plan→govern→commit；同 id 换装只更新 def+prompt，现场不动。
- 本轮引入的派生运行态只有 system prompt；工具集/模型名声明即生效（仍读 def），plugins 实例仍随壳重建。未来派生态（解析后工具表、嵌入缓存等）沿同一 resolve→state 通道扩展，不另开第二管道。
- 不做：文件 watcher、自动触发治理、生产 eval/benchmark 实现、prompt 版本历史。恒真 mock 是占位契约：三段存在、依序、可注入、可拒绝。

## Risks / Trade-offs

- [恒真管道可能被误当已治理] → README/注释明示三段默认恒真，真实评估是接缝。
- [快照 prompt 与重算 prompt 短暂并存] → 恢复后以快照值为准直到下次 Reload，与"恢复现场"语义一致。

## Migration Plan

prototype 内部 BREAKING：改 Apply/ReloadConfig 签名、迁移 TestAgentGovernOnlyOnConfigChange 到三段管道、其余测试调用点改传 Governance{}（恒真）。不建兼容包装。不提交、不归档。
