# projection-organize Specification

## Purpose

空闲期主动投影整理机制：在 runEventLoop 间隙用 LLM 为老旧 EventReference 生成精炼摘要，使后续 ContextCompressor 产出更紧凑的消息、降低紧急压缩触发频率。

## Requirements

### Requirement: 空闲期主动投影整理

当 Agent 空闲（EventBus 无新事件）时，系统 SHALL 主动扫描 Projection 中年龄较大的 EventReference 并为其生成精炼的 EventSummary，且 SHALL NOT 修改 MemoryStore（完整内容永久保留）。整理 SHALL NOT 阻塞新事件处理——新事件到达时 MUST 立即让位。

触发条件：

- `runEventLoop` 中 `PullWithTimeout` 连续返回空 batch 达到 `idleThreshold` 次（默认 2）
- 每轮整理后重置空闲计数，避免连续整理
- 若 Pull 到新事件，立即停止整理，优先处理用户输入

整理范围与操作：

- 只处理 `age > organizeAge` 的 refs（默认 `keepRecent * 2`）
- 每轮最多处理 `batchSize` 个 refs（默认 5）
- 跳过已整理过的 refs（EventSummary < 200 字符视为已精炼）与 `context_compress` 类型 refs
- 对每个待整理 ref：从 MemoryStore 取 FullEvent → 调 summaryModel 生成 ≤150 字符摘要 → 原子更新 Projection 中该 ref 的 EventSummary（不改 MemoryStore）

接口：

```go
type ProjectionOrganizer struct {
    projection   *SessionProjection
    memStore     memory.MemoryStore
    summaryModel model.Model
    organizeAge  int
    batchSize    int
}

// OrganizeOnce 执行一轮整理，返回本轮实际整理的 ref 数量；ctx 取消时立即返回。
func (o *ProjectionOrganizer) OrganizeOnce(ctx context.Context) int

// PullWithTimeout 等待最多 timeout；超时返回空 batch + nil error（空闲）。
func (b *EventBus) PullWithTimeout(ctx context.Context, timeout time.Duration) ([]*AgentEvent, error)
```

约束：LLM 摘要失败时 SHALL 跳过该 ref（不报错，下轮重试）；单轮总耗时 SHALL 不超过 30 秒；`summaryModel` 为 nil 时 SHALL 跳过整理（不报错、不创建 organizer）。

#### Scenario: 正常空闲整理

- **GIVEN** EventBus 持续 60s 无新事件，Projection 有 20 个 refs（其中 15 个 age > organizeAge）
- **WHEN** PullWithTimeout 超时 2 次
- **THEN** OrganizeOnce 被调用，处理前 5 个最老的 refs，生成精炼摘要更新 EventSummary

#### Scenario: 新事件打断整理

- **GIVEN** OrganizeOnce 正在处理第 3 个 ref
- **WHEN** ctx 被取消（PullWithTimeout 检测到新事件）
- **THEN** OrganizeOnce 立即返回（已处理 2 个），runEventLoop 开始处理新事件

#### Scenario: LLM 不可用时优雅降级

- **GIVEN** summaryModel 调用失败（网络超时）
- **WHEN** OrganizeOnce 尝试整理某个 ref
- **THEN** 跳过该 ref（不修改其 EventSummary），继续处理下一个

#### Scenario: summaryModel 未配置

- **GIVEN** TagentConfig 未设置 summaryModel (nil)
- **WHEN** 空闲检测触发
- **THEN** 跳过整理，不报错，不创建 ProjectionOrganizer
