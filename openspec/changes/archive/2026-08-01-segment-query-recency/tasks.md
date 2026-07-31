# segment-query-recency — 实现任务

## 1. 查询路径重构（segment_store.go）

- [x] 1.1 窗口发现阶段：meta 扫描解析 `(windowTS, layer)` 列表（layer 取自 pair.Value，解析失败回退 0/1h），按 windowTS 排序，`timestamp_desc` 逆序遍历
- [x] 1.2 时间剪枝修正：窗口秒边界 ×1000 与毫秒 StartTime/EndTime 比较（B2）；跨度按 layer 取 1h/1d/1w（B3）
- [x] 1.3 整窗粒度收集 + 早停：窗口完整扫描后并入结果，`len(matched) >= offset+limit` 时按 D1 保守规则停止（收满后补扫与已收窗口时间区间重叠的跨层窗口）
- [x] 1.4 跨窗 EventKey 去重：重复时方向无关地保留更高 layer 版本（`seen` 记录已选版本层级，单查询内）
- [x] 1.5 全序确定性：两实现排序键统一为 `(timestamp, EventKey)`（desc 两键同逆），InMemoryStore 同步补齐 tie-break
- [x] 1.6 `QueryOptions.StartTime/EndTime` 注释明确 Unix 毫秒契约（types.go）
- [x] 1.7 诚实截断提示：memory_query / memory_recent / memory_recall query 模式在 `len(results)==limit` 时于 message 注明"可能被截断，可缩时间范围或增 limit"（未达 limit 不提示）

## 2. 文档漂移修正

- [x] 2.1 `key_schema.go` 头注释删除未实现的 `{pid}:L{layer}:*` 键格式，改述"压实复用普通键 + SegmentMeta.Layer"

## 3. 测试

- [x] 3.1 B1 回归：三窗口×各 10 条匹配、limit=10 desc → 全部来自最新窗口；memory_recent 场景（无 keyword、limit=20）→ 全库最新 20 条
- [x] 3.2 B2 回归：毫秒 since 查询返回最近事件（修复前恒 0）；剪枝不剪掉含合格事件的段
- [x] 3.3 B3 回归：L2 日段（窗口天对齐）+ 时间范围落在当天下午 → 段参与扫描
- [x] 3.4 去重回归：同一事件双层并存（模拟压实中间态）→ 只返回一次；**desc 与 asc 两方向均返回高层版本**
- [x] 3.5 offset+早停组合：结果与"全收集→排序→截断"参照实现一致
- [x] 3.6 双实现一致性矩阵：同数据写入 InMemoryStore 与 FileSegmentStore（LocalFileKV），desc/asc × limit 边界 × 毫秒时间范围 × keyword × **同毫秒 tie 事件** 组合断言 EventKey 序列逐位一致
- [x] 3.7 同毫秒稳定性：tie 事件重复查询逐位稳定（无非稳定排序抖动）
- [x] 3.8 诚实截断：结果数恰等于 limit 时 message 含截断提示；少于 limit 时不含
- [x] 3.9 KVScan 有序性断言：`TestKVScanLexicographicOrder` 覆盖 MockRustVikingClient 与 LocalFileKV（顺带修复 Mock 的 KVScan 未排序 + limit 先截断的缺陷）

## 4. 验证与收尾

- [x] 4.1 生产数据验证（已跑：快照 copy 至 /tmp 后真实查询）:以 wechat-bot `.wechat-config/data` 快照（或复制）跑一次真实查询：keyword=彭伟业 limit=10 desc → 返回含 07-31 最新事件；memory_recent → 时间接近快照末尾
- [x] 4.2 全量测试 + 构建干净（`go build ./...`、`go test -short ./...`、`go test ./memory/...` 全绿）
- [x] 4.3 同步 wiki（memory-architecture 查询路径描述 + 已知缺口表若涉及）

## 5. 段边界可信性与早停基准修正（D9/D10）

- [x] 5.1 压实写真实边界：`CompactL1ToL2` / `CompactL2ToL3` 在 filter 后取 `events[0]/events[len-1]` 的 Timestamp 写入 `SegmentMeta.MinTime/MaxTime`（零额外扫描；SealCurrent 不动）
- [x] 5.2 真实上界三档取值：`MaxTime>0` → 用它；`layer≤1` 无 MaxTime → 标称 `start+1h`；`layer≥2` 无 MaxTime → 无上界（不剪不跳）
- [x] 5.3 早停基准改为实际事件时间戳：以 `minCollectedTs`/`maxCollectedTs` 取代 `minScannedStartMs`/`maxScannedEndMs`，且比较改为**严格不等式**（desc: 上界 < minCollectedTs；asc: 下界 > maxCollectedTs）
- [x] 5.4 回归测试：超宽 L2 段（标称 1 天/实际 3 天）×｛带 since 剪枝、desc 早停、asc 早停｝均不丢事件；legacy 段（layer=2 且 MaxTime=0）不被剪/跳；同毫秒边界不被误跳
- [x] 5.5 **LSM 语义对齐（D14，取代三档回退）**：
  - (a) `SealCurrent` 封口时扫当前窗口写入 `MinTime/MaxTime`；
  - (b) 剪枝/早停改为“有 bounds 用 bounds；Sealed=false 或无 bounds 一律扫描”，移除 `windowSpanSec` 的时间推理用途（若无其他调用则删除该函数）；
  - (c) 段放置保持按写入时间（不变，且为正确设计；误导期间的“按事件时间放置”作废）；
  - (d) 新增测试：封口写入边界；活跃段在写入/事件时间分叉下不丢事件（DivergentWriteAndEventTime 转绿）；历史无 bounds 段仍不剪不跳（已有）

## 6. 时间真相源契约（D8）

- [x] 6.1 `memory/types.go`：`FullEvent.Timestamp` 与 `EventKey` 内嵌时间的角色注释（唯一时间轴 vs 放置+决胜）
- [x] 6.2 不变量测试：构造 key 内嵌时间与 Timestamp 显著分叉的事件，断言查询结果与“只按 Timestamp 过滤排序”参照完全一致

## 7. 写入侧不变量与死代码清理（D12/D13）

- [x] 7.1 seqCounter 轻量恢复：`StoreEvent` 判定新窗口时先扫 `{pid}:evt:{窗}:` 取最大 seq，从 `max+1` 续写（空窗口从 0）
- [x] 7.2 回归测试：同窗已有 seq 0..4 → 新建 store 实例（模拟重启）写入 → 新事件占 seq 5、旧事件仍可经 EventKey 取回（防覆写）
- [x] 7.3 `eventCount` 递减：`DeleteEvent` 减 1；压实 `deleteSegments` 按被删段 `EventCount` 递减
- [x] 7.4 删死代码：移除 `ensureSegmentMeta`；从 `MemoryStore` 接口与两个实现移除 `StoreEvents`，更新引用到它的测试

## 8. 遗忘层通电（D11）与规格债处置

- [x] 8.1 修正 `ParseKey` 误用：`checkTTL` / `evictOldest` 从事件 JSON 读 `event_key`（与 `timestamp` 合并为一次 unmarshal）
- [x] 8.2 TTL 测试：写入超期事件 → 跑 `checkTTL` → 断言墓碑存在且 `GetEvent` 报 miss；`context_compress_summary` 不被标记（固化物豁免）；未过期不被标记
- [x] 8.3 生命周期配置化：`MemoryConfig` 新增 lifecycle 字段（`global_ttl_days`/`type_ttl`/`check_interval`/`max_events_per_partition`），替掉 `tagent.go` 两处硬编码 `DefaultLifecycleConfig()`；未配置回退默认、可显式关闭
- [x] 8.4 配置测试：YAML 覆写生效、缺省回退、关闭时不标墓碑
- [x] 8.5 规格债：本 change 的 delta 已含 `ttl-cursor-scan` 全量移除与 `event-lifecycle` 游标条款改写；归档同步时确认主 specs 中 `ttl-cursor-scan` 目录随之清空/删除
- [x] 8.6 流程教训入档：wiki 已知缺口记录“归档应加实现核对：tasks 未完成不得同步 delta 入主 specs”

## 9. 整体验证

- [x] 9.1 生产快照复测：时间范围查询 `[07-27, 07-28]` 返回数 ≈ 真值 1791（修前 682，丢 62%）；keyword/recent/since 三路径均命中最新
- [x] 9.2 全量测试与构建干净（`go build ./...`、`go vet ./...`、`gofmt`、`go test -short ./...`）
- [x] 9.3 wiki 第十六章与已知缺口表与最终实现一致（已修项从缺口表移出或改写）
- [x] 9.4 `openspec validate segment-query-recency --strict` 通过

## 10. 零基 Code Review 修复（第二轮，全部已验证）

> 来源：变更扩大后的零基审查（1 Blocker / 4 Major / 6 Minor-Nit / 2 预存地雷）。每项附回归测试。

- [x] 10.1 **B1**：`NewLifecycleManager` 把负数 `GlobalTTLDays` 钳回 7——负值是"总开关"，改为仅零值回退默认；`getEffectiveTTL` 负全局优于类型表（`TestNegativeGlobalTTLDisablesTTL`）
- [x] 10.2 **M1**：`segmentBounds` 下界 `max(windowStart, MinTime)` 在写入/事件时间分叉下失真——MinTime 存在时直接信任（`TestQueryEvents_MinTimeBelowWindowStart`）
- [x] 10.3 **M2**：碰撞守卫在 evt 写入后执行，被拒写入留下可被段扫描召回的"幽灵事件"——守卫前移到任何写入之前（`TestStoreEvent_RejectedCollisionLeavesNoGhost` 补 QueryEvents 断言）
- [x] 10.4 **M3**：seq 恢复扫描失败降级为 0 会原样复现覆写——改为 fail loud（StoreEvent 返回错误，上游可幂等重试）
- [x] 10.5 **M4**：容量淘汰棘轮——墓碑不递减 eventCount，每周期多杀 excess+10 活事件——`checkTTL`/`evictOldest` 标记墓碑即递减（逻辑存活语义，`TestEvictionDecrementsLiveCount` 含二次周期幂等断言）
- [x] 10.6 **P1（预存数据级地雷）**：日对齐最早源窗使 `l2WindowTS == 源窗`——压实先写后删会删掉刚写入的目标段——`deleteSegments` 排除等于目标窗口的源窗（L2/L3 同构，`TestCompaction_DayAlignedSourceSurvives`）
- [x] 10.7 **P2（预存）**：活跃（未封口）段被卷入压实——`checkL1ToL2/L2ToL3` 只选 `Sealed=true` 的段（`TestCompaction_SkipsUnsealedSegments`）
- [x] 10.8 **m5**：写入已封口窗口使其边界失效——降级回 `Sealed=false`（memtable 语义兜底，`TestStoreEvent_WriteIntoSealedWindowDemotesToMemtable`）
- [x] 10.9 **m2/m3/n1**：parity 矩阵补封口段场景（`TestQueryEvents_ParityWithSealedAndWideSegments`）；InMemoryStore `Limit<=0` 默认统一为 100（双实现一致）；SealCurrent 哨兵改 hasMin 布尔
- [x] 10.10 全量验证：`go build`/`go vet`/`gofmt` 干净；`go test -short ./...` 20 包全绿
