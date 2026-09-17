# 实施笔记（resident-readiness-plan）

## 1.1 基线对比（2026-09-17）

- 实施时 HEAD：`aeb273dee72f8fb72c581a35c31344d6d2db669b` = 评估基线；`git status` 仅新增 `openspec/changes/resident-readiness-plan/`（本变更工件），无任何生产代码漂移。
- 结论：`assessment.md` 的 F01–F11 状态（仍在/部分/已修/未验证）逐条沿用，无需刷新；三份历史报告保持原文未动。
- 变更→测试映射（WP0 范围）：
  - `scripts/race_check.sh` → `scripts/test_race_check.sh`（新增）+ 真实集成两条（合法包/非法包）。

## 1.5 上游 race 豁免台账（登记面：scripts/race_check.sh 头注 RACE_WAIVER_SIGNATURES）

| 测试（skip 点） | 文件 | 依据 | 解除条件 |
|---|---|---|---|
| TestRunEventLoop_EmptyContent_ReasoningFallback | agent/agent_loop_edge_test.go | U2/U3 | 上游修复并升级后删除 skip |
| TestRunEventLoop_TrulyEmptyResponse_DoesNotHang | agent/agent_loop_edge_test.go | U2/U3 | 同上 |
| TestTagentAgent_Run_InjectMessageRoutesToSubAgentBus | agent/agent_loop_edge_test.go | U2/U3 | 同上 |
| TestRunEventLoop_ToolCallResponse | agent/agent_loop_test.go | U2/U3 | 同上 |
| TestSubAgentRun_ToolResultStopsPrematurely | agent/session_subagent_toolstop_test.go | U2/U3 | 同上 |
| TestSubAgentRun_SlowLLM_ToolResultStops | agent/session_subagent_toolstop_test.go | C3（时序 flaky，非 race 豁免） | 定位时序后改为确定性断言 |
| TestSubAgentRun_RequestOrdering_UserAfterSystem | agent/session_subagent_toolstop_test.go | U3 | 同 U2/U3 |
| TestI2_BeforeModelCompleteness_RealPipeline | agent/invariants_i2_test.go | U2/U3（钉测走真实管线） | 行为覆盖保留在非 race CI；race 下恢复条件=上游竞态修复 |

- 上游签名：U2 = runner steer 队列关闭/事件消费并发（invocation/steer 栈族）；U3 = inmemory session service hook 链（session.go 栈族，审计 F-4 的 Session.Clone 描述系误判）。依赖版本 trpc-agent-go v1.10.0。
- 纪律：wrapper 只对「登记签名命中且无 tagent 帧、且无非 race 失败」的输出豁免；未知上游 race 必须 FAIL 并登记，不得因"栈在依赖内"一律放行。禁止扩大 skip 面。

## 1.2/1.3 race wrapper 修复记录

- `scripts/race_check.sh` v2：go test 退出码权威；无 race 文本的失败（非法包/编译/断言/超时）恒非零；豁免需同时满足「全部 race 顶帧属上游 + 命中登记签名 + `--- FAIL:` 数 ≤ race 报告数」。
- 签名对符号行与路径行双匹配（上游符号用点连包名，模块缓存路径含 `@v1.10.0` 版本段，两形态都要覆盖——实测 `trpc-agent-go/session/session\.go` 对缓存路径漏配已修为 `trpc-agent-go.*session/session\.go`）。
- `scripts/test_race_check.sh`：12 例（含旧假绿场景的端到端红），全过。
- 现场实证：修复当天定向 race 即捕获一次 `TestBindDetector_SignalsReachManager` 失败——旧脚本会将其吞成 OK（无 race 文本 → exit 0），新门禁正确拦截。

## 1.6 WP0 准出复验（2026-09-17，Go 1.24.1 darwin/arm64）

| 命令 | 结果 |
|---|---|
| 根 `go build ./...` + `go vet ./...` | 通过 |
| 根 `go test ./... -short -count=1 -timeout=120s` | 29 包 ok |
| `go test ./memory/... ./agent/... ./plugin/... ./rl/... ./evolution/... ./event/... ./tool/... -race -short` | 24 包 ok |
| bot 模块 build/vet/short | ok（独立 go.mod） |
| `python3 scripts/test_verify_restart_prefix.py` | 5 例全过 |
| `bash scripts/test_race_check.sh` | 12 例全过 |
| `openspec validate --all --strict` | 94 通过 / 0 失败 |
| delta 语义核验（MODIFIED/REMOVED 必在主 spec、ADDED 不得碰撞） | OK |

### 新发现 N-1（1.6 捕获，已修）

- 现象：整包并行 -race 下 `TestBindDetector_SignalsReachManager` 偶发 settles=1（期望 2）；单测 20×race/20×非 race 均过。
- 定性：tagent 自有测试确定性缺陷，非产品缺陷、非上游。`watch` goroutine 按既有设计先 `applyStatus`（状态可见）后 `emitBackground→OnSettle`（通知入账）（batch2 1.7/N1 同信号流水两段）；测试观察终态后立即断言通知数，撞进「状态已置、通知未入账」窗口。三类 `drop post-terminal` WARN 为其它用例的预期 fence 输出，与本失败无关（最初误归因已排除）。
- 处置：断言前以 waitUntil 等待 `len(settles)==2`（bind_detector_test.go）；修复后单测 30×race + task 包 race/非 race 全绿。无产品代码改动，无 skip 扩面。

## WP1 实施记录（2.1–2.10，2026-09-17）

**2.1 事件级屏障 fail-before → green**：`memory/segment_store_barrier_test.go`（memory_test，env 守卫子进程）——单事件 StoreEvent 成功后不 Close 直接终止，旧实现丢失（红），屏障落地后独立进程读回原文+索引（绿）。

**2.2 故障注入**：`LocalFileKV` 增加 `ops fileOps` 可注入面（openFile/syncFile/rename/remove/syncDir）；`memory/kv/local_file_kv_fault_test.go` 7 例：WAL open/sync、首次 WAL 目录 sync、快照 rename/目录 sync/WAL remove 注入失败 → Sync/Compact 传播错误 + pending 保留 + 痊愈后重试完成屏障。

**2.3 提交协议**：`FileSegmentStore.StoreEvent` 串行化——碰撞检查 → evt → idx →(meta) → **Sync 屏障** → 缓存/计数发布；成功即已越屏障（独立进程可读）。LocalFileKV：首次 WAL 创建后目录 Sync（失败保留标记重试）；平台不支持目录 fsync（EINVAL/ENOTSUP/EOPNOTSUPP）记 degraded 告警，其余错误传播；KVPut 注释明确异步接收语义。

**2.4 错误可观测**：阈值/定时 flush 失败记入 `LastError()`（pending 保留），不再静默；非“不支持”目录 sync 错误向上传播。

**2.5 typed 错误**：`memory.ErrKeyNotFound`/`ErrDuplicateEventKey` 哨兵；LocalFileKV、RustVikingClient（null 值）、mockKV 对齐；GetEvent 区分 missing/I/O；GetEvents 部分结果+错误；QueryEvents/scanPartition 分区与窗口扫描错误以 firstErr 随部分结果返回；必需 meta 写失败改 fatal。

**2.6 孤儿补齐**：同键同内容重试 = 幂等完成（重跑屏障、补 meta、计数恰一次）；同键异内容仍拒（D15）；半孤儿（idx 有 evt 缺）以重试内容为记录；孤儿可见性为 D1.3 已声明边界，由补齐收敛。

**2.7 后端 parity**：InMemoryStore 重复键拒绝（typed）、无分区查询空、存取双向克隆；FileSegmentStore 缓存/返回克隆；差异文档化：file 有提交协议（同内容幂等），memory 测试后端简单拒绝。调用方对齐：tool/recall 三个测试文件显式授权种子分区。

**2.8 计数重建**：`RebuildLiveCounts`（枚举→去重→墓碑过滤→赋值）接入 wiring（墓碑恢复后、扫描器启动前）；`countsKnown`/`StoreStats.CountsKnown` 可观测；unknown 时 `checkCapacity` 整体短路（不淘汰、不冒充 0）。mockKV 补 ListPartitionIDs/Sync 以可测；无枚举后端（noEnumKV）保持 lazy discovery。

**2.9 递减时点**：计数移到屏障成功后（写失败/重复拒绝不增）；压实搬迁不再递减（搬迁净零 + 墓碑在标记点已减——旧 deleteSegments 递减删除）；DeleteEvent 幂等（typed miss → nil；已墓碑 → nil），杜绝二次递减。

**2.10 验证**：`storage_contract_test.go`（parity/typed/克隆/计数重建/unknown/屏障失败经 ErrorTrackingStore 传播且失败不计数、补齐后恰一次）；全量 short 绿 + memory/plugin/agent/recall 定向 race 绿。fsync 两档（benchtime=200x，M3 Pro）：FSyncOn ≈ 5.63ms/op（每写屏障最坏情形）、FSyncOff ≈ 0.356ms/op（≈15.8×）；生产屏障在事件级、批级摊销，既有 audit 结论（摊销影响可忽略）维持。

**遗留至 WP2**：ErrorTrackingStore 的 mem_spill 兜底在屏障失败场景的落盘行为由既有 fault_injection 覆盖；spill→inbox-v1 改造见 WP2。

## WP2 实施记录（3.1–3.11，2026-09-17）

**已完成（8/11）**：
- **3.1 可判定接收**：`InjectMessageContext`/`PublishContext` 返回 `PublishReceipt{RequestID,Durable}`；终结态拒绝（ErrLoopTerminated）、满队列/超时显式错误（ErrPublishTimeout）、nil 拒绝；`PublishDropped()` 计数（PublishContext 唯一计数点，void 包装只记日志）。
- **3.2 durable inbox**：新组件 `agent/reliability.Inbox`（inbox-v1 目录、tmp+rename+fsync+目录 Sync、零填充 seq 全序、上限 2560 typed 拒绝、quarantine 隔离保留）；`NewReliableEventBus` 改返回 error（fail-loud，ErrLegacySpillNotDrained 拒绝旧格式）；channel 只作唤醒（专用 `inbox_wake` 哨兵，消费端过滤，杜绝双份投递）。并发 10×10 race 测试验证无超车无丢失。
- **3.3 envelope**：RequestID+Source+Messages 整批持久，全 durable 后才 accepted。
- **3.7 迁移/恢复/关闭**：旧 .spill 未排空拒绝启动；损坏项 quarantine；重开时 claimed→pending 重放（attempts 递增）、receipted 保留；`CloseDurable` 挂 agent Close（未确认项留盘，关闭≠丢数据）。
- **3.8 RecoveryResult**：mode/status/scanned/projected/truncated/missing_keys/pages_failed/batch_errors/payload_errors/duration；水合按请求键集合对账（无错短读也记 missing）；payload 坏→failed 不伪报空库；经 `TagentAgent.RecoveryResult()` 供诊断。
- **3.9 fallback 重构**：分页全扫 → 过滤 skipProjectionEvent → 滚动保留最新 500 **有效**事件 → EventKey 升序投影；600 有效+600 内部记录测试：保留 500 全有效、truncated=100、status=partial。
- **3.10 双消费者可见**：diagnostics getter + 首次模型请求尾部一次性 `[recovery]` 提示（不入事实链、不改历史前缀、健康路径零 token）；测试验证 one-shot。
- **3.11**：全量 short 29 包绿；memory/agent/plugin 定向 race 绿；bot 模块绿。

**未勾差距（3.4/3.5/3.6）→ 已于同日收口（见下节）**：
- 处理完成 receipt 目前持久于 inbox 信封 `state=receipted`（ack 前崩溃→跳过重执行 ✓；ack 失败→幂等 ✓），但 **spec 要求的「receipt 注册为事实链非投影事件」未实现**——receipt 真源暂在 inbox 而非事实链。
- 「入库后、执行前崩溃→重执行」成立（at-least-once），但重复执行 turn 会以新 key 再次入库相同事实——**事实链 request-id 幂等去重未实现**，投影可能重复追加一次（与 delta spec 场景 2 的「投影不重复追加」不符）。
- receipt 免 TTL 窗口、30 天保留、outstanding-only 去重索引未实现。

## 3.4/3.5/3.6 收口记录（同日追加）

- **receipt 事实链化**：新类型 `event.TypeInboxReceipt`（registry 单点声明：TTLDays=30 即 request-id 去重窗口、非投影/非嵌入/不可召回）；`ta.finishDurableBatch` 对每个消费信封先 StoreEvent receipt（stored-gate：失败⇒不 receipt 不 ack，信封留盘重放——「绝不无凭据 ack」），成功才 `ConfirmDurable`（receipt+ack）。`skipProjectionEvent` 排除 receipt（重建不入投影）。
- **request-id 幂等去重**：persistBusEvent 成功后把事实 EventKey 回写信封（`RecordEventKeys`）；重放时 claim 事件带 `inbox_dedup_keys` → persistBusEvent 复用**同 key**：GetEvent 校验存在则跳过 StoreEvent（缺失则同 key 同内容确定性补齐），投影 Append 按 key 天然幂等——「原始事件与投影不重复追加」达成。回写失败仅降级 at-least-once（信封自身是 outstanding 真源，「去重索引只随 outstanding」语义达成）。
- **测试**：`inbox_receipt_test.go` 三例——重放不双写+receipt 落链不入投影+确认后清空；receipt 写失败保持 claim；dedup 键缺失 fallthrough 补齐。registry legacy TTL 清单守卫更新（inbox_receipt:30）。
- **验证**：全量 short 29 包绿；agent/memory/event/plugin race 绿；bot 模块绿。

**实现注记**：durable 唤醒哨兵（inbox_wake）过滤；drainChannel 拆 non-wake 变体；m5 全序窄窗随 inbox 串行化自然消除（单点锁分配 seq）。

## WP3 部分实施记录（4.8 / 4.4，2026-09-17）

**4.8 model 流生命周期租约（fail-before → green）**：
- fail-before：`rl/swappable_model_stream_test.go`——Swap 时旧流仍开 → 旧 model 被立即 Close（closed=1，红）；A→B→A 且 A 在用 → sweep 关掉 current（红）。
- 修复：①租约 acquire 移至 GenerateContent 开头、release 挂**转发 goroutine**（包装 returned channel，逐条转发到上游 close/取消）——lease 覆盖全流生命周期；error/nil 流立即释放；泄漏流保守保活（绝不关可能活着的资源）。②sweep 跳过 current inner（A→B→A 不误关重新在用实例；retired 去重防 A bounce 双份）。③Close 恰一次（Eventually 断言）。
- 陷阱记录：初版把 acquire 丢在重构里（只 release 不 acquire → 计数变负/为 0 → sweep 误判空闲）——lease 必须「acquire 于调用点、release 于流尾」配对。
- 验证：rl 包 short + race 绿；全量回归绿。

**4.4 分区哈希碰撞 fail-closed（fail-before → green）**：
- fail-before：共享 localfile store 的两个碰撞名 agent → New 成功（静默合并命名空间，红）。
- 修复：`rc.registerStoreOwner(name, memStore)`——以**装饰前**的底层 store 指针登记 owner（engine bridge 按 agent 各自包装会让指针身份失效，曾致检测不触发）；同 store 内不同名同 pid → 构造期 fail-closed（列明冲突名，绝不自动迁移/改 key）；shell 热更同名登记幂等；隔离 store（空 path）各自实例无误报；`rc==nil`/未初始化对直接 buildAgent 测试调用安全。
- 测试：碰撞对暴力搜索 + 共享 store fail-closed + 隔离 store 无误报。
- 验证：全量 short 29 包绿；根/agent/rl/event race 绿；bot 模块绿。
- 教训：粗粒度文本替换曾截断 `rc := &runtimeConfig{...}` 字面量（用 git diff 审查 caught）——组合根改动必须逐次 build+diff 审查。

## 4.1/4.2/4.3 收口记录（同日追加）

- **4.1 fail-before 实测**：T1 幸存者存活、T2 重开——因 WP1 屏障（closed KV 的 Sync 仍能 flush WAL）呈「巧合持久」而意外绿；T3 冲突拒绝红；T4 中途构建失败租约释放绿。语义显式化后（4.2 落地）四场景全绿 + race 绿。
- **4.2 RuntimeResources**：新 [resources.go](resources.go)——`{kind, canonical path}` 单 entry、acquire/release 租约、fingerprint（type/path/fsync/lifecycle/engine/rvbin JSON）冲突→`ErrResourceConflict`；release 归零→幂等 Close+移除登记（重开=真正新实例）。`resolveMemoryStore` 三后端全走 registry，签名改 `(store, release, error)`；`buildAgentDFS`：常驻 agent acquire（release 绑 `TagentAgent.MemStoreRelease`→`Close` 执行），**执行壳借用不 acquire**（不持租约、绝不触发关闭）；构建失败 `buildOK` defer 统一 release（替代旧 path=="" 特判）。namedMem/File/RV 裸 map 全量退役。
- **4.3 单 writer**：`acquireDirLock`（`.tagent-writer.lock` + `SYS_FLOCK` EX|NB）——跨进程互斥、崩溃 OS 自动释放；release 时 `F_UNLCK`+关闭。测试：第二持有者拒绝（ErrStoreLocked）、释放后可重取。
- **验证**：全量 short 29 包、race 13 包、bot 模块全绿；Ownership 四场景 + WriterLock + PartitionCollision race 绿。
- **边界注记**：`namedEngines`（engine 缓存）本期未租约化——engine 配置已入 fingerprint（变更即拒绝共享 store），engine 生命周期随 store 归零关闭的联动清理列为后续项（engine 本身无跨 agent 危险写路径，向量派生物可重建）。

## 4.5/4.6/4.9/4.10 收口记录（2026-09-18；4.7 部分完成）

- **4.5 身份绑定 + 拓扑增减拒绝**：`rc.residentAgents`（name→常驻 agent 绑定表，New 构建后填充并经 `SetResidentTable` 共享全拓扑）；热更壳子树按 agent 身份借用其常驻 store（build_agent.go shell 分支），绝不全部复用 entryMemStore；reloader 结构分支前置**拓扑增减 fail-closed**（`reachableAgents` 沿工具引用计算启动/最新可达集，新增/删除 agent → ERROR+告警+不换代）。**设计边界**：热更拓扑增减（新增/删除 agent 资源的创建/退役）以「拒绝+提示重启」替代动态生命周期管理——保守正确，动态拓扑增减列为后续变更。
- **4.6 三 agent 混合热更集成**（hotreload_multiagent_test.go）：entry+sub1+sub2（各自隔离 store）同时变更 entry 工具（结构）+ sub1 keepRecent 2→5（数值）→ 断言：三 agent store 指针不漂移、sub1 keepRecent 热生效、sub2 不受影响；sub3 新增 → 拒绝且不入常驻绑定。
- **4.7 部分完成（保持未勾）**：①memory 拒绝不推进 effective（tagent.go 已删 lastMemFP 推进）✓；②hotParamsFor 全量 desired（缺失字段回落解析默认：entry 8000/子 4096/0.8/keepRecent 2/2m/1h）代码已落 ✓；③**introspection 盲区未收口**：`OrgKeepRecent` 读 ContextCompressor.keepRecent，而启动显式值只喂内层 SmartCompressor（双真源）——SeedKeepRecent 初版修补已回滚（需与 cc 构造默认路径合一，见 §9）；FieldDeletion 测试 SKIP 登记待启用。断言不可观察 ≠ 回落语义缺失：ApplyOrgHotParams 全量 desired 的行为正确性由日志与实现保证，待 introspection 合一后以测试固化。
- **4.9 核验通过**：rollback 真源=配置快照（prevSnapshot.cfg，启动代即 ring-0）✓；Close 顺序（StopLoop→cleanup→closers→inbox→memStore 租约→cm→recorder）✓；壳借用不持租约（构建失败不误关常驻资源）✓。
- **4.10 回归**：全量 short 28 包绿（FieldDeletion SKIP 显式）；race 11 包绿；bot 模块绿。

### 事故与教训（§9 登记）

- **S-1 git checkout 单文件回滚事故**：修 introspection 时误执行 `git checkout agent/context_manager.go`，抹掉该文件全部未提交修改（WP2 recovery 字段 + persistBusEvent dedup/回写）——按会话记录重放恢复并回归验证。教训：**多轮未提交工作严禁对已改文件执行 git checkout；回滚前必须 stash 或 diff 备份**。
- **S-2 4.7 introspection 盲区**：cc.keepRecent（ContextCompressor）与 sc.KeepRecentTasks（SmartCompressor）双真源——启动显式值只进 sc，热更 UpdateKeepRecent 双写；待合一（Seed 路径或构造参数贯通）后启用 FieldDeletion 测试。

## 远端 trajectory 分析（2026-09-18，驱动任务 6.7）

数据源：agent mail 远端原始包（83 条 / 17.6MB gzip / batch 187-269，覆盖知识库任务与 F3 图片入史）。分析器：`rl/trajectory_analyze.py`。

- 压缩本窗口零触发（prompt 29.7万→32.9万贴线爬行；触发线 327,680）；09-16 首点后 floor=297,150（72.4%）确认回收不及预期。
- 根因量化：task_settled 结算类 external_input **136 条占 67.8%**（reincarnation-orphan/zombie 批量退役通知以 `---` 合并成 2.2 万字符单条消息固化进投影）——工具对折叠不覆盖 external_input，结构性不可回收。
- chars/token 实测 2.5-2.6（估值器立案支持）。
- 优化立案：任务 6.7（①批量退役汇总单事件 N→1；②settled 类 external_input 票据化折叠；③估值器另案）。分析结论已回信远端（codingweiye↔weiyepeng）。

## WP4 收口记录（2026-09-18，5.1-5.6+5.8）

- **5.1 limits 单点**：HTTPAPILimits{1MiB/32 msgs/256KiB content/1024 fb}——SetLimits 拒绝负值；validateTaskRequest 单点（ContentLength 预检+LimitReader 超判+逐条校验）。
- **5.2 envelope receipt**：bus.PublishEnvelopeContext（durable 单 envelope 多消息；volatile 逐条）+ agent.InjectEnvelope（终结态/novelty gate/总线选择）→ POST /task 202 返回 request_id/durable；注入失败 503 非 202；未实现 envelope 的旧 AgentLoop 走逐条 fallback。
- **5.3 endpoint 策略**：默认禁用 llm_base_url 重定向；启用需 allowlist（精确 host 任意端口）；URL 校验（scheme/userinfo/fragment/host）；SetModelUpdateFnE 失败→502 整批拒绝且不注入；endpointMu 串行化（更新与接收不跨代）。bot main 按 env TAGENT_RL_ALLOW_LLM_REDIRECT/TAGENT_RL_ENDPOINT_ALLOWLIST 接线。
- **5.4 feedback 溢出**：队列上限丢最旧+fbDropped 计数；/feedback/wait 返回 dropped_count/partial（全逐出提示全量重查）。
- **5.5 server 硬化**：rl.NewHTTPServer（显式 Addr+Read/Write/Idle timeouts）；bot srv 改用；fallback 分支同步 srv.Addr（:80 事故教训闭环）。long-poll 挂 r.Context() 取消。
- **5.6/5.8 适配回归**：bot 模块 build+test 绿；主仓 29 包 short 绿 + race 绿。测试 rl/http_api_wp4_test.go 五场景。
- 5.7（隔离部署夹具）保持未勾：需多机/隔离网环境，列授权类。

## cold-eyes review（2026-09-18，CodeReview 子代理对 WP0-WP4 全量变更）

结论：尚不可直接合并，距可合并一轮针对性修复。WP0/WP1/WP3 工程质量高、测试真实；阻塞项集中 WP2/WP4 五个 Major。

**当场修复（全量 short 29 包 + race 7 包绿）**：
- Major 3（critical 数据丢失）：claimDurable 给每条消息放 joined dedup_keys 而 persistBusEvent 只取第一个 → 多消息 envelope 第 2..n 条事实静默丢失 + EventKeys 无界增长。修复：按消息序号分配单数 `inbox_dedup_key`（EventKeys[i]↔Messages[i] 对齐），persistBusEvent 单数优先/复数兼容；inbox_receipt_test 断言同步。
- Major 4（DoS）：/feedback body 无上限——统一 LimitReader+413。
- Warning 2：meditation 清空/空 merge 两处 continue 跳过 finishDurableBatch → claimed envelope 僵尸化（重启循环空跑）。修复：continue 前 finishDurableBatch（幂等收敛）。

**遗留（登记任务 6.8，合并前必修 ①②）**：
- Major 1：MemoryPlugin.onEvent 主输入事实路径不读 dedup key（NewSnowflakeEventKey 新 key 双写）——TestDurableReceipt_ReplayNoDoubleWrite 直调 persistBusEvent 属过拟合，掩盖缺口。
- Major 2：TypeInboxReceipt 只写不读——receipt stored-gate 与 ConfirmDurable 间崩溃窗口 → 重放重执行；receipt 事件无 RequestID 去重。
- Major 5：endpoint allowlist 不约束 30x 重定向（openai client 默认 FollowRedirect）——可降级文档化+迁移说明。
- Warning 1/3、Minor×8：锁外 Close、转发协程 ctx.Done、TryPull NonWake、orphan 计数、日志脱敏等（详见 tasks 6.8）。

**回归风险（发布说明必须含）**：endpoint 动态重定向默认禁用为 breaking change——现有 RL 部署须显式 `TAGENT_RL_ALLOW_LLM_REDIRECT=1` + `TAGENT_RL_ENDPOINT_ALLOWLIST`。

**审查确认正确**：auth 前置单点、validateEndpointURL fail-closed（userinfo/IPv6/大小写/空 allowlist）、chunked body 截断、race_check v2 保真、StoreEvent stored-gate 顺序、build_agent 无双重 release、projection.Append 幂等；9 个新测试文件无 skip 弱化。

## cold-eyes 修复第三批（2026-09-18，Major 1 闭环）

- **Major 1 主输入事实路径 dedup-aware**（审查必修最后数据完整性项）：
  - 通道：复用 RunFlow ctx 注入点（plugin.WithAttribution/WithProjectionSink 同款）——新增 plugin.WithDurableInbound/DurableInboundFrom{Path,RequestID,DedupKey}；cm.turnDurableInbound 由 runEventLoop 每迭代从 claim 事件提取设置（迭代末/两处 continue 清除，长驻循环无 defer 泄漏）。
  - 消费：MemoryPlugin.onEvent 重放时以 DedupKey 同键落库——FileSegmentStore 返回 ErrDuplicateEventKey → 幂等成功（stored=true，projection 按 key 幂等，绝不双写用户输入事实）；首次执行 Path 有 DedupKey 空 → 正常 NewSnowflakeKey。
  - 回写：MemoryPlugin.SetDurableKeySink（agent.go 接线 bus.AppendDurableEventKeys）——首次执行入库后即回写新键，补上「主路径从不回写」的缺口（此前重放永远无 dedup 证据）。
  - 防御：claimDurable EventKeys[i2] 越界检查（多消息 envelope 与回写数不一致时安全降级）。
  - 测试：plugin/memory_dedup_test.go 三场景（重放同键不双写（真实 FileSegmentStore 语义）/首次回写可解析回事实/无 provenance 行为不变）全绿。
- 回归：全量 short 29 包+vet 绿、race 8 包绿、bot 模块绿。
- **6.8 余项**：Major 5（可降级）、Warning 3、Minor 2/3/5/6/7/8。

## cold-eyes R2（2026-09-18 第二轮审查）与修复

R2 结论：Major 2 正确闭环；Major 1 单 envelope 闭环但多 envelope 批次存在「ack 无事实」丢失窗口（M-1）+ 碰撞吞噬语义缺陷（W-1）；Warning 1 修复自身引入 opening-map 数据竞争（M-2）。已逐项修复，全量 short 29 包+race 绿：

- **M-1（结构修，按审查建议①）**：runEventLoop 迭代开始对批次内全部 claim 事件走 persistBusEvent 逐消息预落库（该函数已具备 GetEvent-guard 重放去重/单数 dedup_key/回写）；DurableInbound 加 FactsPrePersisted，MemoryPlugin.onEvent 对预落库 turn 的 user 输入跳过入库（LLM 产出照常）——消除「合并事实 vs 逐消息事实」粒度分裂。回归 TestDurableReceipt_MultiEnvelopeBatchReplay：跨 crash 批次 A(回放)+B(首次) 均落库、均获自有证据、receipt+ack 后 pending=0。
- **W-1**：MemoryPlugin 移除 ErrDuplicateEventKey 吞噬分支与 SetDurableKeySink/agent 接线——D15 碰撞语义恢复（同键异内容=显式失败暴露），replay dedup 职责单一化到 persistBusEvent。
- **M-2**：opening map 读+插均持 r.mu（原 lock-free 读与插入竞态、双 mutex 覆盖可致同路径双开）；新增 TestOwnership_ConcurrentAcquireSamePath（8 goroutine 同路径并发，单实例断言，race 绿）。
- **W-2**：flock 提前到 open 之前（open 启动的扫描器/compactor 可写，原「先 open 后 flock」有跨进程第二写者窗口）；flock 前 MkdirAll（锁文件在 store 目录内，原依赖 open 建目录——回归 TestResolveMemoryStore_FileSamePathShared 已捕获并修复）。
- **S-1**：controlMetaKeys 收录 inbox_path/inbox_request_id/inbox_dedup_key(s)——控制元数据不再泄入 Origin baggage。
- **S-2**：RecordEventKeys append 前去重——重放回写不再使 EventKeys 无界增长。

**审查核验通过项**：Major 2 闭环（双路径收集/时序/幂等/映射生命周期）、turnDurableInbound 单消费者无并发问题、此前已确认项未被本批破坏、inbox_receipt_test 真实链路质量。
**R2 余项**：Major 5（重定向 CheckRedirect 或文档化）、Warning 3（转发协程 ctx.Done）、Minor 2/3/5/6/7/8。

## cold-eyes R2 修复第三批（2026-09-18，6.8 尾批清零）

- **M-1 结构修**：runEventLoop 迭代开始对批次内全部 claim 事件走 persistBusEvent 逐消息预落库（GetEvent-guard 重放去重/单数 dedup_key/回写一次到位）；DurableInbound.FactsPrePersisted 让 MemoryPlugin 跳过预落库 turn 的 user 输入（LLM 产出照常）——粒度分裂消除，多 envelope 跨 crash 批次由 TestDurableReceipt_MultiEnvelopeBatchReplay 固化（A 回放+B 首次同批：均落库、均有自有证据、receipt+ack 收敛 pending=0）。
- **W-1**：MemoryPlugin 移除 ErrDuplicateEventKey 吞噬分支/SetDurableKeySink/agent 接线——D15 碰撞语义恢复，replay dedup 职责单一化 persistBusEvent。
- **M-2**：resources opening map 锁内读插；TestOwnership_ConcurrentAcquireSamePath 重写为 barrier 语义（全 acquire→同实例断言→全 release→重开验证），race count=3 绿。
- **W-2**：flock 先于 open（关第二写者窗口）+ flock 前 MkdirAll（锁文件目录前置创建——TestResolveMemoryStore_FileSamePathShared 捕获后修复）。
- **Minor 批**：2（orphan 同内容重复提交幂等不重计数）、3（recoveryStatusOf 只填空状态——failed/skipped-nonempty 不被派生覆盖；Status 初始改空）、5（envelope 持久化/重建保留消息 Role——system 事件重放不再是 user；eventRole 提取）、6（writeEnvelopeFile 目录 sync 失败显式返回错误）、7（flock 用 syscall.LOCK_EX/LOCK_NB 具名常量 + 本地盘前提注记）、8（feedback/wait partial 语义改为任一丢失即提示全量重查）。
- **Major 5 降级文档化**：SetEndpointPolicy 与 bot main 注释声明「allowlist 只约束初始 URL，30x 跳转需可信 host/离网部署；后续变更装 CheckRedirect」。
- 回归：全量 short 29 包+vet 绿、race 绿、bot 模块绿。
- **6.8 状态：审查项全部闭环（除 Major 5 文档化降级——按审查选项执行）**。
