|---|---|---|---|
| L0 正常 | schema_version=1 全字段合法 | Replace + 回灌三态 | INFO 日志：replay compress event key=... boundary=... |
| L1 字段降级 | meditation_keys/compressed_keys/card_text 缺失或空 | Replace + 回灌（阈值/边界/refs 全保），冥想集/卡片回退到事件章派生 | WARN：snapshot missing optional fields |
| L2 schema 降级 | schema_version 不识别/类型错/必需字段缺失 | **不 Replace、不回灌**，投影保持重推导前状态；后续事件照常 append | ERROR：snapshot invalid, fallback to re-derivation + 汇入 Notices（活路径首次 Compress 前可注入通知） |
| L3 事件缺失 | 重放流中无压缩事件（TTL 过期/从未压缩） | 现状行为：全量 refs 重推导 | 无（现状即默认） |

**理由**：必需字段（refs/boundary/threshold）缺失时半恢复比不恢复危险——Replace 到一半的投影 + 错误边界 = 前缀缓存失配且冥想保护错乱，不如干净回退重推导。可选字段缺失只是保真度下降，Replace 仍正确。

### D6：冥想标记事件章（正交补强）

**决策**：`persistBusEvent`（context_manager.go:501-510 metadata 盖章处）扩展：当事件的 `trigger_source == "meditation"` 时（注入源头知），额外盖 `MetaKeyMeditationMark = "true"` 章。重放回调识别该章 → `MarkMeditationKey`。

**理由**：活路径 Mark 在 `makeOnEventCallback`（session.go:250）内存态，重放不经过它；persistBusEvent 是**所有** bus 事件的落库点，在此盖章使重放路径免费获得标记信息。与 D3 快照 meditationKeys 双保险：快照恢复"死前已知的集合"，事件章恢复"逐事件即时标记"。

**备选否决**：*在重放回调里重放 makeOnEventCallback 全逻辑*：makeOnEventCallback 依赖 contextManager.GetInvocationMetadata()（当前调用元数据），重放时上下文已不存在，只抽 trigger_source 判断可移植。

### D7：R10（双重复原担忧）消解论证

R10 担忧：重放复原与投影复原是两条路径（重放 append 全量 vs 死前 Replace 折叠），状态可能双重复原/不一致。压缩事件化后：**重放状态机遇压缩事件即 Replace(snap.RetainedRefs)**——重放投影与死前投影走同一 Replace 语义收敛到同一终态，不再存在"重放重推导 vs 死前折叠"的分叉。R10 的前提（两条独立复原路径）不复存在 → 消解。design 中明确记录：R10 不需要单独修复，随本变更关闭。

## Risks / Trade-offs

- **[R1] WAL 体积增长**：每轮压缩多一条事件，payload 含全量 RetainedRefs（长会话数百 refs × ~150B ≈ 数十 KB/条）。→ 缓解：retained_refs 是逐字节复原的必需品；估算日均压缩轮次（阈值 0.8 × maxTokens 约 200k → 数百轮/天）× 数十 KB ≈ 数 MB/天级别，可接受；TTLDays:3 使 WAL 自然回收。若实测超预期，v2 再议 refs 压缩编码（如 zstd）。风险接受：先跑实测数据再优化。
- **[R2] 写放大/延迟**：同步写压缩事件 + 序列化全量 refs，单轮 ~ms 级。→ 廄缓：折叠已发生（重活已做完），增量成本可忽略；Notices 通道上报写失败。
- **[R3] 快照与投影一致性窗口**：Replace 投影与写事件非原子（先 Replace 后写）。若写失败，死前进程投影已折叠但 WAL 无快照 → 重放重推导 → 与死前不一致（现状同）。→ 缓解：失败留痕（Notices + ERROR），下一次成功压缩的快照覆盖复原缺口；死亡瞬间的该轮缺失由转世通报断点标记兜底（范围外）。
- **[R4] meditation 双路径竞态**：重放时快照回灌 meditationKeys 与事件章派生 Mark 并发不冲突（同一 map，mutex 保护），但顺序影响最终集合？→ 分析：无竞态——两个来源最终都进同一 set，并集语义，先后无差。
- **[R5] threshold 热更史丢失**：hot-reload 的 threshold 值会被快照捕获（threshold 字段），但**历史轨迹**（何时改的）不存。→ 接受：复原只需终值，不需轨迹；轨迹属审计数据，可由事件流另行分析。
- **[R6] 快照过大导致 Metadata 单事件超限**：超长会话 RetainedRefs 极大（>1000 refs）。→ 缓解：v1 接受；若实测超限，v2 增加分片（multiple compress events per round, sequence 字段）——Open Question 记录。
- **[R7] 降级不可观测**：L2 降级回重推导后，用户无感知前缀缓存已失配。→ 缓解：L2 降级注入 Notices 通知消息（`[compress_replay_degraded]`），活路径首次 Compress 时一并带给 LLM/工程侧。
- **[R8] 重放回调闭包依赖**：状态机需要访问 compressor（SetFullBoundary 等），build_agent.go 闭包当前只持 etsHolder/ta。→ 缓解：经 `ta` 获取（TagentAgent 持有 contextManager → contextCompressor），或新增 getter；编译期解决，无运行时风险。

## Migration Plan

1. 带入字段即可部署：压缩事件写入是新行为（老 WAL 无压缩事件 → 重放走 L3 现状路径，无破坏）。
2. 回滚安全：关闭压缩事件写入（feature 不写），重放遇老 WAL 无压缩事件 → L3 路径；遇已写的压缩事件（schema v1）→ 正常处理。无 schema 迁移，无数据回填。
3. dogfood 验收步骤：①活路径跑长会话触发 ≥3 轮压缩 ②kill 进程 ③重放重起点 ④重放后首次装配 prompt 与死前最后一次装配逐字节 diff（空 = 通过）⑤cached_tokens/prompt_tokens ≥90%（兜底）。

## Open Questions

1. RetainedRefs 超大（>1000 refs）时的分片策略（v2 再议，v1 接受单事件承载）。
2. `threshold_pct_source`（hot_reload|init）是否值得固化进 v1 schema（当前列为可选字段，按需取舍）。
3. 快进统计 `replayState.compressEventsSeen` 是否需要上报到 metrics（当前仅日志）。


## 执行期修订（2026-09-11，实现对账后）

1. **1.3/2.3 关闭（派生方案取代 D6 盖章方案）**：不新增 MetaKeyMeditationMark 常量；冥想标记由 trigger_source=meditation 同源派生——活路径（session.go MarkMeditationKey 调用点）与重放（ReplayProjectionHandler）共享同一真相源（persistBusEvent 已给每事件盖 MetaKeyTriggerSource），无双源漂移风险。
2. **schema 裁剪定格**：v1 实做 9/13 字段；partition_id/agent_name 由事件级字段承载；source_event_keys/firmware_version 裁剪；compressed_keys 已补写入侧（输入 refs 与 RetainedRefs 差集，保持输入序）。
3. **D2 Notices 落地**：persistSnapshotEvent StoreEvent 失败返回 [compress_event_write_failed] 通知，调用点双写 result.Notices（契约字面）与 result.Messages（既有消费路径，[context_compress_error] 先例；实证 Notices 目前无包外读者），当轮装配不阻断。
4. **3.4 范围注记**：重放状态机序列收敛（多次快照后者胜、快照后继续 append、千级事件量）由 agent 包单测覆盖；5000 事件真实 WAL 级端到端重放并入 5.2 dogfood 验证。
