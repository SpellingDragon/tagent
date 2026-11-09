# Tasks: 12-checkpoint-state

耦合簇（12.1→12.2→12.3→12.4 串行依赖），按编排纪律并发不足 3 合并为单派发单实施；12.5 验证随核心。run.go 装配缝/api/event 归编排者单写，实施代理经 D1-D4 冻结签名只改本域面+登记调用。

- [ ] 12.1 契约层：新增 event.TypeCheckpoint + Meta 键 + checkpoint 载荷类型（状态切片 map[消费者名]切片 + 分区 Seq 水位 + 开账清单）；状态接缝签名（Snapshot/Restore 注册）；压缩事实 checkpoint 字段回归纯投影 —— 验证：`go build ./...` EXIT=0 + `openspec validate hot-update-and-purity-closure --strict --no-interactive` EXIT=0 + `grep -rn TypeCheckpoint api/event/` 非空
- [ ] 12.2 协调者：状态接缝注册 + 提交锁下一致性切面聚合（偏移=landedSeq）+ 有界节奏（周期/提交计数 + 折叠触发 + close/quiesce）+ 落 TypeCheckpoint 事实；紧凑已结集合用分区 Seq 水位+开账清单 —— 验证：正 `go test ./assembly/ -run TestCheckpointEmittedAtCadenceAndFold -count=1`（断言 checkpoint 事实偏移=landedSeq、含注册切片）+ 负 `go test ./assembly/ -run TestCheckpointOffsetNotFactSeq -count=1`
- [ ] 12.3 恢复重写：boot 读最近 checkpoint + Restore 切片 + 回放 Seq>offset 事实经 observe/commit 管线；删 deliveryRecoveryFacts 全扫/Unsettled 逐站点 durable 扫/RestoreOwed 全量重投；Unsettled 读恢复后内存态 —— 验证：正 `go test ./assembly/ -run TestBootRestoresFromCheckpointAndReplays -count=1` + `go test ./tests/integration/host/ -run TestEventIntegrity -count=1` EXIT=0 + 负锚（临时令 boot 跳过 checkpoint 直走全史扫）红
- [ ] 12.4 退役 RecoveryTicket + 门清理：删 WriteRecoveryTicket/fileBootRecoveryTicket/recoveryTicketWriter/blob+ticket KV；durable_truth 删 RecoveryTicket 条目(:211/:376)增 checkpoint surface；过 event_integrity_scenarios/fold_contract/FACT-VOCABULARY/gen_godoc/anchored_boot_parity/协议门 —— 验证：`grep -rn 'RecoveryTicket\|WriteRecoveryTicket' assembly/ memory/` 计数=0（归档除外）+ `bash scripts/gates/check-openspec.sh` EXIT=0 + 受影响门逐条 exit 0
- [ ] 12.5 验证与卡：LongRun 六分片全绿 + 每代 boot 成本平坦（不再超线性）+ 「结算与 checkpoint 并发」一致性测试；runtime-simplicity-features 8.1 卡标记 superseded（开账随 checkpoint 存活） —— 验证：`cd examples/wechat-bot && go test . -tags smoke -run '^TestEventIntegrityLongRun$' -race -args -seed=1 -window=32768` EXIT=0（及 seed7/window256）+ `go test ./assembly/ -run TestSettleDuringCheckpointConsistentCut -count=10 -race` EXIT=0 + CI 六长分片 conclusion=success

## 依赖与派发提示

- 顺序：12.1 → 12.2 → 12.3 → 12.4 → 12.5；核心耦合簇单派发（openspec-apply），撞 150 轮上限则编排者核已落盘改动续做。
- 偏移铁律：分区 Seq（landedSeq），禁 factSeq（进程局部+随机）。
- 义务铁律：开账（body/目标/lineage/attempt/hold）必须嵌入 checkpoint，折叠不得吃未清承诺。
- 门命令随令下发；实施代理自面清零，编排者终跑门为唯一终态判据。