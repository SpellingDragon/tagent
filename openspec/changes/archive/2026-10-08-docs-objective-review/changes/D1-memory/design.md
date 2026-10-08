# Design: D1 记忆存储域评审

## 阅读顺序与技术要点

1. `memory-architecture.md`（主体）：FullEvent/EventReference 双表示 → L0-L3 LSM 分层 → 因果链/墓碑 → TTL 遗忘 → 召回协议 → 记忆策展；末章"已知缺口与演进方向"须逐条摘录。
2. `storage-durability-positioning.md`（25 行定位短文）：持久化立场声明，核对与 memory 篇的一致性。
3. `evaluation-suites.md`：票据可召回率等评估——判断"零幻觉"主张是否有量化验收面。

## 重点问题（评审代理必须回答）

- 投影只存轻量引用 + MemoryStore 唯一全量：压缩"永不动存储"的不变量在崩溃/并发窗口下是否真成立？
- TTL 按类型遗忘 vs "事件不可变"哲学是否自洽（遗忘是不是改事实）？
- LSM 压实、TTL、容量三层职责划分是否清晰，还是同一件事三处做？
- 语义召回（RRF 融合）缺 embedding key 时"优雅降级"的实际形态。

## 代码抽查断言候选（≥2 个，须给 文件:符号 佐证）

- L0-L3 分层压实真实存在（memory/ 下 segment/compaction 相关实现）；
- TTL 遗忘曲线默认值与 README 声明一致（3-30 天、`-1` 永久）；
- 票据召回零幻觉在 evals/ 有真实测试守护；
- RelationStore 因果链写入路径与 MemoryPlugin 的关系。

## 风险与回退

抽查若发现文档与代码漂移，如实记录（这正是评审价值），不修改代码。报告过长时优先保六维深度，压缩文章摘录。
