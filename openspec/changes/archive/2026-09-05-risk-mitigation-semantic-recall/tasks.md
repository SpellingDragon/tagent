## 任务清单

本变更分为 4 个阶段，按依赖顺序执行。每阶段独立可验证。

## 向量生成方案详述（Phase 1 前置阅读）

### 数据流

```
事件产生 → 异步队列 → EmbeddingWorker goroutine → 向量存储

MemoryPlugin.OnEvent (同步, ~1ms)
  ├─ StoreEvent(key, fullEvent)                    ← 事件立即可查
  └─ embeddingWorker.Enqueue(key, text)            ← 非阻塞 chan 投递

EmbeddingWorker (异步, ~200ms 后完成)
  ├─ 从 queue 取请求
  ├─ EmbeddingProvider.Embed(ctx, text)            ← HTTP POST /v1/embeddings
  └─ StoreEventWithEmbedding(key, event, vector)   ← 向量可被搜索
```

### 关键设计决策

1. **异步生成不阻塞事件循环**
   - OnEvent 只做 chan 投递，200ms 的 embedding 延迟不影响事件流转
   - "向量不可搜索窗口" ≈ 200ms（该事件仍可通过关键词/时间/EventKey 精确查询）

2. **选择性生成**：仅对 `external_input` 和 `agent_output` 生成 embedding
   - thinking_plan / action_command / context_compress 不生成（中间推理、结构化输出、压缩摘要，检索价值低）
   - 预估比例：10 个事件中约 3-4 个生成，40 次 API 调用/day（100 events/day 场景）

3. **Embedding 文本选择**
   ```go
   func textForEmbedding(event FullEvent) string {
       if event.Content != "" && len(event.Content) <= 8000 { return event.Content }
       if event.Content != "" { return event.Content[:8000] }  // 超长截断
       if event.EventSummary != "" { return event.EventSummary }  // 回退到摘要
       return ""  // 不生成
   }
   ```
   - 8000 字符对齐 embedding 模型输入上限
   - 超长内容截断对语义相似度影响小（主要语义在前几千字符）

4. **向量维度与模型选择**
   - 推荐：**ZhiPu embedding-3 (1024 维)**
     - 与现有 API provider 一致（复用 zhipu 账号）
     - 1024 维在 10 万级事件中足够区分语义
     - 单次延迟 ~80ms，成本 ¥0.5/千次
   - 内存占用估算：1024 维 × 4 bytes × 40,000 events ≈ 160MB（可接受）

5. **向量存储结构（InMemoryStore）**
   ```go
   type InMemoryStore struct {
       // ... 现有字段 ...
       embeddings    map[int64][]float32  // EventKey → embedding vector
       embeddingMu   sync.RWMutex         // 读写锁（搜索时读，写入时写）
       embeddingDims int                  // 首次存储时确定
   }
   ```

6. **持久化策略（localfile 后端）**
   - Embedding 以额外 KV 对存储：`{pid}:emb:{event_key} → JSON []float32`
   - 启动时懒加载（首次 SearchByEmbedding 调用时通过 prefix scan 加载到内存）
   - 崩溃恢复：embedding 丢失不影响事件本身，重启后新事件会重新生成

7. **性能预估（brute-force 余弦搜索）**
   - 10,000 事件 × 1024 维：~2ms
   - 100,000 事件 × 1024 维：~20ms
   - 超 100,000 时可切换 HNSW（当前架构不预留此空间——tagent 数年内不太可能达到此量级）

8. **背压保护（EmbeddingWorker）**
   ```go
   func (w *EmbeddingWorker) Enqueue(key int64, text string) {
       if text == "" || len(text) < 10 { return }
       select {
       case w.queue <- embeddingRequest{Key: key, Text: text}:
           // 成功
       default:
           log.Warnf("[EmbeddingWorker] queue full, dropping embedding for key=%d", key)
       }
   }
   ```
   - 正常速率（~40 events/day）不会满
   - API 大面积超时时才可能积压 → 丢弃是安全的（事件本身已入库）

9. **配置示例**
   ```yaml
   agents:
     tagent:
       memory:
         type: localfile
         path: .wechat-config/data
         embedding:
           model: embedding-3    # ZhiPu embedding 模型
           provider: zhipu       # 引用 providers 注册表
   ```
   - 无 `embedding` 配置时向量功能完全不启动（零开销）

---

## 阶段 1: Embedding 基础设施（语义检索底层）

> 目标: 实现 EmbeddingProvider 接口 + 替换 SearchByEmbedding stub + 异步生成管线
> 交付: `go build ./...` 通过，InMemoryStore 支持真实的向量检索

### Task 1.1: 实现 EmbeddingProvider 接口

- [ ] 新建 `memory/embedding.go`
- [ ] 定义接口:
  ```go
  type EmbeddingProvider interface {
      Embed(ctx context.Context, text string) ([]float32, error)
      Dimensions() int
  }
  ```
- [ ] 实现 `OpenAIEmbeddingProvider`：
  - 字段: `httpClient *http.Client`, `endpoint string`, `apiKey string`, `model string`, `dimensions int`
  - `Embed()` 方法: POST `{endpoint}/embeddings` with `{"input": text, "model": model}`
  - 解析响应 JSON 中的 `data[0].embedding` 数组
  - 超时: 使用 ctx（调用方设置 10s timeout）
  - 错误: 返回 wrapped error（含 HTTP status code）
- [ ] 实现 `NewOpenAIEmbeddingProvider(endpoint, apiKey, model string, dimensions int) *OpenAIEmbeddingProvider`
- [ ] 添加单元测试（mock HTTP server 返回预设向量）

### Task 1.2: InMemoryStore 向量存储

- [ ] 在 `memory/in_memory_store.go` 的 `InMemoryStore` 结构体中新增:
  ```go
  embeddings     map[int64][]float32  // EventKey → embedding vector
  embeddingMu    sync.RWMutex
  embeddingDims  int                  // 向量维度（第一次 Store 时确定）
  ```
- [ ] 实现真实的 `StoreEventWithEmbedding`:
  - 调用 `StoreEvent(key, event)` 存储事件
  - 存储 embedding 到 `embeddings[key]`
  - 如果 `embeddingDims == 0`，设置为 `len(embedding)`
  - 如果维度不匹配，返回错误
- [ ] 实现真实的 `SearchByEmbedding`:
  - 遍历 `embeddings` map，计算每个向量与 query 的余弦相似度
  - 取 topK 个最高分，转为 `[]EventReference`（从 events map 中构建 ref）
  - 按相似度降序返回
- [ ] `SupportsVectorSearch()` 改为: `return len(s.embeddings) > 0`
- [ ] 添加单元测试: 存入 5 个 embedding → 搜索 → 验证返回顺序

### Task 1.3: 余弦相似度实现

- [ ] 新建 `memory/vector_math.go`（或在 embedding.go 中）
- [ ] 实现:
  ```go
  func cosineSimilarity(a, b []float32) float32 {
      // dot(a,b) / (norm(a) * norm(b))
      // 处理零向量情况返回 0
  }
  ```
- [ ] 添加单元测试: 正交向量 → 0，相同向量 → 1，反向量 → -1

### Task 1.4: 异步 Embedding 生成器

- [ ] 新建 `memory/embedding_worker.go`
- [ ] 实现:
  ```go
  type EmbeddingWorker struct {
      provider EmbeddingProvider
      store    MemoryStore
      queue    chan embeddingRequest // buffered channel, cap=256
      ctx      context.Context
      cancel   context.CancelFunc
      wg       sync.WaitGroup
  }
  
  type embeddingRequest struct {
      EventKey int64
      Text     string
  }
  ```
- [ ] `Start()`: 启动消费 goroutine，从 queue 取请求，调 provider.Embed，写入 store.StoreEventWithEmbedding
  - 单条失败: 日志记录，继续处理下一条（不重试）
  - context 取消: 退出
- [ ] `Stop()`: cancel + wg.Wait
- [ ] `Enqueue(key int64, text string)`:
  - 如果 text 为空或长度 < 10，跳过
  - 如果 queue 满，日志 warning，丢弃（背压保护）
- [ ] 添加单元测试: mock provider → 验证事件被 embed 并存储

### Task 1.5: 集成到 MemoryPlugin

- [ ] 在 `plugin/memory_plugin.go` 中:
  - 新增字段 `embeddingWorker *memory.EmbeddingWorker`
  - 在 `onEvent` 方法末尾:
    ```go
    if p.embeddingWorker != nil {
        eventType := string(evt.StateDelta["event_type"])
        if eventType == "external_input" || eventType == "agent_output" {
            content := fullEvent.Content
            if content == "" { content = fullEvent.EventSummary }
            p.embeddingWorker.Enqueue(eventKey, content)
        }
    }
    ```
- [ ] 新增 `SetEmbeddingWorker(w *memory.EmbeddingWorker)` 方法

### Task 1.6: 配置支持

- [ ] 在 `config.go` 中新增:
  ```go
  type EmbeddingConfig struct {
      Model    string `json:"model" yaml:"model"`       // e.g. "text-embedding-3-small"
      Provider string `json:"provider,omitempty" yaml:"provider,omitempty"` // 引用 Providers 注册表
  }
  ```
- [ ] 在 `MemoryConfig` 中新增 `Embedding EmbeddingConfig` 字段
- [ ] 在 `tagent.go` 的 `resolveMemoryStore` 或 `buildAgent` 中，如果配置了 embedding，创建 EmbeddingProvider + EmbeddingWorker 并注入 MemoryPlugin

### Task 1.7: 验证阶段 1

- [ ] `go build ./...` 无编译错误
- [ ] `go test ./memory/ -run "TestEmbedding|TestCosine|TestSearchByEmbedding" -v` 通过
- [ ] `go test ./plugin/ -count=1` 通过
- [ ] `go test ./... -short -count=1` 全部通过

---

## 阶段 2: recall 语义搜索工具

> 目标: recall agent 获得 memory_semantic 子工具
> 交付: recall 可通过自然语言查询语义相关的历史事件

### Task 2.1: 实现 memory_semantic 子工具

- [ ] 新建 `tool/recall/semantic_tool.go`
- [ ] 实现 `NewRecallSemanticTool(memStore MemoryStore, embeddingProvider EmbeddingProvider) tool.CallableTool`
- [ ] Declaration:
  - Name: "memory_semantic"
  - Description: "按语义相似度搜索历史事件。输入自然语言查询，返回含义最相关的事件列表。"
  - InputSchema: `{"query": string (required), "top_k": integer (optional, default 10)}`
- [ ] Call 方法:
  1. 解析 args: query (string), top_k (int, default 10, max 50)
  2. 调 embeddingProvider.Embed(ctx, query) 获取查询向量
  3. 调 memStore.SearchByEmbedding(queryVec, topK) 获取结果
  4. 如果 SearchByEmbedding 返回错误或 SupportsVectorSearch()==false:
     - 降级为 memory_query 的关键词搜索: `memStore.QueryEvents(QueryOptions{Keyword: query, Limit: topK})`
  5. 对每个结果构建 JSON 返回:
     ```json
     {"results": [{"event_key": 123, "event_type": "...", "timestamp": "...", "summary": "..."}]}
     ```

### Task 2.2: 注册到 recall agent

- [ ] 在 `tool/recall/recall_subtools.go` 的 `BuildRecallSubTools` 中:
  - 如果 `cfg.EmbeddingProvider != nil`，注册 `NewRecallSemanticTool`
  - 否则不暴露此工具
- [ ] 在 `agent/tool_agent.go` 的 `PlainToolFactoryConfig` 中新增 `EmbeddingProvider memory.EmbeddingProvider` 字段
- [ ] 在 `tagent.go` 的 `buildPlainToolRef` 中传递 EmbeddingProvider

### Task 2.3: 更新 recall agent prompt

- [ ] 在 `resources/prompts/recall_agent.md` 中增加对 memory_semantic 工具的说明:
  ```
  - memory_semantic: 语义搜索。当用户描述模糊或使用不同措辞时，优先使用此工具。
    输入: {"query": "自然语言描述", "top_k": 10}
  ```

### Task 2.4: 验证阶段 2

- [ ] `go build ./...` 通过
- [ ] `go test ./tool/recall/ -v` 通过（含 semantic tool 测试）
- [ ] `go test ./... -short -count=1` 全部通过

---

## 阶段 3: 归档锚点 + 主动 recall hint

> 目标: L3 归档时提取锚点；空闲时检测归档与当前上下文的相关性
> 交付: 压缩后的信息能被语义搜索命中，且在相关时主动提示 Agent

### Task 3.1: archiveSegment 提取锚点

- [ ] 在 `agent/smart_compress.go` 的 `archiveSegment` 方法中:
  - 在存储归档事件后，提取锚点文本:
    ```go
    func extractAnchorText(seg *TaskSegment) string {
        // 1. 拼接段内用户消息
        var userTexts []string
        for _, msg := range seg.Messages {
            if msg.Role == model.RoleUser && msg.Content != "" {
                userTexts = append(userTexts, msg.Content)
            }
        }
        anchor := strings.Join(userTexts, " ")
        if len(anchor) > 200 { anchor = anchor[:200] }
        if anchor != "" { return anchor }
        
        // 2. 回退: 使用 summary 文本
        if summary != "" && len(summary) > 0 {
            if len(summary) > 200 { return summary[:200] }
            return summary
        }
        return ""
    }
    ```
  - 将锚点存入 `summaryEvent.Metadata["anchor_text"] = anchorText`
  - 如果 EmbeddingWorker 可用，调用 `embeddingWorker.Enqueue(summaryKey, anchorText)`

### Task 3.2: ProjectionOrganizer 增加归档相关性检测

- [ ] 在 `agent/projection_organizer.go` 的 `OrganizeOnce` 方法**末尾**新增步骤:
  ```go
  // 归档相关性检测 (仅在 EmbeddingProvider 可用时执行)
  if o.embeddingProvider != nil {
      o.checkArchiveRelevance(ctx)
  }
  ```
- [ ] 实现 `checkArchiveRelevance(ctx context.Context)`:
  1. 获取 Projection 最近 3 个 refs 的 EventSummary 拼接
  2. 调 embeddingProvider.Embed 获取当前上下文向量
  3. 调 memStore.SearchByEmbedding(contextVec, 10)
  4. 筛选: 只保留 EventType == "context_compress_summary" 的结果
  5. 取相似度最高的一个，如果 > 阈值(0.75) 且其 EventKey 不在当前 Projection 中 且未在 24h 内注入过:
     - 构建 recall_hint EventReference
     - 调 projection.Append(hintRef)
     - 记录已注入的 key + 时间
- [ ] 新增字段:
  - `embeddingProvider memory.EmbeddingProvider`
  - `hintedKeys map[int64]time.Time` (已注入的 key → 注入时间)

### Task 3.3: ContextCompressor 处理 recall_hint 类型

- [ ] 在 `agent/context_compressor.go` 的 `resolveRef` 方法中:
  - 在 `context_compress` 判断之后新增:
    ```go
    if ref.EventType == "recall_hint" {
        return model.Message{
            Role:    model.RoleSystem,
            Content: ref.EventSummary,
        }
    }
    ```

### Task 3.4: 连续失败计数

- [ ] 在 `agent/context_manager.go` 的 `ContextManager` 结构体中新增:
  ```go
  failCounts map[string]int // 工具名 → 连续失败次数
  ```
- [ ] 在 RunFlow 中，遍历 eventCh 的事件，对 `action_command` 类型:
  - 提取工具名（从 StateDelta 或消息内容）
  - 内容含 "error"/"Error"/"failed" → failCounts[name]++
  - 不含错误 → failCounts[name] = 0
- [ ] 当 failCounts[name] >= 3:
  - 在下次 BeforeModel 时注入: `[warning] 工具 {name} 连续失败 {count} 次，建议更换策略或检查参数`
  - 注入后不重置（持续提醒直到成功）

### Task 3.5: 验证阶段 3

- [ ] `go build ./...` 通过
- [ ] `go test ./agent/ -run "TestProjectionOrganizer" -v` 通过
- [ ] `go test ./agent/ -count=1` 全部通过
- [ ] `go test ./... -short -count=1` 全部通过

---

## 阶段 4: 端到端集成 + 配置接线

> 目标: tagent.yaml 配置 embedding 后，完整链路自动工作
> 交付: WeChat Bot 示例配置可正常使用语义搜索

### Task 4.1: tagent.go 接线

- [ ] 在 `tagent.go` 的 `buildAgent` 中:
  - 如果 `acfg.Memory.Embedding.Model != ""`:
    1. 解析 provider（复用 Config.Providers 注册表）
    2. 创建 `memory.NewOpenAIEmbeddingProvider(...)`
    3. 创建 `memory.NewEmbeddingWorker(provider, memStore)`
    4. 调用 `memPlugin.SetEmbeddingWorker(worker)`
    5. worker.Start()
    6. ta.RegisterCloser(worker)（确保 StopLoop 时停止）
  - 将 embeddingProvider 传入 recall 工具的 factory config
  - 将 embeddingProvider 传入 ProjectionOrganizer config

### Task 4.2: 示例配置更新

- [ ] 在 `examples/wechat-bot/tagent.yaml` 中为 entry agent 添加:
  ```yaml
  memory:
    type: localfile
    path: .wechat-config/data
    embedding:
      model: embedding-3
      provider: zhipu   # 引用已有的 provider
  ```

### Task 4.3: recall 工具注册路径

- [ ] 在 builtin.go 的 `recall_query`/`recall_get`/`recall_recent`/`recall_trace` factory 路径中:
  - 确保 `PlainToolFactoryConfig.EmbeddingProvider` 可传递到 `BuildRecallSubTools`
- [ ] 在 recall agent 的 tools 列表中新增 `recall_semantic` tool ref:
  ```yaml
  tools:
    - kind: tool
      id: recall_semantic
  ```

### Task 4.4: 端到端测试

- [ ] 新建 `tests/semantic_search_test.go`:
  - 使用 mock embedding provider（固定返回预设向量）
  - 存入 10 个事件（含 embedding）
  - 验证 SearchByEmbedding 返回正确顺序
  - 验证 recall_semantic 工具调用返回结果
  - 验证无 embedding provider 时降级为关键词搜索

### Task 4.5: 最终验证

- [ ] `go build ./...` 无错误
- [ ] `go vet ./...` 无警告
- [ ] `go test ./... -short -count=1` 全部通过
- [ ] `cd examples/wechat-bot && go build .` 编译通过
- [ ] 在 WeChat Bot 中配置 embedding 并验证 recall_semantic 可用
