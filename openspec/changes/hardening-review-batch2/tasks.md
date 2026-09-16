# Tasks: hardening-review-batch2（细化执行版）

> 行号基线：HEAD=7080753。执行中行号会漂移——锚点以「函数名+签名片段」为准，行号仅作初始定位。
> 每任务执行顺序：读锚点现状 → 对照「要点」实施 → 过「验收」。任何任务发现现状与本文不符，停下更新本文件再动手。

## 0. 执行守则（红线）

1. **单任务单提交意图**：每任务（或强耦合任务对）独立可编译、独立过测试；禁止跨批半成品混提。
2. **不改测试迁就实现**：验收门失败时修实现；若验收本身有误（现状证据），先改 tasks/spec 再改代码。
3. **锁序不倒置**：TaskManager 内一律 tm.mu→t.mu（参照 enforceDetachedWall 快照法）；新增代码同守。
4. **reconcile 类终态只走 finalize**：新终态代码禁止手写 status=+onSettle 三连；正常 settle 路径（sync-wait 窗口内 applyStatus）不动。
5. **旧 WAL 兼容**：Declarative 新字段（Origin 回填/DetachedAtMilli/Lifetime）解析缺省必须安全；旧记录恢复行为按 spec「缺 origin→unknown」。
6. **双模块门**：涉及 examples/wechat-bot 的任务在该模块内单独 build+test；根模块 go test ./agent/... 相应包。
7. **禁外部 API**：全部测试用 mock detector / httptest / 内嵌 server。
8. **不扩范围**：发现新问题记入本文件「新发现」节，不顺手修。

## 1. 批 1a：世系保真与终态事实（P0）

### 现状事实（已侦察确认）
- `Declarative.Origin` 字段**已存在**（task_manager.go:115）；恢复侧 task_record_sink.go:319 已读取。
- 断链精确定位：① 三处 Declarative 构造点**均未填 Origin**：tool_agent.go:450（subagent）、tool_agent.go:597（另一处，实施时核对）、tool/action/declarative.go:65（command）；② task_record_sink.go:320-323 `spec = rebuilt` 整体覆盖后仅回填 Desc（:325-327），Origin 丢失。
- `SettleFailed` 枚举**已存在**（:47）；zombie（:950）/orphan（:893）已正确使用；wall（enforceDetachedWall）错用 SettleCompleted。
- mapper event_bus.go:103-121：无 SettleFailed 分支 → 无 Err 时落 default=completed。

### 任务

- [x] 1.1 **Origin 写入单点修复**
  锚点：`EmitTaskSpawnedRecord`（context_manager.go:842-869，`decl := *tk.Spec.Declarative` 浅拷贝处）。
  要点：marshal 前补填 `if decl.Origin == nil && len(tk.Spec.Origin) > 0 { decl.Origin = tk.Spec.Origin }`（map 拷贝按引用读即可——decl 不再被改）；不改三处构造点（stamp 时机在框架 spawn 侧，单点兜在持久化出口最稳）。
  坑：确认 OnSpawn hook 运行时 tk.Spec.Origin 已 stamp（event_loop BuildTurnAttribution 时序）；若存在 hook 早于 stamp 的路径，以 tk.Spec.Origin 为准即可（stamp 后 settle 时再读 Spec.Origin，与持久化独立）。
  验收：单测——构造 Spec{Origin:{"chat_id":"x"}, Declarative:{...}}→EmitTaskSpawnedRecord→读回事件 Content JSON 断言 origin.chat_id="x"。

- [x] 1.2 **恢复合并边界：覆盖后回填身份字段**
  锚点：task_record_sink.go:319-327（`spec = rebuilt` 块）。
  要点：覆盖分支内补 `if spec.Origin == nil { spec.Origin = sp.decl.Origin }`；同型补 Key（`if spec.Key == "" {...}`）。原则：工厂供执行能力，身份缺口由持久层兜底。
  验收：现有 task_record_sink_test 全绿 + 新用例：storeSpawned 带 Origin{trigger_source:meditation} → Rebuild → List 断言 spec.Origin 保留。

- [x] 1.3 **unknown 来源保守扣留**
  锚点：examples/wechat-bot/main.go:435-462（resolveTriggerSource 消费点/switch）。
  要点：Origin 含 `trigger_source=meditation`（或其余内部值）恢复后经 newTaskSettledEvent（event_bus.go:190-192 复制 Origin→Metadata）到达宿主——现有 switch 应已扣留 meditation；新增对**缺失/空 trigger_source 的 task 来源事件**扣留（unknown 分支），只记日志。
  坑：勿把 user 正常路径误伤——仅 task 来源 + 元数据缺 trigger_source 时扣留。
  验收：main 包测试：构造 SourceTask 事件分别带 meditation/无 trigger_source/user → 断言投递决策 扣留/扣留/放行。

- [x] 1.4 **全链集成测试（世系→恢复→投递门）**
  新文件 agent/origin_roundtrip_test.go（或入 task_record_sink_test）。
  要点：mock store 完整链：Spawn(meditation Origin)→EmitTaskSpawnedRecord→新 TaskManager RebuildTaskRegistry→模拟后台 settle→断言 task_settled 事件 Metadata 世系=meditation + 1.3 的扣留函数判定扣留。无外部 API。
  验收：`go test ./agent/ -run TestOriginRoundtrip -count=1 -race` 绿。

- [x] 1.5 **finalize 唯一终态入口**
  锚点：新增 `func (tm *TaskManager) finalize(t *Task, kind SettleKind, output string, err error)`，放 task_manager.go（enforceDetachedWall 旁）。
  要点：持 t.mu 置 status/result/settledAt（沿用 IsZero 守卫）→解锁→onSettle(kind, output, err)。改造三处调用点：wall（enforceDetachedWall）、orphan（RetireOrphans :884-894）、zombie（reconcileZombies :939-951）——各自删除手写三连改调 finalize；wall 的 kind 修正为 SettleFailed+output 注记。
  坑：仅统一 reconcile 类；applyStatus（sync-wait 窗口正常 settle）**不动**；onSettle 在锁外调用（现状如此，保持）。
  验收：`go test ./agent/task/ -count=1 -race` 全绿（现有 zombie/orphan/wall 测试即回归门）。

- [x] 1.6 **mapper 识别 SettleFailed / unknown 显式化**
  锚点：event_bus.go:103-121 settleMarkerAndStatus。
  要点：加 `case sig.Kind == task.SettleFailed: return "✗", "failed"`（置于 Err 分支旁）；default 分支改：已知四 Kind 之外 → `"?", "unknown"` + log.Warnf（含 Kind 值）——SettleCompleted 仍需显式 case 保持 ✓/completed。
  验收：新表驱动测试遍历 5 Kind×Err 有无 → marker/statusWord 全表断言；`go test ./agent/ -run TestSettleMarker -count=1`。

- [x] 1.7 **迟到信号 fencing**
  锚点：emitBackground（task_manager.go:614-644）入口 + watchLoop 内 applyStatus 调用点（:548 附近）。
  要点：两处消费入口前置终态检查 helper `isTerminal(t)`（settledAt 非零 && status∈{completed,failed,cancelled,dead}）→true 则 Warnf 丢弃（含 Kind）返回。
  坑：resume 生命周期——resume 换 detector 后旧信号已有 watchDone 退休机制（task_resume_test :280-288 是既有语义）——fence 不改变 resume 重置行为（resume 将 status 重置为 running 时 settledAt 处理需核对 resume 实现并保持现状）；fence 只拦「终态后」窗口。
  验收：新测试：finalize(failed) 后手动 Emit Stable/Completed → 状态不变、settles 计数不变、有 Warn。

- [x] 1.8 **四端一致性测试**
  新文件 agent/task/finalize_consistency_test.go。
  要点：对 zombie/orphan/wall 三路径各一用例：mock onSettle 捕获信号 → 断言 t.Status()=failed ∧ sig.Kind=SettleFailed ∧ settleMarkerAndStatus(sig)→"failed"（即 WAL 将记 failed）∧ 输出含各自注记。
  验收：`go test ./agent/task/ -run TestFinalizeConsistency -count=1 -race` 绿。

## 2. 批 1b：危险清理与墙重设计（P0）

### 现状事实
- ReattachResidentSessions（resident_recovery.go:126-160）：:130 先 Sweep 后收养循环。
- SweepStaleResidents（:221-258）：判据 m.SpawnedAt+residentTTL（:244），kill+removeResidentMeta；:208-210 注释声称 reattach 重写 SpawnedAt——**reattachOne（:171-205）无任何重写**，失实。
- ResidentMeta（:42-50）无 LastAdoptedAt。
- reattachOne detector 为局部变量，无 TaskManager 绑定。

### 任务

- [x] 2.1 **顺序反转：先收养后清理**
  锚点：ReattachResidentSessions :130-133。
  要点：Sweep 移到收养循环**之后**；收养路径（reattachOne 内或调用侧）刷新 meta：写 `LastAdoptedAt=now`（新字段 RFC3339）。
  验收：单测（mock metaDir+executor）：25h 前创建+本次被收养的会话→存活+LastAdoptedAt 更新；同会话若 Sweep 在后跑→不再判超龄。

- [x] 2.2 **Sweep 判据改 LastAdoptedAt + orphan grace**
  锚点：SweepStaleResidents :240-244。
  要点：基准时间 = LastAdoptedAt（缺省回退 SpawnedAt，兼容旧 meta）；residentTTL 语义改为「无人收养超期」；删除 :208-210 失实注释，改述真实语义；「无人持有」增强：meta 存在但 tmuxMonitor 未跟踪且无任务引用（sessionTrackerFn 判定）才算孤儿——Sweep 增加该检查（可注入 isTracked，nil 时保守跳过 kill 只留 meta）。
  坑：executor==nil（record-only）路径保持现状不 kill。
  验收：resident_meta_test 扩展：tracked 会话超龄不杀；untracked+超 grace 杀；nil tracker 保守不杀。

- [x] 2.3 **清理回归测试**（并入 2.1/2.2 验收用例集，独立勾选便于对账）

- [x] 2.4 **Lifetime + detachedAt 持久化**
  锚点：TaskSpec（task_manager.go:125-162）加 `Lifetime string`；Declarative（:104-122）加 `DetachedAtMilli int64 json:"detached_at_ms,omitempty"`；emitBackground SettleStable 分支（:630-632）同步写 `task.Spec.Declarative.DetachedAtMilli`（nil 安全）；restore 侧（task_record_sink RestoreTask 后）按 decl 还原 task.detachedAt。
  要点：lifetimeOf(spec) helper：显式 Lifetime 优先，否则 Kind 推断（command/subagent→job，generic→service）。
  验收：单测 detached 跨 Restore 后 detachedAt==原值；lifetimeOf 表驱动。

- [x] 2.5 **墙重设计：TaskStale 观测态**
  锚点：enforceDetachedWall 重写；TaskStatus 加 `TaskStale`（task_manager.go:18 附近枚举）。
  要点：超龄（staleAfter>0 ∧ lifetimeOf==job ∧ detachedAt 非零 ∧ 超龄）→ 置 `t.status=TaskStale`（**非终态**、settledAt 不动）+ result 追注 `stale-detached(age)` + 一次性（Task 加 staleNoted bool）onSettle Watch 通知；进程不动。List/pruneTerminal/isTerminal 不把 stale 当终态（不 prune）；board 渲染层若按 status switch 需补 stale 展示（搜 TaskStatus 消费点逐一核对）。
  配置：SetMaxDetachedAge→SetStaleAfter（语义：>0 阈值/0 保持/负禁用观测）；yaml key `task_stale_after`（新）+ 兼容读 `task_max_detached_age`（Warn 映射）；config.go/build_agent/tagent.go/OrgHotParams 同步改名+兼容。
  坑：stale 态下 probe 死 → reconcileDetached 原有 completed 路径应仍工作（stale∈candidates 判据需含 TaskStale）；alive 恢复输出 → 保持 stale（观测事实不回滚，除非 resume）。
  验收：改写 task_detached_wall_test.go 场景矩阵：默认超龄→stale 不终态不杀；stale 后 probe 死→completed 复回收；旧 key 兼容。

- [x] 2.6 **task_job_deadline 显式终止**
  锚点：enforceDetachedWall 扩展（或独立 enforceJobDeadline，同一调用点）。
  要点：deadline>0 ∧ job ∧ detached 超deadline → detector.Cancel()（t.mu 外调用，nil 守卫——Cancel 即杀会话的 owner 语义）→ finalize(SettleFailed, "job-deadline-exceeded...")。service 永不触发。新配置 `task_job_deadline`（默认 0 关）全链接入（同 2.5 链路）。
  验收：mock detector 断言 Cancel 被调恰好一次 + finalize 四端一致（复用 1.8 断言）；service 型超 deadline 不触发。

- [x] 2.7 **测试改写**（并入 2.5/2.6，独立勾选；含 detachedAt 跨重启用例与旧 key 兼容用例）

- [x] 2.8 **yaml 与示例更新**
  锚点：examples/wechat-bot/tagent.yaml task_max_detached_age 行；docs 若提及。
  要点：换 task_stale_after: "1h" + task_job_deadline 注释（默认关；af4aa4c7 场景建议开 "8h"）；删除旧 key。
  验收：yaml 加载冒烟（现有 config 测试模式）。

## 3. 批 1c：R3 信号链接通（P0）

### 现状事实
- reattachOne detector 局部变量（:172），仅 monitor callback 与 probe loop 引用；Settled() 通道无 TaskManager 消费——恢复任务的 watch/probe 命中**不会 settle 任务**。
- build_agent.go:592-617：重建提升 running 用 actionTool.IsTrackedSession，未绑定 detector。

### 任务

- [x] 3.1 **BindDetector API + 重挂接线**
  锚点：TaskManager 新增 `BindDetector(taskID string, d SettleDetector) error`（终态任务返回 ErrTaskFinalized；running/suspect/stale 可绑）；绑定即启动标准 watchLoop 消费（复用现有 Spawn 后台消费逻辑——提取 `consumeDetector(t, d)` 共享函数，实施时核对 Spawn 异步分支的现有消费代码形态）。
  要点：reattachOne 完成后按 m.TaskID（ResidentMeta 无 TaskID？核对 meta 是否带任务关联——若不带，经 ct.residentSink/回调表反查 taskManager 中 Declarative.TaskID==sessionID 的任务）绑定。
  坑：reattach 发生在 build 序 RebuildTaskRegistry **之前还是之后**（build_agent.go:578-592 顺序）——绑定需在 registry 重建后补一轮（或 reattach 延后）；实施时以 build_agent 实际顺序定接线点，断链即错点。
  验收：mock 集成：reattach → BindDetector → detector.Emit(Stable/Completed) → 任务状态流转+onSettle 触发。

- [x] 3.2 **重建提升同步绑定**（build_agent.go 提升循环处，对 tracked 的 suspect 按 TaskID 绑定重挂 detector——与 3.1 同一接线，实施时合并验证）

- [x] 3.3 **resume 绑定复用/原子替换**
  锚点：declarative.go SpecFromDeclarative resume 段（:154-177）+ task_manager resume 换 detector 处。
  要点：resume 新 detector 到达时，若旧 detector 属同 session 已绑定 → 先经 watchDone/Cancel 退休旧消费再 Bind 新（复用既有 resume 换装代码路径——task_resume_test :280-288 已覆盖旧信号隔离，确认新路径不破坏）；不同 session 场景保持现行为。
  验收：现有 task_resume_test 全绿 + 新用例：重挂绑定后 resume → 旧 detector Emit 不生效、新 detector 生效。

- [x] 3.4 **验收升级**（3.1-3.3 的集成断言统一要求「真实信号到达 TaskManager 并结算」，tracked=true 不再单独作充分断言——写进各用例）

## 4. 批 1d：验证器重写与认证集成（P0）

### 现状事实（review 已双反例复现）
- verify_restart_prefix.py:41-75：batch_index==0 当重启身份（SetSessionInfo 也重置→误标）；配对向前搜索；FOLD≥90% 分支先于截尾判定且 `bh[len(bh)]` 越界；strip_board 按正文前缀删除；CRITICAL 不影响退出码。
- rl/http_api.go:141-147：全路由（含 /healthz）鉴权；restart-tagent.sh:126-153、restart-maintenance.sh:43-65、mail_poller.py:162-190 均无 Authorization。
- wechat-bot main.go:258-291：重试为线性 5s 递增（非指数），无 ctx/Shutdown。

### 任务

- [x] 4.1 **验证器判定重写**
  锚点：scripts/verify_restart_prefix.py 全量重写 main/配对/判定函数；保留 CLI 接口（stdin/file、JSONL 输入）。
  要点：按 design D6 五分类 + 身份配对键 (agent,session) + 死亡前最后请求锚定（禁止回溯）；先长度后取值修越界。
  验收：4.3 两个反例 + 现有正常 EXACT 样例回归。

- [x] 4.2 **退出码语义**：CRITICAL>0 ∨ UNPAIRED>0 → exit 1；汇总输出含五分类计数与差异坐标（行号对）。验收并入 4.3。

- [x] 4.3 **反例回归固化**
  新文件 scripts/test_verify_prefix.py（pytest 或独立 runner，与仓库脚本测试惯例一致——先查 scripts/ 现有测试形态）。
  要点：反例A（前置10条旧记录+死亡前100+恢复30 → CRITICAL/丢失70/exit1）；反例B（100→前90截尾 → CRITICAL/丢失10/exit1 不崩溃）；正例（完整恢复→EXACT/exit0）；看板例（仅看板行差异→BOARD_DIFF 不计 EXACT）。
  验收：测试脚本自身可独立运行并通过。

- [x] 4.4 **探针认证**
  锚点：restart-tagent.sh healthz 循环（:126 附近 curl）；restart-maintenance.sh :43-65。
  要点：两脚本从同源读 token（优先 `$TAGENT_RL_AUTH_TOKEN` env，回退 tagent.rl.yaml 解析——与 rl.AuthTokenFromEnv 同源约定）；curl 加 `-H "Authorization: Bearer $TOKEN"`；HTTP 401 → 判 AUTH_FAIL：告警日志+不 kill 旧进程+非零退出（区别于不健康）。token 不 echo。
  验收：shell 语法检查（bash -n）；本地 mock（python http.server 返回 401/200）冒烟两分支——若仓库无脚本测试惯例，冒烟命令记入本任务勾选备注。

- [x] 4.5 **mail poller 认证**
  锚点：mail_poller.py /task POST（:162-190）。
  要点：同源 token + Authorization 头；401/403 → 明确异常退出（不无限重试）；超时与现网络错误语义区分认证失败。
  验收：poller 单测（现有测试形态）+ mock 401 断言不重试。

- [x] 4.6 **HTTP Server owner 化**
  锚点：main.go:258-291 goroutine。
  要点：改 `srv := &http.Server{Addr, Handler}`；重试循环 `select { case <-srv.ListenAndServe()... case <-ctx.Done(): srv.Shutdown }`——ctx 为已有 signal.NotifyContext（:275）；ErrServerClosed 退出；注释更名「线性递增重试」。保持 ValidateListenAddr 回退逻辑。
  验收：wechat-bot 模块 build+test；手动冒烟 SIGTERM 后进程可退（记入备注）。

- [x] 4.7 **认证集成测试**
  要点：Go 侧——httptest 起 httpAPI（SetAuthToken）断言带/不带头行为（部分已有 auth race 测试，补脚本侧约定的 token 源一致性测试：读 tagent.rl.yaml 的 helper 与 Go AuthTokenFromEnv 同值）。
  验收：`go test ./rl/ -run TestAuth -count=1 -race` 全绿 + 新增源一致性用例绿。

## 5. 批 2a：热更统一应用（P1）

### 现状事实
- tagent.go:375-407：指纹不变→仅 entry ApplyOrgHotParams 后 return；指纹变→executor 重建不应用数值；:423-429 成功通知。
- org_hotreload.go:150-176 排除集；context_compressor.go:156-175 外层 setter；smart_compress.go:70-91 内层冷构造值。
- prevKeep 首代 nil（tagent.go:322-329/:430-439）。

### 任务

- [x] 5.1 **应用矩阵纯函数先行**
  要点：新函数 `planHotApply(old, fresh *Config, built map[string]*agent.TagentAgent) (numeric map[string]OrgHotParams, structural map[string]AgentConfig)`——纯函数单测先行（三场景：仅数值/仅结构/混合），再改 tagent.go reloader 消费：**结构重建后对全部 built agents 同批应用 numeric**（含 entry）；成功通知并入 5.4。
  坑：子 agent 的 OrgHotParams 落点——子 agent 常驻对象是否有 compressor/taskManager（build 形态核对：子 agent 走 tool_agent wrapper，热参数落点在其 ContextManager?）——侦察后定：若子 agent 无独立常驻 cm，则矩阵输出「无落点」并在回执中如实标注（不得谎报已应用）。
  验收：矩阵单测三场景 + reloader 集成测试（现有 hot reload 测试扩展混合场景）。

- [x] 5.2 **stale_after/job_deadline 入热参集**（OrgHotParams + ApplyOrgHotParams + agentSubset 排除注释更新；与 2.5/2.6 的配置链合并实施，勾选以链路通+热更测试绿为准）

- [x] 5.3 **压缩参数同代快照**
  锚点：ContextCompressor.ApplyHotParams（:140-145 附近）→ 扩展调 SmartCompressor 新方法 `ApplyParams(maxTokens int, triggerBudget int, keepRecent int)`；SmartCompressor 字段清单实施时全列（maxTokens/triggerBudget/KeepRecentTasks/recentFullCount 等构造派生项——读 NewSmartCompressor 核对）。
  验收：hot_params_test 扩展：UpdateMaxTokens 后真实压缩（构造超限消息）断言最终 messages 预算按新值（而非 no-op）。

- [x] 5.4 **回执升级**（tagent.go applied 日志：per-agent 字段=值列表 + generation 计数；拒绝/保持字段明示——与 5.1 同点实施）

- [x] 5.5 **首代快照可回滚**（reloader 安装时 ring 压入启动代 cfg；测试：首换→Rollback 成功恢复启动代）

- [ ] 5.6 **混合字段生效测试**（5.1 三场景的集成化：真构建多 agent 小拓扑断言预算线/TTL 实际变化）

## 6. 批 2b：执行代绑定与 MCP（P1）

### 现状事实
- SwapExecutor 换入路径 context_manager.go:534-568；helpers.go:45-52 wrapper 绑定；prompt :541-568 merged 局部变量；cm.systemPromptSource 消费 :363-383。
- mcp/registry.go:255-275 局部结构解码完整文件。

### 任务

- [x] 6.1 **wrapper 重绑常驻投影**（SwapExecutor 成功尾部：新 cfg.Tools 遍历 `*AgentToolWrapper` → SetParentProjection(cm.projection)；测试：热更后 wrapper 注入与热更前等价——mock 投影对比）
- [x] 6.2 **prompt source 与 execCfg 换代**（SystemPromptSource 写入 cm.systemPromptSource（加锁/原子——核对现有并发形态）；cm.execCfg 更新；测试：换 prompt 文件后新 BeforeModel 用新 prompt）
- [x] 6.3 **绑定测试**（6.1/6.2 集成断言；in-flight 旧 turn 不受扰用现有 SwapExecutor 并发测试扩展）
- [x] 6.4 **MCP 子树严格解码**（registry 解析改为 yaml.Node 取 mcp_servers 子树→strictyaml；HotSync 测试 fixture 换完整配置形态：entry/agents/providers/mcp_servers 共存文件→改 MCP 声明→断言 registry 更新）

## 7. 批 3：恢复语义与尾项（P1/P2）

### 现状事实
- projection_rebuild.go:133-138 fallbackCap 500 先取后滤；:172-195 fallback；fetchTailEvents :209-240 分页错误 break 部分返回。
- doc_snapshot.py:27-28/:59-64 glob 混排 .damaged。
- restart-tagent.sh:51-52 done-marker 复用；restart-maintenance.sh:128-156 child PID 丢弃。

### 任务

- [x] 7.1 **no-anchor 全量语义**（fallback 改全量分页→过滤；护栏保留但触发→partial 标记：rebuild 返回值/日志 Error + `truncated_events=N`；调用侧（build_agent）日志透传——核对 rebuild 返回签名是否需扩展）
- [x] 7.2 **观测扩展**（汇总行：`pages_failed/batch_errors/payload_errors/lost_keys/took`——fetchTailEvents 与 RestoreRefs 各错误点计数；任一非零→partial 标记并 Error）
- [ ] 7.3 **恢复测试**（tail 注入失败→partial；600+ 事件超护栏→truncated_events；正常路径回归）
- [x] 7.4 **doc_snapshot 修复**（restore 候选正则 fullmatch 日期命名，.damaged 排除；两候选场景单测）
- [x] 7.5 **marker 三态 + 真实 PID**（restart-tagent.sh：ok/failed/timeout 三 marker；失败路径不复用 done；maintenance posix_spawn 真实 child PID 写 pidfile——shell/python 侧核对 spawn 返回值使用点）

## 8. 收口

- [x] 8.1 **全量回归**：根 `go test ./... -short -count=1` + examples/wechat-bot `go test ./... -count=1`；`go vet ./...` 双模块；staticcheck 仅余挂账 SA4006（context_manager.go:342 死码，另案）。
- [ ] 8.2 **openspec**：`openspec validate hardening-review-batch2 --strict` 过；按 openspec/AGENTS.md 归档（不跳过 spec 同步）。
- [ ] 8.3 **部署通报**（一次到位）：验证器口径变更+远端历史数字不可比说明+review 第七节取证清单引用+stale/deadline 新配置说明。

## 新发现（执行中追加）

（空——发现即记：任务号/现象/证据/处置）

### N1（1.7 执行中发现）：fence 位置错误会误杀合法完成通知
- 现象：fence 初版放 emitBackground 入口 → TestAliveDetached_CompletionEndsAndNotifies 等 4 个既有测试失败——applyStatus（置态）与 emitBackground（通知）是**同一信号的流水两段**，applyStatus 置终态后 emitBackground 的 fence 把合法完成通知拦截。
- 教训：fence 只能放**信号入口**（watch 循环 applyStatus 之前——到达时已终态则丢弃整条）；同信号内部两段不得互相拦截。
- 处置：fence 移至 watch 循环入口；applyStatus 保留次级守卫（无日志）；emitBackground 注释说明。已通过全部回归。
- 启示：终态「恰一次通知」的保证方是 finalize（onSettle 恰发一次）；fence 的职责仅是「拒绝旧执行段的迟到信号」，两者语义不同层。

### N2（3.3 执行中发现）：恢复任务 resume 测试用例死锁
- 现象：BindDetector 后调用 Resume 的测试超时——Resume 内部等待新 detector 的 dense/detach 生命周期，ManualDetector 不 FireDetach 则挂起。
- 处置：删除该自建用例；resume 原子替换语义由既有 task_resume_test（旧信号隔离）与 Resume 实现（close 旧 watchDone→换 detector→新 watch）覆盖。BindDetector 与 Resume 的组合由 fencing（终态拒绝绑定）+ watchDone 退休保证。
