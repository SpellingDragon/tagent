## Context

tagent 的上下文压缩系统当前由三个组件构成：

1. **ContextCompressor** (`context_compressor.go`, 387 行)：BeforeModel 入口，从 Projection 解析 refs → 检查 token 预算 → 超限时调用 SmartCompressor
2. **SmartCompressor** (`smart_compress.go`, 1383 行)：四级压缩引擎（L0 保留 / L1 选择性 / L2 摘要 / L3 归档）
3. **支撑模块**：EventValuator (`event_value.go`, 396 行) + ChunkSplitter (`chunk_splitter.go`, 354 行)

总计 **2520 行**用于"决定哪些消息保留、哪些压缩"。其中 EventValuator 调用 LLM 评估每段价值（超时 5 分钟），ChunkSplitter 对大消息做语义分块再逐块摘要。这两个模块的复杂性与收益不成比例。

事件管线方面，`runEventLoop` 当前是纯串行的 Pull → RunFlow → Pull 循环，没有"空闲时主动整理投影"的能力。压缩只在 BeforeModel 时被动触发。

## Goals / Non-Goals

**Goals:**

- 压缩决策瞬时、确定、可测试（无 LLM 调用参与分级决策）
- 压缩代码总量从 ~2520 行降至 ~800 行（含新增 organizer）
- 引入空闲期主动整理，使 BeforeModel 紧急压缩极少触发
- 保留"信息不丢"原则：L3 归档到 MemoryStore，任何被压缩的信息可通过 recall 工具召回
- 保留可观测性：结构化 JSON 日志记录压缩和整理指标
- 代码清晰到可外包实现：每个函数职责单一、输入输出明确

**Non-Goals:**

- 不修改 MemoryStore 接口或存储后端
- 不修改 EventBus / Projection 的核心数据结构
- 不修改框架 Runner 的调用方式
- 不实现"中断当前 RunFlow"能力（用户中断仍通过 TryPull 在下次 BeforeModel 时注入）
- 不涉及记忆层 compaction/lifecycle/tombstone（那是独立的长期基建）

## Decisions

### D1: 确定性压缩分级替代 LLM 评估

```go
// deterministicLevel 根据段的年龄和内容特征确定压缩级别。
// 输入完全确定，输出完全确定，无副作用，无网络调用。
//
// 参数:
//   seg:        任务段（含 Messages、HasUserInput、IsToolOnly 等特征）
//   segIdx:     该段在所有段中的索引（0 = 最老）
//   totalSegs:  总段数
//   keepRecent: 保留最近 N 段不压缩（配置项，默认 2）
//
// 返回:
//   0 = L0 保留原样
//   1 = L1 保留用户消息 + 关键工具结果，摘要其余
//   2 = L2 保留用户消息，摘要全部执行过程
//   3 = L3 全段归档到 MemoryStore + 内联摘要替代
func deterministicLevel(seg *TaskSegment, segIdx, totalSegs, keepRecent int) int {
    age := totalSegs - 1 - segIdx // 0 = 最新，越大越老

    // 最近 N 段永不压缩
    if age < keepRecent {
        return 0
    }

    // 含用户原始输入的段: 保留用户消息，选择性摘要执行
    if seg.HasUserInput {
        if age < keepRecent*2 {
            return 1 // 较近：选择性保留
        }
        return 2 // 较远：只保留用户消息
    }

    // 纯工具执行段（无用户消息）: 更积极压缩
    if age >= keepRecent*3 {
        return 3 // 很老：全段归档
    }
    return 2 // 中等：摘要替代
}
```

**理由**：事件类型 + 年龄是稳定的信号。LLM 评估不稳定（超时、幻觉、成本高），且其决策与"类型 + 年龄"的简单规则高度相关。确定性规则可在单元测试中完全覆盖。

**放弃方案**：保留 LLM 评估作为可选增强。放弃原因——增加了配置复杂性和故障模式，且 5 分钟超时在生产中频繁触发降级，实际效果等同于不用。

### D2: 移除 ChunkSplitter，源头控制大消息

当前 ChunkSplitter 的作用是将大消息切分为多个 chunk，逐 chunk 生成摘要。但问题的根源是"工具返回了过大的内容"。

**新策略**：

1. ActionTool 返回结果时，超过 `maxToolResultChars`（默认 2000）的部分保存到文件，只在消息中放摘要 + 文件路径（已部分实现：`handleStateChange` 中的 `output > 2000` 分支）
2. SmartCompressor 不再需要处理"单条消息过大"的情况——所有消息进入压缩层时已经是合理大小
3. 如果仍有超大消息遗漏（防御性），直接截断到 `maxToolResultChars` + `[截断, 完整内容在 MemoryStore key=X]` 标记

**理由**：在源头控制输入大小，比在下游反复切分处理更简单可靠。

### D3: SmartCompressor 简化为三阶段管线

```
简化后的 SmartCompressor.Compress 流程:

输入: []model.Message
输出: []model.Message (压缩后)

阶段 1 - 分段 (TaskSegmenter, 保留):
  按 user message 边界切分为 []TaskSegment
  
阶段 2 - 分级 (deterministicLevel, 新):
  对每段计算 L0/L1/L2/L3 级别
  
阶段 3 - 执行 (保留核心, 精简):
  L0: 原样保留
  L1: 保留 userMsgs + keyMsgs，对 nonKeyMsgs 调 LLM 摘要（失败则 firstStageCompress）
  L2: 保留 userMsgs，对 execMsgs 调 LLM 摘要（失败则 firstStageCompress）
  L3: archiveSegment 到 MemoryStore，替换为内联 [历史摘要] 消息

删除的阶段:
  - EventValuator 评估阶段 (整个 event_value.go)
  - ChunkSplitter 分块阶段 (整个 chunk_splitter.go)
  - archiveCache 去重缓存 (简化为每次独立归档)
```

### D4: 空闲期主动整理 (ProjectionOrganizer)

```go
// ProjectionOrganizer 在事件循环空闲期主动整理投影。
//
// 触发条件: runEventLoop 中连续 N 次 Pull 返回空 batch (无新事件)
// 整理内容: 对年龄 > organizeAge 的 EventReference，如果 EventSummary
//           仍是原始内容（较长），则调 LLM 生成精炼摘要并更新 ref
// 效果: 下次 BeforeModel 时 ContextCompressor.Compress 从 Projection
//       解析到的消息更紧凑，减少紧急压缩的触发概率
//
// 关键约束:
//   - 每轮最多整理 batchSize 个 refs（默认 5），避免长时间占用
//   - 如果 Pull 到新事件，立即停止整理，优先处理用户输入
//   - 整理不修改 MemoryStore（原始内容永久保留），只更新 Projection ref 的 EventSummary
type ProjectionOrganizer struct {
    projection   *SessionProjection
    memStore     memory.MemoryStore
    summaryModel model.Model
    organizeAge  int // 只整理年龄超过此值的 refs（默认: keepRecent * 2）
    batchSize    int // 每轮最多整理的 refs 数（默认: 5）
}

// OrganizeOnce 执行一轮整理，返回整理的 ref 数量。
// 调用者（runEventLoop）在确认空闲后调用此方法。
func (o *ProjectionOrganizer) OrganizeOnce(ctx context.Context) int
```

**在 runEventLoop 中的集成点**：

```go
func (ta *TagentAgent) runEventLoop(ctx context.Context, bus *EventBus, cm *ContextManager) {
    idleCount := 0
    
    for {
        events, err := bus.Pull(ctx)
        if err != nil { ... }
        
        if len(events) == 0 {
            idleCount++
            if idleCount >= 2 && ta.organizer != nil {
                ta.organizer.OrganizeOnce(ctx)
                idleCount = 0 // 重置，避免连续整理
            }
            continue
        }
        
        idleCount = 0 // 有事件到达，重置空闲计数
        // ... 正常 BuildInvocation + RunFlow 流程 ...
    }
}
```

**注意**：当前 `bus.Pull` 是阻塞的（等待第一个事件才返回）。要支持空闲检测，需要修改为带超时的 Pull：

```go
// PullWithTimeout 等待最多 timeout 时间。
// 如果超时前有事件到达，正常返回 batch。
// 如果超时，返回空 batch + nil error（表示空闲）。
func (b *EventBus) PullWithTimeout(ctx context.Context, timeout time.Duration) ([]*AgentEvent, error)
```

### D5: firstStageCompress 作为统一降级策略

当 LLM 摘要生成失败时（网络错误、超时、空响应），统一降级到 `firstStageCompress`：

```go
// firstStageCompress 是无 LLM 依赖的确定性降级压缩。
// 保留 user/assistant 文本消息，丢弃 tool 消息（工具执行细节）。
// 这确保即使 LLM 不可用，压缩仍能工作，只是信息保留粒度粗一些。
func (sc *SmartCompressor) firstStageCompress(msgs []model.Message) []model.Message
```

这个函数已存在，保持不变。它是整个压缩系统的"兜底"——无 LLM 时仍可确定性压缩。

## Risks / Trade-offs

**[R1] 确定性规则可能过于粗糙**

LLM 评估理论上能识别"虽然很老但极其重要"的段。确定性规则只看类型和年龄，可能把重要段压缩过度。

缓解：
- L3 归档保证信息不丢（MemoryStore 永久保留，recall 可召回）
- `keepRecent` 默认 2 可调大（如 3-4）减少误压
- 未来可在空闲整理阶段增加"重要性标记"逻辑（不在紧急压缩中做）

**[R2] 空闲整理依赖 SummaryModel 可用性**

如果 LLM 不可用（限流、宕机），空闲整理无法生成摘要。

缓解：
- 整理失败不影响正常运行——只是 Projection 中的 EventSummary 保持较长，下次 Compress 时按正常路径压缩
- 整理不是必须的，它只是优化——使紧急压缩"很少触发"

**[R3] PullWithTimeout 改变了事件循环的行为语义**

当前 Pull 是"一定有事件才返回"。改为 PullWithTimeout 后，循环多了一个"超时空转"分支。

缓解：
- 超时设为 30s（与冥想检查间隔匹配），不会造成 CPU 忙等
- 空转时只执行轻量整理（最多 5 个 refs），不影响新事件的响应延迟

**[R4] 删除 ValueFloors 配置是 BREAKING CHANGE**

已部署的 wechat-bot tagent.yaml 中有 `value_floors` 和 `valuation_timeout_ms` 配置。

缓解：
- tagent 未公开发布，用户仅限内部
- 迁移成本 = 删除 YAML 中的 2 行配置
- 提供明确的迁移说明
