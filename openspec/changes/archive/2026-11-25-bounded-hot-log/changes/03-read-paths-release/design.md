# Design · 03

## D1 消费面清单（03.301）

- `recall_get`/`recall_trace`：已知键按身份读，跨档透明；三态错误原样上返（不折叠）。trace 父链热 order 未中时回落事件正文的父关系，仍经 `GetEvent` 统一身份读；`ErrArchiveMissing`/`ErrArchiveCorrupt` 不得折成 `missing_ancestor`/`relation_error`。
- 归档读缓存：resident 与 segment read cache 不得服务归档来源旧副本；归档 locator 的 digest 验证缓存绑定段文件 stat 签名（大小＋mtime），签名变化必须重算。热缓存仍可服务热来源。
- 历史查询/导出（`memory/query`、`features/rl`）：`FactReader.SessionFacts` 改为 `SessionFacts(ctx, addr) ([]event.Event, error)`；assembly 提供 durable adapter，按 `EachSessionFact` 枚举身份、正文只走 `GetEvent`，错误直返。批量读复用同一读入口，禁第二读路径。
- 全史回读（恢复/审计）：接受归档读延迟，须有超时与具名失败。
- 禁止：任一消费面把归档正文复制进热缓存形成第二真相（读缓存须可丢、按身份校验）。


## D2 验收锚（03.302）

- S1：`TestHotLogPlateausAcrossGenerationsAndOldFactsStayReadable`——N 代写入；每代收尾记录热字节（分母＝窗口提交字节）；第 1 代老键逐字节读回＋`recall_get` 同正文；平台带按 01.102 判据；同锚机理断言：最新 cut 段与开段不在 manifest（恒热），全史封段数＝成功 cut 数（热卷常态仅余最新 cut 段＋开段），`configured = hot_log_max_bytes + 2×当前热段最大日志区间 ≥ H` 的正常负载下 `hot_over_budget` 为零，**开档完整性校验字节有界于热窗（不随归档增长；归档段读时校验）**。
- 条件语义两锚成对：S1（正常负载绿）＋`TestQuietPointUnreachableToleratesOverBudget`（故障注入：安静点不可达→容忍超界、不封段、`hot_over_budget` 状态为 1 且具名上报）——两锚合并钉死「正常路径结构保证＋故障路径观测义务」；任何文档措辞越过该语义即与第二锚矛盾。
- S2：`TestCorruptArchiveIsNamedNotAbsent`、`TestMissingArchiveIsNamedNotAbsent`（manifest 缺/段缺/校验失败三态）。
- 结构负锚：`TestHottestSegmentNeverArchived`——搬迁尝试覆盖最新 cut 段/开段时须不发生（水位规则拒绝）；`TestFailedCutLeavesNoSegmentBoundary`——失败 cut 的残帧留在开段，不产生段界。
- 恢复引用读：`TestCheckpointSliceReadsArchivedBodyByIdentity`——checkpoint 切片引用的 `blob:/ref:` 正文已归档时，恢复经定位读逐字节读回（属常规引用读，非 `archive-tail-read`）。
- 回退：从未归档的根在无 `archive_root` 下运行同一场景，字节行为与基线一致（零 diff 探针）；已归档根移除声明后归档键不可得，不属受支持回退。
- 归档声明：`archive_bytes(gen)` 与跨档读 p50/p90 单列；**开档恢复成本单列**（boot 校验字节/耗时、索引重建量，有界于热窗；与 01.101 基线对比）。

## D3 停止条件

- 某消费面无法经统一定位读（需改导出格式/身份）→回设计门。
- 平台锚需要放宽判据才绿→停，判据不得追认缩水。

## D4 设计门裁定（2026-11-24）

- 触发：原 03.301 写面不含 ledger/assembly，但父链、暖缓存与导出均需跨层改动，命中 D3 停止条件。
- 选择：扩展 03.301 写面到 `memory/ledger`、`assembly`、`tool/recall`、`features/rl`；`FactReader` 单契约改 ctx/error，所有消费者同批迁移；不新增并存 reader 接口。
- 否决：保留无错误 `FactReader` 再由 adapter 吞错＝无法满足三态原样上返；warm cache 继续服务归档旧副本＝违反可丢校验约束。
- 兼容：公共 API 属 pre-stable，同批更新全部消费者与示例；外部可感知破坏记入升级说明。
