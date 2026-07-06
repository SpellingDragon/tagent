## Context

tagent 的 persistent-event-loop 已完成 OTLP 可观测性增强。每个 batch 创建 `tagent.loop.batch` span，记录输入事件内容、工具调用、token 使用量、TTFT、最终响应（think + response）。框架内部的 TraceChat/TraceToolCall 自动创建子 span，形成完整 trace 层级。

但 OTLP span 属性有两个限制：
1. **截断**：span 属性中 content 截断到 1000 字符，日志中截断到 200 字符——面向人类阅读，不适合 RL 训练数据
2. **非结构化**：span 属性是 key-value 对，不是 RL trainer 期望的结构化 trajectory 格式

RL 训练需要的是**全保真、结构化**的 trajectory 数据。AReaL 的 `InteractionWithTokenLogpReward` 结构展示了 RL trainer 的数据需求：input_tokens、output_tokens、output_logprobs、reward。其中 logprobs 由 AReaL proxy 在代理层捕获，tagent 不需要关心。tagent 需要提供的是：completion_id（`Response.ID`）→ reward 映射。

### AReaL 集成架构

```
┌──────────────────────────────────────────────────────────────┐
│ AReaL (Python)                                               │
│  ┌─────────────┐    ┌──────────────┐    ┌─────────────────┐  │
│  │ PPOTrainer  │←──│ RolloutCtrl  │←──│ OpenAI Proxy    │  │
│  │ (actor/crit)│    │ (orchestr.)  │    │ (logprob capt.) │  │
│  └─────────────┘    └──────┬───────┘    └────────┬────────┘  │
│                            │                      │           │
│                     ┌──────┴───────┐              │           │
│                     │ AgentAdapter │              │           │
│                     │ (Python)     │              │           │
│                     └──────┬───────┘              │           │
└────────────────────────────┼──────────────────────┼───────────┘
                             │ HTTP                 │ OpenAI API
                             ▼                      ▼
┌──────────────────────────────────────────────────────────────┐
│ tagent (Go)                                                  │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────────┐  │
│  │ HTTP API     │   │ Persistent   │   │ model.Model      │  │
│  │ /task        │──→│ Event Loop   │──→│ (→ AReaL proxy)  │  │
│  │ /trajectory  │   │ + Trajectory │   └──────────────────┘  │
│  │ /healthz     │   │   Collector  │                         │
│  └──────────────┘   │ + RewardFunc │                         │
│                     └──────────────┘                         │
└──────────────────────────────────────────────────────────────┘
```

1. AReaL 启动 proxy + PPO trainer
2. AReaL 的 AgentAdapter 通过 HTTP 向 tagent 发送任务
3. tagent 的 persistent loop 处理任务，LLM 请求通过 AReaL proxy（捕获 logprobs + completion_id）
4. tagent 的 TrajectoryCollector 采集全保真 trajectory（含 completion_id）
5. tagent 的 RewardFunc 计算 reward
6. AgentAdapter 通过 HTTP 获取 trajectory（含 completion_id → reward 映射）
7. AgentAdapter 返回 reward 给 AReaL

## Goals / Non-Goals

**Goals:**
- 全保真 trajectory 采集：每个 batch 生成一条结构化 Trajectory 记录
- completion_id 捕获：每条 LLM 响应的 `Response.ID` 记录到 trajectory（用于 AReaL reward 映射）
- 可插拔 reward 接口：内置 reward + HTTP callback 外部评估器
- AReaL bridge：Python adapter + tagent HTTP API，端到端可运行
- 向后兼容：未配置 RewardFunc 时 trajectory 仍采集但不计算 reward

**Non-Goals:**
- 不修改 trpc-agent-go 框架源码
- 不实现 PPO/GRPO 等训练算法（AReaL 负责）
- 不捕获 logprobs（AReaL proxy 在代理层捕获）
- 不实现分布式 trajectory 存储（单进程内存 + 文件导出足够）
- 不修改 persistent-event-loop 的 OTLP span 逻辑（trajectory 并行采集）

## Decisions

### D1: Trajectory 结构体 — 全保真，与 OTLP span 并行

```go
type Trajectory struct {
    ID            string           `json:"id"`             // batch ID (uuid)
    BatchIndex    int              `json:"batch_index"`    // batch 序号
    UserID        string           `json:"user_id"`
    SessionID     string           `json:"session_id"`
    StartTime     time.Time        `json:"start_time"`
    EndTime       time.Time        `json:"end_time"`
    Duration      time.Duration    `json:"duration"`

    // 输入
    InputMessages []InputMessage   `json:"input_messages"` // batch 中的原始消息

    // LLM 交互序列（每次 LLM 调用一条）
    Interactions  []Interaction    `json:"interactions"`

    // 最终结果
    FinalResponse string           `json:"final_response"`
    FinalReasoning string          `json:"final_reasoning"`
    HasFinal      bool             `json:"has_final"`

    // 统计
    ToolCallCount int              `json:"tool_call_count"`
    InputTokens   int              `json:"input_tokens"`
    OutputTokens  int              `json:"output_tokens"`
    TTFT          time.Duration    `json:"ttft"`

    // RL
    CompletionIDs []string         `json:"completion_ids"` // Response.ID 列表
    Reward        *float64         `json:"reward,omitempty"` // nil = 未计算
    Status        string           `json:"status"`          // completed/error/cancelled
}

type InputMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"` // 全保真，不截断
}

type Interaction struct {
    EventIndex     int           `json:"event_index"`     // 事件序号
    CompletionID   string        `json:"completion_id"`   // Response.ID (AReaL reward 映射)
    Model          string        `json:"model"`
    Type           string        `json:"type"`             // tool_call/tool_result/final/error
    Content        string        `json:"content"`          // LLM 输出内容
    Reasoning      string        `json:"reasoning"`        // thinking content
    ToolCalls      []ToolCallRec `json:"tool_calls,omitempty"`
    ToolResultID   string        `json:"tool_result_id,omitempty"`
    ToolResultLen  int           `json:"tool_result_len,omitempty"`
    PromptTokens   int           `json:"prompt_tokens"`
    CompletionTokens int         `json:"completion_tokens"`
    TTFT           time.Duration `json:"ttft"`
    ErrorType      string        `json:"error_type,omitempty"`
    ErrorMessage   string        `json:"error_message,omitempty"`
}

type ToolCallRec struct {
    ID     string `json:"id"`
    Name   string `json:"name"`
    Args   string `json:"args"` // 全保真
}
```

**理由**：与 OTLP span 属性相比，Trajectory 是全保真的（不截断 content/args），结构化的（嵌套 JSON 而非扁平 key-value），面向机器消费的。两者并行采集，不互相依赖。

**放弃的方案**：复用 OTLP span 属性作为 trajectory 数据源。放弃原因：span 属性已截断，且 OTel span 属性有长度限制（默认 2MB total），不适合存储全保真内容。

### D2: TrajectoryCollector — 在事件转发循环中同步采集

```go
type TrajectoryCollector struct {
    mu          sync.Mutex
    trajectory  *Trajectory
}

func NewTrajectoryCollector(batchIndex int, userID, sessionID string) *TrajectoryCollector

// 在 loop() 的事件转发循环中调用
func (c *TrajectoryCollector) RecordInput(msgs []model.Message)
func (c *TrajectoryCollector) RecordEvent(eventIndex int, evt *event.Event)
func (c *TrajectoryCollector) RecordError(err error)
func (c *TrajectoryCollector) Finalize(status string) *Trajectory
```

**集成点**：在 `loop()` 函数的 `for evt := range eventCh` 循环中，与现有的 OTLP span 属性设置和日志记录**并行**调用 `collector.RecordEvent()`。不改变现有逻辑，只新增采集。

### D3: TrajectoryStore — 内存 + JSONL 导出

```go
type TrajectoryStore struct {
    mu          sync.RWMutex
    trajectories map[string]*Trajectory // ID → Trajectory
    exportPath  string                 // 可选：JSONL 文件路径
}

func NewTrajectoryStore(exportPath string) *TrajectoryStore
func (s *TrajectoryStore) Add(t *Trajectory)
func (s *TrajectoryStore) Get(id string) (*Trajectory, bool)
func (s *TrajectoryStore) List() []*Trajectory
func (s *TrajectoryStore) ExportJSONL() error // 追加写入文件
```

**理由**：内存存储支持 HTTP API 实时查询；JSONL 导出支持离线分析和 AReaL 离线 reward 计算。不引入外部依赖（Redis/SQLite），保持 tagent 轻量。

### D4: RewardFunc 接口 — 可插拔，内置 + 外部

```go
// RewardFunc 计算 trajectory 的 reward。
// 返回的 reward 会被写入 Trajectory.Reward 字段。
type RewardFunc interface {
    Compute(trajectory *Trajectory) (float64, error)
}

// 内置：任务完成 reward（有 final response = 1.0，否则 0.0）
type TaskCompletionReward struct{}

// 内置：工具调用效率 reward（工具调用次数越少 reward 越高）
type ToolCallEfficiencyReward struct {
    MaxToolCalls int // 超过此次数 reward = 0
}

// 外部：HTTP callback reward（调用外部评估器服务）
type HTTPCallbackReward struct {
    Endpoint string // POST endpoint，body = Trajectory JSON
}
```

**配置**：
```go
type TagentConfig struct {
    // ... 现有字段 ...
    RewardFunc      RewardFunc      // 可选：reward 计算函数
    TrajectoryStore *TrajectoryStore // 可选：trajectory 存储（nil = 不存储）
}
```

**理由**：内置 reward 覆盖常见场景；HTTP callback 支持外部 LLM-as-judge 或规则评估器。RewardFunc 是 interface，用户可自定义实现。

### D5: HTTP API — 供 AReaL adapter 调用

```go
type HTTPAPI struct {
    agent *TagentAgent
    store *TrajectoryStore
}

// POST /task
// Body: {"messages": [...], "user_id": "...", "session_id": "..."}
// Response: {"trajectory_id": "..."} (batch 完成后 trajectory 可查)

// GET /trajectory/{id}
// Response: Trajectory JSON

// GET /trajectories
// Response: []Trajectory summary JSON

// GET /healthz
// Response: {"status": "ok", "loop_active": true}
```

**理由**：AReaL 是 Python，tagent 是 Go，HTTP 是最简单的跨语言桥接方式。tagent 作为长驻进程运行，adapter 通过 HTTP 交互。

### D6: AReaL Python Adapter

```python
class TagentARealAdapter:
    """AReaL agent workflow adapter for tagent."""

    def __init__(self, tagent_url: str, reward_fn=None):
        self.tagent_url = tagent_url  # e.g. "http://localhost:8080"
        self.reward_fn = reward_fn    # 可选：Python 侧 reward 函数

    async def run(self, data: dict, **extra_kwargs) -> float | dict[str, float]:
        # 1. 通过 HTTP API 提交任务到 tagent
        # 2. tagent 处理任务（LLM 请求走 AReaL proxy）
        # 3. 轮询 GET /trajectory/{id} 直到 status = completed
        # 4. 获取 trajectory（含 completion_ids）
        # 5. 计算 reward（Python 侧 reward_fn 或 tagent 侧已计算）
        # 6. 返回 reward（float 或 {completion_id: reward}）
```

**AReaL 配置示例**：
```yaml
# AReaL config
rollout:
  openai:
    mode: inline
    workflow: areal.tagent_adapter.TagentARealAdapter

# tagent 启动时配置
# LLM BaseURL → AReaL proxy URL
# RewardFunc → TaskCompletionReward 或 HTTPCallbackReward
```

**理由**：adapter 在 Python 进程内运行（AReaL 的 inline 模式），通过 HTTP 与 tagent Go 进程通信。tagent 的 LLM 请求通过 AReaL proxy，proxy 捕获 logprobs + completion_id。adapter 获取 tagent 的 trajectory（含 completion_id），计算 reward 后返回 AReaL。

### D7: completion_id 捕获 — Response.ID

框架的 `model.Response` 有 `ID string` 字段，对应 OpenAI API 的 `response.id`。当 tagent 的 LLM endpoint 指向 AReaL proxy 时，proxy 为每次 LLM 调用分配唯一的 completion_id。TrajectoryCollector 在 `RecordEvent()` 中捕获 `evt.Response.ID`，写入 `Interaction.CompletionID`。

batch 完成后，trajectory 的 `CompletionIDs` 列表包含所有 LLM 交互的 ID。adapter 返回 `{completion_id: reward}` 时，AReaL 将 reward 映射到对应的 `InteractionWithTokenLogpReward`。

## Risks / Trade-offs

**[R1] Trajectory 内存增长**
 缓解：TrajectoryStore 设置最大条数（默认 1000），超出时 FIFO 淘汰。JSONL 导出可选。生产环境建议配置 exportPath 定期导出后清理。

**[R2] HTTP API 增加 tagent 复杂度**
 缓解：HTTP API 是可选的（仅 AReaL bridge 模式启用）。使用标准库 `net/http`，不引入框架依赖。API 最小化（4 个端点）。

**[R3] Python adapter 轮询延迟**
 缓解：adapter 轮询间隔 100ms，典型 batch 处理 2-10s，轮询开销可忽略。未来可改为 SSE/WebSocket 推送，但初期轮询足够。

**[R4] Reward 计算时机 — 同步 vs 异步**
 选择：同步。batch 完成后立即调用 RewardFunc.Compute()。reward 计算通常快速（规则匹配或 HTTP callback < 1s）。如果 reward 计算耗时（如 LLM-as-judge），使用 HTTPCallbackReward 异步计算。

**[R5] 多 LLM 调用的 reward 分配**
 接受：一个 batch 可能包含多次 LLM 调用（tool call → LLM → tool call → LLM → final）。trajectory 记录所有交互的 completion_id。reward 可以是整体 reward（返回 float，AReaL 分配到最后一个 completion）或 per-completion reward（返回 dict[str, float]）。adapter 根据需求选择。

**[R6] tagent 的 LLM 请求可能不走 AReaL proxy**
 缓解：AReaL bridge 模式下，tagent 的 `openai.WithBaseURL()` 必须指向 AReaL proxy。这是配置要求，不是代码约束。adapter 文档中明确说明。

## Migration Plan

1. **Phase 1**: 新建 `agent/trajectory.go` + `agent/trajectory_test.go`。不修改现有文件。验证 trajectory 采集逻辑。
2. **Phase 2**: 新建 `agent/reward.go` + `agent/reward_test.go`。修改 `agent/tagent_agent.go` 集成 collector + reward。验证 loop 集成。
3. **Phase 3**: 新建 `agent/http_api.go`。新建 `areal/tagent_adapter.py` + `areal/README.md`。端到端验证。
4. **回滚策略**: Phase 1-2 不修改现有 API（TagentConfig 新增字段是可选的）。Phase 3 的 HTTP API 是独立文件，不启用不影响现有功能。

## Open Questions

1. **trajectory 是否需要持久化到磁盘？** 当前设计支持 JSONL 导出。是否需要 SQLite 存储？取决于 RL 训练的数据量。初期 JSONL 足够。
2. **adapter 是否需要支持 AReaL 的 subprocess 模式？** 当前设计仅支持 inline 模式（adapter 和 tagent 在不同进程，通过 HTTP 通信）。subprocess 模式需要 tagent 可被 Python 进程管理，增加复杂度。初期不支持。
3. **多 agent 场景下的 trajectory 隔离？** 当前设计每个 TagentAgent 实例有自己的 TrajectoryStore。如果 AReaL 并行运行多个 tagent 实例，每个实例独立。这是正确的——AReaL 的 rollout worker 是独立的。
