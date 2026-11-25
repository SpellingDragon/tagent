# 升级 / 回滚演练口径

> 本篇原为 inbox-v1 时代的运维 runbook（spill 遗留、目录锁、分区预检等）。该生产实现已由 runtime-architecture-convergence 移除：交付与恢复责任现全部落在事实链上（owed 记账、重启补交、config_change 控制流水），独立的 inbox/锁/指纹机制面不复存在。历史原文见 git。governance 演练引用本页时，按下面的口径记录。

## 一条升级/回滚演练必须记录什么

| 项 | 判据 |
|---|---|
| 运维可观测符号与只读诊断 | 真实错误串/具名错误值；定位只用「文件 + 符号」，不写行号；先给出不改状态就能回答问题的动作 |
| 纠正措施 | 显式、可审计；禁止隐式改写历史数据 |
| 佐证 | 具名测试（崩溃窗口/并发/重启各至少一腿），非人工 PASS |
| 回滚前提 | 存在未结清义务（如 owed）时拒绝降级，排空为 0 方可；**热卷归档是第二条边界**：归档键只在保留 `archive_root` 声明的构建里可读，**受支持的回退是保留归档声明（由仍认这三个键的构建服务归档键）或离线折回/前向修复**；移除声明后热卷只剩它当前预算内的那些封段与开段，已归档键按不存在作答（`ErrOriginalMissing` → miss，不报错），该形态**不是受支持的静默回退**，只能记作运维前置，且本仓没有「归档并回热卷」的工具（`recoverArchive` 只做归档侧结构恢复，零生产调用）——故回滚前提是「没有必须读到已归档事实的读面」，否则不得回滚；演练须记下哪些键会因此读不到 |

## 现行演练与 03.301 API 升级

跨重启补交：进程在结果产生后、投递确认前被杀，重启后 owed 记账驱动补交恰一次（`tests/integration/host/entry_delivery_owed_test.go`）。配置热换与回退：发布换代在途请求保旧视图、失败不换代不落事实（`tests/integration/host/` 的 reload 族锚）。热卷归档的回滚演练（bounded-hot-log）：在声明 `archive_root` + `hot_log_max_bytes` 的根上取若干切点并促成一次搬迁后，① 用当前构建重开，已归档键按身份读回（`TestHottestSegmentNeverArchived`、`TestMissingArchiveIsNamedNotAbsent`／`TestCorruptArchiveIsNamedNotAbsent` 分别钉住"新段永不归档、缺段与坏段不伪装成不存在"）；② 用不声明归档的配置重开同一根，已归档键答不存在而不是报错（`ErrOriginalMissing` → miss），热键照常可读——这是**边界实测，不是受支持的回退形态**（受支持的回退只有保留归档声明或离线折回/前向修复），演练记录必须写明"哪些键因此读不到、是否可接受"，不得只记"服务起来了"。另须先确认归档卷在线且 manifest 完整：manifest 双槽皆无且段目录无最终名制品才读成空归档（键答不存在）；manifest 全丢而段目录有最终名段/段索引则拒 `ErrArchiveCorrupt`（不判空归档、不删该名文件）；manifest 在而段缺失或 digest 不符则开档/读面具名拒（`ErrArchiveMissing`／`ErrArchiveCorrupt`）。
03.301 将 `features/rl.FactReader.SessionFacts(addr) []event.Event` 改为 `SessionFacts(ctx, addr) ([]event.Event, error)`，随实现生效；`ChainExporter.Export` 签名与输出不变，但实现、生产调用、测试替身和示例须同批迁移，不留旧签名或双 reader。触发：依赖构建含 03.301，或跨档读遇 `ErrArchiveMissing`/`ErrArchiveCorrupt`/取消/超时；错误必须原样上返，禁止吞错或以空/部分事实冒充成功。消费者：`features/rl/export_chain.go`、`features/rl/export_chain_test.go`、`tests/integration/learning/{chain_export,scenario10,event_integrity}_test.go`、`examples/contract/features_rl/FactReader_example.go`、`examples/wechat-bot/smoke_data_test.go`。迁移：接口与全部实现加 ctx/error；assembly durable adapter 经 `EachSessionFact` 枚举身份、正文只走 `GetEvent`；exporter 先处理错误再遍历；生成文档同步。自查：`rg -n 'rl\.FactReader|NewChainExporter' --glob '*.go'`、`rg -n 'SessionFacts\(addr event\.Address\) \[\]event\.Event' features/rl examples/contract/features_rl`、`go test ./features/rl ./assembly ./tests/integration/learning -count=1`。`state.MemoryStore` 新增内部 handover 方法 `AcceptNoticeClaimed(ctx, notice) (state.WorkSeat, error)`，返回实际认领的站点与序列；重放仅认领原站点仍 pending 的条目，已认领或已消费返回 `ErrClaimConflict`。`state.Store` 公共接口与外部 `AcceptNotice` 语义均不变。
