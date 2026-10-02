# 常驻可靠性边界历史任务（已废弃，不可执行）

状态：SUPERSEDED。唯一执行计划为 [complete-resident-reliability-protocol/tasks.md](../complete-resident-reliability-protocol/tasks.md)。本文件移除全部可执行复选框，仅保留原任务与历史状态供追溯；“历史曾勾选”不等于已验收。不得继续 apply，也不得因零任务按完成归档或同步本目录 delta。

历史依赖说明：1→2→3→4→5→7；6 曾允许与2–5并行；8依赖存储语义、9依赖本地修复。此顺序及以下说明均已被新计划取代。

## 1. 基线、契约与失败证据

- 历史曾勾选（未重新验收）：1.1 记录实施时 HEAD、工作树差异与 Go 环境，核对相对 `a16fdce` 是否已有修复；不覆盖用户改动。
- 历史未完成（不再执行）：1.2 逐项建立 F1–F10 的 fail-before 用例或明确无法复现说明，记录入口、触发条件和真实观察结果，不能仅修改断言使测试通过。
- 历史曾勾选（未重新验收）：1.3 增加用户确认的批次用例：A+B 合并一次输入/一个 turn，C 在执行中到达留到下一批；durable、volatile 与传输重试均覆盖。
  <!-- volatile: TestRunEventLoop_ABOneTurnCNextTurn; durable: TestRunEventLoop_DurableBatch_ABStoredCDeferred; transport retry: pending inbox-v2 D2 -->
- 历史未完成（不再执行）：1.4 建立源文件→既有单元/集成/消费者测试映射，保留现有 role、来源优先级、Metadata 合并、框架工具结果和压缩票据行为作为参照。

## 2. 公共写入与内部重放分离（F3、F8，D4）

- 历史曾勾选（未重新验收）：2.1 在 memory 定义显式重放能力及结果/错误契约，不修改 MemoryStore 主接口；两个内置后端恢复公共重复 key 拒绝，包括同内容重复。
- 历史曾勾选（未重新验收）：2.2 为 FileSegmentStore 实现 canonical 核对与实际 orphan 补写：idx 有而 evt 缺、evt 有而 idx 缺、必要 meta 缺失；异内容、错身份、I/O 和墓碑均明确处理。
- 历史曾勾选（未重新验收）：2.3 将同分区提交/修复、删除、墓碑及压实发布纳入一致锁序，确保屏障成功前不发布成功结果；避免无条件重置已存在的层级/封口元数据。
- 历史曾勾选（未重新验收）：2.4 统一逻辑 live-count 派生：新增 +1、已有重放 +0；不确定失败标 unknown，修复/恢复成功后只重算受影响分区，unknown 时暂停容量淘汰。
- 历史曾勾选（未重新验收）：2.5 engineBridge、ErrorTrackingStore 透传重放能力及错误，索引按 key 幂等、已有重放不新增容量计数；可靠输入重放失败不再额外进入 mem_spill。
- 历史曾勾选（未重新验收）：2.6 mem_spill 使用相同 canonical 重放契约，覆盖已提交重复及异内容冲突；不支持能力的自定义后端明确报错并保留条目。
- 历史曾勾选（未重新验收）：2.7 运行全部关联 memory/kv/engine/lifecycle 测试及定向 race，覆盖并发同键、缓存淘汰、失败修复、独立进程 reopen、压实重复槽及墓碑不复活；正常写路径无逐事件全库扫描。

## 3. 无损 inbox 与固定身份准备（F1、F4，D2）

- 历史曾勾选（未重新验收）：3.1 实现 inbox-v2 的 version、固定消息槽 source_event/prepared_fact、receipt_key 与 completion 编解码，保留单文件接收全序、容量上限及文件/目录屏障。
  <!-- agent/reliability/inbox.go 整体重写为 v2 并淘汰 v1（无兼容读路径）：inboxDirName inbox-v1→inbox-v2；Envelope 显式 Version(=envelopeVersion 2)+RequestID/Source/State/Attempts/ReceiptKey/Completion(raw)/ReceiptNote；EnvelopeMessage(role/content)+EventKeys 删除，换 MessageSlot{Slot 固定不可压紧, SourceEvent raw 无损快照, PreparedFact raw 写前冻结}。readEnvelope 严格校验：version 不识别/必要字段缺失/槽位错序/缺 source_event 一律判错→reopen 与 claim 扫描隔离(quarantine)，绝不作为有效输入消费。Enqueue 盖 version=2+按 i 赋固定 Slot+校验每槽有 source_event(空拒收，不留静默丢字段)，保留单文件全序 seqPath/容量 max/整批 tmp+fsync+rename+dirsync 屏障。新增写前准备/完成耐久原语：PrepareFacts(claimed 上一次性冻结逐槽 prepared_fact+receipt_key，异 receipt_key/fact 冲突 ErrReceiptKeyConflict)、RecordCompletion(冻结 completion，同载荷幂等/异载荷 ErrCompletionConflict)。RecordReceipt 强制两阶段序：completion 未耐久则拒(F1 落地点)。Ack 的 dir sync 失败返错(D3-9 保留可重试)。ConfirmDurableByReceiptKey 按固定 receipt_key 收敛(4.6 用)。reliability leaf 不 import agent/memory，source_event/prepared_fact/completion 皆 json.RawMessage。leaf go vet+test -race 全绿。 -->
- 历史曾勾选（未重新验收）：3.2 PublishContext/PublishEnvelopeContext 保存 ID、Source、Type、Timestamp、完整 Message 和业务 Metadata 快照；不可 JSON 编码时返回接收错误，接收后调用方修改不影响已保存载荷。
  <!-- event_bus.go：durable 分支对整条 AgentEvent 做 json.Marshal 无损快照（ID/Type/Source/Timestamp/完整 Message/业务 Metadata 全入 source_event），装入 MessageSlot{SourceEvent}；PublishContext 单事件、PublishEnvelopeContext 先逐条 NewExternalInputEvent+Marshal 再整批 Enqueue——任一不可编码即 Enqueue 前整批拒绝（绝不半收），返回 `durable acceptance refused` 包装错误。快照先写后，调用方后续改内存事件不损已存载荷。channel 只送 wakeEvent 哨兵（事件不再双通道，避免重复消费）。 -->
- 历史曾勾选（未重新验收）：3.3 agent 使用 typed claim 引用传递路径/request ID/槽位，移除可靠控制键经业务 Metadata/Origin/模型/宿主字段的传播；新增事实身份键集中定义在 event 包。
  <!-- AgentEvent 加非序列化字段 claim *durableClaim（json:"-"）：{Path,RequestID,Slot,ReceiptKey,PreparedFact}，仅 claimDurable 消费侧装配。claimDurable 从 source_event decodeSourceEvent 还原全字段事件并 attach typed claim（decode 失败 log+skip 该槽，保留 claim 不 ack）。DurableProvenance 改读 evt.claim.Path/RequestID。event/metadata.go 集中定义 MetaKeyInboxRequestID/MetaKeyInboxSlot/MetaKeySourceEventID 三事实身份键。删除 eventRole/eventContent 及 event_loop controlMetaKeys 的 inbox_* 条目——v1 经 Metadata/Origin/模型/宿主字段泄漏的控制键彻底淘汰（F1/F4 根因）。 -->
- 历史曾勾选（未重新验收）：3.4 首次 claim 为整个 envelope 生成并耐久保存固定 prepared facts 与 receipt_key，再允许第一条事实写入；prepared 内容含完整规范化消息、来源及首次归因，重启不得重新盖章。
  <!-- 写前准备屏障：lifecycle.prepareDurableFacts(events) 按 claim.Path 分组保序，nslots=max(Slot)+1（槽位不压紧 F4），复用已冻结 slot（PreparedFact 非空→continue verbatim 不重盖），未准备 slot 用 cm.buildBusFact 派生 canonical FullEvent（规范化 RoleSystem→RoleUser、Snowflake key、ExtractEventType/GenerateEventSummary、agent_name/trigger_source/rollout_id/bundle_id/task settle 归因一次冻结），receiptKey 空则 FormatEventKey(NewSnowflakeEventKey) 预留；一次 PrepareEnvelope→inbox.PrepareFacts 原子冻结逐槽 fact+receipt_key（claimed 上，异 key/fact 冲突拒）。runEventLoop persist 循环前先 prepare；成功后把冻结字节回灌 ev.claim.PreparedFact/ReceiptKey。persistBusEvent 三分支：有 claim+PreparedFact→Unmarshal 冻结字节 verbatim（重启不重盖章）；有 claim 无 PreparedFact→返 false 门控（准备失败不写，claim 留待重放）；无 claim→volatile 照常。durable claim 命中 GetEvent 已存在→replayed：不重存、不重投影、不重 feedback（投影=事实链折叠不变量 F4）。淘汰 v1 事后回写 AppendDurableEventKeys/RecordEventKeys。测试：TestDurableReceipt_ReplayNoDoubleWrite 共享事实链断言冻结键幂等，TestPersistBusEvent_UnpreparedClaimGates 断言屏障失败零事实写入。agent+reliability+event -race 全绿。 -->
- 历史曾勾选（未重新验收）：3.5 构造可靠模式时校验显式重放能力；启动检查旧 spill/v1 未完成和未处置隔离项，拒绝静默迁移及 volatile 降级。
  <!-- 两部分：(a) 启动门控——inbox.go 新增 sentinel ErrLegacyInboxV1NotDrained/ErrQuarantineUndispositioned + const legacyInboxV1DirName；抽出只读 checkUpgradeGates(parent, quarantineDir)：① parent 有 *.spill→ErrLegacySpillNotDrained ② dir/inbox-v1 有 *.json→ErrLegacyInboxV1NotDrained ③ inbox-v2/quarantine 有非目录项→ErrQuarantineUndispositioned。NewInbox 在两次 MkdirAll 后、os.ReadDir 扫描前调用它，拒绝即返错不动树（原扫描后 .spill 块移除）。拒绝先于扫描保证入口 quarantine 空→既有 Corrupt/UnknownVersion 测试（残件写 inbox-v2、扫描时才隔离）不被误拒。(b) 能力门控——event_bus.go 加 EventBus.Durable()；agent.go NewTagentAgent 在 bus.Durable() 时断言 memStore 实现 memory.EventReplayer，否则 fail-loud（不 volatile 降级）。默认 InMemoryStore/FileSegmentStore/engineBridge/ErrorTrackingStore 均实现 ReplayEvent，既有 durable New 路径不破。修 stale：NewReliableEventBus doc inbox-v1→v2；finishDurableBatch doc 去 AppendDurableEventKeys/inbox_dedup_keys 改述 PrepareFacts 冻结+replay-guard。测试：inbox_test.go 加 LegacyV1DirBlocksUpgrade/EmptyV1DirDoesNotBlock/QuarantineUndispositionedBlocksReopen/UpgradeGateIsReadOnly；persist_bus_event_test.go 加 nonReplayStore(嵌入 memory.MemoryStore 接口隐藏 ReplayEvent)+DurableRequiresReplayCapableStore(durable 拒/volatile 受)。agent+reliability+event+drill 全绿，定向 race 全绿。leaf 无 agent/memory import。 -->
- 历史曾勾选（未重新验收）：3.6 补齐 inbox/bus 测试：字段往返、system role 不被原地修改、序号不压紧、准备屏障失败零事实写入、A/B 部分提交和重启错配反例、满额/格式/能力拒绝。
  <!-- 新建 agent/reliability_matrix_test.go：(字段往返)TestReliableBus_FieldRoundTrip_Lossless——全字段事件 PublishContext→TryPull，断言 ID/Type/Source/Timestamp(ns)/Message(role,content)/business Metadata 逐一复原，claim 带 RequestID 且 Metadata 无 inbox_path 泄漏；(格式拒)TestReliableBus_UnencodableEventRefused——Metadata 含 chan 使 json.Marshal 失败，返错 rec.Durable=false pending=0（绝不静默剥离字段接受有损快照）；(序号不压紧)TestReliableBus_FixedSlotsNotCompacted——3 槽信封 slot1 为合法 JSON 但不可解码为 AgentEvent，claim 跳过 slot1 且 batch[0].claim.Slot=0、batch[1].claim.Slot=2（不压紧，F4），叠加 leaf 层 TestInbox_EnqueueStampsVersionAndSlots；(system role 不原地改)TestPersistBusEvent_SystemRoleNotMutatedInPlace——RoleSystem 事件 persist 后 projection ref Role=user 而 evt.Message.Role 仍 RoleSystem（buildBusFact 拷贝非原地）。既有覆盖：准备屏障失败零写=TestPersistBusEvent_UnpreparedClaimGates；A/B 部分提交+重启错配=TestRunEventLoop_DurableBatch_ABStoredCDeferred/TestDurableReceipt_ReplayNoDoubleWrite/MultiEnvelopeBatchReplay/TestInbox_Reopen_RequeuesClaimed_KeepsReceipted；满额=TestInbox_FullIsExplicitRejection；能力拒=TestNewTagentAgent_DurableRequiresReplayCapableStore。agent(28.4s)/reliability 全绿，vet 净。 -->

## 4. 固定批次与确认协议（F2、F5，D1/D3）

> **落地机制见 design.md「D3 实施细化」（待审阅确认）**。4.3 前先完成两项地基前置：
> - 历史未完成（不再执行）：4.0a `buildBusFact` 对带 claim 的 durable 输入写入集中身份键 `MetaKeyInboxRequestID`/`MetaKeyInboxSlot`/`MetaKeySourceEventID`（4.6 事实链自证的先决条件）。
> - 历史未完成（不再执行）：4.0b `persistBusEvent` claim 分支改走 `ReplayEvent`（替换 `StoreEvent`+`GetEvent` 近似），`replayed`/conflict 由 `ReplayResult` 分类，删除 `GetEvent` 守卫；volatile 分支不变。

- 历史曾勾选（未重新验收）：4.1 移除 assembleRequest 的 TryPull 消费，固定 runEventLoop 单消费点；冻结原始 claims，分离 selected/skipped，不从过滤后的 slice 推导完成集合。
- 历史曾勾选（未重新验收）：4.2 将 persistBusEvent 拆为准备与提交结果；selected 输入全部提交后才运行合并 turn，部分失败保留同批可取消地退避，失败不投影、不标已提交、不让新输入越过。
- 历史未完成（不再执行）：4.3 MemoryPlugin 改用 invocation 级已提交输入回显标识，仅跳过对应的合并输入；验证 assistant/tool、其他 user 输入及其他 invocation 不受影响。
- 历史未完成（不再执行）：4.4 completion 记录逐消息 processed/skipped、批次处理 completed/failed 和错误摘要；mixed meditation/有效空输入有明确终态且不进入 projection，取消不伪造完成。
- 历史未完成（不再执行）：4.5 completion 耐久后提交固定 receipt_key 的事实 receipt，再 RecordReceipt/Ack；completion 或 receipt 写失败不得重跑已耐久 completion，未完成输入不得被删除。
- 历史未完成（不再执行）：4.6 启动按 outstanding envelope 的 receipt_key 直接核对事实，替换仅依赖 projection 恢复扫描的确认来源；覆盖 receipt key 早于 compaction 边界及 partial 恢复。
- 历史未完成（不再执行）：4.7 贯通 outstanding receipt 的 TTL/容量保护及 Ack 后释放；删除/目录同步失败可在当前进程和重启后重试，不提前释放确认约束。
- 历史未完成（不再执行）：4.8 运行全部关联 agent/plugin/event/rl 工作流测试及定向 race：输入失败但 receipt 可写、模型错误、有限重试、关闭取消、过滤、completion 窗口，以及 A+B/C 的实际请求和 turn 计数。

## 5. 恢复提示最终装配（F9，D6）

- 历史曾勾选（未重新验收）：5.1 删除 runEventLoop 拼接 invocation.Content 的 recovery notice，移到历史与 live task board 完成后的统一请求尾部装配。
- 历史曾勾选（未重新验收）：5.2 明确一次性消费点：装配前失败、跳过和取消不消费；首次实际提交后不重复，diagnostics 状态持续可读，不改 system/历史/工具声明前缀。
- 历史未完成（不再执行）：5.3 捕获真实 runner 的 durable、volatile、one-shot 与热更请求，断言 partial/failed 提示可见、full 无提示，FullEvent/projection/reopen 历史无该运行态文本。

## 6. 同代资源 owner（F6、F7，D5）

- 历史曾勾选（未重新验收）：6.1 每次 acquire 返回捕获具体 entry 的一次性 release；为 TagentAgent.Close 添加统一幂等同步与首次错误结果，覆盖并发重复调用。
- 历史曾勾选（未重新验收）：6.2 将共享 engine 归入 resourceEntry 同代 owner，移除独立 namedEngines 生命周期；各 agent 的 capacity hook、错误追踪及读权限继续独立，执行壳只借用。
  <!-- resourceEntry 加 engine 字段；acquire 返回 (store,engine,release)；open 闭包经 buildSharedEngine 同代建引擎+接向量移除器；releaseGen 末租约 closeResource（先 engine.Close 停 worker、后 closeStore flush）；wireMemoryEngine 共享分支借 newEngineBridgeBorrow（不关共享引擎）、isolated 保 per-agent；彻底删除 namedEngines/namedEngineMu/engineCacheKey 及 namedMem/File/RVStores 死块。TestEngineOwnership_SharedReopenGetsFreshEngine 锁定同代共享/兄弟释放不拆/末次关闭/reopen 全新。 -->
- 历史曾勾选（未重新验收）：6.3 调整构造顺序：写锁→backend 恢复→engine/回调→后台生产者→发布；立即登记失败清理，覆盖 engine wiring、中途构建失败和 embedding 降级。
  <!-- openLocalFileStore/openRVStore 改返 openedResource；新增 buildSharedResource 按 tombstoneOf(恢复)→RebuildLiveCounts(计数)→buildSharedEngine(引擎+vecRemover 回调)→startStoreProducers(生产者最后) 编排，消除 compactor 早于 vecRemover 接线的孤儿向量窗口；拆分删 wireStoreLifecycle；backend 中途失败 closeKV 释放未接管 KV(engine 构建失败降级 nil 不硬失败,发布同代关闭)。file/localfile 共享 store 经 registry 首次正确装配引擎。 -->
- 历史曾勾选（未重新验收）：6.4 调整关闭顺序：接收/turn/runner→扫描与压实生产者→engine worker→backend flush/close→锁与登记；错误可达首次 Close，未确认 worker 停止时不开放新 writer。
  <!-- FileSegmentStore 新增 StopProducers(仅停 compactor+lifecycle,不碰 rel/kv),Close 复用它(幂等)。closeResource 改序为 StopProducers→engine.Close→store.Close(生产者先停因其回调 engine.Remove,engine 排空先于 kv 最后 flush);返回 (workerStopped,err)。releaseGen 返 error:workerStopped 才 unlockDirLock,否则持锁不开放新代(留 fd/flock 至进程退出,OS 释放)+ Warnf。onceRelease 返 func() error 并缓存首次结果(重复 release 同错误,D5 不吞)。错误经 acquire→resolveMemoryStore→build_agent memStoreRelease(func() error)→agent Close 汇入 errs。agent 层:StopLoop 取消 loopCtx 并等 loopWg 排空在途 turn/runner 执行 → memStoreRelease → contextManager.Close(仅关 runner 对象,不写 memStore,顺序对存储中性;trajectoryRecorder 须晚于 contextManager 的不变量保留)。TestCloseOrder 锁定关序+错误可达+未确认持锁 reopen 拒(ErrStoreLocked)/确认释锁可重开。 -->
- 历史曾勾选（未重新验收）：6.5 同路径 acquire 与最终 release 共用 per-key 协调，旧 release 不影响新代，其他路径不被慢关闭的 registry 全局锁阻塞。
  <!-- 提取 openingLock(key) helper(在 r.mu 下 create-on-first-use),acquire 与 releaseGen 共用同一 per-key mutex。releaseGen 改为先取 opening(锁序 opening→r.mu,与 acquire 一致避免反序死锁),detach→closeResource→unlockDirLock 全程持 opening:r.mu 释放后才关闭(不持全局锁做 I/O)。消除旧窗口:此前 releaseGen 在 r.mu 释放后、close 前,同路径 acquire 会命中"entry 已删但 flock 仍持"→LOCK_NB 误报 ErrStoreLocked;现 acquire 阻塞在 opening,待旧代关完 flock 释放后干净重开。不同路径各持自身 opening,不被慢 flush 阻塞。TestReleaseCoordination_SamePathReopenWaitsForClose 用 blockCloseStore 锁定:关闭中同路径 reopen 阻塞不误报、无关路径不阻塞、关完后重开成功(修复前必失败)。 -->
- 历史曾勾选（未重新验收）：6.6 用 mock embedder 覆盖共享 A/B、重复 Close、同进程 reopen、旧代检索隔离、构建失败、capacity-only 及执行壳；运行 ownership/lock/engine/热更全部关联测试和定向 race。
  <!-- 覆盖矩阵:共享 A/B 同引擎(TestEngineOwnership eng1 Same eng2)、兄弟释放不拆/末次关闭 Ready=false、重复 Close/陈旧 release(Stale + onceRelease 缓存)、同进程 reopen 全新引擎(NotSame+Ready)、旧代检索隔离(新增:reopen 得 NotSame store1/store2 全新 backend)、共享路径 engine 构建失败降级 nil + capacity-only 借桥(新增 TestEngineOwnership_SharedBuildFailureDegradesToCapacityOnly:no-such-provider→eng nil,hook 独立触发,MemoryEngine()==nil)、执行壳借用 store 身份稳定(hotreload_multiagent_test MemStore identity)、关序+错误可达+持锁(TestCloseOrder)。全部关联包(root 含热更 + memory/... + agent + offline_bench)build/vet/race 全绿。 -->

## 7. 最终消费者与跨进程回归

- 历史未完成（不再执行）：7.1 wechat-bot 独立模块通过模拟发送接口验证 durable chat_id 到目标会话，meditation 派生任务的非空 final 被扣留；不连接真实渠道。
- 历史未完成（不再执行）：7.2 resident E2E 实际启用 inbox-v2+localfile+真实 runner/插件，固定 A+B/C 到达时序，覆盖可用输入、过滤输入、模型失败与恢复提示的完整链路。
- 历史未完成（不再执行）：7.3 在 claim、prepare、事实提交、completion、事实 receipt、Ack 删除/目录同步处受控终止测试子进程，逐项对账 accepted ID、槽位、原文、投影、请求与投递决定；不使用优雅 Close 替代崩溃。
- 历史未完成（不再执行）：7.4 更新升级回滚演练：旧格式/隔离项阻断升级，v2 未完成项阻断降级，原件保留；不以测试演练授权操作真实数据。
- 历史未完成（不再执行）：7.5 对最终 diff 重新核对所有测试映射，运行根模块和 wechat-bot 各自 build/vet/short，以及全部受影响包的定向 race；保存完整失败输出和确切跳过原因。

## 8. 基准能力与数据重建（F10，D7）

- 历史曾勾选（未重新验收）：8.1 countingKV 显式保留 Sync 及实际使用的分区枚举能力，增加编译期断言、调用计数及底层 Sync 错误透传测试，禁止 no-op 假能力。
  <!-- 8.1 前次会话未落盘，本会话重建 Sync/ListPartitionIDs + 编译期断言 + TestCountingKVSyncErrorPropagates -->
- 历史曾勾选（未重新验收）：8.2 增加包装后单事件未 Close 子进程退出与独立进程读回测试，分别验证 fsync-on 和 flush-only 语义，记录证据边界。
  <!-- TestBenchWrapperBarrierDurableWithoutClose：syncs 增量>=1 + 独立进程读回，fsync-on/flush-only 双子测通过 -->
- 历史未完成（不再执行）：8.3 分开 Get/Query 延迟，报告 actual written、sampled、Sync 次数、源码/环境/配置，Go HeapInuse/Sys 不再标为 OS RSS。
- 历史未完成（不再执行）：8.4 显式运行完整离线矩阵：`RUN_OFFLINE_BENCH=1 BENCH_REPORT=<报告路径> go test ./tests/offline_bench/ -run TestOfflineBenchmark -timeout 60m -v`；长任务后台运行并等到完成，失败不以部分 cell 代替全矩阵。
- 历史未完成（不再执行）：8.5 保留旧 JSON，更新 REPORT.md 区分旧 storage 语义失效与新结果，准确标明 20k 采样；建立同语义回归基线，不虚构压缩/tokenizer 重测或线上收益。

## 9. 文档、证据与交付边界

- 历史未完成（不再执行）：9.1 更新相关架构/操作文档：固定批次、来源真源、重放能力、completion/receipt/Ack、v2 升降级、资源 owner 与恢复提示边界；不修改历史归档任务来制造完成记录。
- 历史未完成（不再执行）：9.2 逐项填写 F1–F10 的最终代码、回归测试、命令和结果；无新增反例测试的项说明原因，确保修复效果到达 design 指定的消费者。
- 历史未完成（不再执行）：9.3 执行 `openspec validate fix-resident-reliability-boundaries --strict --no-interactive`；检查六项 delta 与实际实现一致，旧 role 注入重复条款的移除能在后续正常归档时完成，不提前同步主规格。
- 历史未完成（不再执行）：9.4 交付本地结果与仍需授权/环境的发布门清单；72h 长跑、真实模型/渠道、生产升级和独立发布审查未执行则明确待验，不执行部署、提交/推送或自动归档。
