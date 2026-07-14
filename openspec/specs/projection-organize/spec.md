## 能力: projection-organize

空闲期主动投影整理机制，在 runEventLoop 间隙用 LLM 为老旧 refs 生成精炼摘要。

## 需求

### 核心行为

当 Agent 空闲（EventBus 无新事件）时，主动扫描 Projection 中年龄较大的 EventReference，为其生成精炼的 EventSummary。这使得后续 ContextCompressor 解析 Projection 时产出更紧凑的消息，减少紧急压缩的触发频率。

### 触发条件

- `runEventLoop` 中 `PullWithTimeout` 连续返回空 batch 达到 `idleThreshold` 次（默认 2）
- 每轮整理后重置空闲计数，避免连续整理
- 如果 Pull 到新事件，立即停止整理，优先处理用户输入

### 整理范围

- 只处理 `age > organizeAge` 的 refs（默认: `keepRecent * 2`，即第 5 个 ref 起）
- 每轮最多处理 `batchSize` 个 refs（默认 5）
- 跳过已整理过的 refs（EventSummary 长度 < 200 字符视为已精炼）
- 跳过 `context_compress` 类型的 refs（这些是压缩生成的摘要，不需要再整理）

### 整理操作

对每个待整理的 ref：
1. 从 MemoryStore 获取 FullEvent 完整内容
2. 调 LLM（summaryModel）生成 ≤150 字符的精炼摘要
3. 更新 Projection 中该 ref 的 EventSummary（原子操作）
4. **不修改 MemoryStore**（完整内容永久保留）

### 接口

```go
type ProjectionOrganizer struct {
    projection   *SessionProjection
    memStore     memory.MemoryStore
    summaryModel model.Model
    organizeAge  int // 只整理年龄超过此值的 refs
    batchSize    int // 每轮最多整理的 refs 数
}

// OrganizeOnce 执行一轮整理。
// 返回本轮实际整理的 ref 数量。
// ctx 取消时立即返回（响应新事件到达）。
func (o *ProjectionOrganizer) OrganizeOnce(ctx context.Context) int
```

### EventBus 扩展

```go
// PullWithTimeout 等待最多 timeout 时间。
// 超时返回空 batch + nil error（表示空闲）。
// 有事件返回正常 batch。
// ctx 取消返回 nil + ctx.Err()。
func (b *EventBus) PullWithTimeout(ctx context.Context, timeout time.Duration) ([]*AgentEvent, error)
```

### 约束

- 整理不得阻塞新事件的处理——新事件优先
- 整理不修改 MemoryStore（只改 Projection ref 的 summary 字段）
- LLM 摘要生成失败时跳过该 ref（不报错，下轮重试）
- 整理操作的总耗时不超过 30 秒（超时则停止本轮）
- summaryModel 为 nil 时跳过整理（不报错）

### 可观测性

每轮整理完成后输出结构化日志：
```
[OrganizeProjection] organized=3 skipped=2 failed=0 duration=1200ms
```

### 场景

#### 场景: 正常空闲整理

- **GIVEN** EventBus 持续 60s 无新事件，Projection 有 20 个 refs（其中 15 个 age > organizeAge）
- **WHEN** PullWithTimeout 超时 2 次
- **THEN** OrganizeOnce 被调用，处理前 5 个最老的 refs，生成精炼摘要更新 EventSummary

#### 场景: 新事件打断整理

- **GIVEN** OrganizeOnce 正在处理第 3 个 ref
- **WHEN** ctx 被取消（PullWithTimeout 检测到新事件）
- **THEN** OrganizeOnce 立即返回（已处理 2 个），runEventLoop 开始处理新事件

#### 场景: LLM 不可用时优雅降级

- **GIVEN** summaryModel 调用失败（网络超时）
- **WHEN** OrganizeOnce 尝试整理某个 ref
- **THEN** 跳过该 ref（不修改其 EventSummary），继续处理下一个

#### 场景: summaryModel 未配置

- **GIVEN** TagentConfig 未设置 summaryModel (nil)
- **WHEN** 空闲检测触发
- **THEN** 跳过整理，不报错，不创建 ProjectionOrganizer
