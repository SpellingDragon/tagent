## Context

dev 领先 origin/main 43 提交（fast-forward 关系）。合入前评审（7 主题并行 + 10 项高危人工复核，全部属实；证据 `.git/review-notes/01-findings.md`）发现 2 P0 + 14 P1 + 29 P2。**合入定位**：pre-release、无外部消费者，允许破坏性变更；不承诺旧盘上数据兼容迁移——新代码冷启动从事实链干净重建即合格。目标形态：45 项发现一次清零、specs 与实现零矛盾、判定逻辑单一真源，main 无已知债务。

两轮深潜（冷启动重建/压缩经济学/热更换脑/数据层崩溃一致性，笔记 `.git/review-notes/02-explore-deep-dive.md`）已确认各修复的实施面与可行性。

## 设计原则：机制完善，拒绝补丁

每项修复接受同一检验——修复后该机制的特设状态/特设路径数必须**净减少**：
1. **属性落在它本来的层**（信号属性挂信号、声明属性挂声明、瞬时状态不进持久层）；
2. **复用既有模式**（系统内已有先例：ExitCode 信号级字段、deliverTaskSettled 路由、default 分支豁免、"未知即停"）；
3. **消除特例而非给特例加例外**。

溯源警示：本轮三项 P1 本身就是上轮症状级补丁的产物（e900b3e 谱系走样→Origin 污染；c14cdbc 只优化通知面→丢记账路由；0a31e46 只改单条分支→批量折叠漏改）。凡修复方式会新增"需要人记住的同步点"，一律视为补丁重设计。

## Goals / Non-Goals

**Goals:**
- 45 项发现全部闭环（验收 = 01-findings.md 逐项核对），main 合并即收敛态。
- 谱系判定、结算通知识别收敛为单一真源；折叠域在产生侧定义。
- 全部修复通过"机制完善"检验：特设路径净减少。

**Non-Goals:**
- 不做与 45 项发现无关的重构（即使顺手）。
- 不承诺旧数据迁移：LocalFileKV 旧单文件 kv.json、无 `settle_notice` 标记的旧事件、无白名单谱系的旧 Origin——均不做兼容读，冷启动重建/安全方向降级。
- go.mod 上游依赖（PR #2637）非本地可消除：保留 replace + CI `go mod verify` + 摘除跟踪任务，是唯一声明的尾巴。

## Decisions

### 甲组：机制完善型（属性归位）

**D1 退役谱系信号化（C-P1-1/C-P1-2）**
spec 措辞本就是"退役外发的**结算信号**不继承谱系"——实现把信号属性写进了任务声明（`Spec.Origin` 可变写）。修复：`SettleSignal` 增 `Lineage` 字段（沿用 ExitCode 的"信号级不持久化"先例，task_manager.go:98-108），`finalizeRetired` 删 Origin 改写、设信号级戳；`newTaskSettledEvent` 取值优先级 `sig.Lineage 非空 > Origin`。读方（grep 证实 6 处全部读事件 Metadata）零改动。**修复后 Spec.Origin 回到 spawn 后不可变**——全系统少一个突变点。

**D2 批量折叠产生侧分流（C-P1-3，修订版）**
原设计（消费侧分流）是补丁：把折叠域定义在宿主回调，每个未来宿主都要重复实现。修订：`finalize` 在 batchCollect 分支前查 `t.Spec.Origin[MetaKeyInvocationID]`——有委派归属的条目**不进批**，照常走完整 `onSettle`（其 deliverTaskSettled 对"父循环已退出"本就有兜底总线语义）；批内天然只剩无主结算，宿主 OnBatchRetire 闭包零改动。折叠域="无主可路由的结算"成为产生侧定义。

**D3 execution gate 失败显式呈现（A-P1-2）**
三个失败点（verify 拒绝/迭代器创建 err/通道 err/nil）一律 yield 携带 `Response.Error` 的失败响应——与同库 `modelCallBudgetIterModel` 同构，兑现框架"流内错误编码进 Response.Error"契约。零新概念。

**D4 折叠豁免 run 级化（B-P1-1，修订版）**
推演证实混合 run 不可能：run 是连续 settle 通知段，成员"后方第一个 agent_output"位置相同 → consumer 存在性 run 内一致 → Active 与非 Active 不混。原"尾部剔除"设计是对不可能形态的过度设计。修订：单条豁免规则（default 分支）推广为 run 级——**run 含 Active 成员则整 run 原样保留**。一条规则，无新增算法。顺带补单条票据分支 Timestamp==0 防护（B-P2-1，与 buildSettleFoldRef 对齐）。

**D5 spill 租约释放对齐责任边界（E-P1-1）**
责任真源是文件行：行移除前该行仍是 pending 责任。`ReplayWithNotify` 循环只记成功 key 集合，`rewrite` 成功后统一 `ReleaseKey`；rewrite 失败不释放任何 key（下轮 AlreadyCommitted 自然重走释放路径）。`Release` Godoc 修正为按持有者释放语义。

**D6 spec 对齐方向（G-P0-1/G-P0-2）**
两处 P0 均为 spec 落后于已裁决实现，修 spec 不回退实现；REMOVED 措辞沿用归档 delta 原文避免漂移。

**D7 wrapper 防线归位（F-P1-1）**
删除 wrapper 的越权预过滤，删除侧文件直传 codetools 触发 MISSING-HEAD 硬拒（check.go:56-62 已核实）；仅纯新增批次放行。硬拒逻辑单点在工具层。

**D8 补齐型小修（不改变任何语义，消除死角）**
A-P2-1 settle 路由 teardown 窗口：unbind 后对 invBus 终态排空转发 persistentBus，兑现 route() 注释承诺；A-P2-2 材料读失败补 releaseRetention 兜底；A-P2-4 构造失败补 CloseDurable；C-P2-1 quarantine rename 补 dirsync+错误传播、`pending.Add(-1)` 绑定隔离成功；C-P2-2 ClaimNext 锁内补 closed 复查；C-P2-3 DefaultTTL 注释对齐"永开无禁用"（哲学已裁决，不改实现）；C-P2-4 reconcileTTL 对 detector=nil 的恢复任务补会话级回收或显式告警；E-P2-1 sweepRetired 的 current 读移入写锁内；E-P2-2 idx 删除失败保留墓碑（下轮幂等重试）；E-P2-3 deleteSegments 清理失败聚合上报；E-P2-4 locateOrphanEvtSlot 对 ListSegments 失败 fail-loud；E-P2-7 store 构建失败补 relation store 释放；E-P2-9 trajectory 通道路径补 (nil,nil) 防护；D-P2-1 smuggle 正则排除 `&&`（`(?:^|[^&])&`）；F-P2-2 soak 父进程显式 `-timeout 45m`；D-P2-4 resume detector 重注册 monitor 回调（TmuxMonitor 增 RebindCallback，恢复与进程内同构）；E-P2-6 restart 脚本归档截断移至旧进程确认退出、新进程 spawn 之间的交接窗口。

### 乙组：语义收敛型（BREAKING，方向=更彻底的既有哲学）

**D9 谱系判定白名单同源化（B-P2-2 根治）**
现状：`internalLineageValues` 负名单（compress）与投递门扣留清单两份手工同步——每新增内部谱系要记两处，`task-unstamped` 漏配即来。修复：提炼单一 `deliverable` 白名单（外显谱系值域），投递门与 `isExternalizedNotice` 同源消费；**白名单外一律内部（fail-closed）**——与 unknown-withhold 保守哲学同向且更彻底。破坏点：以前误判外显的未知值现正确判内部，行为更收敛无风险。

**D10 结算通知结构化标记（B-P2-3 根治）**
现状：`isSettleNoticeRef` 纯正文前缀 `[task settled` 判定，用户可伪造。修复：`newTaskSettledEvent` 源头写 `settle_notice=true` metadata，识别只认标记；无标记旧事件不折叠（原样保留——安全方向）。前缀启发式退役删除。**形态裁决（奥卡姆复审）**：专用事件类型方案否决——`TypeExternalInput` 是架构声明的唯一总线触发器（event_bus.go:25），且 R2 任务板重建按 `external_input + settle_status` 元数据查询结算（task_record_sink.go:480），改类型破坏两条既有约束；`Source==task` 复用次否决——ref 不携带且语义过宽。metadata 标记实现时与 TelemetryDispositions 的既有 GetEvent **合并为一次读取**（dispositions 本就对每条 notice 查库取 trigger_source，同点顺取标记，零新增查库成本）。

**D11 LocalFileKV 分区快照（E-P2-5 根治）**
现状：每事件提交触发全库 JSON 快照重写（O(n²) 写放大）。修复：快照按分区分片（每分区独立文件 `kv-<pid>.json`），`Sync()` 只重写本次触碰的分区文件——**分区文件本身就是屏障粒度，不引入独立 dirty 集合结构**（StoreEvent 调用链已携带分区身份）。事件级屏障语义不变（Sync 成功=新进程可读回该分区全部键值），写放大 O(全库)→O(分区)。与存储层"分区自治"（mutationMu 分区级、计数分区级、压实分区级）哲学同构。旧 kv.json 不迁移。

**D12 死面清理（G-P2-3 + fsync 死旋钮）**
`DiagnosticsSnapshot.WALQuarantined` 字段与测试桩直接删除（WAL 语义已 REMOVED）。**`memory.fsync` 配置键与 `WithFSync` 选项一并删除**（BREAKING）——该旋钮恒为 no-op，"保留为兼容残留"是配置面上的谎言实体，人工维护成本为负收益；pre-release 无消费者，直接剃除，config.go 的"被接受但不产生任何效果"注释区整体消失。

### 丙组：卫生清零

**D13 注释与文档卫生**：7+ 处错乱前缀人工校正（action_tool.go:55,840、settle.go:86、tmux_executor.go:117、tmux_monitor.go:93,493、declarative.go:14、mcp/call.go:38）；SessionError Godoc 对齐失败极性主载体；README/rl-architecture/compression-and-telemetry/storage-durability-positioning 文档对齐；归档 evidence.md 绝对路径脱敏；wiki 索引重复段删除；全部 specs Purpose TBD 回填；recovery.go 死残留删除；lint.sh 成功打印移位。

**D14 上游依赖跟踪（唯一尾巴）**：go.mod replace 保留；CI 增 `go mod verify`；建摘除跟踪任务（PR #2637 合入后删 replace、升 a2a-go 正式 tag）。

**D16 奥卡姆复审记录（2026-10-02 终审）**：剃除的候选实体——①结算通知专用事件类型（破坏唯一总线触发器与 R2 重建查询两条既有约束）；②独立 dirty 分区集合（分区文件即粒度）；③`memory.fsync`/`WithFSync"兼容保留"（死旋钮直接删除）；④任何新增配置项（本变更全部决策为零新配置，配置面净减少一项）。保留项均通过"复用既有模式"检验：信号级字段（ExitCode 先例）、产生侧分流（onSettle 既有兜底）、白名单（负名单同源收敛）、metadata 标记（既有 Metadata 通道）。

### 丁组：MR 形态

**D15**：修订按主题提交序列落 dev（每提交可独立 revert），全绿后 openspec 归档收口，push 创建 dev→main 的 PR（merge commit 保留主题提交拓扑）；PR 描述承载评审摘要+45 项闭环清单+BREAKING 声明；CHANGELOG 显式记录 BREAKING 与全量修复。

## Risks / Trade-offs

- [范围全量（45 项）回归面大] → 分主题提交+每主题配 fail-before/pass-after 测试；合并前全量：build/vet/单测/E2E 模块/race（D1/D2/D8 触点）/soak 手动跑一轮/offline bench。
- [D2 产生侧分流改变批次组成] → 现有 BatchRetire 测试断言先 grep 排查同步；新增"有归属条目不进批/路由递减可达"钉住测试。
- [D9 白名单值域遗漏真外显谱系] → 枚举现网 Origin 值域（grep 全部 trigger_source 赋值点）后再定白名单；白名单收敛过头表现为"该外投的没外投"——投递门测试覆盖全部外显值。
- [D10 旧事件无标记不折叠] → 方向安全（多保留不丢内容）；soak/回放测试用新标记数据。
- [D11 快照分片改变盘上布局] → 冷启动空库重建（定位已裁决）；offline bench 断言新布局跨进程读回。
- [D12 删字段破坏诊断消费者] → pre-release 无外部消费者；rl 测试桩同步删除。
- [谱系/通知判定多处消费] → D1/D9/D10 各自 grep 全消费点核对（01/02 笔记已存消费点清单）。

## Migration Plan

无数据迁移（定位裁决）。回滚粒度=单主题提交。PR 合并即 main 收敛；go.mod 摘除为合并后独立跟踪项。
