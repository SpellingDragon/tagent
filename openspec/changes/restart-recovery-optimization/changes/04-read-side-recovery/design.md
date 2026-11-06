# 04-read-side-recovery 设计

## D1 判据

恢复第二步 SHALL 由**增量**决定工作量，而不是由**历史**决定：装载快照后，从 `landedSeq` 起只 land / 折 / Submit 那个位置之后的事件。可机械断言：一次 boot 里 `EachEvent` 的 visit 条数 = 增量条数（历史 200 条 + 增量 5 条时必须 = 5，今天 = 205×3 遍）。时间不是判据。

## D2 四样状态

| 面 | 状态 | 现在的开机重建 | 改法 |
|---|---|---|---|
| 观察表 | `observedEntry{record EventRecord, ref OriginalRef}`（`memory/store_query.go`） | `ObserveStore` 全量 land | 进唯一快照；只 land 增量 |
| `landedSeq` | `map[partition]seq` | `seedLandedSeq` 全量扫 | 进唯一快照；作为增量起点 |
| 回放投影 | 每会话 `ReplayProjection` + `FoldWindow` + 压缩产物 | `seedReplay` 全量折 | 进唯一快照；只折增量 |
| 向量缝 | `VectorSeam` 的 `vectors map[EventID][]float32` | 全量 Submit | **不进快照**（见 D4） |

前三样的写入点都在既有增量路径上（`assembly/facts.go`），不新增提交点、不新增事实族。

## D3 快照形状与原子序

完成点仍写**唯一一份**快照：既有恢复状态（`runtime/state/recovery.go` 的 `recoveryTables`）+ 观察表 + 回放投影 + `landedSeq`，同一 payload、版本号随 `recoverySchema` 升版；blob 先、票据后，digest 与长度校验，外族版本拒绝。四样里任一缺失或版本不符：退回全量重建。

## D4 向量缝（待用户裁定）

- (i) 持久化向量分片：语义零变化，快照随历史线性涨；
- (ii) 服务开放后后台补齐 + 可观测进度（推荐）：开机不重建，补到哪可观测可判红；
- (iii) 查询时只对候选算：开机不重建，向量腿的候选面收窄。

无论哪条，都要有一致性锚：同一 query 的 Top-K 与全量重建逐条一致（(ii)(iii) 以补齐完成后为断言时点）。

## D5 失败退回

无票据 / blob 缺 / digest 或长度不符 / 外族 schema / 四样状态任一版本不符或 `landedSeq` 不可信 → 一律退全量重建（今天的路径原样保留），具名告警，结果逐面等价。绝不猜。

## D6 不改的东西

事件契约、事实族、插件管线、交付/冥想语义；「分区位图/Init 游标」预案按实测删除，不在本域复活。
