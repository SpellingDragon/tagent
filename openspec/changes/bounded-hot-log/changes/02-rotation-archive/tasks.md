# Tasks · 02

- [ ] 02.201 段轮转与封段索引 —— 写面：`memory/ledger/journal*.go`（轮转点/段头/索引生成）＋测试。封段时点＝cut（安静点提交批）；段界只在 barrier 返回成功后推进；同输入段界确定（cut 序号＋提交序）；索引可从段重建。—— 验证：正锚＝`TestSegmentRotationIsDeterministic`、`TestSealedSegmentIndexLocatesEveryKey`（逐键偏移读回逐字节相等）；负锚＝`TestFailedCutLeavesNoSegmentBoundary`、索引损坏具名拒；消费者锚＝提交与开档真实路径读同一段头/段索引。
- [ ] 02.202 搬迁与 manifest —— 写面：`memory/ledger/archive*.go`（新文件）＋测试。固定序；四切点注入中断后重开一致；重复搬迁幂等。—— 验证：正锚＝`TestArchiveCrashRecoveryIsIdempotent`（copy 后/fsync 后/manifest 后/删除中各注入一次）；负锚＝`TestCorruptManifestRefusesArchiveRead`，不得把 manifest 缺当“无归档”；消费者锚＝boot 重开经 manifest 定位已归档段。
- [ ] 02.203 热盘 GC 与索引收缩 —— 写面：`segment_store.go`/`original_store`/`reference.go`。删除只发生在 manifest 已陈述之后；热索引收缩到热段+段级定位；`slots` 头 `Seq/ValuesSize/OrderSize` 水印按新热文件重述。—— 验证：正锚＝`TestHotIndexShrinksAfterArchive`（热条目有界于热段键数）、`TestSlotWatermarkRestatesOnShrunkHotFiles`；负锚＝manifest 未提交时删热段→拒（构造注入）；消费者锚＝开档收缩后按身份读热段键。
- [ ] 02.204 定位读路径 —— 写面：`memory/ledger` 读入口＋`api/durable` 文档。三态错误分离；跨档读逐字节相等。—— 验证：正锚＝`TestIdentityReadCrossesHotAndArchive`（老键经文件真实读）；负锚＝`TestArchiveMissingIsNamed`、`TestArchiveCorruptIsNamed`；消费者锚＝`recall_get` 工具读面（03.301 复核）。
