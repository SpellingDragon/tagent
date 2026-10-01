# 执行任务拆解（提交单元制）

> **拆解原则**：每个 `## N` 组 = 一个提交单元（CU）= 一次独立提交，组内条目为原子步骤（一步一验证）。**禁止跨组混合提交**。
> **防跑偏三件套**：
> 1. **执行日志**（`.git/review-notes/03-execution-log.md`）：每 CU 开始/结束各记一行（改动文件、验证命令与结果、偏离记录），禁止凭记忆续作。
> 2. **偏离协议**：任何与 design.md 决策冲突的实现选择，必须先改 design.md 再写代码——不允许"先做后补记"。
> 3. **检查点**：CU-0 建基线 → CU-07 后中点复审（对照设计原则）→ CU-14 后预收口（45 项映射勾验）→ CU-15 收口。
> 每条任务后的 `〔发现#〕` 为 01-findings.md 编号，闭环验收以此映射为准。

## 0. CU-0 执行基线（防跑偏基建）

- [x] 0.1 建立执行日志骨架 `.git/review-notes/03-execution-log.md`（每 CU 一节：开始时间/改动文件/验证命令与结果/偏离记录）
- [x] 0.2 基线快照：`go build ./... && go vet ./... && go test ./... 2>&1 | tee` 记录到日志——区分"本来就 fail"的既有失败（后续验证以此为准，不背锅）
- [ ] 0.3 确认工作区干净、`openspec status` 为 4/4、tasks 全未勾

## 1. CU-1 规格对齐 〔G-P0-1/G-P0-2〕

- [x] 1.1 主 spec `event-sourced-projection`：以 delta MODIFIED 块整块替换"无锚恢复不静默截断"Requirement（含两个 Scenario）
- [x] 1.2 主 spec `event-segment-store`：删除"WAL 中间坏行容错"与"LocalFileKV 写路径 fsync 耐久"两个 Requirement 块；在原位插入 delta ADDED"LocalFileKV Sync 为原子快照屏障"块
- [x] 1.3 验证：`openspec validate` 通过；`grep -n "500 个有效事件\|replayWAL\|默认开启" openspec/specs/event-sourced-projection/spec.md openspec/specs/event-segment-store/spec.md` 零命中
- [x] 1.4 提交（spec/docs 组）：`docs(openspec): P0 规格对齐——无锚全量复原与 KV 快照屏障真契约`

## 2. CU-2 agent 核心运行时 〔A-P1-1/A-P1-2/A-P1-3/A-P2-3/A-P2-4〕

- [x] 2.1 `agent/agent.go`：SetAuditLine 调用移入 `cfg.Meditation.Enabled` 块内（597 行构造之后）
- [x] 2.2 新测试 `TestMeditationAuditLineWired`：Meditation 启用时冥想消息含 self-audit 审计行（修前恒缺失）
- [x] 2.3 `execution_gate_model.go`：verify 失败分支（71-74）改为 yield `Response.Error` 失败响应后 return（保留日志）
- [x] 2.4 `execution_gate_model.go`：迭代器创建 err 分支（77-80）同改；`GenerateContent` 通道分支（60-63）与 iter 的通道分支（84-87）对 err/nil 同改
- [x] 2.5 新测试三场景：迭代器创建 err → failed turn；inner 返回 (nil,nil) → 失败响应不挂死；verify 拒绝 → 失败可观测——断言持久输入不以 completed 口径 ack
- [x] 2.6 `agent/session.go`：104 行 registerLiveCM 移至 119 行 Err() 检查通过之后
- [x] 2.7 新测试：owner 关闭后到达的委托调用被拒 → LiveCMCount 与 Obligations.Invocations 归零
- [x] 2.8 `settle_routing.go`：Run 的 defer unbindSettleBus 之后追加 invBus 终态排空（TryPull 循环转发 persistentBus）
- [x] 2.9 新测试：unbind 与循环退出窗口内发布的 settle 事件仍到达 persistentBus
- [x] 2.10 `event_bus.go`：QuarantineEnvelope 材料读失败路径补 releaseRetention（隔离副本重读材料键或 DrainRetentionCleanups 兜底）；`agent.go:453-462` 两个构造失败分支补 `bus.CloseDurable()`
- [x] 2.11 删除 `agent/recovery.go:60` 死残留
- [x] 2.12 验证：`go test ./agent/ -run 'Meditation|ExecutionGate|LiveCM|SettleRoute|Quarantine' -count=1` 全绿 + `go vet ./agent`
- [x] 2.13 提交：`fix(agent): 模型入口失败显式呈现/settle 路由排空/租约兜底/审计接线归位`

## 3. CU-3 谱系信号化 〔C-P1-1/C-P1-2〕（依赖：无）

- [x] 3.1 `task/task_manager.go`：SettleSignal 增 `Lineage string` 字段，Godoc 注明"信号级、不持久化、恢复任务重新裁决"（对照 ExitCode 先例）
- [x] 3.2 `finalizeRetired`（1162-1170）：删除 `t.Spec.Origin` 改写三行，改设 `SettleSignal.Lineage = LineageRetired`（新常量 `"task-retired"`）
- [x] 3.3 `newTaskSettledEvent`（event_bus.go）：trigger_source 取值改 `sigLineage 非空 ? sigLineage : Origin[MetaKeyTriggerSource]`
- [x] 3.4 消费点核对：按 review-notes/02 的 Q1 清单逐处确认读事件 Metadata 零改动正确；grep `MetaKeyTriggerSource` 非测试 11 处复核
- [x] 3.5 新测试 `TestRetireNeverMutatesOrigin`：retire×watch 并发（-race），断言 Origin 无写、结算事件谱系为 task-retired
- [x] 3.6 新测试 `TestResumeSettleKeepsOriginalLineage`：退役终态 Resume 后再结算，事件谱系为原值、投递门外投
- [x] 3.7 验证：`go test -race ./agent/... -run 'Retire|Resume|Lineage|BatchRetire' -count=1` 全绿
- [x] 3.8 提交：`fix(task): 退役谱系改挂结算信号——Spec.Origin 恢复 spawn 后不可变`

## 4. CU-4 批量折叠产生侧分流 〔C-P1-3〕（依赖：CU-3，onSettle 须先正确处理信号谱系）

- [x] 4.1 先 grep `OnBatchRetire\|BatchRetired\|batchCollect` 全部测试断言，列出受影响清单入日志
- [x] 4.2 `finalize`（task_manager.go:1194-1198）：batchCollect 分支前判 `t.Spec.Origin[event.MetaKeyInvocationID] != ""` → 有归属走正常 onSettle 路径（不进批）
- [x] 4.3 新测试：有归属条目被 TTL 批量退役 → 不进汇总、按 per-invocation 路由递减记账、父循环静止退出（带超时上限断言，非依赖硬超时）
- [x] 4.4 新测试：无归属条目行为不变（单条汇总）；嵌套批回归（TestBatchRetire_NestedNoDoubleDelivery 必须仍绿）
- [x] 4.5 按 4.1 清单同步既有断言
- [x] 4.6 验证：`go test ./agent/... -run 'BatchRetire|RetireOrphans|Reconcile' -count=1` 全绿
- [x] 4.7 提交：`fix(task): 批量折叠产生侧分流——有 invocation 归属的退役条目不进批`

## 5. CU-5 inbox 与 TTL 加固 〔C-P2-1/C-P2-2/C-P2-3/C-P2-4〕

- [x] 5.1 `reliability/inbox.go` quarantineFile：返回 error + 成功后 syncDir；调用点 `nextClaimable` 仅隔离成功才 `pending.Add(-1)`，失败跳过该文件并返回错误
- [x] 5.2 `ClaimNext` 持锁后补 `in.closed` 复查（与 Enqueue 对称）
- [x] 5.3 新测试：隔离 rename 失败（注入坏路径）→ pending 不穿透、错误上抛；Close 后并发 claim 拒绝
- [x] 5.4 `task_manager.go` DefaultTTL 字段注释改为"永开无禁用路径，<=0 一律 10min 地板（async-task-lifetime 10.5）"
- [x] 5.5 `reconcileTTL`：detector==nil 且有 Declarative.TaskID 的受害者，退役前发"会话仍在运行"告警事件（会话级回收留给后续，本步只堵静默）
- [x] 5.6 新测试：恢复任务被 TTL 退役 → 产生告警事件可观测
- [x] 5.7 验证：`go test ./agent/reliability/... ./agent/task/... -count=1` 全绿
- [x] 5.8 提交：`fix(reliability,task): quarantine 屏障与容量扣减绑定/ClaimNext 关闭复查/TTL 静默面可观测`

## 6. CU-6 识别与谱系同源化 〔B-P2-3/B-P2-2，BREAKING〕

- [x] 6.1 枚举值域：grep 全部 trigger_source 赋值点（context_manager 的 cm.triggerSource 各来源 + event_loop 391/396 + user/wechat/host 等），产出外显值清单入日志
- [x] 6.2 据清单提炼 `event.DeliverableLineage(ts string) bool` 白名单函数（单一真源，event 包）
- [x] 6.3 投递门扣留判定改调该函数；`compress/telemetry.go` isExternalizedNotice 改调同函数
- [x] 6.4 删除 `internalLineageValues` 负名单与 telemetry_audit.go 的 `auditLineageInternal`（改调同函数）
- [x] 6.5 新测试：白名单外任意值（含 "task-unstamped"、随机串、空）→ 投递门扣留 ∧ 判内部（双向一致断言同一输入）
- [x] 6.6 回归：全部外显值的投递门既有测试逐值通过（漏配即红）
- [x] 6.7 `newTaskSettledEvent` 源头写 `Metadata["settle_notice"]=true`；`isSettleNoticeRef` 改只认标记（与 TelemetryDispositions 的 GetEvent **合并为一次读取**，同点取标记与 trigger_source，不新增查库），删除前缀常量 settleNoticePrefix
- [x] 6.8 新测试：用户消息以 `[task settled` 开头（无标记）→ 不折叠不票据化；标记通知正常折叠/降级；旧格式（无标记）事件原样保留
- [x] 6.9 验证：`go test ./agent/compress/... ./event/... -count=1` + 投递门相关测试全绿
- [x] 6.10 提交：`refactor!(event,compress): 谱系白名单同源化+结算通知结构化标记（BREAKING：旧前缀启发式退役）`

## 7. CU-7 折叠豁免 run 级化 〔B-P1-1/B-P2-1〕（依赖：CU-6 的标记识别先行）

- [x] 7.1 `foldSettleRuns`：进入折叠前对 run 查 dispositions，任一成员为 TelemActive → 整 run 走原样保留（append run...）
- [x] 7.2 单条票据分支补 `ts==0 → 1` 防护（与 buildSettleFoldRef 对齐）
- [x] 7.3 新测试：批量相邻 Active（≥2，模拟首压缩轮）→ 整 run 不折叠不截断；全 Demote run → 正常折叠；单条 Timestamp=0 → 不丢观测
- [x] 7.4 验证：`go test ./agent/compress/... -run 'Fold|Telemetry|Settle' -count=1` 全绿
- [x] 7.5 提交：`fix(compress): 折叠豁免 run 级化——未消费通知整 run 不可折叠`
- [x] 7.6 **中点检查点**：对照 design.md"机制完善三原则"复审 CU-2~CU-7 全部 diff——每项修复特设路径净减少？（记入日志，不过关项返工）

## 8. CU-8 租约与静默错误清零 〔E-P1-1/E-P2-2/E-P2-3/E-P2-4/E-P2-7〕

- [x] 8.1 `mem_spill.go` ReplayWithNotify：循环内改记 `replayedKeys []int64`；rewrite 成功后循环 ReleaseKey；rewrite 失败直接返回不释放
- [x] 8.2 `retention_lease.go` Release Godoc 改"按持有者释放；归零解除；重复释放会递减他人计数，调用方须保证每持有者恰一次"
- [x] 8.3 新测试：rewrite 注入失败 → 同 key 两轮重放后 refs 不穿透；修复 rewrite 后下轮 AlreadyCommitted 路径释放可达
- [x] 8.4 `compaction.go` finalizeTombstones：KVBatch idx 删除失败 → 跳过本批 RemoveTombstones（注释：墓碑保留安全、finalize 幂等下轮重试）
- [x] 8.5 `compaction.go` deleteSegments：收集各窗口 KVScan 错误，函数尾聚合返回
- [x] 8.6 `segment_store.go` locateOrphanEvtSlot：ListSegments 失败 → 返回 `fmt.Errorf("orphan-evt segment list failed pid=%d: %w", ...)`（与内层 KVScan fail-loud 对齐）
- [x] 8.7 `wiring.go` openLocalFileStore/openRVStore：两个失败分支补 rel 释放（InMemRelationStore 无 Close 则补 snapshot+close 方法）
- [x] 8.8 新测试：压实 idx 删除失败 → 墓碑仍在、ErrEventForgotten 仍拒复活；store 构建失败 → journal fd 不泄漏（打开计数断言）
- [x] 8.9 验证：`go test ./memory/... -count=1` 全绿
- [ ] 8.10 提交：`fix(memory): spill 释放对齐落盘移除/静默错误清零/构建失败资源释放`

## 9. CU-9 分区快照 〔E-P2-5，BREAKING〕（依赖：CU-8 后 memory 包稳定）

- [ ] 9.1 `local_file_kv.go`：内部 map 改按分区桶（`map[int]map[string]string`）；快照文件 `kv-<pid>.json` 每分区一个，dirty 集合记录变更分区
- [ ] 9.2 `Sync()`：只序列化+tmp+rename dirty 分区并清空 dirty；全量语义保持（成功=新进程可读回全部键值）
- [ ] 9.3 启动装载：扫 `kv-*.json` 装载全部分区；`ListPartitionIDs` 改读分片文件名；旧 `kv.json` 不迁移（存在即忽略，日志提示冷启动重建）
- [ ] 9.4 新测试：多分区写入 → 仅 dirty 分片 mtime 变化；跨进程（子进程 Sync 后退出）新进程读回全部键值
- [ ] 9.5 offline bench 适配：新布局断言 + 写放大对比（单分区提交成本 ∝ 分区键数，与全库键数解耦）
- [ ] 9.6 验证：`go test ./memory/... ./tests/ -run 'Bench|KV|Snapshot' -count=1` 全绿
- [ ] 9.7 提交：`refactor!(memory/kv): 快照按分区分片+dirty 屏障（BREAKING：旧 kv.json 不迁移）`

## 10. CU-10 死面清理 〔G-P2-3 + fsync 死旋钮，BREAKING〕

- [ ] 10.1 删除 `DiagnosticsSnapshot.WALQuarantined` 字段与 `WalQuarantined()` 可选能力断言及 rl 测试桩（http_api_closeout_test.go:35,42 等）
- [ ] 10.2 删除 `config.go:428` `FSync` 配置键与 `memory/kv/local_file_kv.go` `WithFSync` 选项（含构造传参与"被接受但无效"注释区）；同步删除 config 相关测试
- [ ] 10.3 验证：`go build ./...` + `grep -rn "WALQuarantined\|wal_quarantined\|WithFSync\|memory.fsync" --include="*.go" --include="*.md" config.go memory/ agent/ rl/ docs/ openspec/specs/ | grep -v archive` 零命中
- [ ] 10.4 提交：`refactor!(memory,config,rl): 死面清理——WAL 诊断字段与 fsync 死旋钮删除（BREAKING）`

## 11. CU-11 rl 与周边 〔E-P2-1/E-P2-9/D-P2-4/E-P2-6〕

- [ ] 11.1 `swappable_model.go` sweepRetired：`current := m.inner` 读移入写锁内（删 RLock 快照两行）
- [ ] 11.2 新测试：Swap A→B→A（inFlight>0 期间）→ A 不被 Close、后续请求正常
- [ ] 11.3 `trajectory_recorder.go` recordGenerateContent：`respCh == nil` 时记 error Response 并返回 (nil,nil)（与迭代路径对齐）
- [ ] 11.4 新测试：inner 返回 (nil,nil) → 不阻塞、Close() 的 gcWg.Wait() 不死锁
- [ ] 11.5 `tmux_monitor.go` 增 `RebindCallback(sessionID, cb)`；`declarative.go` rebuiltResumeClosure 恢复后重绑到新 detector
- [ ] 11.6 新测试：跨重启 resume → 新 detector 收到状态迁移、settle/ExitCode 有供给
- [ ] 11.7 `restart-tagent.sh`：归档+截断段移至旧进程 SIGTERM 确认退出之后、新进程 spawn 之前（OLD_TRAJ_BYTES 采集位置随行）；`bash -n` 校验
- [ ] 11.8 验证：`go test ./rl/... ./tool/action/... -count=1` 全绿
- [ ] 11.9 提交：`fix(rl,action): 换模竞态/nil 流防护/resume 回调重绑/归档窗口移位`

## 12. CU-12 tool 注释与护栏 〔D-P2-1/D-P2-2/D-P2-3〕

- [ ] 12.1 `smuggle_hint.go` 正则改 `(?:^|[^&])&\s*(?:disown\b)?`；新测试：`nohup make 2>&1 | tee log && echo done` 与 `curl 'http://x?a=1&b=2'` 不告警，`cmd &` 与 `nohup cmd & disown` 仍告警
- [ ] 12.2 `tmux_executor.go` SessionError Godoc 改为失败极性主载体语义（非零/信号死/失明超限/强拆/未装配）
- [ ] 12.3 逐处校正 7 处错乱前缀：action_tool.go:55,840 / settle.go:86 / tmux_executor.go:117 / tmux_monitor.go:93,493 / declarative.go:14 / mcp/call.go:38
- [ ] 12.4 验证：`go test ./tool/... -count=1` + `scripts/gen_godoc.sh --check`（若 CI 有 doc 门则跑 lint.sh）
- [ ] 12.5 提交：`fix(tool): 走私正则排除&&误报/注释面前缀校正/Godoc 对齐`

## 13. CU-13 工程化 〔F-P1-1/F-P2-1/F-P2-2/D14〕

- [ ] 13.1 `check_comment_only.sh` 重写收集循环：删除侧（base 有 head 无）直传 codetools；仅全批纯新增才 exit 0；数组+引号化
- [ ] 13.2 构造性验证三例（记入日志）：删除 .go 批次 exit≠0 且输出 MISSING-HEAD；纯新增批次 exit 0；混合批次删除文件被拒
- [ ] 13.3 `lint.sh`："lint: ok" echo 移至 proc-refs 块之后
- [ ] 13.4 `ci.yml`：soak job 加 `-timeout 45m`（-args 之前）；新增 step `go mod verify`
- [ ] 13.5 验证：`bash -n scripts/check_comment_only.sh scripts/lint.sh` + 13.2 三例 + `actionlint` 或 yaml lint（可用）
- [ ] 13.6 提交：`fix(scripts,ci): comment-only 门禁防线归位/lint 信号诚实/soak 超时/go mod verify`

## 14. CU-14 文档与 spec 卫生 〔G-P1-1..5/G-P2-1/G-P2-2〕

- [ ] 14.1 `README.md:339`：认知资产防线表述改"D1 漂移审计需显式 working_dir，未设时跳过"
- [ ] 14.2 `rl-architecture.md:118-120`："已知缺口"段改真实缺口三条（守卫可枚举输出/键集重算成本/遥测参数标定）
- [ ] 14.3 `compression-and-telemetry.md:26`：截断表述改全量复原；`storage-durability-positioning.md:11-19`："fsync 默认开启"改"旋钮存在但无效果；屏障=快照原子 rename"，"行为仍在"限定快照屏障
- [ ] 14.4 归档脱敏：`grep -rln "file:///Users/" openspec/changes/archive/` 全部替换为仓库相对路径（8+ 处）
- [ ] 14.5 `docs/wiki/README.md:31-38`：删除重复引用块一份
- [ ] 14.6 全部 TBD Purpose 回填：`grep -rln "TBD - created by archiving" openspec/specs/` 逐文件一句话能力陈述
- [ ] 14.7 验证：`scripts/lint.sh` 的 doc-refs 门 + `grep -rn "file:///Users/\|TBD - created" openspec/ | wc -l` 为 0
- [ ] 14.8 提交：`docs: doc-truth 全量对齐——README/wiki/positioning/归档脱敏/Purpose 回填`
- [ ] 14.9 **预收口检查点**：产出 45 项映射勾验表（见下方映射节），逐项对照 01-findings.md 勾验并附证据行号；缺项回补，不得进入收口

## 15. CU-15 全量回归与 MR 收口

- [ ] 15.1 全量：`go build ./... && go vet ./... && go test ./... -count=1`；`cd test && go test ./...`（E2E 模块）
- [ ] 15.2 race：`go test -race ./agent/... ./memory/... ./rl/... -count=1`（对照 0.2 基线，无新增失败）
- [ ] 15.3 门禁：`scripts/lint.sh` 全套 + `scripts/check_test_merge.sh` + `openspec validate`
- [ ] 15.4 soak（workflow_dispatch 或本地 `-tags soak` 一轮）+ offline bench 全绿
- [ ] 15.5 CHANGELOG：Unreleased 段记 BREAKING×5（白名单/标记/分区快照/诊断字段/fsync 旋钮删除）与全量修复摘要
- [ ] 15.6 `openspec archive merge-review-remediation`（deltas 并入主 specs，含 CU-1 已先行对齐的两处不冲突）
- [ ] 15.7 push origin dev；创建 dev→main PR（标题含 BREAKING 标识；描述含：评审摘要链接、45 项勾验表、BREAKING 清单与冷启动说明、go.mod 跟踪项）
- [ ] 15.8 CI 绿后合并（merge commit 保留 CU 拓扑）；合并后建 go.mod 摘除跟踪任务（PR #2637）

## 附：45 项发现 → CU 映射（闭环验收清单）

| CU | 承载发现 | 计数 |
|---|---|---|
| CU-1 | G-P0-1, G-P0-2 | 2 |
| CU-2 | A-P1-1, A-P1-2, A-P1-3, A-P2-1, A-P2-2, A-P2-3, A-P2-4 | 7 |
| CU-3 | C-P1-1, C-P1-2 | 2 |
| CU-4 | C-P1-3 | 1 |
| CU-5 | C-P2-1, C-P2-2, C-P2-3, C-P2-4 | 4 |
| CU-6 | B-P2-2, B-P2-3 | 2 |
| CU-7 | B-P1-1, B-P2-1 | 2 |
| CU-8 | E-P1-1, E-P2-2, E-P2-3, E-P2-4, E-P2-7 | 5 |
| CU-9 | E-P2-5 | 1 |
| CU-10 | G-P2-3 | 1 |
| CU-11 | D-P2-4, E-P2-1, E-P2-6, E-P2-9 | 4 |
| CU-12 | D-P2-1, D-P2-2, D-P2-3 | 3 |
| CU-13 | F-P1-1, F-P2-1, F-P2-2, E-P2-8（防护） | 4 |
| CU-14 | G-P1-1, G-P1-2, G-P1-3, G-P1-4, G-P1-5, G-P2-1, G-P2-2 | 7 |
| **合计** | P0×2 + P1×14 + P2×29 | **45** |

- E-P2-8（go.mod 上游 replace）为唯一非本地消除项：防护在 CU-13（CI go mod verify），摘除在 CU-15.8（PR #2637 合入后）。
- 验收动作（CU-14.9）：逐行对照 `.git/review-notes/01-findings.md` 勾验并附证据 commit/行号，缺项不得进入收口。
