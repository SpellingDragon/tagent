# implementation-hardening — 设计

## Context

- 输入：三份第二轮外部评审（2026-09-13）+ 本地逐条核验（proposal 的 V1-V14 证实清单与 4 项反证、3 项待深钻）；
- 仓库现态：main @ 848b412 之后（pruneTerminal 已修、死代码一清已毕、staticcheck 全绿）；审计 F-1~F-11 在案；无外部用户（0 star），处于 v0.1.0 冻结承诺面前夜——破坏性修正的窗口就是现在；
- 评审定位共识：全部短板属「收尾纪律」而非「能力缺陷」，方向是收敛而非新增。

## Goals / Non-Goals

**Goals**：闭合 V1-V14；三处行为变化（fsync 默认开、YAML strict、RL fail-closed）落地且各有测试；agent 包进 race 门禁；v0.1.0 tag 作为终点门。

**Non-Goals**：见 proposal（子 agent store 漂移、治理匹配强化、token 计量替换、exec 超时、新子系统）。

## 关键决策

### D1 耐久：WAL 每写 fsync，而非批量/周期 fsync（含 *bool 坑）

`appendWALLocked` 在 `w.Flush()` 后追加 `f.Sync()`；snapshot 写入后对目录句柄 `Sync()`（macOS/Linux 目录 fsync 语义差异容忍 best-effort）。配置落点 `MemoryConfig.FSync *bool`（**必须指针**——`fsync,omitempty` 布尔零值无法区分「未设」与「显式 false」，会误关 fsync；nil 归一为 true）。管道：config.go MemoryConfig → wiring.go:287 `NewLocalFileKV`（构造选项 `WithFSync(bool)` 或变体构造器）；flushLoop（2s 周期）与 threshold=50 提前 append 两路均汇入 appendWALLocked，fsync 单点生效。

- **理由**：RelationStore 每行 Sync 是库内既有先例，耐久标准统一；事件写入 QPS 低（每 turn 数条），每写 fsync 代价可承受；「常驻记忆中枢」定位下正确性优先。
- **替代（拒）**：周期/阈值 fsync——掉电窗口内丢已确认事件，恰是卖点场景；group commit——复杂度不成比例。
- **测试**：durability 测试补「Sync 后模拟崩溃（不 Close）→ 新实例可读」路径；fsync 关闭路径回归。

### D2 RL 认证：API 层 token + 宿主侧 loopback 守卫（依侦察 R9/R26 修正）

侦察定谳：`rl.HTTPAPI` 是 `http.Handler`（ServeHTTP 路由，rl 包无 config 节），监听地址由宿主 `http.ListenAndServe` 决定（wechat-bot main.go:235 监听 `:port` 全接口）。故 D2 落地面修正为：

- **token 中枢在 ServeHTTP 顶部**（单一强制点，路由前）：`h.SetAuthToken(token)`（或构造选项）启用后全端点验 `Authorization: Bearer`，401 拒绝；提供 `rl.AuthTokenFromEnv()`（读 `TAGENT_RL_AUTH_TOKEN`）助手。
- **loopback 守卫为导出助手** `rl.ValidateListenAddr(addr, token) error`：token 未设且 addr 非 loopback（127.0.0.1/::1/localhost）返回错误（列三出路）；宿主启动前 MUST 调用。wechat-bot main.go 接线。
- **部署迁移**（现有 wechat-bot 监听 `:8080` 类全接口，fail-closed 直接打破）：wechat-bot 默认改读 `TAGENT_RL_AUTH_TOKEN`，未设时默认改绑 127.0.0.1 并在日志给出指引；远端部署邮件同步（9.3）。

- **替代（拒）**：默认全开放 + 文档警告——评审已证「可信内网」假设不成立即最严重缺口；mTLS——对单部署场景过重；在 tagent config 加 rl 节——rl 本无 config 面，为认证单点扩 config 面不值。

### D3 nil 裂缝：RestoreTask 补全生命周期字段 + 模式级审计

V1 根修：RestoreTask 置 `watchDone: make(...)`、`firstSettle: make(...)`（detector 保持 nil——跨重启不可复原是设计语义）。Resume 的 `newWatch` 判定基于 `task.detector == nil || detector != task.detector`，nil 分支直接走新 watch，不再依赖 close 旧 chan。Spawn/Resume 的 select 对 nil detector 增加 `if detector != nil` 的 Detached 分支守卫（纯同步语义：nil detector 只等 firstSettle，与注释宣称的「blocks here until settle」对齐）。**模式级审计**：全库 grep `close(` / 接口方法裸调点，输出清单入 LEDGER。

- **fail-before 先行**：Resume-on-restored-task 的 panic 复现测试先落，再修。
- **替代（拒）**：detector 类型化非 nil 保证（包装类型）——改动面大于收益，守卫式修复与库内既有风格（pruneTerminal 修复）一致。

### D4 KeepRecentTasks：参数传递替代共享字段改写

`context_compressor.go:333-335` 的「暂存-改写-defer 恢复」在并发压缩时是数据竞争。修复：SmartCompressor.Compress 增加可选参数（`CompressOptions{KeepRecentTasks int}`），调用方传 `cc.keepRecent`，删除共享字段改写。单一 BeforeModel goroutine 契约仍保留，但不再依赖它防此特定竞争。

### D5 资源回收：in-flight 计数 + 延迟 Close

- **旧 runner**：SwapExecutor 已返回旧 runner（context_manager.go:414）。调用方维护代际序列：ring-2 保留最近两代（回滚需要），跌出 ring 的 runner 以 in-flight 计数延迟 Close——RunFlow 持有 runner 引用期间计数 >0，归零且已跌出 ring 才 Close（若实现 io.Closer）。**定时兜底**：retired 清单另配年龄阈值清扫（如换代后 10 分钟仍因持续负载未归零则强制断言式 Close）——补教「持续多流负载下全局 in-flight 可能永不归零」的软点（评审后自查发现）。
- **旧 model**：SwappableModel.GenerateContent 包裹计数，Swap 换下的 model 同型延迟 Close。
- **lastEventKeys**：封顶 4096，超限按 value（int64 eventKey，时间单调）淘汰最旧一批（全量扫描 4096 条可忽略）。
- **替代（拒）**：立即 Close——in-flight turn 会崩；不 Close（现状）——评审已证泄漏。

### D6 配置：KnownFields strict + 环检测（两个解析点）

`yaml.NewDecoder + KnownFields(true)` 替换 `yaml.Unmarshal`——侦察定谳共**两处**：config.go:882（主配置）与 tool/mcp/registry.go:274（mcp 段），两处同步 strict。错误信息列出全部未知字段。buildAgent 递归入口加 `visited map[string]bool`，遇环返回配置错误。Strict 属破坏性变更：仓库内全部 yaml（示例/测试/资源 + wechat-bot 独立模块）跑一遍迁移验证。

### D7 幽灵清理：删而不藏

TypeToolUse 常量、NewToolUseEvent、相关注释段整体删除（git 历史即档案）；event_bus.go 头注释改写为如实表述：「turn 间事件邮箱 + turn 内框架原生 ReAct」。modelref 死导出同删。IsTmuxAvailable 改 `exec.LookPath("tmux")` 真探测，调用方 :176 的降级分支随之获得真实语义。

### D8 文档对齐：改文案不改默认

README「事件永久入库/永久存储」→「事件不可变入库，默认按类型 TTL（3-30 天）遗忘曲线，可配置永久」；心智模型表「永久」列改「按类型 TTL（可配永久）」。**默认 TTL 不改**——无界存储不是合理默认，叙事让位于现实而非相反。compaction.go 化石注释改真（L3 = 低价值类型清空 Content）。guardrail 评估输出在治理关闭时附一行显式声明（`governance disabled: denial/critical signals unavailable`）。

### D9 冷分区与重启（侦察已定谳大半）

- **冷分区（0.1 定谳：评审证实）**：`FileSegmentStore.Init()` 实为空函数（segment_store.go:166-176 自注 "lazily initialized on first access"），分区惰性发现——重启后未再写入的分区不在内存态，TTL/容量/压实对其停摆。修复：启动时枚举分区集（LocalFileKV 侧可全量扫内存 data map 的 `{pid}:meta:` 前缀模拟前缀扫描；KV 六方法接口若不敷用，经 FileSegmentStore 内部通道而非扩接口）接入遗忘扫描；eventCount 恢复或显式文档化放弃。startup-active-partition-bitmap spec 明注「未实现/等价形态达成」，不复活位图方案。
- StopLoop→StartLoop：静态侦察 event_loop.go 无 close(outputCh)、StopLoop（lifecycle.go:196-208）不关 outputCh——重启大概率安全；0.2 e2e 定谳，**并须加查 meditationMgr 是否随 StartLoop 重建**（StopLoop 停冥想，若 StartLoop 不重建则冥想静默失效——新识别坑）。
- spec 线性漂移：grep 未检得「线性/linear」，留 0.3 通读定谳。

### D10 race 门禁与 v0.1.0（依 N8 探索轮修正）

V17 定谳：agent 包 -race 十案中**无本地可修案**——上游内部（inmemory session service / steer 关闭）为主，另含测试 mock 自身竞态。处置：① A 类→CI 豁免清单 + 向上游 trpc-agent-go 报 issue（附栈证据）；② B 类（loopMockTool.getCallCount）→测试侧加锁修复；③ 审计 F-4 的「Session.Clone」描述在 LEDGER 更正；④ **不为过门禁而改生产码**。CI 锚点 ci.yml:39-52（race job 现排除 agent 包，注释 :48-50）。全量回归绿 → LEDGER 回写 → 打 v0.1.0 tag + CHANGELOG 定稿。

### D11 架构防呆立法：正确性上梯，四件入案，其余路由

背景：行为模式问题（收尾纪律跑输设计雄心）只能用结构治——结构不会疲倦。防呆梯级：L0 注释/口头契约（靠人）→ L1 守卫+fail-before（靠测试喊疼）→ L2 类型（靠编译器拒绝）→ L3 结构单点（只有一个地方能写）→ L4 可删除（错了就删，代价近零）。本案新增项落 L1/L2/L3 各一，与既有正例（buildMode=L2、EventTypeSpec=L3、git 原生=L4）共构梯子全景；「能上梯则上梯」入守则。路由表（立法三件+软点补丁入案，其余缓行）：

| 学说项 | 路由 | 依据 |
|--------|------|------|
| 分层依赖方向断言（L2） | **本案 7A.1** | 已核验现状全绿（memory/plugin/event 无违规 import、agent 不引根包、event 纯叶子）——纯新增固化，零修复成本 |
| 上游假设钉：I2/插件管线同步等待（L1） | **本案 7A.2** | 已核验 invariants_test.go 有注无测（I2 恰是依赖上游内部行为的那条）——「定时炸弹」拆引信 |
| 红色耦合台账 | **本案 7A.3** | 上游假设+隐式耦合显式化，逐项钉测/豁免标注 |
| 解析单点（L3） | **本案 6.1 内** | 两处 Unmarshal 抽共享 strictDecode，一实现两调用点 |
| retired 定时兜底 | **本案 5.1 内** | 持续负载软点补丁（D5） |
| god file 职责图解体 | **缓行（冻结后首变）** | 冻结前夜不动大结构；准绳（变更局部性：一个意图一个落点）随本案立法入 spec |
| detector/EventKey/PartitionID 类型化（L2） | **缓行（backlog）** | 改动面触及全部调用方与测试，与收敛性质冲突；守卫（1.2/1.3）先行已消除可达 panic |
| 「新增后端」注册表化（L3） | **缓行（backlog）** | 当前仅两后端，无第三实例支撑——建缝判据（变更已发生过或已在 roadmap）尚未满足 |

## 风险与缓解

| 风险 | 缓解 |
|------|------|
| fsync 拖慢写入（大量事件场景） | 可配置关闭；durability 测试量测并记录开销数据于 LEDGER |
| strict YAML 破坏现有部署配置 | 仓库内 yaml 全量迁移验证；错误信息列出未知字段；v0.1.0 前是唯一窗口 |
| RL fail-closed 破坏现有远端训练部署 | 部署文档同步；错误信息给出三选一指引（配 token/改 loopback/显式 `allow_insecure` 逃生门——默认无） |
| 删 TypeToolUse 影响序列化兼容 | 零生产者零消费者已证；存储中不存在该类型事件 |
| 延迟 Close 引入新竞态 | in-flight 计数以 atomic 实现；Close 幂等（io.Closer + sync.Once 模式，库内已有先例 segment_store.go:141） |

## 迁移与顺序

实施序：WP0 深钻定谳 → WP1（正确性）→ WP2（耐久）→ WP3（安全）→ WP5（死码，先删再做 WP4 免得给死码做回收）→ WP4（资源）→ WP6（配置）→ WP7（文档）→ WP8（race 门禁 + soak + tag）。每包独立可提交、独立可回归；WP8 以 WP1-7 全绿为前置门。

## 侦察定谳汇总（R1-R28，2026-09-14 只读核验，实施勿再臆测）

| # | 定谳 | 对任务的影响 |
|---|------|-------------|
| R1 | yaml.v3 v3.0.1 在用，KnownFields 可用 | 6.1 无版本障碍 |
| R2 | Unmarshal 共两处：config.go:882 + tool/mcp/registry.go:274 | 6.1 须两处同步 |
| R3/R28 | SmartCompressor 为自有具体类型（*SmartCompressor），无上游接口赋值 | 1.4 加参安全 |
| R7/R23/R24 | LocalFileKV 构造唯一于 wiring.go:287；Close=close(flushDone)+Sync；flushInterval 2s/threshold 50；MemoryConfig.Type/Path 在 config.go:393 | 2.1 管道与 *bool 坑已标注 |
| R9/R26 | HTTPAPI 是 Handler，无 rl config 节；wechat-bot main.go:208/235/241 为接线点 | D2 已按 API 层重写 |
| R11 | 上游 model.Model/runner.Runner 无 Close 方法（须 io.Closer 类型断言式回收） | 5.1/5.2 实现形态定为类型断言 |
| R12 | event/registry.go 无 tool_use 条目 | 4.1 删除面仅 agent/event_bus.go |
| R13/R14 | 我方无 Session.Clone 调用点（竞态在上游内部） | 8.1 必须实测栈定位 |
| R16 | ci.yml:39-52 race job 现排除 agent（注释 :48-50） | 8.2 单行扩面 |
| R17/R22 | Init() 空函数、分区惰性发现；bitmap spec 自注未实现 | 0.1 定谳=证实；2.4 修复路径已定 |
| R19 | recall.go:71-72 items 分支无上限 | 0.4 定谳=证实；3.3 锚点 |
| R20 | 评估事件构建于 evolve.go:218 improvementContent | 7.3 声明落点 |
| R25 | RestoreTask 唯一生产调用方 task_record_sink.go:272 | 1.1 测试构造走 TaskManager 直调 |

## 探索轮定谳（N1-N12，2026-09-14 深钻，覆盖 R15 之误）

| # | 定谳 | 对计划的影响 |
|---|------|-------------|
| N1（翻案） | StartLoop 循环 goroutine defer `close(ta.outputCh)`（lifecycle.go:180），StartLoop 复用成员不重建：Stop→Start 后消费者读已关通道；二次 Stop 再 close 已关通道 → panic 逃逸 recover → 进程崩。R15 反证系核验不完整（漏看 StartLoop 侧 defer） | 新增 V15；1.6 实施裁决（探索后改判）：**方案 D——二次 StartLoop 显式拒绝**（spec 允许之 (b) 形态）。原方案 A（重建成员）被证据否决：① 宿主 wechat-bot main.go:315 `for range outputCh` 依赖 range-close 契约（去 close 的方案 B 否）；② AppendEventHook 闭包捕获**构造期局部通道**（agent.go:391-414），重建成员须连带重绑 hook 与 cm.outputCh——为无已知用例的同实例重启动构造序，不值；同实例重启无既有消费者（grep 零用例）。修复：StopLoop 终结态 + 二次 Start 返回明确错误 |
| N2 | meditationMgr 随每次 StartLoop 重启（lifecycle.go:186-188；Start 每次新 ctx/wg 合法） | 1.6 无需冥想修复 |
| N3 | spec 线性定级实锤（spec:24-30 vs 代码指数 {k,2k,4k}） | 0.3 定谳=漂移；7.2 修 spec |
| N4 | Resume 构造路径定谳：合法源四态（:864）；ResumeFn nil 提前报错（:874）；close(nil) 在 :900 | 1.1 测试构造：RestoreTask(status=TaskStable) + Spec.ResumeFn 返回新 detector |
| N5 | 冷分区全链实锤：lifecycle.go:132/199 正是 `store.partitions.Range`（sync.Map，segment_store.go:136），分区仅首次写入惰性注册 | 2.4 修复=启动时把持久化分区注册进 sync.Map；LocalFileKV 加 `ListPartitionIDs()`（内存 data map 扫 `^(\d+):meta:`），FileSegmentStore 以**类型断言式可选接口**消费（与 lifecycle.go Close 断言风格同），不扩 KVStore 六方法 |
| N6 | LocalFileKV = kv.json 快照 + wal.jsonl；compaction 时重写快照+截断 WAL | 2.1 目录 fsync 落点=快照重写处 |
| N7 | SwapExecutor 注释自言「调用方决定处置」；currentRunner RLock 取引用即放（in-flight turn 持旧引用跑完） | 5.1 细节：ContextManager 维护 retired 列表 + 全局 in-flight 计数（RunFlow 入口 inc/defer dec），dec 归零时扫描 retired 逐个 `io.Closer` 断言 Close（保守案：换代后到下一空闲点才关，可接受）；tagent.go 换代后把跌出 ring-2 的 old 调 `cm.RetireRunner(old)` |
| N8 | race 十案三分类：A=上游内部（inmemory session service / steer 关闭，tagent 帧仅路过，无本地可修）；B=测试 mock（loopMockTool.getCallCount）；F-4「Clone」描述失准 | 8.1 改为分类处置（A→豁免+上游 issue；B→测试修）；**不改生产码修竞态**；审计 F-4 在 LEDGER 更正 |
| N9 | buildAgent cache 于尾部填充（:259/:592）→ 环=真栈溢出（评审正确） | 6.2 visited 检测必要性坐实 |
| N10 | `ta.Rollback()` 已存在（task_record_sink.go:125-129）；ring-2 在 reload 闭包（prevKeep/prevSnapshot，tagent.go:323-328） | 5.4 纯宿主接线（SIGUSR2→ta.Rollback()），核心零改动 |
| N11 | recallByItems 真身在 memory_recall.go:84/97（非 recall_subtools.go） | 3.4 锚点修正 |
| N12 | ServeHTTP 路由 switch（/task、/diagnostics、/feedback/wait）；/feedback/wait 为 30s long-poll | 3.1 认证落 ServeHTTP 顶部确认；long-poll 在认证后无豁免问题 |
