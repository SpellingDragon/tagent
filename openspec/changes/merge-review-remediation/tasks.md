## 1. P0 规格对齐与文档真源

- [ ] 1.1 `event-sourced-projection` 主 spec 应用 delta：无锚恢复条款重写为全量复原（按 delta MODIFIED 块整块替换）
- [ ] 1.2 `event-segment-store` 主 spec 应用 delta：REMOVED 两条 WAL 系条款 + ADDED"Sync 原子快照屏障（分区分片）"条款
- [ ] 1.3 `docs/wiki/agent/compression-and-telemetry.md`"保留最新 N 条"截断表述改为全量复原（与 1.1 同源）
- [ ] 1.4 `docs/storage-durability-positioning.md`"码面事实"纠正（FSync 旋钮无效力、屏障=快照原子 rename）
- [ ] 1.5 `docs/wiki/rl/rl-architecture.md` 自相矛盾"已知缺口"段改为真实缺口陈述
- [ ] 1.6 `README.md` 认知资产防线"零必填配置"纠正（D1 依赖显式 working_dir）
- [ ] 1.7 归档 evidence.md 个人绝对路径批量脱敏（8+ 处）；`docs/wiki/README.md` 重复段删除
- [ ] 1.8 全部 `openspec/specs/*/spec.md` 的 TBD Purpose 回填（30+ 处，一句话能力陈述）

## 2. agent 核心运行时修复（A 组）

- [ ] 2.1 `agent/agent.go:590` SetAuditLine 移入 Meditation.Enabled 块；测试：启用时冥想消息含审计行
- [ ] 2.2 `agent/execution_gate_model.go` 三个失败点 yield 失败 Response（verify/迭代器 err/通道 err/nil）；测试对照 persistent-event-loop delta 三场景（failed turn、不 ack completed、nil 流不挂死）
- [ ] 2.3 `agent/session.go` 拒绝分支补 unregisterLiveCM+Close（注册移到 Err() 检查后）；测试：拒绝后 LiveCMCount/Obligations 归零
- [ ] 2.4 `agent/settle_routing.go` Run 的 unbind 后 invBus 终态排空转发 persistentBus（兑现 65-67 行注释承诺）；测试：teardown 窗口内 settle 不丢
- [ ] 2.5 `agent/event_bus.go` QuarantineEnvelope 材料读失败补 releaseRetention 兜底（或隔离副本重读）；`agent/agent.go:453-462` 构造失败补 CloseDurable
- [ ] 2.6 删除 `agent/recovery.go:60` 死残留 `var _ = sync.Mutex{}`

## 3. 常驻可靠性修复（D1/D2 + C 组）

- [ ] 3.1 `SettleSignal` 增 `Lineage` 字段（注释沿用 ExitCode"不持久化"先例）；`finalizeRetired` 删 Origin 改写、设信号级戳
- [ ] 3.2 `newTaskSettledEvent` 取值优先级 `sig.Lineage 非空 > Origin`；grep MetaKeyTriggerSource 消费点核对零遗漏（清单见 review-notes/02）
- [ ] 3.3 测试：退役不写 Origin（-race retire×watch 并发）；Resume 恢复轮按原谱系外投；跨重启干净
- [ ] 3.4 `finalize` 产生侧分流：batchCollect 分支前查 `Origin[MetaKeyInvocationID]`，有归属走完整 onSettle 不进批；宿主 OnBatchRetire 零改动
- [ ] 3.5 测试：有归属条目不进批/路由递减可达/父循环静止退出；grep 既有 BatchRetire 断言同步；嵌套批回归
- [ ] 3.6 `agent/reliability/inbox.go` quarantineFile 补 syncDir+错误返回，`pending.Add(-1)` 绑定隔离成功；ClaimNext 锁内补 closed 复查；测试补齐
- [ ] 3.7 `task_manager.go` DefaultTTL 注释对齐"永开无禁用"；reconcileTTL 对 detector=nil 恢复任务补会话级回收或显式"会话仍在运行"告警事件；测试补齐

## 4. compress 修复（D4/D9/D10 + B 组）

- [ ] 4.1 `foldSettleRuns` 豁免 run 级化：run 含 Active 成员整 run 原样保留；单条票据分支补 Timestamp==0 防护
- [ ] 4.2 测试：批量 Active 整 run 不折叠不截断；混合（Demote+Internal）run 正常折叠；Timestamp=0 不丢观测
- [ ] 4.3 D9 白名单同源化：grep 全部 trigger_source 赋值点枚举外显值域 → 提炼单一 deliverable 白名单 → 投递门与 `isExternalizedNotice` 同源消费；`internalLineageValues` 负名单删除
- [ ] 4.4 测试：白名单外任意值（含 task-unstamped、未知值）双向一致判内部；全部外显值正常外投（投递门测试覆盖）
- [ ] 4.5 D10 结构化标记：`newTaskSettledEvent` 源头写 `settle_notice=true`；`isSettleNoticeRef` 只认标记、前缀启发式删除（不留回退）
- [ ] 4.6 测试：用户消息以 `[task settled` 开头不再误判；标记通知正常折叠/降级；旧格式事件（无标记）原样保留

## 5. memory 修复（D5/D11 + E 组）

- [ ] 5.1 `mem_spill.go` ReplayWithNotify：记录成功 key 集合，rewrite 成功后统一 ReleaseKey，失败不释放；测试：rewrite 失败后重放不重复释放、下轮 AlreadyCommitted 补释放可达
- [ ] 5.2 `retention_lease.go` Release Godoc 修正（按持有者释放、非幂等）
- [ ] 5.3 `compaction.go` finalizeTombstones 在 idx 删除失败时跳过 RemoveTombstones；deleteSegments 清理失败聚合上报（不静默 continue）；测试补齐
- [ ] 5.4 `segment_store.go` locateOrphanEvtSlot 对 ListSegments 失败 fail-loud 返回错误
- [ ] 5.5 D11 分区快照：LocalFileKV 快照按分区分片、Sync 只重写 dirty 分区、启动按分区装载（ListPartitionIDs 适配）；旧 kv.json 不迁移
- [ ] 5.6 测试：offline bench 断言新布局跨进程读回；单分区提交成本与分区键数成正比（全库键数不放大）
- [ ] 5.7 `wiring.go` openLocalFileStore/openRVStore 失败路径补 relation store 释放；测试补齐
- [ ] 5.8 D12 诊断清理：删除 `DiagnosticsSnapshot.WALQuarantined` 字段与 rl 测试桩（http_api_closeout_test.go:35,42 等）

## 6. rl 与周边修复（E/D 组）

- [ ] 6.1 `rl/swappable_model.go` sweepRetired：current 读移入写锁内（消除 Swap(B→A) 后误关在用模型）；测试：换回旧模型场景不 Close
- [ ] 6.2 `rl/trajectory_recorder.go` 通道路径补 (nil,nil) 防护（与迭代路径对齐）；测试：nil 流不挂死、Close 不死锁
- [ ] 6.3 `tool/action` resume detector 重注册：TmuxMonitor 增 RebindCallback，`rebuiltResumeClosure` 恢复后回调切到新 detector；测试：跨重启 resume 的 settle/ExitCode 有供给方
- [ ] 6.4 `examples/wechat-bot/restart-tagent.sh` 归档截断移至旧进程 SIGTERM 确认退出后、新进程 spawn 前的交接窗口（OLD_TRAJ_BYTES 已提前采集，语义不变）

## 7. tool 注释与护栏（D 组）

- [ ] 7.1 `smuggle_hint.go` 正则改 `(?:^|[^&])&\s*(?:disown\b)?`（排除 `&&`/URL 参数误报）；测试：`nohup x 2>&1 | tee && echo done` 与 `curl 'http://x?a=1&b=2'` 不告警
- [ ] 7.2 `tmux_executor.go` SessionError Godoc 对齐失败极性主载体语义
- [ ] 7.3 7+ 处注释错乱前缀人工校正（action_tool.go:55,840、settle.go:86、tmux_executor.go:117、tmux_monitor.go:93,493、declarative.go:14、mcp/call.go:38）；跑 gen_godoc --check

## 8. 工程化（F 组）

- [ ] 8.1 `check_comment_only.sh`：删除侧直传 codetools（MISSING-HEAD 硬拒生效）、仅纯新增批次放行、循环与传参加引号
- [ ] 8.2 构造性用例：删除 .go 批次硬拒、纯新增放行、混合批次删除文件被拒
- [ ] 8.3 `lint.sh`"lint: ok"移至全部四道门之后；`.github/workflows/ci.yml` soak job 加 `-timeout 45m`；CI 增 `go mod verify` 步骤（D14 防护）

## 9. 回归验证与 MR 收口

- [ ] 9.1 全量回归：`go build ./...`、`go vet ./...`、`go test ./...`、`cd test && go test ./...`、`.github/scripts/run-go-tests.sh`
- [ ] 9.2 门禁回归：`scripts/lint.sh`（棘轮+name-check+doc-refs+gen_godoc+proc-refs）、`check_test_merge.sh`、openspec validate；race：`go test -race ./agent/... ./memory/...`（D1/D2/3.6 触点）
- [ ] 9.3 soak 手动跑一轮（workflow_dispatch）+ offline bench；45 项对照 `review-notes/01-findings.md` 逐项勾验闭环
- [ ] 9.4 按主题提交序列落 dev（spec/agent/常驻/compress/memory/rl/tool/工程化/文档），CHANGELOG 记录 BREAKING 与全量修复
- [ ] 9.5 openspec 归档本 change（deltas 并入主 specs）；push origin dev；创建 dev→main PR（描述含评审摘要+45 项闭环+BREAKING 声明）；CI 绿后合并
- [ ] 9.6 合并后跟踪项建档：PR #2637 合入 → 删 go.mod replace、升 trpc-a2a-go 正式 tag（唯一声明尾巴）
