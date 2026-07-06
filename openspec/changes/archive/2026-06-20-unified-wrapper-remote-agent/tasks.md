## 1. ExternalContextEntry 类型与序列化

- [x] 1.1 在 `agent/tool_agent.go` 中定义 `ExternalContextEntry` struct（EventKey int64, EventType string, EventSummary string）及 JSON tags
- [x] 1.2 实现 `serializeExternalContext(events []memory.FullEvent) ([]byte, error)` — 仅提取 EventKey/EventType/EventSummary，不含 Content
- [x] 1.3 实现 `deserializeExternalContext(data []byte) ([]memory.FullEvent, error)` — 反序列化为 FullEvent 切片（Content 为空）
- [x] 1.4 验证：go build 通过

## 2. AgentToolWrapper 泛化改造

- [x] 2.1 将 `AgentToolWrapper.agent` 字段类型从 `*TagentAgent` 改为 `agent.Agent`
- [x] 2.2 修改 `NewAgentToolWrapper` 签名：参数 `agent *TagentAgent` → `agent agent.Agent`
- [x] 2.3 修改 `Declaration()` 方法：`w.agent.name` → `w.agent.Info().Name`
- [x] 2.4 重构 `Call()` 方法：删除 `IngestExternalEvents` + `RunSimple` 调用，改为序列化 external_context → 构造 Invocation（含 RuntimeState）→ 调用 `agent.Run(ctx, inv)`
- [x] 2.5 保留 EventKey 解析逻辑（步骤 1-2 不变）和 event.Event 输出收集逻辑（步骤 5 不变）
- [x] 2.6 验证：go build 通过，go vet 通过

## 3. TagentAgent.Run 增加 RuntimeState 读取

- [x] 3.1 在 `TagentAgent.Run` 方法中，在现有 `pendingExternalEvents` 检查之前，新增 RuntimeState 读取：从 `inv.RunOptions.RuntimeState["external_context"]` 反序列化并调用 `IngestExternalEvents`
- [x] 3.2 确保 RuntimeState 路径和 pendingExternalEvents 路径不冲突（RuntimeState 先读取并 IngestExternalEvents，然后 pendingExternalEvents 检查会消费它）
- [x] 3.3 验证：go build 通过，现有测试不受影响

## 4. 配置扩展 — ToolRef.Remote

- [x] 4.1 在 `config.go` 中定义 `RemoteConfig` struct（`URL string`）
- [x] 4.2 在 `ToolRef` 中新增 `Remote *RemoteConfig` 字段（指针，nil = 本地）
- [x] 4.3 验证：go build 通过

## 5. tagent.go 远程 agent 创建

- [x] 5.1 在 `buildAgentToolRef` 中，当 `ref.Remote != nil` 时创建 `a2aagent.A2AAgent`：`a2aagent.New(WithAgentCardURL(ref.Remote.URL), WithTransferStateKey("external_context"))`
- [x] 5.2 将 A2AAgent 包装为 AgentToolWrapper（与本地 TagentAgent 路径统一）
- [x] 5.3 当 `ref.Remote == nil` 时保持现有 factory 路径不变
- [x] 5.4 验证：go build 通过

## 6. A2A Server

- [x] 6.1 新建 `agent/a2a_server.go`，实现 `NewA2AServer(ta *TagentAgent, host string) (*a2a.A2AServer, error)`
- [x] 6.2 内部调用 `a2a.New(a2a.WithAgent(ta, true), a2a.WithHost(host))`
- [x] 6.3 验证：go build 通过

## 7. 依赖提升

- [x] 7.1 在 `go.mod` 中将 `trpc.group/trpc-go/trpc-a2a-go` 从 `// indirect` 提升为直接依赖
- [x] 7.2 运行 `go mod tidy` 确保依赖一致
- [x] 7.3 验证：go build 通过

## 8. 测试

- [x] 8.1 修改 `agent/tagent_agent_loop_test.go` 中引用 `NewAgentToolWrapper` 的测试，适配新签名（`*TagentAgent` → `agent.Agent`）
- [x] 8.2 新增测试：AgentToolWrapper 通过 RuntimeState 传递 external_context（本地 mock agent）
- [x] 8.3 新增测试：ExternalContextEntry 序列化/反序列化正确性
- [x] 8.4 新增测试：TagentAgent.Run 从 RuntimeState 读取 external_context 并注入
- [x] 8.5 验证：go test ./... 全部通过

## 9. 文档修订 — agent-architecture.md

- [x] 9.1 更新 §四 TagentAgent 结构体说明：新增 RuntimeState 读取路径说明
- [x] 9.2 更新 §六 AgentToolWrapper 说明：`agent *TagentAgent` → `agent agent.Agent`，Call 方法流程改为 RuntimeState 路径
- [x] 9.3 新增 §十三 A2A 通信章节：本地/远程调用路径、A2A Server 模式、metadata → RuntimeState 自动映射
- [x] 9.4 新增 §十四 配置分层说明：tagent YAML（agent 定义）vs trpc Go options（通信配置）的关系

## 10. 文档修订 — tool-architecture.md

- [x] 10.1 更新 §四 事件上下文传递机制：新增远程路径（RuntimeState → A2A metadata → RuntimeState）
- [x] 10.2 更新 §七 KnowledgeAgent / §六 RecallAgent 的 AgentToolWrapper 描述：统一为 agent.Agent 接口
- [x] 10.3 新增远程子 agent 配置示例（ToolRef.Remote）
- [x] 10.4 更新 §十三 关键设计决策：新增"为什么用 RuntimeState 而非 struct 字段"决策

## 11. 最终验证

- [x] 11.1 go build ./... 通过
- [x] 11.2 go vet ./... 通过
- [x] 11.3 go test ./... 全部通过
- [x] 11.4 确认 wiki 文档与代码一致
