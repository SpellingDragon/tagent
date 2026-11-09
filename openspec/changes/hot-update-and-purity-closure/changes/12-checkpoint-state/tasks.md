# 12-checkpoint-state 任务表

> 目标：框架级统一 checkpoint 事实族承载全部可恢复状态；boot=最近 checkpoint+回放偏移后事实；删除三条全史读者；退役 RecoveryTicket。
> 契约：design.md D1-D7 冻结，实施不得擅改签名；偏离上报编排者。
> 范围纪律：新发现先分本次回归/历史缺陷/后续优化，禁自动扩范围。
> 派发：12.2-12.6 为耦合簇（共享 api/event、assembly/run、host_delivery），合单派一个 openspec-apply 顺序实施；12.7 测试在契约冻结后可并行；编排者终跑门为唯一终态判据。

## 一、契约与类型

- [x] 12.1 契约冻结（编排者已完成，design.md D1-D7；r47 回填干净终点形态：任务终态纳入、restoreChains 降兜底、投影走折叠锚、inputs 切片）；实施前 grep 实证符号/行号 —— 验证：本表签收
- [x] 12.2 `event.TypeCheckpoint="checkpoint"` + `CheckpointPayload` 类型（Offset/Projection/Delivery/Mailbox/Tasks）+ Meta 键约定；登记 FACT-VOCABULARY 与 golden durable_truth checkpoint surface —— 验证：`go build ./api/event/`；FACT-VOCABULARY 门绿；grep TypeCheckpoint 落盘

## 二、协调者与接缝（耦合簇核心）

- [x] 12.3 状态接缝 `Stateful`{SnapshotState/RestoreState} + run 注册入口；框架协调者：节奏（提交计数 N + 折叠触发 + close/quiesce）、提交锁下一致性切面、聚合切片落 `checkpoint:<partitionSeq>` 事实；N 登记 window_bounds —— 验证：`go build ./assembly/`；单测「N 条提交后落一条 checkpoint，Offset=landedSeq」
- [x] 12.4 状态持有者接线（投影/送达/邮箱/欠账输入四切片已成，往返单测随 12.7） snapshot/restore：送达账（Host 全局含子会话，开账 body/目标/lineage/attempt/hold + 已结水位）、邮箱（MemoryStore 欠账）、投影幸存者（per-session，折叠锚）、未应答输入 —— 验证：snapshot→restore 往返等价（12.7a）
- [x] 12.4b 任务终态切片（r47 用户裁决纳入，最干净形态）：`tasksStateSlice`（run.go:598 注册，SnapshotCheckpoint/RestoreCheckpoint 走 runtime/task/checkpoint.go）承载任务终态集合+链 revision 水位；restoreChains 以 `CheckpointCoverage()` 判覆盖——已覆盖且 `durable.Revision ≤ stated` 零链读跳过、已覆盖有尾链只 fold `chainPast`（stated revision 起）、未覆盖年轻链全量兜底，`CheckpointRestored` 为 covered 任务出 outcome 并解 owner —— 验证：`go build ./...` exit 0；`go test ./assembly/ ./runtime/state/ ./runtime/task/ -count=1` 三包 ok（assembly 128.5s）；TestTaskRestoreBoundedByCheckpoint（8 覆盖任务零链读，日志 chains=3 links=6 covered=8；覆盖判定置死后 chains=11 → 红）、TestTaskRestoreFoldsTheTailOfACoveredChain（本单新增：覆盖链 1 尾链 — chains=1 links=1；抽掉 chainPast 则 links=2 → 红）、TestTaskRestoreWithoutCheckpointFoldsEveryChain（无 checkpoint 全量兜底与折叠逐项一致）、runtime/task TestRegistryCheckpointRoundTrip + TestRegistryCheckpointStatesTheAccountNotTheHistory + TestRestoredTaskKeepsTheRevisionTheTailExpects 绿。残留（上报编排者）：`durable.TaskIDs()` 仍对已驻留内存的 taskID 集做 O(N) 枚举，body 已由 durable load 全量读入，非本单可消
- [x] 12.5 恢复重写（boot 读最新 checkpoint→RestoreState→回放；删 deliveryRecoveryFacts 全扫改 past-cut、删 unsettledSessionFacts 逐站点扫（Unanswered 改内存态）、删 RestoreOwed；坏 checkpoint 拒 boot；投影按折叠锚回放〔r47 裁定〕；TestKVDeliveryMigratedToFacts 已修绿）—— 验证：go build ./... 绿；三读者旧入口零残留；assembly 全量测试绿（含该红项 count=1 ok）
- [x] 12.6 退役 RecoveryTicket：删 fileBootRecoveryTicket/WriteRecoveryTicket/recoveryTicketWriter/blob+ticket KV；durable_truth 移除其条目 —— 验证：grep RecoveryTicket/WriteRecoveryTicket 代码与测试面零残留；go build ./... 绿；durable_truth 门 surfaces=16 errors=0；assembly+ledger 测试绿

## 三、测试与门

- [x] 12.7a 一致性测试：结算与 checkpoint 并发（晚于 Offset 靠回放、早于 Offset 在快照）；开账嵌入（折叠未清义务含 body 进 checkpoint，8.1 场景由构造绿）—— 验证：`go test ./assembly/ -run 'TestDeliverySettlementAndCheckpointConcurrent|TestCheckpointCarriesOpenObligationAcrossFold' -race -count=10` exit 0（20/20 PASS，`DATA RACE` 计数 0，终态树跑）；并发一致性由四结算×四保留与十次切面交错覆盖（快照与回放恰好各认一次，重启 debt=4）；开账嵌入由构造绿：fold 之后 checkpoint 的 delivery 切片 `Owed` 带 body/target/lineage/retry hold/attempt，重启发现恰 1 次、二次重启读到已结且通道未再投
- [ ] 12.7b boot 成本平坦：LongRun 校准每代 boot 时长不随代数增长（六分片 seed×window 全绿）—— 验证：`go test ./examples/wechat-bot -tags smoke -run TestEventIntegrityLongRun`（CI 同参）；CI event-integrity-long 六分片 conclusion=success
- [x] 12.7c anchored_boot_parity：checkpoint 恢复与全量重放逐条一致；坏 checkpoint 拒 boot 负锚 —— 验证：`go test ./assembly/ -run 'TestAnchoredBootMatchesFullBoot|TestDefaultBootRebuildsFromTheAnchor' -race -count=10` exit 0（20/20 PASS，`DATA RACE` 计数 0）；parity 逐条一致（锚定与全量重放的 landed 分区序列、`RenderProjection` 字节、判决集三方同，且 `Decoded < 全量`）；负锚新增 `TestBootRefusesAnUnreadableCheckpoint` 三子例（不可读 payload / 异版本 / 身份不符）断言 boot 具名拒（`errors.Is(ErrCheckpointUnreadable)` + 消息点名 `checkpoint:9999`）——撤掉 `restoreCheckpoint` 的拒 boot 分支（改为返回空 offset 照常起）→ 3/3 红（"the boot started beside the unreadable checkpoint checkpoint:9999"），复原（sha256 4b7259f5…）→ 绿
- [x] 12.7d 上报②判定：真实缺陷，已修——尾链 fold 不刷移动戳，使 checkpoint 覆盖+尾链的任务把 pre-cut `ChangedAt` 带进终态清扫，而纯链 fold 任务戳为零不被清扫（两恢复路径对同一任务给两种清扫结果）。修：`foldLocked` 按 live 路径同一语义 `entry.markMovement(fact.Status, time.Time{})`（链上无可计算时刻 ⇒ 戳未知，与链重建的记录一致），并按事实改 `TaskCheckpoint.ChangedAt` 注。验证：新增 `TestTaskTailFoldLeavesNoSweepStamp` 绿（`-count=5`）——断言覆盖+尾链任务戳为零、紧随其后的 admission 清扫（生产路径）保住该记录、`PruneTerminal` 不丢；撤掉该行 → 红（"the settled record reads as (0, task: no task with that identity) beside the admission that swept"、"the booted task carries the movement stamp 2026-11-09 08:35:40.400948"），复原 → 绿；`go test ./runtime/task/ ./runtime/state/ -count=1` exit 0
- [ ] 12.8 全门：durable_truth/event_integrity_scenarios/fold_contract/FACT-VOCABULARY/gen_godoc/anchored_boot_parity/window_bounds + `bash scripts/gates/lint.sh` + `go test ./...` + race；编排者终跑 —— 验证：逐条 exit 0 记录

## 四、收尾

- [ ] 12.9 8.1 延后卡标记 superseded（runtime-simplicity-features/tasks.md 8.1：由 12.4 开账嵌入消解）；ORCHESTRATION 终审轮回写 —— 验证：openspec validate --strict 绿