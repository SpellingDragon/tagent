# Design: hardening-review-batch2

## Context

一周内落地的 R1–R4 常驻连续性、投递门禁、热更 Phase 1、stale-detached 墙等能力，经 2026-09-16 独立深度审查（129 commits）确认存在跨层断链：恢复路径丢世系、结算事实错配、清理误杀、验收器放行、热更互斥分支漏字段、换代引用未重绑。共同形态是「局部动作完成」被当作「端到端能力生效」。本设计逐链修复，原则：**真源唯一、执行权归属明确、默认态保守、成功必须分层证明**。

## Goals / Non-Goals

**Goals:**
- 内部任务世系跨重启保真，投递门禁不可被「合法但错误的来源」绕过。
- 终态事实在内存/事件/WAL/反馈四端一致；迟到信号不得复活终态。
- 长驻会话不被年龄规则误杀；R3 重挂后输出与结算链路真实接通。
- stale-detached 从「无证据强判失败」改为「观测事实 + 可选 owner 终止」。
- 验收器判定可核验：确定丢失必须判失败并非零退出。
- 热更形成统一应用模型：数值与结构非互斥、逐 agent、内外参数同代、回执报告 effective。
- 恢复不静默截断；partial/truncated 显式化。
- 运维探针/邮件入口与认证集成，401 不触发误杀。

**Non-Goals:**
- S6 无 anchor 全量回放性能优化（用户豁免）。
- 新监控组件/平台；观测增强复用现有 diagnostics/日志/trajectory。
- wechat-bot openspec change 入库裁决变更。
- 撤销 R4 常驻架构本身（仅收紧执行代边界）。

## Decisions

### D1 世系持久化：Declarative 携带 Origin，恢复闭包只补执行能力
- 写侧：`task_spawned` 的 Declarative JSON 增加 `origin`（深拷贝 `Spec.Origin`：trigger_source + 来源事件键）。
- 恢复侧：registry 重建时持久化身份（ID/Kind/Key/Desc/Origin）以 WAL 为准；`rebuildClosures` 工厂返回的 spec 仅用于执行能力（runner/probe/resume），合并时**身份字段以持久层为准、执行字段以工厂为准**，工厂输出不得整体覆盖。
- 历史记录无 origin → 恢复为 `origin: unknown`；宿主投递门对 unknown 与内部来源同等扣留（保守默认，未知不得升级为可投递）。
- 替代否决：独立 `task_origin` 事件——引入第二事实源，恢复需合并两流，复杂度不配收益。

### D2 终态唯一入口 + 迟到信号 fencing
- 新增 `TaskManager.finalize(t, kind, output, err)`：唯一终态转换点——置状态（kind+err 映射 completed/failed）、settledAt、发 onSettle（语义与状态一致：failed 必带 `SettleFailed` 或 `SettleCompleted+Err`）。
- `event_bus` settle mapper：`SettleFailed` 或 `Err != nil` → `failed`（负面反馈）；显式列出识别的 Kind，未知 Kind → `unknown` 并告警，不默认 completed。
- fencing：`applyStatus`/`emitBackground` 入口检查——任务已终态（settledAt 非零）时丢弃后续 detector 信号并记 Warn（含被丢弃的 Kind），不复活、不重复结算。
- zombie/orphan/wall 全部改走 `finalize`。替代否决：逐点修补各 emit 调用——治标，仍会新增错配。

### D3 stale-detached 重设计：观测默认，终止显式
- 状态模型：超龄 detached → 新观测态 `TaskStale`（非终态）：看板标注、`finalize` 之前的一次性 Watch 通知 + result 注记 `stale-detached`；进程不动。
- 生命周期显式化：`TaskSpec.Lifetime ("job"|"service")`，默认按 Kind 推断（command/subagent→job，generic→service）；显式 Lifetime 覆盖。
- 终止策略：`task_job_deadline`（可选 duration，默认关）：job 型超 deadline → `finalize(failed, "job-deadline-exceeded")` **并**调用该任务 detector 的 `Cancel()`（owner 确认退出），一次结算。service 型永不因年龄终止。
- `detachedAt` 进 Declarative 持久化；恢复后墙/观测沿用原时间（不用恢复时间替代）。
- 移除 `7080753` 的默认 1h 强 failed 语义；`task_max_detached_age` 改名语义为 `task_stale_after`（仅观测阈值，默认 1h）。替代否决：保留强 failed 加开关——默认危险面已实证（Kind≠生命周期、无进程证据），不保留。

### D4 resident 清理：先收养，后按孤儿证据清理
- `ReattachResidentSessions` 顺序反转：全部候选先尝试收养（任务引用/owner 在册即收养并刷新 `last_adopted_at` metadata）→ 剩余无人持有且超过独立 `resident_orphan_grace`（新配置，默认 24h，自**最后收养时间**起算）才 kill。
- `SpawnedAt` 只作展示，不作清理依据；删除「重挂刷新 SpawnedAt」的失实注释。

### D5 R3 detector 消费链接通
- 唯一绑定：TaskManager 新增 `BindDetector(taskID, detector)`；重挂路径对每个恢复任务绑定 detector 并启动 monitor（AddSessionWithCallback 后显式 Start）。
- registry 重建：suspect→running 提升时若会话被跟踪，同步绑定重挂 detector。
- 跨重启 resume：会话相同→复用现有绑定；不同→原子替换（先 Cancel 旧 detector 的消费注册，再 Bind 新的），避免新旧生产端并存。
- 验收标准升级：测试必须观察到一次真实 watch/settle 信号到达 TaskManager，`tracked=true` 不再是充分证据。

### D6 验证器重写：身份配对 + 死亡前锚定 + 截尾即失败
- 配对键：`(agent, session)` + `batch_index==0` 标重启点；Before 记录 = 同身份死亡前**最后一条**请求（禁止向前搜索更易匹配的旧记录）。
- 判定：前缀完整 → EXACT；截尾（恢复请求是死亡前请求的真前缀但更短）→ CRITICAL（记录丢失条数）；膨胀/分歧起点在正文 → REVIEW；分歧仅在系统注入段（看板行等）→ 单列 `BOARD_DIFF`，不计为 EXACT 也不计为 CRITICAL；无法配对 → UNPAIRED（计入失败）。
- 退出码：`CRITICAL>0 ∨ UNPAIRED>0` → exit 1；修复 `bh[len(bh)]` 越界（先长度后取值）。
- 两个已复现反例固化为回归测试（30/100 判 EXACT 反例、90/100 截尾崩溃反例）。

### D7 热更统一应用模型
- reloader 每次成功加载 `fresh` 后：对**每个已构建 agent**（不只 entry）计算两组差集——数值集（threshold/maxTokens/keepRecent/terminalTTL/staleAfter/jobDeadline）→ `ApplyOrgHotParams`；结构集（指纹变化）→ executor 重建。两集同批执行，互不排斥。
- 压缩参数同代：`ContextCompressor.ApplyHotParams` 构建不可变 `EffectiveParams` 快照并同步内层（`SmartCompressor.ApplyParams`：maxTokens/triggerBudget/keepRecent + 派生容量一次换装）；每次压缩读同一快照。
- 回执：`[org-hotreload] applied` 日志升级为 per-agent 列出 desired/effective 字段值 + config generation；被拒绝/保持的字段明示。
- 首代快照：reloader 安装时保存启动代 cfg 进 ring，首次结构热更后 Rollback 可用。
- R4 绑定：executor 重建成功换入常驻 cm 前——新 cfg.Tools 的 wrapper `SetParentProjection(cm.projection)`；新 `SystemPromptSource` 写入 `cm.systemPromptSource` 并使 prompt getter 按执行代快照读取（in-flight 旧 turn 持旧 getter）；`cm.execCfg` 更新为生效代。

### D8 MCP 子树严格解码
- registry 读完整配置文件 → 以 `yaml.Node` 定位 `mcp_servers` 子树 → 仅对该子树 strictyaml 解码。根字段合法存在不再误判 unknown；严格校验不放松。
- HotSync 测试 fixture 改用真实形态完整配置（含 entry/agents/providers）。

### D9 恢复语义与观测
- no-anchor fallback：移除「先取 500 再过滤」——全量分页读取后过滤，投影上限仅作为内存护栏；若达护栏则 rebuild 结果标记 `partial: true` 并 Error 级日志 + diagnostics 字段（`truncated_events=N`），禁止静默。
- lostKeys 观测扩展：tail 分页失败、批量 GetEvents 错误、payload 解析失败分别计数并进汇总日志行（`pages_failed/batch_errors/payload_errors/lost_keys/took`）。

### D10 运维认证集成与状态链
- token 源：探针与 poller 从受控源读取（`TAGENT_RL_AUTH_TOKEN` 环境或 `tagent.rl.yaml`），请求带 `Authorization: Bearer`；不写入日志。
- `/healthz` 保持鉴权不豁免；探针响应分类：401→AUTH_FAIL（不触发 kill，告警）、连接拒绝→可能旧进程已退（正常等待）、200+loop_active→健康。
- restart marker 三态分离：`restart.ok` / `restart.failed` / `restart.timeout` 独立文件，done-marker 不再被失败路径复用；maintenance 丢弃 child PID 的问题记为 tasks 内修复（posix_spawn 返回的真实 PID 写 pidfile）。
- HTTP Server owner 化（wechat-bot main）：`http.Server` 由宿主持有，重试循环受 ctx 取消，SIGTERM 走 `Shutdown` 等待；重试间隔保持线性递增但更名不再称指数退避。

## Risks / Trade-offs

- **验证器口径收紧**：远端历史 EXACT/FOLD 数字与新口径不可比，需在部署通报中说明（预期变化，非回归）。
- **stale 默认不终止**：假活会继续占资源直到宿主配置 deadline——换取「不误杀无证据进程」的安全默认；观测告警保证可见。
- **D5 绑定重构触碰 resume 热路径**：以现有 resume 测试 + 新增真实信号验收兜底；分两步落地（先接线后清理旧路径）。
- **热更统一应用改动面大**（tagent.go reloader 核心分支）：混合字段生效测试（同次改结构+数值、只改子 agent 数值、移除字段）作为回归门。
- **`task_max_detached_age` 改名 `task_stale_after`**：yaml 兼容——旧 key 读取时 Warn 并按新语义映射（观测阈值），不破坏启动。
