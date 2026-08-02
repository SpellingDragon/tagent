# rolling-summary-anchor

## Why

生产实证（wechat-bot 2026-08-02 日志）：滚动摘要（`context_compress` ref）在完整回合数 K≥7 时**从模型上下文消失**。根因是结构性的：滚动摘要 ref 被 prepend 到投影最前 → 渲染成第一条消息 → 落进**段0**（与第一回合同段）→ 段0 段龄最高、最先升 L3 → `applySegmentLevel` L3 返回 nil，**滚动摘要消息连同段0 一起被丢出模型上下文**。

```mermaid
graph LR
    R[滚动摘要消息] --> S0[段0 最老, 段龄最高]
    S0 --> L3[升 L3 → 整段 nil]
    L3 --> M[模型看不到摘要<br/>远期历史彻底失忆]
```

日志铁证：02:05:15 段0 非 L3 → 摘要在；02:05:37 首次 seg=7、L3=1 → 摘要消失；此后段数恒为 7、摘要恒不可见。**ref 其实一直在投影里（buildRetainedRefs 每轮重建），但消息被 L3 丢出模型上下文**——"ref 活着，消息死了"。

这意味着：压缩激活后（compress-digest-reconnect 修复了触发器饿死），长会话里模型对远期历史**彻底失忆**——看不到卡片、不知道历史存在、不会去 recall，只能幻觉或"不记得"。而最讽刺的是：**摘要在短会话（K=3–6）可见但那时不太需要，长会话（K≥7）最需要时反而消失**。

另外两个伴随问题：

- **配置参数过多**：keep_recent_tasks / compress_threshold / max_tokens / recent_full_count / card_max_chars / compact_keys_listed / archive_cache_cap … 多个独立旋钮，部分已有派生关系但未系统化。
- **前缀缓存不友好**：滚动摘要在上下文**最开头**，而它每轮压缩都变（计数累加、卡片重整），导致整个 LLM 前缀缓存被频繁失效。线性定级（L1/L2/L3 各跨 keepRecent 一段）使段每 2 轮就换一次级别、内容重渲染，进一步加剧前缀抖动。

## What Changes

1. **保护滚动摘要不被 L3 吞（核心）**：`compressSkeleton` 把 `context_compress` 滚动摘要消息像 `SplitSystemMessage` 一样从段结构中摘出，压缩后无条件回填到 result 最前——**滚动摘要永远可见**（类比系统消息的常驻地位）。段0 的其余部分（第一回合）仍正常定级。
2. **L1/L2/L3 定级改指数衰减**：`deterministicLevel` 的段龄阈值从线性 `{k, 2k, 3k}` 改为**指数 `{k, 2k, 4k}`**（边界 = keepRecent×2^level）。段在每个级别驻留更久（L2 段跨度翻倍），被折叠进滚动摘要的频率降低 → 滚动摘要与前缀内容更新更慢 → **前缀缓存复用率提升**。
3. **配置参数公式化**：以 `max_tokens`（预算）与 `keep_recent_tasks`（保留）为**主变量**，其余尽量由公式派生：`threshold = compress_threshold × max_tokens`、`recent_full_count = keepRecent × DefaultRefsPerTurn`、定级边界 = keepRecent 的指数函数、`card_max_chars`/`compact_keys_listed` 由预算/保留经简单公式推出。减少独立旋钮。

不改：卡片生成的纯工程本质（extractCardLine 零 LLM）、condenseCardLines 的可选 LLM 浓缩、素材律（归档缓存）、memory_turn / recall 召回语义。

## Capabilities

### Modified Capabilities

- `task-skeleton-compression`：滚动摘要消息豁免 L3（常驻可见）；定级从线性改指数 `{k,2k,4k}`；配置参数公式化（主变量 max_tokens / keep_recent_tasks 派生其余）。

## Impact

- **行为**：K≥7 的长会话里，模型持续看到滚动摘要（有界卡片 + `[Compacted N]` + recall 指引），远期历史感知不再失忆；recall（memory_recall / memory_turn / recall_query）有了稳定的召回入口。
- **性能**：指数定级降低段折叠频率 → 滚动摘要与前缀更新更慢 → LLM 前缀缓存复用率提升；保护滚动摘要不引入 LLM 调用（纯结构改动）。
- **配置**：独立旋钮减少，主变量派生其余。
- **风险面**：滚动摘要常驻占用固定 token（有界，card_max_chars=6000 封顶）；指数定级使段在 timeline 驻留更久（retained 段略增，预算内）；需回归测试覆盖"K≥7 摘要仍可见"与各级别边界。
