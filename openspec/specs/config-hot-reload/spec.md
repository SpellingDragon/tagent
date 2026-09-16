# config-hot-reload Specification

## Purpose
TBD - created by archiving change hardening-review-batch2. Update Purpose after archive.
## Requirements
### Requirement: 配置热更统一应用模型
配置热更 MUST 以「每次加载形成逐 agent 完整有效配置」为单位统一应用：数值参数集（compress_threshold / max_tokens / keep_recent_tasks / task_terminal_ttl / task_stale_after / task_job_deadline）与结构参数集（指纹覆盖字段）MUST 在同一次加载中非互斥地全部应用——结构重建不得跳过数值应用，数值应用不得仅覆盖 entry agent。

#### Scenario: 同次修改结构字段与数值字段
- **WHEN** 配置文件在一次保存中同时修改了 entry agent 的 system_prompt（结构）与子 agent knowledge 的 max_tokens（数值）
- **THEN** executor 重建完成后，knowledge agent 的压缩器预算线 MUST 已按新 max_tokens 生效，两处变更同批落地

#### Scenario: 仅修改子 agent 数值
- **WHEN** 只修改非 entry 子 agent 的 keep_recent_tasks 而指纹不变
- **THEN** 该子 agent 的压缩器 MUST 收到新值，entry agent 不受影响

### Requirement: 压缩参数同代快照
热更后的压缩参数 MUST 作为同一有效代快照应用：外层触发线（ContextCompressor）与内层压缩目标（SmartCompressor 的 maxTokens/triggerBudget/keepRecent 及派生容量）MUST 在一次应用中同步换装，单次压缩 MUST 读取同一快照。

#### Scenario: 运行中缩小窗口后触发压缩
- **WHEN** 运行中把 max_tokens 从 512k 调小到 128k 并随后触发压缩
- **THEN** 内层压缩目标 MUST 按新预算执行（最终请求降到新预算内），而非沿用冷构造旧值导致 no-op

### Requirement: 热更回执报告 effective 状态
每次配置热更 MUST 产出回执：逐 agent 列出实际应用字段与生效值（desired/effective）、config generation、被拒绝或保持的字段及原因；回执 MUST 反映真实生效状态而非仅「已换 runner」。

#### Scenario: 热更回执核对
- **WHEN** 任一次热更完成
- **THEN** 日志回执中每个已构建 agent 的每个热参数 MUST 可与后续实际行为（预算线日志、TTL 行为）互相印证

### Requirement: 执行代绑定完整性
结构热更换入新 executor 时，新代对象 MUST 完成对常驻状态的绑定后才视为成功：工具 wrapper 的 parentProjection MUST 指向常驻投影（非新壳空投影）；system prompt getter MUST 按执行代不可变快照读取；常驻 cm 的 execCfg MUST 更新为生效代配置。

#### Scenario: 热更后子 agent 自动上下文注入
- **WHEN** 结构热更成功后调用支持 event_keys 的子 agent 且未显式提供 event_keys
- **THEN** 自动上下文注入 MUST 与热更前等价（读取常驻投影而非空投影）

### Requirement: 首代可回滚
热更 reloader MUST 在安装时保存启动代有效配置快照；第一次结构热更成功后 Rollback MUST 可用（不得因无上一代快照而失败）。

#### Scenario: 首次热更后立即回滚
- **WHEN** 进程启动后的第一次结构热更成功，随后调用 Rollback
- **THEN** 回滚 MUST 恢复到启动代配置并再次可用，不得报「无上一代快照」

