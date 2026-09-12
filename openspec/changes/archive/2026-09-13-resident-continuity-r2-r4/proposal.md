## Why

resident-continuity-roadmap 的 R2（任务重启接续）/R3（超长运行+异步/交互命令）/R4（配置全量**非重启**热更）尚未实施。用户裁决（2026-09-12）：**一次规划、一次实现**——R2+R3+R4 收进单变更（偏离 roadmap D6「每阶段独立子变更」，roadmap 已记录此合并；缓解=任务分节+节间门禁）。

R1（event-sourced-projection）已落地归档（b09b6f2），确立「状态=事实链旁路产物+事件回放重建+外置 cm」立范模式。R2/R3 是同一模式在任务/常驻两层的延伸；R4 是「状态⊥执行器」轴的另一面（换执行器、状态原封流过）。

**本轮新核实的代码事实（补 roadmap 验证）**：
- R2：`TaskSpec`（agent/task/task_manager.go:93-124）的 `Relaunch/ResumeFn/Alive` 是**三个闭包**（不可序列化）、`Origin map[string]string` 已有；registry 主 spec 明文「纯内存、SHALL NOT 跨进程重启持久化」（task-registry-and-board:17）——本变更定向修正；task 层**无 spawn 事件**（仅 settle 经 OnSettle→SourceTask→persistBusEvent），重建数据源缺失。
- R3：`ResidentMeta`（resident_recovery.go:29-39）缺 `Command/Origin/TaskID`；枚举死代码坐实（`NamedSessionName="n-"+logical` tmux_executor.go:351 vs `ListSessions` 只收 `te.prefix`（默认 "tagent"）:479-481，交集恒空）。
- R4：`org_hotreload.go` 已有 **fingerprint 白名单**（computeOrgFingerprint SHA-256：Model/Provider/PromptDir/SystemPrompt/Tools/…：111-135；memory.* 黑名单须重启）与 **orgSnapshot/builtAgent 类型声明（D1 原子快照/D2 drain-free/D3 指纹/D4 fail-closed）但无任何消费者**（incremental B 画饼）；其引用的 wechat-bot design.md **不存在**（悬空引用）——R4-B 按其注释意图实现。

## What Changes

**R2 任务连续**（能力 `task-registry-and-board` MOD）：
- 新增 `task_spawned` 一等事件（正 key，载完整 Declarative 含 StartedAt）；**inline settle（窗口内完成）补发终态记录** + settle 事件结构化 `Metadata[task_id]/[settle_status]`（沿用既有 external_input+subtype 形态，不新增 settle 类型）→ **registry=事实链 fold**（运行期内存维护、重启纯全量回放重建 active 态）；看板仍每轮重渲染（live 快照语义不变）。
- **TaskSpec 声明式化（按 Kind 拆分承诺）**：command=Relaunch/Resume/Alive 全可用（Resume 跨重启由 R3 重挂供能）；**subagent=Relaunch 可用、跨重启 Resume 不承诺**（rounds 轮次链无事件源，返回「请 relaunch」引导）；generic=仅展示。Params 枚举 ActionArgs 全 spawn 字段集（含 Timeout/ProbeFailures，未知拒绝）。
- **TaskManager 提升 org 级单例**（非「外提到 cm/TagentAgent」——TagentAgent 每代新实例正是换代丢 registry 病因）：build 路径构造一次、注入 cm/meditationMgr/ActionTool，跨执行器代共享（R4 无损热换前提）。

**R3 常驻/异步连续**（新能力 `resident-session-continuity`）：
- ResidentMeta 补 `Command/Origin/TaskID` → 会话生命周期入事实链（spawn/参数/状态）；`resident_meta_dir` 可配（默认 /tmp）。
- **修枚举死代码+orphan 语义重定义**：ListSessions 双条件（prefix ∨ n-）；**CleanupOrphanSessions 排除 n-**（orphan=仅无主生成名会话——否则 cleanup 先于 reattach 执行会屠杀全部常驻会话）。
- 存活探测三态化（list-sessions 替代 has-session：dead/unknown 同 exit1 不可辨）；monitor 层 err→assume-dead 屠杀路径加闸（err=unknown 计数于 TmuxSession，连续 N 才 dead）。
- 启动重挂**唯一挂载点**（仅 entry agent，非 per-agent）接 build_agent（R1 rebuild→R2 registry→R3 reattach 序）；在途 tmux 任务跨重启 resume 由重挂供能。

**R4 非重启全量热更**（新能力 `swappable-executor`，**cm.runner 级换代**）：
- **A 行为面固化**：既有细粒度热同步（SwappableModel/MCP registry/prompt.Source/ApplyOrgParams+orgReloader 懒检查）收敛统一触发与观测；懒检查重排：**mtime 变→先 canonical diff memory 段（命中=拒绝+须重启）→再 fingerprint 比对**。
- **B 结构换缝（cm.runner 级 SwapExecutor，非整代换入）**：cm 增 RWMutex 守护的 runner 缝，`RunFlow` per-turn RLock 取引用（in-flight turn 用旧 runner=drain-free turn 级）；`Reload`=fingerprint 变→仅重建执行器装配（fwAgent/runner/tools/prompt，按 ownership 表复用进程级共享物、不重复 RegisterCloser/AddChannel）→**失败 fail-closed（旧 runner 原样）**→成功 SwapExecutor+代际日志；**TagentAgent/loop/bus/cm/projection/TaskManager/monitor 常驻不换、宿主入口零变化**（orgSnapshot 整代换入已被第六轮 fresh-eyes 证伪：常驻 loop 形态下无 drain-free 语义可立）。
- **C 可逆治理**：热换记入版本化日志（代次+fingerprint），ring 2 上一代配置+Rollback；与 evolution 体系衔接。
- 中途换工具集安全（事件溯源史=不可变事实，R1 红利）；CONFIRM 前置：provider 对「历史引用已移除工具」容忍实测。

## Capabilities

### New Capabilities
- `resident-session-continuity`: 常驻/交互 tmux 会话的跨重启连续——元数据完备（command/origin/task_id）入事实链、枚举修复+orphan 重定义（cleanup 排除 n-）、三态探测、fail-dead 加闸、唯一挂载点重挂、在途任务 resume 供能。
- `swappable-executor`: 执行器=配置可重导出的无状态函数——懒检查先序（memory 拒绝/fingerprint 检测）、**cm.runner 级 SwapExecutor**（build-validate-then-swap、fail-closed、drain-free turn 级、ownership 表）、代际日志与回滚。

### Modified Capabilities
- `task-registry-and-board`: registry 从「纯内存禁持久化」反转为「事实链 fold+重启回放重建」（含 inline settle 终态记录+结构化 Metadata）；TaskSpec 声明式化按 Kind 拆分承诺；TaskManager org 级单例；幂等 spawn/看板每轮重渲染/age-out 语义不变。

## Impact

- **代码**：`agent/task/`（Declarative+工厂+OnSpawn+inline 补发+RebuildTaskRegistry）、`agent/context_manager.go`（persistBusEvent settle Metadata 拷入+**runner 可换缝 SwapExecutor**）、`agent/agent.go`/`build_agent.go`（TaskManager org 级单例装配+重挂接线+Reload 触发）、`tool/action/`（ActionTool 注入化+ResidentMeta 补全+resident_meta_dir）、`tool/action/tmux_executor.go`（双条件枚举+**cleanup 排除 n-**+三态探测）、`tool/action/tmux_monitor.go`（unknown 加闸）、`org_hotreload.go`（Reload+ownership 表；orgSnapshot 类型重定义或删除）、`event/registry.go`（task_spawned/resident_session 新类型）。
- **不变量**（resident-continuity-roadmap specs）：状态外置（TaskManager org 级）/事件溯源模式（R2 registry=纯全量回放，无 compaction snapshot）/执行器可换（cm.runner 缝，org 基础设施常驻）。
- **风险**：单变更体量大→三节+节间门禁；R2 org 级单例迁移动作面（task wiring 回归，契约测试先行）；R4 换代粒度=cm 级（多 agent org 非 entry 不随换代——已知边界）；subagent 跨重启 resume 不承诺（引导文案兜底）。
- **分支**：dev-resident-bugfixes（R1 已在此落地）。
