> 本清单是阶段级检查点（伞形路线图）。各阶段细粒度任务由派生子变更的 tasks.md 承载；
> 准出门禁见 specs/resident-continuity（fresh-eyes 前置 + fail-before 回归 + 三道门禁）。
> CONFIRM = 核对 design.md 对应 D 的预留确认项：形成决议或显式降级（标注 DEGRADED）。

## 0. 路线图启动

- [x] 0.1 用户批准统一架构原则（状态⊥执行器）与三阶段依赖序 — 2026-09-12「我同意+请」
- [x] 0.2 R1 子变更（event-sourced-projection）按本图对齐修订（定位=模式立范者 + 共享约定）

## 1. R1 上下文连续（进行中 · 模式立范者）

- [x] 1.1 event-sourced-projection 过第二轮 fresh-eyes — 2026-09-12 完成：二轮抓 5🟠+5🟡 全部折进 artifact，三轮快核 14/15 PASS、唯一 FAIL（R2 snapshot 预设 4 处残留）已修，复验双变更 valid；裁决=放行实现
- [x] 1.2 实现落地（写路径 compaction 事件 / 冷启动 snapshot+tail 重建 / 移除 dev 快照补丁 + 测试同步）+ 三道门禁 — 2026-09-12 完成 33/33（含实现后 review 抔 2🟠 修复）；归档 archive/2026-09-12-event-sourced-projection，主 specs 已同步
- [x] 1.3 CONFIRM（R1 自身遗留）— 决议：载荷存 **Metadata**（CompactionPayloadMetaKey，召回正文=叙事不受污染；L3 不清 context_compress_summary 的 Content/Metadata，fresh-eyes ✅）；compaction key 与 retained key 序断言已入回归（主路径坐实+spill 例外入 D2 边界）
- [x] 1.4 立范固化：确认 R2/R3 可直接套用「事实链旁路产物 + 事件回放重建进空态（R1 为 snapshot+tail 特例，无压缩语义的状态层纯全量回放）+ 状态外置」一般模式 — 已确认：R1 实现（旁路产物/回放重建/外置）与 R2/R3 预期无偏差；唯 R2 须注意 task_* 事件无折叠语义→纯全量回放（roadmap design D2 已明）

## 2. R2 任务连续（与 R3 可并行）— **合并执行裁决（2026-09-12 用户）：R2+R3+R4 收进单变更 `resident-continuity-r2-r4` 一次实施**（偏离 D6 分阶段；缓解=节间门禁；节序 R2→R3→R4）

- [x] 2.1 ~~派生子变更 task-board-event-sourcing~~ → 已并入 resident-continuity-r2-r4 §1（含外提）→ 已随其归档落地
- [x] 2.2 CONFIRM：声明式 TaskSpec 字段完备性（command/origin/task_id/状态）；在途异步任务跨重启 resume 语义 → **已由 r2-r4 §1 实现裁决**：Declarative 含 Command/TaskID/Origin/AgentName/MessageBody/Params/StartedAt（Params 枚举 ActionArgs 全 spawn 字段含 Timeout/ProbeFailures）；resume 语义按承诺表拆分：tmux 存活（重挂跟踪）→ rebuiltResumeClosure 真供能，已死→relaunch 引导；subagent 跨重启 Resume 不承诺（返回引导）
- [x] 2.3 实现：任务板事件溯源（复用既有 task_* 事件类型）+ TaskSpec 声明式化（闭包→工厂重建）+ **TaskManager 外提到 cm**（执行器外，R4 前提）+ fail-before 回归（重启后板不丢、任务可接续）→ **已由 resident-continuity-r2-r4 §1 落地（2026-09-13）**：task_spawned 一等事件（实现时裁决 settle 沿用 external_input+结构化 Metadata 不新增类型）+ inline settle 补发终态记录；Declarative+承诺表工厂（subagent 跨重启 Resume 不承诺，rounds 无事件源）；TaskManager 经 D3 裁决（TagentAgent 常驻）天然 org 级单例（原「外提到 cm」由换代单元缩小消解）
- [x] 2.4 门禁 + 勾选回写 → r2-r4 §1 节门禁绿（build+三包短测+fail-before×8）；本图 2.3/2.4 已回写

## 3. R3 异步/常驻连续（R1 后；与 R2 可并行）

- [x] 3.1 ~~派生子变更 resident-session-continuity~~ → 已并入 resident-continuity-r2-r4 §2（含死代码修复）→ 已随其归档落地（resident-session-continuity 主 spec 已建）
- [x] 3.2 CONFIRM：tmux 三态探测 CLI 面（has-session dead/unknown 同 exit 1 的替代：list-sessions）；monitor fail-dead 屠杀加闸与 liveness 权衡 → **已由 r2-r4 §2 实现裁决**：SessionAlive3（list-sessions 单源：在列表=活/不在=死/err=unknown）；加闸=ProbeUnknownCount 于 TmuxSession 字段、连续 N（默认 3）才 dead；权衡：unknown 保留窗口可能延迟真死清理（可观测 WARN+看板 ⚠ 兑底）
- [x] 3.3 实现：ResidentMeta 补全（command/origin/task_id）+ 会话态事件溯源 + **修 resident_recovery 枚举死代码**（n-* 命名 vs prefix 过滤不相交）+ 重挂存活 tmux + fail-before 回归（重启后常驻会话重挂、异步结果可回投）→ **已由 resident-continuity-r2-r4 §2 落地（2026-09-13）**（含 CleanupOrphanSessions 排除 n- 的 orphan 重定义、唯一挂载点、TaskID 桥、跨重启 resume 真供能）
- [x] 3.4 门禁 + 勾选回写 → r2-r4 §2 节门禁绿（真 tmux 契约验证：cleanup 后 n- 存活）；本图 3.2/3.3/3.4 已回写

## 4. R4 非重启全量热更（R2/R3 后）

- [x] 4.1 CONFIRM（前置实验）：~~→ 已并入 resident-continuity-r2-r4 task 5.2~~ → r2-r4 已裁决 **DEGRADED**（无真实 key 不盲发付费调用；防御=Reload 移除工具对未折叠历史引用记 WARN；live 验证留 dogfood）
- [x] 4.2 ~~swappable-executor-A~~ → 已并入 resident-continuity-r2-r4 §3a → 已随其归档落地（懒检查收敛+memory 先序检测）
- [x] 4.3 ~~swappable-executor-B~~ → 已并入 resident-continuity-r2-r4 §3b（含 config diff=既有 fingerprint 白名单；**设计基座更新**：dev 已有 computeOrgFingerprint+orgSnapshot 类型声明；R4-B 按 roadmap D5 原案落地 **cm.runner 原子缝**（SwapExecutor，org 基础设施常驻），orgSnapshot 整代方案经第六轮 fresh-eyes 证伪、类型按 r2-r4 D3.5 重定义或删除）→ **已落地（2026-09-13）**：executorMu+currentRunner/SwapExecutor（buildRunner 抽取）；executorOnly 模式按 ownership 表跳过共享物副作用；懒检查 fp 变分支编排 Reload（memory 先检→build-validate-then-swap→fail-closed）；代际日志+ring2/Rollback；org_coordinator.go 死代码删除
- [x] 4.4 ~~swappable-executor-C~~ → 已并入 resident-continuity-r2-r4 §3c → 已随其归档落地（代际日志+ring2/Rollback）
- [x] 4.5 端到端验证：TestOrgHotReload_ExecutorSwapEndToEnd（-race）——结构变更→executorOnly 重建（🔴1 回归：壳复用常驻 memStore/sessionSvc）→SwapExecutor 换入→非重启生效（Runner 引用换代）→常驻不变量原封（store/svc/TaskManager 引用不变）；fail-before 对照：不 Swap 时 Runner 引用不变（结构变更不可见）；TestOrgHotShift_EndToEnd（incremental A 全链路：mtime→懒检查→热应用/保持服务/fail-closed）继续守护既有语义
- [x] 4.6 门禁 + 勾选回写 → r2-r4 §3 节门禁绿（-race）+ 4.5 e2e；本图 4.1-4.5 已回写

## 5. 路线图收尾

- [x] 5.1 全阶段完成 → 本伞形变更归档（LEDGER 回写裁决与实测数据）→ R1（event-sourced-projection，33/33）+ R2/R3/R4（resident-continuity-r2-r4，36/36 + review 修复 f55e8ac）全部落地；LEDGER 已记提案/落地/审查修复三行
- [x] 5.2 架构不变量（specs/resident-continuity）并入主 specs，作为后续变更的守护契约 → 已建 openspec/specs/resident-continuity（四条不变量：状态⊥执行器/事件溯源模式/执行器可换语义/依赖序），validate --strict 88/88
