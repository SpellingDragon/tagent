# 02 · 段轮转/搬迁/GC/定位读（rotation-archive）

## 边界与依赖

- 父变更 bounded-hot-log；依赖 01.104 签发的 `contracts/archive.md` 与判据。
- 依赖：`pointReadLog` 窄口（runtime-coherence 已落）、`memory/ledger` 现行日志/索引/原文族。
- 被依赖方：03 消费与验收。
- 接口面：`memory/ledger` 内部（段轮转/封段/manifest/定位读）；对外只经 `pointReadLog` 窄口与既有读入口，读侧三态错误按契约上返；配置面仅 `archive_root`/`hot_log_max_bytes`/`archive_max_age`。
- 禁止：复活 relocation/RetireFacts/冷档探测；热段删除先于 manifest 提交；两套索引各存一版。

## 范围

热日志在 cut（与 checkpoint 同一提交批）时封段，字节阈值只触发提前 cut 请求；封段自描述索引；copy→fsync→manifest 原子提交→删热段→收缩热索引；定位读序；崩溃切点恢复。

## 完成定义

- 轮转与封段确定性（同输入同段界，由 cut 序号与提交序决定）；封段索引键→偏移完整。
- 搬迁幂等、崩溃安全（四个切点注入中断后可重开一致）。
- 归档后热字节回落、热索引收缩；跨档读逐字节相等、三态错误分离。
