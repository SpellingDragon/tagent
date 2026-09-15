## 1. 事件 schema 与常量定义

- [x] 1.1 在 `tagent/event/` 新增压缩快照 payload 结构体（`compress_snapshot` JSON schema v1：schema_version/full_boundary/threshold/meditation_keys/compressed_keys/card_text/retained_refs/partition_id/agent_name/created_at/source_event_keys/firmware_version），字段名用稳定 snake_case 契约；含序列化/反序列化与必需/可选字段校验函数（design D4）
- [x] 1.2 单测：schema 往返序列化、必需字段缺失拒绝、可选字段缺失容忍、版本不识别拒绝（覆盖 specs"快照版本化与显式降级"三场景）
- [x] 1.3 （已关闭：派生方案取代 D6 盖章，见执行期修订 1）定义 `MetaKeyMeditationMark` 常量与 metadata 盖章判定（design D6），含单测

## 2. 压缩事件写入（活路径）

- [x] 2.1 `agent/context_manager.go`：Compress 返回后判定"实际折叠"（RetainedRefs 与输入 refs 不等），投影 Replace 成功后同步写 `context_compress` 事件（payload=compress_snapshot JSON，design D2 时机：先 Replace 后写）
- [x] 2.2 写失败处理：不阻断当轮装配，ERROR 日志 + `[compress_event_write_failed]` 通知注入 Notices 通道（design R2/R7）
- [x] 2.3 （已关闭：同执行期修订 1，双路径同源派生）`persistBusEvent`（context_manager.go:466-546）metadata 盖章处扩展：trigger_source=meditation 的事件额外盖 `MetaKeyMeditationMark`（design D6）
- [x] 2.4 单测：折叠后写事件（含全量 retained_refs 与三态快照字段）、under-budget 不写、写失败留痕不阻断、冥想章盖章

## 3. 重放状态机（复原路径）

- [x] 3.1 `tagent/build_agent.go` SetReplayProjection 回调状态机化（design D3 伪码）：压缩事件→解析快照→Replace(快照.retained_refs)→SetFullBoundary+UpdateThreshold+MarkMeditationKey×N；普通事件→现状 AppendProjectionRef；L2 降级（schema 不识别/必需字段缺失）→不 Replace 不回灌+ERROR 留痕+跳过
- [x] 3.2 冥想章派生：重放遇 `MetaKeyMeditationMark` 事件 → MarkMeditationKey（与快照回灌并集语义），单测覆盖两来源并集场景
- [x] 3.3 单测：单压缩事件 Replace 终态、连续压缩事件后者胜、压缩事件后普通事件继续 append、降级三场景（版本不识别/必需缺失/可选缺失）
- [x] 3.4 （序列收敛已由 agent 包单测覆盖；5000 事件 WAL 端到端并入 5.2 dogfood，见执行期修订 4）集成测试：构造含 5000 普通事件+3 压缩事件的 WAL 重放，验证终态收敛到最后快照（长会话快进）+ 三态回灌正确

## 4. 不动点与确定性验证

- [x] 4.1 不动点单测：回灌三态后（模拟 over-budget）触发首次 Compress，断言 retained refs 与分区划分不重排快照前状态、决策轨迹（压缩产出卡片文本+边界锚点）与旧进程一致（specs"确定性复原验收"不动点场景）
- [x] 4.2 逐字节一致性测试：模拟活路径 3 轮压缩→序列化状态→重放复原→装配 prompt，diff 为空（specs 强判据场景）

## 5. 门禁与 dogfood 验收

- [x] 5.1 build/vet/test 全绿（tagent 仓库门禁），含 race 检测（meditationKeys mutex 并发路径）
- [ ] 5.2 dogfood：活路径长会话触发 ≥3 轮压缩→kill 进程→重放→首次装配 prompt 与死前逐字节 diff 为空；不可测环境用 cached_tokens/prompt_tokens ≥90% 兜底（specs 验收两场景）
- [ ] 5.3 design.md 中 R10 消解论证（D7）经代码审阅确认：重放与投影经同一 Replace 入口收敛，无双重复原分叉；push dev


## 执行期修订（2026-09-11，实现对账后）

1. **1.3/2.3 关闭（派生方案取代 D6 盖章方案）**：不新增 MetaKeyMeditationMark 常量；冥想标记由 trigger_source=meditation 同源派生——活路径（session.go MarkMeditationKey 调用点）与重放（ReplayProjectionHandler）共享同一真相源（persistBusEvent 已给每事件盖 MetaKeyTriggerSource），无双源漂移风险。
2. **schema 裁剪定格**：v1 实做 9/13 字段；partition_id/agent_name 由事件级字段承载；source_event_keys/firmware_version 裁剪；compressed_keys 已补写入侧（输入 refs 与 RetainedRefs 差集，保持输入序）。
3. **D2 Notices 落地**：persistSnapshotEvent StoreEvent 失败返回 [compress_event_write_failed] 通知，调用点双写 result.Notices（契约字面）与 result.Messages（既有消费路径，[context_compress_error] 先例；实证 Notices 目前无包外读者），当轮装配不阻断。
4. **3.4 范围注记**：重放状态机序列收敛（多次快照后者胜、快照后继续 append、千级事件量）由 agent 包单测覆盖；5000 事件真实 WAL 级端到端重放并入 5.2 dogfood 验证。
5. **死代码发现（2026-09-11，4.1/4.2 执行期入账）**：`persistSnapshotEvent` 扫描 `"[context_compress"` 前缀消息取 CardText 的路径在生产代码无构造点（全仓 grep 实证），生产快照 CardText 恒空——重放侧卡片行从 refs 经 `extractCardLine` 纯工程重建（context_compressor.go:803），确定性不受影响。不阻塞本 change，建议后续 change 清理死路径。
6. **WAL 尾查询分区缺口（2026-09-12 00:25 换装 dogfood 生产发现）**：NOTICE writer 的 fetchWALTail 未传 PartitionIDs，而 FileSegmentStore.resolvePartitions（segment_store.go:652-663）对无分区查询返回 nil → 扫 0 分区恒空——实测"0 WAL events"降级而 KV 数据完好（主 agent 事件在分区 144=PartitionIDFromName("tagent")，kv.wal.jsonl 磁盘实证）。已修：fetchWALTail 显式传 agent 名分区（e6bd8ef），TestFetchWALTail 锁死分区透传契约。同轮事故另修 restart-tagent.sh 与 cron 保险的竞态：flock 互斥（同一锁路径）+ spawn 前 fd9 关闭（防 bot 永久持锁致保险失灵）+ healthz 监听者身份校验（防假阳性 RESTART OK），见 4d94bf5。
