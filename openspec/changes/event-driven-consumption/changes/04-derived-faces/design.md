# 04 技术设计

## 文件清单

- `features/rl/export_chain.go`：配对判据。
- `assembly/request.go`：一次装配取一次工具/环境快照。
- `runtime/agent/loop.go` 的 `modelViewOf` / capture 快照调用改为传入同一快照（若必须在 01 白名单内改动，则由编排者协调合单）。
- 对应 tests。

## 技术要点

### 1. 导出配对

- 遍历会话事实取 `agent_output`，读其 Meta 输入键集合；
- 按输入键把 `external_input` 归到该输出，形成一行样本；
- 未被任何输出携带的输入不产样本（保持未消费）；
- `EpisodeID` 取输出事实的 episode；`Turn` 编号按 episode 内输出顺序递增（与既有语义一致）。

### 2. 请求内单快照

装配入口取一次 `state.ToolFace(binding)` 与可见技能集合，作为值传给：

- `Request.Tools` 与 `ToolOrder`；
- 环境块 `- tools/- skills` 两行；
- `modelViewOf`（capture 的 visibleTools/visibleSkills）。

禁止在装配内部再次调用 `ToolFace`/`visibleSkillsOf`。

### 3. 不引入缓存

快照只在一次装配内有效，不跨请求复用。

## 验证

- 正锚：`TestExportPairsByConsumedInputs`（跨来源合批样本配对正确）。
- 负锚：`TestUnconsumedInputProducesNoSample`。
- 消费者锚：`rl.NewChainExporter` + 正式 `Run` 读取；工具快照锚经 `assembly.Assemble` 正式路径。
- 域内：`go test ./features/rl/... ./tests/integration/learning/... ./assembly/... -count=1`。

## 风险与回退

- 导出配对改动会影响既有学习用例的样本行；逐条对齐后迁移。
- 回退：还原 `export_chain.go` 与 `request.go` 相关段。
