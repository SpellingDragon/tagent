# 04-window-recovery 孙任务

## A. 折叠锚（裁定 r45 核心）

- [x] 4.a1 产物载荷扩展：`Execute` 批次末用 `replay.FoldedEntries` 纯函数按投影序记录结构化幸存者（不透明 checkpoint 字节，`Checkpoint/WithCheckpoint`）—— 验证：`TestAnchoredRebuildEqualsFullWalk` 绿（full decoded=92 → anchored=5, anchors=1，逐字段含 Time/Summary/Role/Generation 一致）；`61df08d3`
- [x] 4.a2 `seedAnchoredReplay`：索引按冻结类型 `typ == event.CompressProductType` 发现锚（`EachIndexNewestFirst` 参数回调，零前缀、零正文读）→ 按 `anchorSlot{partition,session}` 隔离重建 + 边界 seq 尾回放 —— 验证：**11 个锚全绿**（等价含 `RenderProjection` 字节相等/空类型拒锚/坏载荷具名退回/文案拒收/向量缝拒收/未折叠退全量/落地序号/tail>0/跨地址隔离/全新 Run 稀疏账本/快照-提交间插入事件不丢）。独立全量拷问 `7addd70d` 在 pinned `2e7dd53d` 返回 FRONTIER NOT EMPTY 七条，逐条修复见 r54；`1284461e`
- [x] 4.a3 golden 复验：折叠契约向量未随载荷扩展变化（压缩策略零改动），`TestFoldPlanGolden` 绿；四条 MUT-FOLD mutation 逐个 `gate: ok`（仍 killed）。无需重生成 —— 验证见 r51 同批门禁输出
- [ ] 4.a4 状态注入钩子（折叠瞬间向会话主权人要不可推导状态快照；注册制；不注册=行为同今天）—— 验证：注入者重启后状态恢复；不注入者不受影响

## B. #19 派生态全量清单审计

- [ ] 4.b1 枚举 Run/Host 全部内存态字段，逐项标注持久通道（链事件/自持日志/锚捕获/重装配/刻意冷启动），三无者红 —— 验证：审计表入台账；三个已知嫌疑（relations 父图/endedAt-rangeOpen/pinnedFacts）逐项定性

## C. 读侧退役（boot 只做自己的消费）

- [ ] 4.c1 `TierQuerySource`：查询直读持久层；观察表+ObserveStore+land 钩子原子删除（含能力测试/示例/骨架）—— 验证：同一查询结果与今天逐条一致；启动 land=0
- [ ] 4.c2 向量缝整型删除：检索按候选现嵌入 —— 验证：Top-K 与今天逐条一致；启动 Submit=0
- [ ] 4.c3 tier 打开懒校验 —— 验证：启动不读正文；坏分片首读报错退全量

## D. 载体与收尾

- [ ] 4.a5 **boot 接线（依赖 4.c1）**：`seedReplay` 改用锚路径 —— 曾试接并实测三套回归全红（投递恢复仍读 `r.facts` 活账本，稀疏锚账本重复结算），已回退并留源码注释与 r48 证据；须先完成 4.c1 查询直读持久层再接
- [ ] 4.d1 载体时间纯度审计（terminalTTL 等）—— 验证：纯→结案；不纯→移出快照面+等价锚扩时钟断言
- [ ] 4.d2 载体游标方案测量与裁定（seq 元数据 / 单一 WAL）—— 验证：折叠锚后 boot 剖面数据入台账
- [ ] 4.d3 消费者锚：真实 120 重启 —— 验证：boot 在 gen1/20/120 同量级 + 数据准确性对账绿
