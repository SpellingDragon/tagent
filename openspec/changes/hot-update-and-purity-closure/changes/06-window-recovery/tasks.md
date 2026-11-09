# 04-window-recovery 任务表（r82 按最终路径重写）

> **r46 勘误（用户裁决，domain 12 取代）**：本域投影折叠锚机制（4.a1-4.a5、E2-E9）已完成且证据封存，不改写。其 boot 恢复路径由 domain 12-checkpoint-state 收编：4.a2/4.a5 的「坏锚退全量」独立回退、4.c3 的「坏 snapshot/ticket 走既有全量重放」票据路线，均由统一 checkpoint 恢复取代（坏 checkpoint 具名拒 boot，RecoveryTicket 退役，见 12/design.md D4/D5）。投影幸存者成为 checkpoint 的 Projection 切片；折叠锚一致性切面定理被继承。下方 [x] 证据为当时事实，仅作历史封存。

> 目标：事件链是唯一真相；完整重放是恢复基准；折叠锚只是可校验的读优化。
> 硬门：本变更完成前 4.a5 必须让锚启动默认开启，并证明与完整重放逐条一致。**r46：默认 boot 恢复由 domain 12 统一 checkpoint 收编，本硬门的历史达成证据封存，最终验收以 domain 12 合并门（LongRun 六分片 + boot 成本平坦）为准。**
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
- [x] 4.c4 **零消费者面删除（`f01629fb` 于 `a0b1f174` 返回 FRONTIER EMPTY）**：删除仅测试占用的 `EachEventNewestFirst`，收窄仅内部使用的 `FoldKindOf`；`EachSessionFact` 因集成测试真实读取机制事实而保留，禁止为删除改用复制夹具；修正 `candidateTextSource` 与 ReferenceSource 的文档边界 —— 验证：对应调用点编译与测试绿，grep 无旧导出/空壳；不得新增替代抽象

## 三、恢复正确性缺口（先修真实断链，不做新机制）

- [x] E1 父图缺口已由 4.c1 的按需链推导关闭，无独立关系状态
- [x] E2 **P1 / 阻断 4.a5（`de32c775` 于 `6288a0ac` 返回 FRONTIER EMPTY）**：pin 恢复先于 seeded trim，重启后固定保留内容不丢 —— 验证：带 pin 的 boot 锚与 trim 反证
- [x] E3 **P1 / 阻断 4.a5（`d2558e4e` 于 `e5cb2663` 返回 FRONTIER EMPTY）**：从持久链发现所有未结清投递，不限热窗口 —— 验证：窗口外 owed 在重启后仍被发现且只处理一次
- [x] E4 **P1 / 阻断 4.a5（`6f57a9ec` 于 `5c1cba7e` 返回 FRONTIER EMPTY）**：从已恢复事实播种最大 delivery-N，禁止复用旧挂起编号 —— 验证：重启后新 held body 使用新编号并正确 park
- [x] E5 **P1 / 阻断 4.a5**：从链恢复每个 session 的最大轮次号，禁止旧 final_round 请求污染新回合 —— 验证：重启后编号不复用、旧请求不误收尾。`seedRoundsLocked` 在首个 pull 读链上事实的轮次号（跳过 final_round.request，producer 会为该轮预写判词），`TestRestartContinuesTheRoundCounter` 正锚（`#c2` 且非收尾轮）+ 负锚（关掉播种 → `ran the closing round "rounds"`）；`runtime/agent` 与 `tests/integration/execution` 全绿。见 `/tmp/hupc-H2/run-notes.md`
- [x] E6 **P2**：从链重建 ActiveSessions/Unanswered —— 验证：重启后的活跃与待答集合等于重启前。`seedReplay` 在挂载重放后经唯一入口 `establish` 把链上每条会话链立为本实例站点（`Run.sites`），`runUnsettled.Unanswered` 持 tier 时改读持久链（`EachSessionFact`，提交序）而非驻留窗口。正锚 `go test ./assembly/ -run TestRestartRebuildsActiveSitesAndUnansweredRounds -count=1`（重启后 2 站点与唯一 owed `b1-in` 逐项相等）；负锚同测试「已答 `a1-in` 不得报 owed」+ `TestUnansweredRoundsStayInsideTheirChain`（已答链答零、开链只答自身、链读与窗口读一致）；撤 `seedSites` 复跑 → 红「answers 0 active addresses」后复原
- [x] E7 **P2**：恢复 retry attempt 与 next-backoff，不重置预算 —— 验证：重启前后剩余重试次数与等待时间一致
- [x] E8 **P2**：从 compress.request 重建待执行 judgments —— 验证：无产物的 pending 请求重启后仍执行且只执行一次。`noteSeededJudgment` 挂在 `seedFact`（唯一「链事实入活账」点），`compress.request` 事实按 Owner 会话交回 consumer；链上 product 在 seed 时已 `NoteProduct`，故已答判断不重跑。正锚 `go test ./assembly/ -run TestRestartExecutesTheChainJudgmentOnce -count=1`（重启重建 22 条 pending、下一提交执行 1 条、每个判断只答一产物）；负锚同测试「已答链重启 pending=0」；撤 `seedFact` 的记录 → 红「rebuilt no judgment from the chain」后复原
- [x] E9 **G9 / 4.a5 硬前置**：锚路径补齐 landed、完整 NoteProduct、covered hydrate、survivor trim、pins/compress/fold/boundary/recovery 播种 —— 验证：锚启动与全量启动在所有可观察面逐项一致。落定：`landed` 由 `report.Landed` 回答；NoteProduct 补到 `replayRef`（尾段与回退两路），锚本产品照旧；`restoreSurvivors` 后按窗界裁（`trimReplayProjection`）；judgments 经 `seedFact` 播种；`Compaction()`/`RecoveryHint()` 两路皆 nil（测试断言）；covered hydrate 判为等价——锚幸存者重建后的渲染字节与全量逐条相同。正锚 `go test ./assembly/ -run 'TestAnchoredBootMatchesFullBoot|TestAnchoredSurvivorsFollowTheResidentBound' -count=1`；负锚：撤 `replayRef` 的 NoteProduct → 红「fallback rebuild answers 0 judgments」、撤 survivor trim → 红「holds 4106 entries, want 2048」，皆复原
- [x] E10 **刻意冷启动**：endedAt/rangeOpen 不续跑；replayEntry.window 只按当前声明重算 —— 验证：boot 锚钉住该语义；不新增持久状态
- [x] E11 ReplayProjection.fold/boundary/recovery 只能由 canonical rebuild 播种；无生产写入者的字段直接删除 —— 验证：读写双方都有消费者，或符号零残留。残余无生产读者的 `FullBoundary()` 已删，`grep -rn "FullBoundary()" --include=*.go .` 零命中，`TestProjectionReplayAndBudget`/`TestEventIntegrityProjection` 绿（`docs/api` 为派生生成物，随编排者终跑门重生成）
- [x] E12 快照 Cursor 必须用于界 delta，否则删除 —— 验证：游标读取证明增量正确，或写而不读字段零残留

## 四、默认锚启动与最终验收

- [x] 4.a5 **默认锚 boot（完成硬门）**：仅在 4.c1、E2–E5、E9 全绿后把 `seedReplay` 切到锚路径；不提供隐藏配置开关，不允许完成后仍 test-only —— 验证：同一持久根的全量/锚启动逐条一致，锚路径解码数低于全量，所有消费者回归绿。已切：`seedReplay` 走 `seedAnchoredReplay`（`report.Landed` 回填 `landedSeq`），失败/坏锚/未折叠链按既有回退退全量，无配置开关。正锚 `go test ./assembly/ -run TestDefaultBootRebuildsFromTheAnchor -count=1`（默认 boot 解码数 < 链索引数、投影与全量 `RenderProjection` 逐字节相同、未折叠链仍全量）；负锚：临时改回 `seedFullReplay` → 红「decoded 92 of the 92 indexed facts」。回归 `go test ./assembly/ -count=1` EXIT=0（258.8s）、`go test ./tests/integration/... -count=1` EXIT=0
- [x] 4.d1 载体时间纯度审计：terminalTTL 等不纯时间不得进入快照语义；必要时移出快照面并补时钟锚
- [x] 4.d2 载体游标测量：记录锚启动剖面，决定 seq 元数据或单一 WAL；无测量不得新增载体 —— 裁定：接受现状为达标，不另加载体。`BenchmarkRecoveryBootMeasure`：facts=100 266,139 ns/op（163,757 B/op、816 allocs）、1000 2,942,056、4000 22,599,389（近线性）；锚剖面 `full decoded=92 anchored decoded=6 anchors=1`。测量入 `/tmp/hupc-H2/run-notes.md`
- [x] 4.d3 真实 120 重启：gen1/20/120 的 boot 同量级，消息身份不重不漏，fault/drain/storage/folds 全绿；证据入 run notes。已跑（`-rounds=30 -events-per-round=4`）：基线 b438c9fb 上 recovery 读回 120 task 且身份逐条相等、resident drain 干净；但同一 soak 的泄漏腿在基线即红（`round 9 holds 16 goroutines, want at most 8 over warm baseline 7`），且工作区在途兄弟改动下 `no snapshot under <root>` —— 「全绿」不成立，未勾。J2 复跑（工作区，2026-11-08）：`go test ./tests/integration/host/ -run '^TestSoakContinuity$' -rounds=30 -events-per-round=4 -timeout=1800s -count=1` → 仅泄漏腿红，同一行「round 9 holds 16 goroutines, want at most 8 over warm baseline 7」；`no snapshot under <root>` 已不复现（storage 腿全绿）。红项归属：干净基线 worktree `b438c9fb` 同命令复跑 → 同一子测同一行失败（`/tmp/hupc-J2/baseline`），故 16 协程为既有缺陷、非本变更引入；但任务行验收面要求「全绿」，故仍不勾。M5 定位泄漏源（2026-11-08，工作区 `-rounds=12`）：协程栈比对 `runtime/agent.(*Consumer).run`（`runtime/agent/consumer.go:296` select）逐轮 +1，总协程 `6→6+n`、常驻消费循环 `2+n`，4 次调用/轮不添一条；`run.Close` 后残留 0 条消费循环、总协程 4（过程件 `/tmp/hupc-M5/stacks`、`/tmp/hupc-M5/soak-dump2.log`）。即泄漏面＝每个不同会话地址一条常驻消费循环，由 `RecycleIdle` 仅在该地址静止满 `defaultIdleAddressTTL`（10 分钟，无生产调用方下调）后回收；`specs/consumer-loop-convergence` 明令该循环不得在一次模型调用返回后退出。故本 soak 每轮新开会话、秒级内跑完时，「基线 + 8」的常量松弛在设计上不可达——属既有 soak 与其被钉住的循环规格冲突，非本变更引入；改判据（按活地址计预算）属取向裁定，停此上报，不勾

## 五、完成定义

1. 上述任务全部完成，独立审者逐阶段 FRONTIER EMPTY；4.c0/4.c3/4.c4/4.a5 不得在评审中扩大范围。
2. 全量重放与默认锚启动逐条一致，且锚路径解码数低于全量。
3. 查询、Top-K、trace、权限、投递与压缩策略无未声明变化。
4. 删除机制无生产或测试空壳残留；旧格式拒绝路径具名，无静默丢历史。
5. 120 重启消费者证据、CI、F0/F1/F2/F3/F4 全部通过后才允许归档。
