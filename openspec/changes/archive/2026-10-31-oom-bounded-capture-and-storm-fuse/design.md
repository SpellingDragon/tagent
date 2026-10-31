# 设计

## 总纲

**捕获有界、风暴有闸、增长可观测。** 三个嫌疑（S1 常驻深拷贝、S2 链副本、S3 退化风暴）按「先设界、再收窄、后熔断」处理；S4（堆棘轮）以观测面钉死，不做无证据的预优化。

## D1 capture 设界策略（S1 主修）

| 方案 | 说明 | 裁定 |
|---|---|---|
| 环形缓冲（定长 N decision） | 简单；字节量随会话长度漂移（67K 会话一个快照 250KB × N） | 弃——字节无界 |
| **字节预算 LRU + 落盘换出** | 常驻集合按字节预算（默认 64MB）淘汰最旧；被淘汰快照经 `MarshalSnapshot`（capture.go:516 已有）落 attempts 存储侧，seal/导出按需读回 | **取** |
| 纯落盘（零常驻） | 每次 Snapshot 直写盘；IO 放大、attempts 追加写放大 | 弃 |

- 界限作用面：`CaptureAdapter.fixed`+`snapshots`（capture_wiring.go:75,166）；`scoped`/`order` 读侧同步感知换出（未命中→按需读回）。
- 预算可配（`capture_budget_mb`，0=缺省 64MB；纯落盘形态已在上表裁定为弃，任何配置值不可达）。
- seal 后整集释放（进程一生只到 seal——现状即如此，明确化）。

## D2 深拷贝收窄（S1 辅修）

快照的现存消费方是训练导出（需要完整请求视图）与投影策略。裁定：**快照结构不变、拷贝时机不变，但换出后内存只剩索引**——即 D1 的 LRU 换出即等效收窄；不另做字段级裁剪（避免破坏导出契约）。若 D1 落地后单快照峰值（一个 134K-token 请求 ≈500KB）仍构成瞬时压力，属 S4 观测面裁决，不在本变更预改。

## D3 单响应工具调用数上限（S3 主闸）

- 上限 K：默认 **64**（victim 风暴 6,277 的 1%；正常会话单响应调用峰值远低于此——attempts.jsonl 实证面在 M1 一并取样）。
- 超限行为：**整批挂审批，不截断**——整个响应登记为恰一条人审请求（摘要只列 `{tool, args_digest}`，call ID 单列在请求说明里、不进摘要）；等待发生在原本要派发它的那次派发内，不占模型席位。批准=该响应全部调用按发出顺序派发、每个各得一份受理回执；拒绝/超期/取消=零执行、整批回一份拒绝回执；无审批簿=整批 fail-closed 全拒。全程不重试、不降级为 user message——堵住 sanitize 回链放大路径。
- 配置：per-agent 声明 `max_calls_per_response`（0=不设限，向后兼容缺省关、生产文档推荐开 64）；原稿写的 `governance.*` 全局键按 ORCHESTRATION 台账的偏差记录修正为 per-agent 声明（与 `max_tool_iterations` 同模式）。
- 用户裁定记录（B→B'）：截断形态已交付后由用户裁定改为审批形态——人审要看的是被拦下的**整批**，而不是让首 K 个在无人知情时先跑；据此 B 单重做为 B'，本段语义以 B' 为准。

## D4 降级消息限长（S3 副闸，fork 面）

fork `internal/toolcall/sanitize.go` 降级 user message 的 payload 段上限 **4KB**（超出截断 + `[truncated N bytes]` 标记）。fork 提交沿用 `-tagent.N` 序列。

## D5 长上下文防线（S3 第三道）

prompt_tokens > **50K** 且该响应仍发多个调用时：**整批挂审批**——与 D3 同一代码点（`rulingOf`/`parkedBatchOf`），不让长上下文下的并行调用在无人知情时一次铺开；只发一个调用的响应照旧受理。批准/拒绝/超期/取消/无簿五态与 D3 完全一致（批准=整批按序执行、拒绝类=零执行、无簿=整批全拒）。原稿的「提前触发 fold + 并行降串行」不实施：折叠节奏归压缩域自有门（`compress_threshold`），本防线只在派发前拦下整批。阈值 per-agent 声明 `long_context_tokens`，0=关。

## D6 facts 淘汰（S2）

- fold 消费侧：`ReplayProjection.originals/seen/consumed` 在 fold 重写时同步 delete 被折叠 key（fold.go:120 面）。
- `Run.facts`：按 session 热窗口（每会话保留最近 2,048 条 + 全部未闭合段事实；更早的仅 WAL/磁盘）——重启恢复路径不变（restoreChains 读 tier）。

## D7 pprof 观测面（S4 钉死）

host face（127.0.0.1:8089）加 `/debug/pprof/{heap,goroutine,allocs}`，仅 localhost 绑定；`TAGENT_GCTRACE=1` 环境开关透传 GODEBUG。验收含一次 victim 复现形态的 heap 采样证据。

## D8 三级验收

- 孙任务级：每任务验证命令 exit 0（见 tasks.md 各行）。
- 域级：bounded-trajectory-capture / storm-fuse / memory-observation 三 spec 的 scenarios 全绿。
- 变更级 DoD：M1 症状锚红转绿（完整回滚本变更必红、修复后必绿，两次证据入 run notes）+ 长跑回归（-race 下 2h 模拟会话 RSS 稳态有界，pprof 佐证）+ M7 对比锚双形态封死。

## 风暴传播双形态（M7 对比锚的判据）

| 形态 | 旧基线（c111d7a2） | 新基线（本变更后） |
|---|---|---|
| A：notice/输入逐个开 turn → 模型多次响应 | 存在（用户观察） | 消费循环已封（已归档变更） |
| B：单响应海量调用 → 请求巨型化 → 组装期 OOM | 存在（victim 实证） | D3/D4/D5 封死 |
| 注入判据 | — | 同一注入（模拟模型单响应 N>K 调用）：无裁决时为 0 受理 + 恰 1 条审批请求；批准后受理数 =N（恰一次）、链增量 = N 条结果事实 + 1 条决策事实（无 N×2 降级回灌）；输入风暴下模型响应次数不随输入量线性增 |
