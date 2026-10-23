## Context

上一轮 config-state-twinship 交付了 StatePlan/三段管道/生效 prompt。本轮是设计评审八条裁决+三点修正（pull 偶然保留、管线关系找 state、中间输出=内部输入事件）的落地，全部为 prototype 内部语义收敛，不引入生产机制。

## Decisions

### D1 演进命名（改名，不改语义）

- `Governance` → `Evolution`（contracts.go），`runGovernance` → `runEvolution`，所有引用点（state/config/两测试/README）同步。
- 三段 Eval→Benchmark→ApplyGate 与拒绝零改动语义原样保留。
- 运行时拦截不属于 Evolution：由治理插件承担（见 D4）。

### D2 中间输出与 Reply 的 final 语义

- contracts.go 事件常量增 `EventModelDelta`（模型中间输出：带工具调用时的 Output 与 Reasoning）。
- invokeModel：`len(ToolCalls) > 0` 时——Output/Reasoning 非空则各 publish 一条 EventModelDelta（Source="model"），随后 publish ToolCall 事件；**不发 Reply**。`len(ToolCalls) == 0` 且 Output 非空才 publish Reply。
- 核心消费：EventModelDelta 进上下文投影，不 driving、不 Deliver（与归零前原型一致；这是对被删除行为的恢复而非新设计）。
- 子 agent 收口无需特判：Deliver 只挂在 Reply 路由上，进 Deliver 的必是无工具调用的 final。

### D3 投递任务化与管线关系单点

- EventReply 路由不再直调 Deliver：`owner.Tasks.Spawn(ToolCall{Name: "deliver", Args: e.Data}, run, e.Source)`，run 即装配方注入的 Deliver 闭包（签名 func(ctx, string) (string, error)，接收 Data 为参数——与 ToolFunc 同形，天然任务化）。
- 投递成功/失败成为 ToolResult 通知事件回管线（进上下文、驱动模型），Run 循环不再因投递失败终止。BaseTAgent.Deliver 字段类型改为 ToolFunc 形态。
- TaskManager 的 emit func(Event) 闭包替换为 owner *AgentState 字段：通知一律经 owner.Publish 投回所属管线（"找 state 要"）；NewTaskManager 签名改为 (owner *AgentState, window int)。任务与管线的映射只有 state 一份真值。
- 重投循环（模型见投递失败又发 Reply）是模型决策域风险：注释标注，原型不做输出通道治理；集成测试用确定 mock 演示"投递失败→模型收到通知→放弃"的收敛路径。

### D4 可选聚合窗与治理插件

- AgentConfig 增 `TaskWindow int`（0=即发）。编译：newAgentState 时传入 TaskManager。窗口语义=条数窗：完成通知攒满 window 条批量投递，Close 前 flush 残余；无定时器。窗口只是任务侧可选攒批，**模型触发粒度仍由 pull 自然聚合决定**——窗口不等待输入、不控制整体节奏（裁决：TaskManager 不可能也不试图全部控制住）。
- plugins.go 增 `DenylistPlugin(owner *AgentState, names ...string) Plugin`：Kinds=[EventToolCall]，Before 把命中名单的调用变换为 `Event{Type: EventToolResult, Status: "denied", Error: "denied by governance plugin"}`（保留 Call/Data）——核心按 Before 之后的 Type 路由，走 ToolResult 路径进上下文并驱动模型，模型读到拒绝事实；未命中原样放行由核心 spawn。HITL=同变换内先问人（原型注释接缝，不实现）。
- 被否方案：投递失败杀循环（不对称）、emit 闭包副本（关系扩散）、时间窗定时器（原型复杂化）。

## 验收与取舍

- 集成场景：①投递失败后循环存活且模型收到 denied/failed 通知并收敛作答；②带工具调用的中间输出进上下文、不进 Deliver、子 agent 不误收口；③DenylistPlugin 拦截后工具零执行、模型读到拒绝；④TaskWindow=2 时两完成合并到达（一次 Next 收两条）、窗口 0 即发；⑤Evolution 拒绝零改动回归保持；⑥既有恢复/热更场景不回退。
- 不做：HITL 实现、时间窗、输出通道治理、投递重试、生产 eval/benchmark。
- 验证命令：`GOPROXY=off go test ./prototype -count=10 -timeout=90s` 与 `-race -count=10 -timeout=180s`。

## D5 合并与 DoD

单代理串行改 prototype（改动跨五文件强耦合，不再拆域）；wiki 标注独立代理并行（仅 docs/wiki/README.md）。主代理复验后勾 F 项；不提交、不归档；Governance 残留 grep 清零。
