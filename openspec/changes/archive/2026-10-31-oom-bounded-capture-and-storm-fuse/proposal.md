# 提案：OOM 根治——轨迹捕获设界与风暴熔断

## Why

wechat-bot 长跑 31 小时 RSS 122M→2.84G，内核 OOM 击杀（第 8 次）。远端只读取证 + 本地代码逐条实证，根因结构：

- **S1（主增长源，高置信）**：RL 轨迹捕获对每个 decision 深拷贝整请求（`SnapshotRequest`→`deepCopyMessages`）且 `fixed`/`snapshots` 只增不删、常驻进程一生（features/rl/capture.go:243-281、capture_wiring.go:75,166-167）。67K-token 会话每 decision 拷贝约 250KB，随会话变长**平方级加速**；31h 估算 100-300MB，无上界。
- **S3（引爆点，高置信）**：长上下文（67K）下模型退化——单响应发出 6,277 个无效工具调用（幻觉资源名清理、字母序递增、JSON 截断），fork `sanitize.go` 把每个降级为 user message 带全文 payload 回链（无长度上限）；一次性注入 ~6,277 个失败 tool-result 进事实链，下一请求巨型化，进程死于组装期（20:32 风暴 → 21:22 OOM，最后一轮无 model response）。
- **S2（结构性无界，现阶段影响小）**：事件链多层常驻副本——`Run.facts` 全仓无 delete、replay `originals/seen` fold 时不清（replay_budget.go:113、fold.go:120）。
- **S4（GB 级残差候选，中置信）**：大 turn 的 assembly+多次 JSON marshal+深拷贝把 Go 堆 arena 棘轮抬升（MADV_FREE 延迟归还），叠加风暴轮瞬时峰值；需 pprof 钉死。

### 与消费循环收敛（已归档变更）的关联（用户假设核查）

用户观察到「task 执行异常时触发 turn start 风暴、模型多次响应」——该形态属**旧驱动模型**（victim 基线 c111d7a2 不含消费循环）：每个 notice/输入单独开 turn、单独模型调用。消费循环（一次拉取共同消费）已治该形态。但本次 OOM 的实际引爆是**另一形态**——单响应海量调用（无论新旧驱动模型均无熔断）——两形态共享 S3 放大器。故本变更的 S3 熔断同时封死两条风暴传播路径，消费循环与本次变更互补而非重复。

## What Changes

1. **M1 症状锚（红）**：跑 M 个 decision 后 TrajectoryCapture 常驻快照集合有界——当前实现必红。
2. **M2 capture 设界**：`fixed`/`snapshots` 改环形/LRU（按 decision 数或字节预算），`CaptureAdapter.scoped/order` 同步清理；seal 后释放；`MarshalSnapshot` 已有，可「落盘+按需读回」。
3. **M3 深拷贝收窄**：snapshot 只留投影策略所需字段/摘要或 fact key 引用，不深拷贝全量 `req.Messages`。
4. **M4 风暴熔断（S3）**：单响应 tool-call 数上限；sanitize 降级消息长度上限（fork 面）；prompt 超阈值的长上下文防线（强制 fold/收窄或拒绝并行工具调用）。
5. **M5 事实清理（S2）**：fold 同步清理 `originals/seen/consumed` 被折叠条目；`Run.facts` 按 session/age 淘汰（WAL 可重建，内存只留热窗口）。
6. **M6 运行时观测面（S4 验证）**：`/debug/pprof`（heap+goroutine）端点 + `GODEBUG=gctrace` 开关；作为 S4 钉死与长期回归门。
7. **M7 风暴传播对比锚**：注入式复现（旧基线 vs 新基线同注入）——单响应调用量、模型响应次数、链增量三面断言；验证「消费循环+S3 熔断」封死双形态。
8. **M8 验收**：长跑回归（bounded 内存下的持续运转）+ 文档。

## Capabilities

- `bounded-trajectory-capture`（M1/M2/M3：捕获有界）
- `storm-fuse`（M4/M7：风暴熔断与传播防线）
- `memory-observation`（M6：pprof/gctrace 观测面）
- S2 清理并入 `bounded-trajectory-capture` 的恢复/淘汰面

## 影响范围

- 本仓：features/rl/capture*.go、memory/replay/、assembly/facts.go、host 观测面
- fork（trpc-agent-go v1.11.2-tagent.2）：internal/toolcall/sanitize.go（降级限长）
- 不改：消费循环/段语义（已归档变更的成果，仅以其为基线）
