# 04-window-recovery 孙任务

## A. 折叠锚（裁定 r45 核心）

- [x] 4.a1 产物载荷扩展：`Execute` 批次末用 `replay.FoldedEntries` 纯函数按投影序记录结构化幸存者（不透明 checkpoint 字节，`Checkpoint/WithCheckpoint`）—— 验证：`TestAnchoredRebuildEqualsFullWalk` 绿（full decoded=92 → anchored=5, anchors=1，逐字段含 Time/Summary/Role/Generation 一致）；`61df08d3`
- [x] 4.a2 `seedAnchoredReplay`：索引按冻结类型 `typ == event.CompressProductType` 发现锚（`EachIndexNewestFirst` 参数回调，零前缀、零正文读）→ 按 `anchorSlot{partition,session}` 隔离重建 + 边界 seq 尾回放 —— 验证：**12 个锚全绿**（等价含 `RenderProjection` 字节相等/空类型拒锚/坏载荷具名退回/文案拒收/向量缝拒收/未折叠退全量/落地序号/tail>0/跨地址隔离/全新 Run 稀疏账本/快照-提交间插入事件不丢/`mappedEntryOf` 守卫拒收带内容机制事实，删守卫即红）。独立全量拷问 `7addd70d` 在 pinned `2e7dd53d` 返回 FRONTIER NOT EMPTY 七条，逐条修复见 r54；`1284461e`
- [x] 4.a3 golden 复验：折叠契约向量未随载荷扩展变化（压缩策略零改动），`TestFoldPlanGolden` 绿；四条 MUT-FOLD mutation 逐个 `gate: ok`（仍 killed）。无需重生成 —— 验证见 r51 同批门禁输出
- [x] 4.a4 **通用状态注入钩子按用户裁定撤回并零残留删除**：`WithAnchorState`、`RegisterAnchorState`、`States/WithStates`、`compress_states`、所有者注册表/捕获/恢复器与专属测试全部移除；锚只保留链可重建的幸存者检查点。`TestBatchCheckpointMatchesPostBatchProjection` 独立保留（旧码 47 vs 7、解码 50 vs 8）；不可推导状态以后只能先建模为事件或既有日志。r61 的放行仅证明旧实现自洽，最新裁定取代其公开 API 方向

## E. 4.b1 审计发现的 12 缺口（r57 归类；A 真实缺口 / B 刻意冷启动 / C 次要）

- [ ] E1 **P1/A** relations 父图（改判为 4.c1 读侧任务）：撤回新增的 `withRelations`、在线 `SetParent`、`relationHeads` 与 full-boot 写图方案并零残留删除；不新增第二套关系状态。召回 trace/get-parent 在 4.c1 按需从持久链推导，既有 `MemoryRelations` journal、Host 打开与 RecallPort 注入随读侧退役；memory tier/空 Root 不落磁盘 journal。G1
- [ ] E2 **P1/A** pinnedFacts 空窗：`trimSeededFacts` 早于投递恢复 pin —— 修：拆分 seed/trim，把投递恢复+pin 置于其间；或持久化 pin 键并在 trim 前 seed。G2
- [ ] E3 **P1/A** 投递账发现仅热窗口：unsettled 义务离开窗口即永不发现 —— 修：从持久链/索引发现 owed 身份，或快照投递账。G3
- [ ] E4 **P1/A** Host.heldSeq 仅旧迁移恢复：eventified delivery-N 未计，复用 delivery-1 会静默不 park 新 held body —— 修：从已恢复结算事实的 max delivery-N 播种，或实例作用域 ID。G4
- [ ] E5 **P1/A** Host.invocations 归零：`session#1` 复用，旧 final_round.request 可把新 range 拽入收尾回合 —— 修：从链中 max <session>#N 播种计数器，或 minted ID 纳入实例 ID。G5
- [ ] E6 **P2/C** Run.sites 不重建：`ActiveSessions` 空、`Unanswered` 漏旧 pending —— 修：从链会话重建 sites，或按耐久链解析 send/unanswered。G6
- [ ] E7 **P2/C** 重试 attempt/backoff 不恢复：每次重启发新预算 —— 修：结算 meta/账快照持久化 attempt+next-backoff 并 seed consumer。G7
- [ ] E8 **P2/C** compress.judgments 不 seed：pending 请求无产物则永不执行 —— 修：boot 从 request 事实重建 pending judgments 并 Record。G8
- [ ] E9 **P2/A** 锚路径非 boot 等价（4.a5 硬前置）：无返回 landed、稀疏 r.facts、NoteProduct 只锚、survivor 不 trim、fold/boundary/recovery 未播种 —— 接线前逐项补齐，否则保持 test-only。G9
- [ ] E10 **P2/B** endedAt/rangeOpen 刻意冷启动：补 boot 锚钉住语义；将来实现 range 续跑时须重新分类。replayEntry.window 由声明重算（G10 同类）：修：持久化注册窗口或声明变更时重挂。
- [ ] E11 **P2/C** ReplayProjection.fold/boundary/recovery 无生产写入者（RecoveryHint 读但从不写）—— 修：以 canonical rebuild 作 seed 路径或移植字段 + hint/boundary 锚。
- [ ] E12 **P2/C** 快照 Cursor 写而不读（与 D7 相悖）—— 修：用 cursor 界 delta 或换 per-task 单调游标，否则删除。G12

## B. #19 派生态全量清单审计

- [x] 4.b1 状态通道审计（只读，ca5e8c99）：枚举 Run/Host/ReplayProjection 内存态字段并逐项定性，结论 AUDIT COMPLETE 12 gaps（P1 5 / P2 7），三个已知嫌疑定性：relations 父图=真实缺口 G1、endedAt/rangeOpen=刻意冷启动、pinnedFacts=真实缺口 G2。审计表与回答见 r57；缺口清单见 E 节 —— 验证：只读审计报告 + r57 归类 + E 节接线

## C. 读侧退役（boot 只做自己的消费）

- [ ] 4.c1 `TierQuerySource`（实现候选，待独立拷问）：查询直读持久层；`StoreQuerySource`+ObserveStore+land 钩子已原子删除（含能力测试/示例/骨架）；召回父边按需从链推导，`MemoryRelations` journal、Host 打开与 RecallPort 注入已退役，晚绑定 Trace 由 RecallPort 转发 —— 验证：直接查询与既有内核逐项一致、构造不读正文、热窗口外仍可读、父边提交序与跨地址隔离、Bind 前构造的工具 Bind 后可读；19 锚与 race 全绿，启动 land=0、无运行期关系写入
- [ ] 4.c2 向量缝整型删除：检索按候选现嵌入 —— 验证：Top-K 与今天逐条一致；启动 Submit=0
- [ ] 4.c3 tier 打开懒校验 —— 验证：启动不读正文；坏分片首读报错退全量

## D. 载体与收尾

- [ ] 4.a5 **boot 接线（本变更完成前必须默认开启；硬前置：4.c1 + E 节 5 条 P1 + G9）**：`seedReplay` 改用锚路径 —— 曾试接并实测三套回归全红（投递恢复仍读 `r.facts` 活账本，稀疏锚账本重复结算），已回退并留源码注释与 r48 证据；接线前须补齐：返回 landed 映射、全产物 `NoteProduct`、covered 事实显式 hydrate、幸存者 trim、pins/compress 状态播种（G9 明证锚路径非 boot 等价；relations 已改判为 4.c1 读侧退役，不在锚路径另建父图）；否则保持 test-only
- [ ] 4.d1 载体时间纯度审计（terminalTTL 等）—— 验证：纯→结案；不纯→移出快照面+等价锚扩时钟断言
- [ ] 4.d2 载体游标方案测量与裁定（seq 元数据 / 单一 WAL）—— 验证：折叠锚后 boot 剖面数据入台账
- [ ] 4.d3 消费者锚：真实 120 重启 —— 验证：boot 在 gen1/20/120 同量级 + 数据准确性对账绿
