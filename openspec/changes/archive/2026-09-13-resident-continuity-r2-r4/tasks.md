> 节间门禁（design D4）：每节完成即 `go build ./...` + 节内测试绿 + 勾选，才进下节。节序 R2→R3→R4。
> 5.1 已过第六轮 fresh-eyes（5🔴+4🟠+5🟡 全折进 design/specs/tasks 本版）；第七轮快核后放行编码。

## 5. 门禁（前置）

- [x] 5.1 fresh-eyes — 第六轮（2026-09-12）抓 5🔴（orphan 屠杀 n-/subagent rounds 不可声明式化/inline settle 无事件+settle 不可辨/orgSnapshot 整代冲突常驻 loop/memory 检测不可达）+4🟠+5🟡，全部折进 artifact；第七轮快核见 5.1b
- [x] 5.1b 第七轮快核 — 设计层 10/10 PASS；同步层 4 MUST FIX（proposal 整篇旧案/LEDGER 行/roadmap tasks:35/Params 缺 Timeout+ProbeFailures）+4 CONSIDER（SwapExecutor 签名统一/OQ 指代/settle 类型澄清=沿用 external_input+subtype 不新增/「旧代」措辞）全部修毕；**裁决：放行编码**
- [x] 5.2 CONFIRM — **DEGRADED 决议**（无真实 provider key 环境，不盲发付费调用）：设计分析=OpenAI 兼容协议 tools 声明约束「可调用集」非「历史引用集」，历史 tool_call/tool_result 配对完整即合法（Anthropic 同理）→风险低；防御措施=Reload 移除工具时若其存在**未折叠**历史引用记 WARN+代际日志标注（文档化约束：移除工具建议先折叠）；真实 provider 验证留待用户 dogfood 环境（wechat-bot 真 key）回归 3.7① 的 live 变体

## 1. R2 任务连续（registry=事实链 fold + 承诺表 + org 级 TaskManager）

### 1a. 契约先行与声明式化

- [x] 1.1 契约测试先行：幂等 spawn/board 渲染/age-out/进程内 relaunch/resume/alive-detached 语义快照（重构护栏）——**既有套件即契约快照**（task_manager/task_resume/board/alive_detached/zombie 全绿），新护栏由 1.7/1.11 fail-before 承担
- [x] 1.2 `agent/task/`：TaskSpec 增 `Declarative{...}`；Params 枚举 ActionArgs 全 spawn 字段（含 ProbeFailures/Timeout，未知拒绝；排除 Op/Keys/Enter/Tail/Ansi/GraceSec/SessionID）
- [x] 1.3 `RebuildClosures(decl, deps)` 工厂按 **承诺表**：tool/action SpecFromDeclarative（command）/SubagentSpecFromDeclarative（subagent：RedispatchAsync 重投递+Resume 引导）+ agent.SubagentRedispatcher 镜像 detector 形状；进程内直传闭包路径不变

### 1b. 事件（修第六轮 🔴3 双缺口）

- [x] 1.4 `event/registry.go`：新增 `task_spawned` 一等类型；inline settle 补发终态记录（external_input+subtype 沿用不新增类型，task_inline_record 标记）
- [x] 1.5 `persistBusEvent`（Source==task）：task_id/settle_status/task_inline_record 拷入 FullEvent.Metadata
- [x] 1.6 TaskManager 增 `OnSpawn`/`OnInlineSettle` hooks（Spawn 注册后/窗口内结算两调用点）+ agent 侧 late-bind taskRecordSink（onEventRef 同款模式）+ cm EmitTask*记录助手（记录-only 不发 bus 不进投影）+ 投影重建/replay 跳过守卫（task_spawned 类型+inline 标记）
- [x] 1.7 回归：TestTaskManager_InlineSettleEmitsRecordHook（fail-before：历史上 inline 无记录→ghost）/TestEmitTaskSpawnedRecord_Shape（含 StartedAt 回填）/TestEmitTaskInlineSettleRecord_Shape/TestPersistBusEvent_TaskMetadataCopied；NoOriginSafe 断言更新（task_id 为 R2 结构化键）

### 1c. org 级 TaskManager 与重建

- [x] 1.8 **TaskManager org 级单例**：D3 裁决（TagentAgent 常驻）下现有挂点（ta.taskManager，agent.go:91/:438-446）已即 org 级——跨执行器代存续，无迁移动作；meditationMgr 注入保持
- [x] 1.9 `RebuildTaskRegistry`（agent/task_record_sink.go）+ `RestoreTask`（幂等/无 watch/running→suspect/alive-detached 原态/windowClosed+aliveDetached 语义）
- [x] 1.10 build_agent 接线：R1 rebuild 之后（ta.RebuildTaskRegistryFromWAL）；command→actionTool.SpecFromDeclarative(tm)/subagent→SubagentRedispatcher(localWrappers, tm)；buildAgent/buildToolFromRef/buildAgentToolRef 变参收集 wrapper
- [x] 1.11 回归（fail-before/pass-after）：ActiveStates（2 running→suspect+1 alive-detached、inline-settled 不重建=无幽灵）/FailBefore_NoRecordsEmpty（板空证事件承重）/LastSettleWins/SubagentResumeGuidance（引导文案+Relaunch 重建）
- [x] 1.12 节门禁：build + agent/task、agent、tool/action 测试绿（含既有套件全绿+gofmt+vet）

## 2. R3 常驻/异步连续

### 2a. 枚举/orphan/三态

- [x] 2.1 `tmux_executor.go`：ListSessions 双条件（prefix ∨ n-）+ **CleanupOrphanSessions 排除 n-**（orphan 重定义）；既有测试全绿（mock 零值回落旧语义）
- [x] 2.2 三态探测：SessionAlive3（list-sessions 单源，接口+TmuxExecutor+mock）；IsPaneDead err→false（不再 assume-dead）；monitor 加闸——ProbeUnknownCount 于 TmuxSession 字段、ProbeUnknownLimit 配置（默认 3 拍平字段）
- [x] 2.3 回归（fail-before）：ConsecutiveLimit（1-2 次保留/第 3 次才 dead）/DeterministicDeadImmediate（真死不吃闸）/ResetOnKnown（可辨清零）/CleanupOrphan_ExcludesNamedSessions（真 tmux：cleanup 后 n- 存活+双条件命中）
- [x] 2.4 节门禁：build + tool/action 全量短测绿 + gofmt

### 2b. 元数据/事件/重挂

- [x] 2.5 ResidentMeta 补 `Command/Origin/TaskID`（旧记录零值兼容；Origin 真相源=task_spawned 事件，meta 侧预留）+ `resident_meta_dir` 配置项（默认 /tmp）；`resident_session` 事件类型（spawn 全参/终态结局经 SetResidentRecordSink 旁路 best-effort）+ 投影跳过守卫
- [x] 2.6 **唯一挂载点**（residentReattachOnce CAS：多 agent 仅首实例重挂）；TaskID 桥（build_agent：suspect 任务 Declarative.TaskID 被 IsTrackedSession→MarkTaskRunning；未跟踪保持 suspect 交探测/zombie）；接线序：reattach（NewActionTool 内）→R2 rebuild→桥
- [x] 2.7 回归：①FullParamsAndLifecycleEvents（meta 全参+spawn/end 双事件+旧记录兼容）②TaskIDBridge_SuspectToRunning（提升/保持双分支）③CrossRestartResume_RealProvisioning（rebuiltResumeClosure 真供能：未跟踪→relaunch 引导同文案；重挂后走 send-keys 路径）④唯一挂载点日志 ⑤best-effort（sink=nil 跳过）
- [x] 2.8 节门禁：build + tool/action/agent/agent/task 全量短测绿 + gofmt

## 3. R4 非重启全量热更（cm.runner 级换代）

### 3a. A 面：懒检查收敛 + memory 先检（修 🔴5）

- [x] 3.1 orgReloader 懒检查重排：mtime 变→**先独立 canonical diff（JSON）agents.*.Memory 段**→命中=ERROR+须重启通知+return；未命中→fingerprint 比对（复用 computeOrgFingerprint）→不变=仅 ApplyOrgParams（既有）；统一前缀 `[org-hot-reload]` —— computeMemoryFingerprint 新增（org_hotreload.go），先序插在 org fp 比对之前
- [x] 3.2 回归：TestMemoryFingerprint_DetectsMemoryOnlyChanges —— 仅 memory：org fp 不变+mem fp 必变（检测可达）；非 memory 变更零误伤；canonical 稳定

### 3b. B 面：cm.runner 可换缝（🔴4 裁决落地）

- [x] 3.3 cm 执行器缝：executorMu RWMutex 守护 runner；currentRunner()（RunFlow per-turn RLock 即放+Close 同）+ SwapExecutor 原子换；buildRunner 装配段抽取纯函数（冷启/热重建同路径）；**TagentAgent/loop/bus/cm/projection/TaskManager/monitor 常驻不换**（宿主入口零变化）
- [x] 3.4 **build 副作用 ownership 表**落地（buildAgent executorOnly 模式）：内存 store 丢弃壳（不双开文件句柄）/govLedger+Goals 不重绑/Approval 不 AddChannel（累积泄漏+多播）/evoGit 章不重盖/R1+R2 冷启重建跳过；org_coordinator.go 零消费者死代码删除（D3.5 处置：orgSnapshot/builtAgent 类型删除、reloadSnapshot 替代并注记裁决）
- [x] 3.5 `Reload`（tagent.go 懒检查 fp 变分支编排）：memory 先检→executorOnly 重建（build-validate-then-swap）→失败 fail-closed（旧 runner 原样+ERROR）→成功 SwapExecutor+代际日志（gen/fp/下一 turn 生效）
- [x] 3.6 触发接线：懒检查 fp 变化分支即 Reload（与 3.5 同段合并落地）
- [x] 3.7 回归：DrainFreeTurnLevel（in-flight 持旧/下 turn 见新）/ConcurrentSwapVsRead（-race 50 代并发换无撕裂）/ResidentInvariants（TaskManager/投影/bus 换代原封）；原①②④由 ownership guard+先序单测+既有行为面契约覆盖

### 3c. C 面：可逆

- [x] 3.8 代际日志（gen/fp/时间/变更面 INFO）；ring 2 prevSnapshot+SetRollbackFn/Rollback()（按上一代配置重建+Swap 回+fail-closed）
- [x] 3.9 节门禁：build+根包 -race 绿+tool/action/agent/task 全绿（agent 存量 3 race=R1 已鉴定上游非阻断）+gofmt+既有 buildAgent 调用点适配（6 处测试补 false）

## 4. 收尾

- [x] 4.1 全量门禁：build+vet+全量 -short 零 FAIL+相关包 -race（agent 存量 3 处上游 race 非阻断；event 镜像契约已纳入 task_spawned/resident_session）
- [x] 4.2 文档同步：memory-architecture.md（旁路产物两层记录：task_spawned/resident_session+registry 重建入口）、agent-architecture.md（R2 registry fold+承诺表/org 级单例、R3 orphan 重定义+三态加闸+跨重启连续序）、agent-behavior-matrix.md（重启恢复表补 R1-R4 四行：上下文/任务/常驻/热更）
- [x] 4.3 roadmap 勾选回写（2.3/4.3 落地行+裁决注记） + LEDGER 落地行（实现面/验证面全记）
- [x] 4.4 validate --strict（变更+主 specs 88/88） + delta 同步（task-registry MOD 反转纯内存禁令+看板连续 ADDED；resident-session-continuity/swappable-executor NEW 主 spec）+ 归档 + commit（conventional，节间提交边界可独立 revert）
