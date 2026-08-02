# rolling-summary-anchor — 实现任务

## 1. 滚动摘要消息豁免 L3（D1 核心修复）

- [x] 1.1 新增 `splitRollingSummaryMessage`（类比 `SplitSystemMessage`）：识别领先 `context_compress` 消息（`MessageEventType == TypeContextCompress`）并摘出
- [x] 1.2 `compressSkeleton`：SplitSystemMessage 后调用 splitRollingSummaryMessage 摘出滚动摘要（不进 SegmentMessages/定级），压缩后无条件回填到 result（系统消息后）
- [x] 1.3 测试：滚动摘要 + 8 回合（段0 达 L3）→ `result.Messages` 仍含 context_compress（系统消息后）且段0 不含它；`RetainedRefs` 仍重建摘要 ref
- [x] 1.4 fail-before 验证：改动前同输入摘要消息被 L3 丢出 Messages

## 2. 定级改指数衰减（D2 缓存复用）

- [x] 2.1 `deterministicLevel`：线性 `{k, 2k, 3k}` 改指数 `{k, 2k, 4k}`（边界 k×2^level，底数固定 2）
- [x] 2.2 测试：keepRecent=2 时 age=3→L1、age=5→L2、age=7→L2、age=8→L3；fail-before（线性下 age=6→L3）

## 3. 配置参数公式化（D3）

- [x] 3.1 `card_max_chars` 未设置时默认 `M / 20`；`compact_keys_listed` 未设置时默认 `card_max_chars / 200`（显式设置优先，向后兼容）
- [x] 3.2 测试：M=128000 未设置 → card_max_chars=6400、compact_keys_listed=32；显式设置 6000 → 用 6000

## 4. 收尾

- [x] 4.1 全量验证：`go build ./...`、`go vet ./...`、`gofmt` 干净；`go test -short ./agent/compress/...` 及全仓库相关包绿；关键测试 fail-before/pass-after
- [x] 4.2 `openspec validate rolling-summary-anchor --strict` 通过
- [x] 4.3 上线观察点记入 design：K≥7 的 BeforeLLM [1] 持续为 context_compress；SmartCompress 出现 L2 龄4-7、L3 龄≥8
