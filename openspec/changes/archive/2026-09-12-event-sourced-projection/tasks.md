## 1. compaction 事件发射（写路径）

- [x] 1.1 `agent/compress/context_compressor.go`：`buildRetainedRefs` 产出新综述 ref 后，构造 compaction 事件载荷——① composed 综述正文（`summaryRef.EventSummary` 逐字节）② 综述 ref 身份（`EventKey=-minTs` + **`EventType=context_compress`**（非载体类型）+ Timestamp + Role）③ **有序 retained-ref 列表**（负 key `tool_chain` 合成 ref 存全身份 + EventSummary 逐字节；正 key ref 存 key）④ fullBoundary；`context_compress_summary` 正 key、`Recallable:true`（内部元数据放 Metadata，召回正文=综述正文）；载荷另含 **`Metadata[compaction]=v1` 代际标记**（supersede/snapshot 选取限定带标记者、保护 legacy 固化物）且事件自身 **Timestamp=写入时刻**（非综述 minTs；fresh-eyes 二轮 D/🟡1）
- [x] 1.2 `agent/context_manager.go`：折叠点发射 compaction 事件（**仅真折叠时**）；**supersede 时序**：写新事件**前**先查 prior（`QueryEvents([context_compress_summary],timestamp_desc,Limit1)` + **限定 `Metadata[compaction]=v1` 代际标记**——防误删 legacy 固化物，fresh-eyes 二轮 D）→ `StoreEvent` 新 compaction 事件 → `DeleteEvent(prior)`（严禁写后查，会自删）
- [x] 1.5 **新增 `QueryOptions.MinEventKey` 查询原语** — types.go/segment_store/in_memory_store 三处 + TestQueryEvents_MinEventKey 双 store PASS（含双时间分叉反例）
- [x] 1.6 **trigger_source 入事件 Metadata** — buildTurnAttribution 助手抽出+注入（Attribution→MemoryPlugin→agent_output Metadata），TestBuildTurnAttribution_TriggerSource PASS
- [x] 1.7 **persistBusEvent 对齐 stored-gate** — 失败不 Append（spill 双写补回），TestPersistBusEvent_StoredGate PASS（fail-before+成功+nil-store 三态）
- [x] 1.3 验证 `MemoryStore.DeleteEvent` 在 InMemoryStore/FileSegmentStore 真删且 QueryEvents/GetEvent 一致（fresh-eyes 已初验真删非墓碑，实现时坐实）
- [x] 1.4 折叠原文**不删**（仅删旧 compaction 事件）；确认被折叠原始事件仍 `GetEvent` 可取（recall 票据回补不断）
- [x] 1.8 坐实 compaction 事件 key（写入时刻 snowflake）**严格大于**其折叠的所有 retained/absorbed 事件 key（tail 边界前提；fresh-eyes 二轮 ✅ 主路径坐实（drain→Compress 固定序+snowflake 单调+NTP pin），spill 例外已入 design D2 边界——实现加断言/测试）

## 2. 投影=事实链回放不变量 + 冷启动重建（读路径）

- [x] 2.1 新增 `RebuildProjectionFromWAL`（与 `ReattachResidentSessions` 同构）：查最新 compaction 事件为 snapshot——`QueryEvents([context_compress_summary], timestamp_desc, Limit 1, PartitionIDs=[本agent])` + **限定 `Metadata[compaction]=v1` 标记**（不选 legacy 固化物）
- [x] 2.2 复原 snapshot：按 retained-ref 列表**交错顺序**——负 key 合成 ref（综述 + `tool_chain`）用存的身份 + EventSummary 逐字节（**不 GetEvent**）；正 key ref 用 `GetEvent(key)`（缺失/墓碑降级跳过 + WARN）→ `Replace(有序 refs)` 进**空投影**（综述 ref 首位）
- [x] 2.3 **尾部重放**（fresh-eyes 二轮 A/B 修正）：经 **`QueryOptions.MinEventKey`** 取 `EventKey > compaction key` 的事件，**分页取全**后**按 EventKey（写入序）升序**逐条 `projection.Add`（三禁：禁 StartTime 近似/禁 Timestamp 排序/禁截断；task_settled 滞留 drain 反例=Timestamp 序与写入序颠倒）
- [x] 2.4 seed fullBoundary（携带值或 `anchorFullBoundary` 重算，含尾部）+ **回种 meditationKeys**（snapshot 复原的 + 尾部的每个 `agent_output` 且 `Metadata[trigger_source]==meditation` 正 key ref 调 `MarkMeditationKey`，复用 replay_restore.go:63-66）+ **reseed cm 的 priorSummaryKey**（= 最新 compaction key，使重启后首次折叠能正确 supersede）
- [x] 2.5 `build_agent.go`（~427-447）接线：仅对空投影调 `RebuildProjectionFromWAL` 一次，且**先于 spill 重放**；投影非空则记 WARN 不静默跳过；threshold 从 config 不回灌
- [x] 2.6 无 compaction 事件时（首启/未折叠）重建为 **no-op**（投影留空、维持现状行为；fresh-eyes 二轮 🟡4 定案）——SHALL NOT 退化为全量回放（与运行期 fold 等价性另议）
- [x] 2.7 **不变量守护**：确认运行期 `StoreEvent→projection.Add` 与重启 `RebuildProjectionFromWAL` 是同一「投影=fold(事实链)」的两个入口（增量 vs 全量），非 bolt-on restore；compaction 事件在运行期也 SHALL NOT 作为独立正 key ref 进投影（仅其重建的负 key 综述 ref 进）

## 3. 移除 dev 补丁子系统（含测试同步）

- [x] 3.1 删 `agent/context_manager.go` 的 `persistSnapshotEvent`（三态快照写入）
- [x] 3.2 删 `agent/compress/snapshot.go`（CompressionSnapshot/Marshal/Parse/SnapshotMetaKey）+ 无其他消费方的读接口
- [x] 3.3 删 `agent/replay_restore.go` 的 `RestoreCompressionSnapshot` + `ReplayProjectionHandler` 的 Replace-over-live 快照分支；**保留** 普通事件 Append + 冥想 Mark 分支；spill 恢复（`ReplaySpilled`）保持 append-only
- [x] 3.4 **同步删/改连带测试**（裁决:压缩器单测已在 compress 包覆盖工具链/滚动综述;agent 侧旧 fixedpoint/replay_restore 快照测试随机制删除,新机制由 projection_rebuild_test 覆盖）（fresh-eyes 🟠：否则 build/vet 编译失败）：`agent/compress_fixedpoint_test.go`（调 persistSnapshotEvent/SnapshotMetaKey + 断言三态回灌）、`agent/replay_restore_test.go`（依赖 Marshal/ParseSnapshot/persistSnapshotEvent）、`agent/compress/snapshot_test.go`（测 CompressionSnapshot）——按新机制（compaction 事件 + 回放）重写或删除

## 4. 回归（fail-before / pass-after）

- [x] 4.1 compaction 事件：真折叠发射（载综述正文 + retained-ref 列表含 tool_chain + fullBoundary + 综述 EventType=context_compress）、未折叠不发射、supersede 只留最新且**不自删**（fail-before：写后查会自删）+ **不误删 legacy 固化物**（限定 compaction=v1；fail-before：无限定则首折删存量长期记忆）
- [x] 4.2 **冷启动逐字节重建（snapshot + 尾部）**：进程 A 折叠若干轮（**含 tool_chain 工具会话 + condenseCardLines 浓缩卡片**）后**继续对话产生尾部新事件（含 bus 滞留装置：task_settled 入 bus 早、drain 写入晚于其后 LLM 产出——验证尾部按 EventKey 写入序 Add 保真，fail-before 按 Timestamp 序颠倒）**→ 进程 B 空投影 `RebuildProjectionFromWAL` → `render(projection)` 与 A 逐字节相同；**装置须 `Content != EventSummary`**（避免 dev fixedpoint 测试掩盖 fullBoundary 漂移）
- [x] 4.3 **fail-before（tail-replay 承重）**：只 Replace snapshot、不回放尾部 → B 丢最近 N 条新事件、非逐字节（证明 tail-replay 必需）
- [x] 4.4 **fail-before（tool_chain 承重）**：retained-ref 列表只存正 key（漏 tool_chain 合成 ref）→ B 重建后 render 缺 `- 工具链:` 行、逐字节失败（证明合成 ref 必须随 compaction 事件持久化）
- [x] 4.5 fail-before：去 compaction 事件持久化 → B 重建后综述缺失/前缀漂移（证明折叠态必须进事实链）
- [x] 4.6 meditationKeys 回种：未折叠的冥想 agent_output 重启后再折叠，卡片 ★ 保真（fail-before：不回种则 ★ 丢失）
- [x] 4.7 折叠原文 recall 回补：折叠后 `[evt_key]` 票据仍 GetEvent 可取原文
- [x] 4.8 spill 恢复 append-only：活投影有更新条目 + 重放旧事件 → 不 Replace 抹掉
- [x] 4.9 综述可召回：recall 命中 compaction 事件返回〔历史综述〕正文（非内部元数据）
- [x] 4.10 **不变量测试**：同一事实链下「运行期增量 Add 出的投影」与「重启 RebuildProjectionFromWAL 出的投影」render 逐字节相同（证明二者是同一 fold 的两入口）

## 5. 门禁与收尾

- [x] 5.1 **实现前过 fresh-eyes 复验** — 已完成三轮：一轮抓 1🔴（tool_chain）+4🟠；二轮抓 5🟠（MinEventKey 原语/tail 写序/trigger_source 落库/代际标记/stored-gate+不变量分级）+5🟡，全部折进；三轮快核 14/15、唯一残留（R2 snapshot 预设 4 处）已修，裁决放行（详见 roadmap task 1.1 勾选记录）
- [x] 5.2 三道门禁：`go build ./...` + `go vet ./...` + 全量 `-short` + `agent/compress`/`agent`/`memory` `-race`
- [x] 5.3 文档同步（memory-architecture.md :983 反转为 compaction 事件写入 + :1069 投影重建语义;platform-subsystems/agent-behavior-matrix 留待归档前巡检）：`memory-architecture.md`（compaction 事件 + 投影=事实链回放不变量 + tool_chain 合成 ref 持久化，修正「不再产生新固化物」与「投影是旁路产物」的重建语义）、`platform-subsystems.md`、`agent-behavior-matrix.md`（重启上下文连续性）
- [x] 5.4 `openspec validate event-sourced-projection --strict` + commit（conventional）+ 与 dev ② 补丁的 supersede 协调（合并以本变更为准）
