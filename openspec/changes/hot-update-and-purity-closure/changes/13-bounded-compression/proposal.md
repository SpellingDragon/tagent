# 13-bounded-compression 提案

## Why

长程校准（合并门）六分片全红，统一根因：压缩产物涨到 88MB 超 journal 帧上限。抬帧上限（32→64MB）是输掉的创可贴——产物随历史单调增长，会涨过任何上限。

挖到核心：三段龄折叠名存实亡。LevelOfAge 算了 L0-L3 级，但 FoldByAge 只分「新 vs 非新」，中+老全塌进一条历史摘要；摘要机制是确定性行拼接，historySummaryLine 对上一折叠产物返回 entry.Content 全文，每摘一次把上一摘要全文再抄一遍 → 滚雪球到 88MB。「压缩」根本不压缩，是累积全文。这是「有界恢复」没真正达成的核心，也是反复修 CI 失败暗示的架构不清处。

## What Changes

- 三段龄按**世代递推**重定义（用户裁决）：新=recent 保留；中=上次折叠时还在新的（这次折叠过程细节）；老=上次已在中/老的（这次摘要）。每折一次晋升一级，取代绝对段数分级。
- 折叠/摘要分职：新原样 / 中 foldProcessDetail（结构重组）/ 老 summarization（有界压缩）。修当前「折新带、中老塌摘要」错位。
- 老带摘要 **LLM 语义摘要优先**（设计本意）：有配置走 LLM 有界 gist 落摘要事实（回放读存储事实保确定），无配置降级机械有界（recent-K 行＋Covered-key 引用），失败降级/记欠账。
- 摘要有界幂等：上一产物不抄全文，改有界引用；fold(fold(x))==fold(x)；体积稳定。
- 配套：帧上限回退合理值、fold golden 重生成、async filer parity off-by-one 修。

## Impact

- 能力：bounded-compression（新增）；window-recovery（投影有界）。
- 代码：memory/replay/age_segments.go（FoldByAge/historySummaryOf/historySummaryLine/世代 band）、assembly/compress_consumer.go（LLM 摘要触发）、assembly 模型面接线、memory/ledger/journal.go（帧上限）、golden fold 向量。
- 行为变更（已裁决）：模型上下文从逐字全史变为近期逐字＋中段骨架＋老段 LLM 有界摘要（按需回链查原文）。
- 合并门：LongRun 六分片整代 PASS、产物有界、boot 平坦。