# 04-window-recovery 任务表（r82 按最终路径重写）

> 目标：事件链是唯一真相；完整重放是恢复基准；折叠锚只是可校验的读优化。
> 硬门：本变更完成前 4.a5 必须让锚启动默认开启，并证明与完整重放逐条一致。
> 范围纪律：评审只能对照本表验收；新发现先分为本次回归、历史缺陷、后续优化，禁止自动扩大当前任务。
> 删除纪律：观察表、关系 journal、通用状态注入、VectorSeam、跨消息正文共享一律不得恢复；零消费者接口优先删除而非保留占位。

## 一、已完成且封存的机制

- [x] 4.a1 折叠产物携带摘要、死亡名单、按投影序幸存者与 seq 边界；检查点只含链可重建信息。证据：`TestAnchoredRebuildEqualsFullWalk`、`TestBatchCheckpointMatchesPostBatchProjection`、`61df08d3`
- [x] 4.a2 `seedAnchoredReplay` 测试路径：按冻结类型找锚、按 (partition,session) 隔离、幸存者边界后回放；坏锚退全量。12 锚与四条 fold mutation 通过；`1284461e`。默认 boot 未接线，不在本项冒充完成
- [x] 4.a3 压缩策略零变化：`TestFoldPlanGolden` 与 MUT-FOLD-01..04 全绿
- [x] 4.a4 通用状态注入、所有者注册表与外部状态载荷全部删除，无 `WithAnchorState/RegisterAnchorState/States/WithStates/compress_states` 残留
- [x] 4.b1 Run/Host/ReplayProjection 派生态审计完成；12 项缺口分类为 E1–E12，见下文
- [x] 4.c1 查询直读持久链；观察表、land 钩子、关系 journal、在线父图全部删除；trace/get-parent 按需推导并支持晚绑定。独立审者 `038a30dc` 于 `b6045d72` 返回 FRONTIER EMPTY
- [x] 4.c2 VectorSeam、VectorSource、DefaultVectorQueue、启动预热、提交时 Submit 与 Close 排空全部删除；仅在查询时嵌入授权候选。独立审者 `d7d0acc0` 于 `8cd47e52` 返回 FRONTIER EMPTY

## 二、当前波次：删除非预期机制与空壳

- [x] 4.c0 **原文正文按 EventID 独立（用户裁定，本变更内完成）**：删除 digest→blob 共享表；不同 EventID 同内容也各存一份；同 EventID 多 scope 仍指同一事实；digest/length 仅校验本条消息；旧共享格式具名 `ErrSnapshotCorrupt`，空正文可跨重启恢复 —— 验证：`TestOriginalReferenceIsolation`、`TestStorageCapabilities`、`TestOriginalStoreRefusesLegacySharedBodies`；history/acceptance/capabilities/assembly/runtime-state 与全门绿。最终 `fc788e8b`；独立审者 `fc7b2557` 返回 FRONTIER EMPTY
- [x] 4.c3 **最小 tier 懒校验，不重写 OriginalStore**：open 只登记分片；段装载只读数值分区 idx/meta；原文仓只把既有整库 load 延后到首次实际原文访问，其他语义不变；坏 snapshot/ticket 走既有全量重放 —— 验证：OpenTier/Assemble 不读 ref/blob 分片、首读具名错误、物理坏 snapshot/ticket 与独立全量结果一致。非目标：逐 key/逐 digest 读穿、Put/References/BlobCount/完整性模型变化、新 CLI 命令。最终 `cc61a46c`；独立审者 `4c6e6844` 返回 FRONTIER EMPTY
- [ ] 4.c4 **零消费者面删除**：删除仅测试占用的 `EachEventNewestFirst`，收窄仅内部使用的 `FoldKindOf`；`EachSessionFact` 因集成测试真实读取机制事实而保留，禁止为删除改用复制夹具；修正 `candidateTextSource` 与 ReferenceSource 的文档边界 —— 验证：对应调用点编译与测试绿，grep 无旧导出/空壳；不得新增替代抽象

## 三、恢复正确性缺口（先修真实断链，不做新机制）

- [x] E1 父图缺口已由 4.c1 的按需链推导关闭，无独立关系状态
- [ ] E2 **P1 / 阻断 4.a5**：pin 恢复先于 seeded trim，重启后固定保留内容不丢 —— 验证：带 pin 的 boot 锚与 trim 反证
- [ ] E3 **P1 / 阻断 4.a5**：从持久链发现所有未结清投递，不限热窗口 —— 验证：窗口外 owed 在重启后仍被发现且只处理一次
- [ ] E4 **P1 / 阻断 4.a5**：从已恢复事实播种最大 delivery-N，禁止复用旧挂起编号 —— 验证：重启后新 held body 使用新编号并正确 park
- [ ] E5 **P1 / 阻断 4.a5**：从链恢复每个 session 的最大轮次号，禁止旧 final_round 请求污染新回合 —— 验证：重启后编号不复用、旧请求不误收尾
- [ ] E6 **P2**：从链重建 ActiveSessions/Unanswered —— 验证：重启后的活跃与待答集合等于重启前
- [ ] E7 **P2**：恢复 retry attempt 与 next-backoff，不重置预算 —— 验证：重启前后剩余重试次数与等待时间一致
- [ ] E8 **P2**：从 compress.request 重建待执行 judgments —— 验证：无产物的 pending 请求重启后仍执行且只执行一次
- [ ] E9 **G9 / 4.a5 硬前置**：锚路径补齐 landed、完整 NoteProduct、covered hydrate、survivor trim、pins/compress/fold/boundary/recovery 播种 —— 验证：锚启动与全量启动在所有可观察面逐项一致；任一缺失即禁止接线
- [ ] E10 **刻意冷启动**：endedAt/rangeOpen 不续跑；replayEntry.window 只按当前声明重算 —— 验证：boot 锚钉住该语义；不新增持久状态
- [ ] E11 ReplayProjection.fold/boundary/recovery 只能由 canonical rebuild 播种；无生产写入者的字段直接删除 —— 验证：读写双方都有消费者，或符号零残留
- [ ] E12 快照 Cursor 必须用于界 delta，否则删除 —— 验证：游标读取证明增量正确，或写而不读字段零残留

## 四、默认锚启动与最终验收

- [ ] 4.a5 **默认锚 boot（完成硬门）**：仅在 4.c1、E2–E5、E9 全绿后把 `seedReplay` 切到锚路径；不提供隐藏配置开关，不允许完成后仍 test-only —— 验证：同一持久根的全量/锚启动逐条一致，锚路径解码数低于全量，所有消费者回归绿
- [ ] 4.d1 载体时间纯度审计：terminalTTL 等不纯时间不得进入快照语义；必要时移出快照面并补时钟锚
- [ ] 4.d2 载体游标测量：记录锚启动剖面，决定 seq 元数据或单一 WAL；无测量不得新增载体
- [ ] 4.d3 真实 120 重启：gen1/20/120 的 boot 同量级，消息身份不重不漏，fault/drain/storage/folds 全绿；证据入 run notes

## 五、完成定义

1. 上述任务全部完成，独立审者逐阶段 FRONTIER EMPTY；4.c0/4.c3/4.c4/4.a5 不得在评审中扩大范围。
2. 全量重放与默认锚启动逐条一致，且锚路径解码数低于全量。
3. 查询、Top-K、trace、权限、投递与压缩策略无未声明变化。
4. 删除机制无生产或测试空壳残留；旧格式拒绝路径具名，无静默丢历史。
5. 120 重启消费者证据、CI、F0/F1/F2/F3/F4 全部通过后才允许归档。
