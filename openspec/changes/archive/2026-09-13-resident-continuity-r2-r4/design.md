## Context

R1（event-sourced-projection，b09b6f2）已落地「投影=事实链纯回放」。本变更一次实施 roadmap 剩余 R2/R3/R4（用户裁决合并单变更，D6 分阶段被覆盖、roadmap 已记）。

**第六轮 fresh-eyes（2026-09-12，5🔴+4🟠+5🟡 全部折进本版）确立的关键代码事实**：
- **R2**：TaskSpec 三闭包中 **subagent 的 ResumeFn 捕获 `rounds *subagentRounds` 内存轮次链**（tool_agent.go:571-623，无事件承载、空则直接报错）、Relaunch 捕获 `inv *Invocation`+agent 实例引用——**subagent 跨重启 resume 不可声明式化**（🔴2）；**窗口内 inline settle 无任何事件**（task_manager.go:402-446，最常见形态）、background settle 落库为 external_input 且 **Metadata 不含 task_id/settle_status 不可机器辨认**（context_manager.go:612-623，🔴3）；TaskManager **已被 TagentAgent 持有**（agent.go:91/:438-446，外提动作量高估）但**每代一个新实例**→换代丢 registry（🟠6）。
- **R3**：ListSessions 双条件修复会触发 **CleanupOrphanSessions 屠杀全部 n- 会话**（tmux_executor.go:495-512 杀掉列表全会话；cleanup 在 reattach **之前**执行 action_tool.go:147-153，🔴1）；resident meta 全局 /tmp 不分 agent→多 agent 重复重挂（🟠9）；unknown 计数须放 TmuxSession 字段（🟡12，KillRetryCount 先例 :134）。
- **R4**：**事件循环/bus/cm/TaskManager/meditationMgr/ActionTool+closers 全是 TagentAgent 成员**（agent.go:293-466），`New()` 只返回 entry TagentAgent、宿主持其引用——**orgSnapshot 整代换入与常驻 loop 形态根本冲突**（旧 loop 无终止协议/宿主入口失效/旧 OnSettle 发旧 bus/旧 ActionTool Close 杀全会话，🔴4）；**memory 被 fingerprint 白名单排除**而 Reload 只在 fingerprint 变化时触发→memory 静默变更检测永不可达（tagent.go:356-365，🔴5）；buildAgent 副作用重入（双 store 实例/AddChannel 累积/RegisterCloser 交叉杀共享 evoGit/MCP，🟠8）。

## Goals / Non-Goals

**Goals：** R2 任务跨重启接续（按 Kind 拆分承诺）；R3 常驻连续（orphan 语义重定义+三态+唯一挂载点）；R4 非重启热更（**cm.runner 级换代**，org 基础设施常驻）。

**Non-Goals：** 不换 TagentAgent/loop/bus/cm 本体（🔴4 裁决：常驻）；subagent 任务跨重启 **resume 不承诺**（仅 relaunch，🔴2 裁决）；不做 rounds 轮次链事件化（另立变更）；R4-C 仅代际日志+配置回滚。

## Decisions

### D0：执行裁决与第六轮修正

单变更一次实施（用户）；节间门禁 R2→R3→R4。**第六轮 fresh-eyes 5🔴 全折进**：R4-B 换代单元从「orgSnapshot 整代 TagentAgent」缩小为「cm 内执行器」（D3 重写）；R2 按 Kind 拆分承诺（D1）；R3 orphan 语义重定义（D2）。

### D1（R2）：registry=事实链 fold + 按 Kind 拆分承诺 + org 级 TaskManager

1. **事件（修 🔴3 双缺口）**：①新增 `task_spawned`（载 Declarative+**StartedAtUnixMilli** 🟡10）；②新增 `task_settled` 结构化标记：**persistBusEvent 对 Source==task 的事件把 `task_id`（全量 UUID）与 `settle_status` 拷入 FullEvent.Metadata**（重建以结构化字段关联，不解析正文）；③**inline settle（窗口内完成）也发终态事件**：Spawn 的窗口内结算路径补发 `task_settled`（经 OnSettle 同款 hook，spawn 与 settle 可合并为单条复合终态事件——实现取简者）。spawn/settle 事件均 best-effort（失败仅 ERROR 日志——**残余缺口**：spawn 落库失败则重启丢该任务，🟡11 明示）。
2. **Declarative 按 Kind 拆分（修 🔴2）**：`Declarative{Kind, Desc, Key, Command, AgentName, MessageBody, EventKeys, Origin, TaskID, Params, StartedAtUnixMilli}`；`RebuildClosures` 承诺表——**command**：Relaunch✅（重跑 Command）/Resume✅（依赖 R3 重挂供能）/Alive✅（Params.probe）；**subagent**：Relaunch✅（AgentName+MessageBody+EventKeys 经常驻 agents map 重新投递）/Resume❌（rounds 轮次链无事件源，跨重启调用返回引导文案「请 relaunch」，进程内不受影响）；**generic**：均❌（仅展示）。Params 枚举 ActionArgs 全 spawn 字段集（🟡14：WorkDir/Env/Mode/Name/IsTUI/Watch/Probe/ProbeIntervalSec/**ProbeFailures**/QuietTimeout/**Timeout**，未知字段拒绝；排除项=Op/Keys/Enter/Tail/Ansi/GraceSec/SessionID 非 spawn 参数，Command 已单列）。
3. **重建**：`RebuildTaskRegistry` 纯全量回放 spawned/settled（结构化 Metadata 关联）→ active 重建（running→suspect 交 R3 三态裁决；alive-detached/suspect 原态；终端不重建；**无 settle 记录且非 inline 终态 → suspect**，由探测裁决消解幽灵）。
4. **org 级 TaskManager（修 🟠6）**：TaskManager 从 NewTagentAgent 代内构造（agent.go:347）**提升为 org 级单例**——build 路径构造一次、注入各代 cm（`cm.taskController` 既有接点 :439）+ meditationMgr（:446）+ ActionTool。R4 换代不换 TaskManager（自身 spec「换执行器不丢任务板」由此成立）。
5. **跨重启 resume 供能时序（修 🟠7）**：tmux 跨重启 resume 依赖 R3 重挂（detector/monitor 重建，action_tool.go:468-491）——**R2 节只验 relaunch**，跨重启 resume 回归移 R3 节 2.7。

### D2（R3）：orphan 语义重定义 + 三态 + 唯一挂载点

1. **枚举修复+orphan 重定义（修 🔴1）**：ListSessions 双条件（prefix ∨ n-）；**CleanupOrphanSessions SHALL 排除 n- 前缀会话**（orphan 新语义=仅无主生成名会话；cleanup 与 reattach 的时序冲突由此消除）+回归「cleanup 后 n- 存活」。
2. **三态+加闸**：探测基于 list-sessions（alive/dead/unknown）；monitor err→unknown 保留+计数（**计数放 TmuxSession 字段**，🟡12），连续 N（默认 3）unknown 才 dead。
3. **唯一挂载点+路径（修 🟠9）**：ReattachResidentSessions 仅在 **entry agent** 装配时执行一次（非 per-agent）；ResidentMeta 路径可配（默认仍 /tmp，配置项 `resident_meta_dir`）。
4. ResidentMeta 补 Command/Origin/TaskID + `resident_session` 生命周期事件；重挂按 TaskID 桥与重建 registry 重关联；build_agent 接线序：R1 rebuild → R2 registry → R3 reattach（仅 entry）。

### D3（R4）：cm.runner 级换代——org 基础设施常驻（🔴4 裁决，重写）

**换「orgSnapshot 整代」被第六轮证伪**（loop/bus/cm/TaskManager/ActionTool 全是 TagentAgent 成员、宿主持 entry 引用、旧 ActionTool Close 杀会话——整代换入无 drain-free 语义可立）。**修正：换代单元=cm 内执行器三元组 {fwAgent, runner, tools 装配}**——回到 roadmap D5 原案（SwappableModel 模式上提）：

1. **可换执行器缝**：cm 增加 `executorMu sync.RWMutex + runner`（替普通字段，context_manager.go:47）；`RunFlow` 每 turn RLock 取 runner 引用后释放（in-flight turn 用旧 runner 跑完=drain-free turn 级成立）；`SwapExecutor(newRunner)` Lock 原子换（tools 装配随 newRunner 构建完成，签名以实现为准）。**TagentAgent/loop/bus/cm/projection/TaskManager/monitor/org 级全部常驻不换**（宿主入口 StartLoop/InjectMessage/outputCh 零变化）。
2. **Reload 缩小**：懒检查 mtime 变→**先独立 canonical diff（JSON）agents.*.Memory 段（修 🔴5）**→命中=ERROR+须重启通知+return；未命中→fingerprint 比对→不变=仅 ApplyOrgParams（既有）→**变化=仅重建 entry（及被引 subagent）agent 的 fwAgent+runner+tools 装配**（不重跑 store/engine/ledger/goals/approval/closers——**build 副作用 ownership 表**，修 🟠8：进程级共享=memStore/engine/govLedger/goals/evoGit/MCP registry/hintTracker；代级重建=fwAgent/runner/tools/prompt 装配；换代动作=复用共享+只重建代级+**不重复 RegisterCloser/AddChannel**）→ 构建失败 fail-closed（旧 runner 原样）→ 成功 SwapExecutor+代际日志（gen/fingerprint/时间/变更面）。
3. **触发**：既有 orgReloader 懒检查（BeforeModel 前）扩展，统一前缀 `[org-hot-reload]`；换代对下一 turn 生效。
4. **C 可逆**：ring 2 上一代配置摘要+`Rollback()`；代际日志入 evolution 事件流。
5. orgSnapshot/builtAgent 类型声明（org_hotreload.go:25-38）按本裁决**重新定义或删除**（注释指向本变更）；悬空 design.md 引用注释修正。
6. **历史工具引用**（CONFIRM 5.2）：provider 容忍实测（deepseek/zhipu/openai 兼容各一）。

### D4：节间门禁（不变）

节序 R2→R3→R4；每节 build+节内测试绿+勾选；5.1 fresh-eyes 通过（第六轮已折，第七轮快核后放行）。

## Risks / Trade-offs

- **R4 换代粒度=cm 级**：多 agent org 中非 entry/未被引 agent 不随换代重建（其配置变更在下次被引用构建时生效）——记录为已知边界（整 org 同步换代需 future 变更）。
- **subagent 跨重启 resume 不可用**（🔴2 裁决）：引导文案兜底；rounds 事件化另立变更。
- **inline settle 补发事件**改变 spawn 热路径（多一次 StoreEvent）——best-effort 不阻塞；密集 spawn 场景写放大可观测后优化。
- **org 级 TaskManager**：OnSettle 回调的 bus 归属=构造时的常驻 bus（不再有代际问题）。

## Migration Plan

1. R2 节：契约测试先行→Declarative+工厂（承诺表）→task_spawned/settled 结构化+inline 补发→org 级 TaskManager→RebuildTaskRegistry+接线→回归（relaunch 跨重启；resume 留 R3）。
2. R3 节：ListSessions 双条件+**cleanup 排除 n-**→三态+Session 字段计数→ResidentMeta 补全+事件→唯一挂载点重挂+TaskID 桥→回归（含跨重启 resume）。
3. R4 节：A 收敛→**cm.runner 缝+SwapExecutor**→ownership 表 checklist→Reload(memory 先检/fail-closed)→触发+代际日志+Rollback→回归。
4. 收尾：全量门禁+文档+roadmap 回写+归档+commit（节间提交边界）。
- **回滚**：三节独立 revert；Reload 失败天然 fail-closed。

## Open Questions

1. ~~OQ2 消费方清单~~ 已随 D3 缩小消解（消费方=cm.RunFlow 单点 RLock）。
2. inline settle「合并复合终态事件」vs「spawn+settle 两事件」——实现取简者（复合=窗口关闭时单写；两事件=spawn 即写+窗口内结算补写），实现时定。
3. org 级 TaskManager 的构造参数（TaskManagerConfig 的 OnSettle 需常驻 bus）在 build 路径的参数化形状——实施时定。
