# 01 · 有界判据与归档契约（bounds-contract）

## 边界与依赖

- 父变更 bounded-hot-log（启动门：runtime-coherence 的 CI 收口后）。
- 依赖：`memory/ledger` 现状只读；`bounded-recovery`/`session-checkpoint-material` 现行措辞。
- 被依赖方：02、03 全部实施叶。
- 接口面：本域只产判据、契约与签发，不改产品代码。
- 禁止：以现状声明当目标；删/改已提交语义；引入与“退位/冷档”同形的机制。

## 范围

实测增长构成（帧体/original/ref/索引/节奏记录在 journal 字节中的占比）；形式化“热卷平台 + 归档增长声明”的验收判据；冻结归档定位契约（manifest、段自描述索引、定位读、失败语义）与两个配置字段；产出实施签发。

## 完成定义

- 01.101 给出四族字节实测与可搬性判定（哪些字节属帧、哪些可随段移动、有无共享正文）。
- 01.102 冻结判据：平台带形式、分母口径、S1 锚草案（第一天红）。
- 01.103 冻结契约：manifest 唯一权威、段索引格式、`ErrArchiveMissing/ErrArchiveCorrupt`、`archive_root`/`hot_log_max_bytes` 声明与严格解码、契约措辞修订清单。
- 01.104 签发区齐（每叶精确写面、三锚、回滚触发、独立审通过）。
