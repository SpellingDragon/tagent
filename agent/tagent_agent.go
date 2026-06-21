// Package agent provides tagent's core agent mechanism coordination.
//
// TagentAgent wires together:
//   - LLMAgent (framework-native React loop)
//   - Runner (framework orchestration with plugins)
//   - MemoryPlugin (OnEvent: event persistence + causal chain)
//   - ContextIntervention (BeforeModel: token budget + SmartCompress)
//
// Core principle: LLMAgent is the React loop skeleton,
// tagent's differential logic is injected via callback/plugin.
//
// TagentAgent implements agent.Agent, so it can be wrapped as agent.Tool
// for tool-agent composition.
//
// Top-level usage: StartLoop / InjectMessage / StopLoop (persistent event loop only).
// Sub-agent usage: agent.Run() via AgentToolWrapper.Call() (invoked by parent LLM).
//
// NOTE: This package does NOT depend on tagent/tool.
// Application-level wiring (KnowledgeAgent assembly, WireActionTool, etc.)
// lives in the root tagent package.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	telemetrytrace "trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
)

// Closer is implemented by components that hold resources requiring cleanup
// on agent shutdown (e.g., ActionTool stops its TmuxMonitor).
// Using an interface avoids a direct dependency on tagent/tool.
type Closer interface {
	Close() error
}

// Verify TagentAgent implements agent.Agent at compile time.
var _ agent.Agent = (*TagentAgent)(nil)

// TagentAgent is tagent's top-level Agent assembly.
// It implements agent.Agent so it can be used both as a standalone agent
// and as a tool-agent (wrapped via AgentToolWrapper).
type TagentAgent struct {
	llmAgent *llmagent.LLMAgent
	runner   runner.Runner
	memStore memory.MemoryStore
	config   *TagentConfig

	// Agent identity (for agent.Agent interface)
	name        string
	description string

	// Session context for event injection (set on first Run)
	sessionMu     sync.Mutex
	lastUserID    string
	lastSessionID string

	// External events pending ingestion (set before Run)
	// These are converted to internal context messages at the start of the next run.
	pendingExternalEvents []memory.FullEvent

	// Resource closers — components like ActionTool that need cleanup on shutdown.
	// Closed in Close() before the runner is stopped.
	closers []Closer

	// TrajectoryRecorder (optional) — records LLM calls to JSONL when enabled.
	// Set via SetTrajectoryRecorder. StartLoop calls SetSessionInfo on it.
	trajectoryRecorder *TrajectoryRecorder

	// Persistent Event Loop — 持久事件循环（StartLoop 模式）
	mailbox    chan model.Message // 事件邮箱（并发写入，单 goroutine 消费）
	outputCh   chan *event.Event  // 持久输出 channel（Loop 模式下不关闭）
	loopCtx    context.Context    // Loop context（StopLoop 取消）
	loopCancel context.CancelFunc // Loop cancel
	loopActive atomic.Bool        // Loop 是否运行中
	loopWg     sync.WaitGroup     // 等待 Loop goroutine 退出
}

// TagentConfig holds configuration for creating a TagentAgent.
type TagentConfig struct {
	Model             model.Model        // Required: LLM model
	MemoryStore       memory.MemoryStore // Optional: external MemoryStore (default: InMemoryStore)
	SystemPrompt      string             // System prompt loaded from AGENTS.md/SOUL.md/USER.md/TOOLS.md
	Tools             []tool.Tool        // CallableTools to register
	MaxToolIterations int                // Default: 200
	MaxTokens         int                // Token budget for context (default: 8000)
	CompressThreshold float64            // Compression trigger threshold (default: 0.8)
	SummaryModel      model.Model        // Optional: for Stage 2 LLM summary
	Temperature       float64            // Optional: LLM temperature (default: 0.7)

	// Agent identity (for agent.Agent interface)
	Name        string // Default: "tagent"
	Description string // Default: "TagentAgent - AI assistant powered by tagent"
}

// Default configuration values
const (
	DefaultMaxToolIterations = 200
	DefaultMaxTokens         = 8000
	DefaultCompressThreshold = 0.8
	DefaultAgentName         = "tagent"
	DefaultAgentDescription  = "TagentAgent - AI assistant powered by tagent"
)

// NewTagentAgent creates a new TagentAgent with the given configuration.
func NewTagentAgent(cfg *TagentConfig) (*TagentAgent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if cfg.Model == nil {
		return nil, fmt.Errorf("model is required")
	}

	// Apply defaults
	if cfg.MaxToolIterations <= 0 {
		cfg.MaxToolIterations = DefaultMaxToolIterations
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = DefaultMaxTokens
	}
	if cfg.CompressThreshold <= 0 || cfg.CompressThreshold > 1 {
		cfg.CompressThreshold = DefaultCompressThreshold
	}

	// 1. Create MemoryStore (use provided or default to InMemoryStore)
	var memStore memory.MemoryStore
	if cfg.MemoryStore != nil {
		memStore = cfg.MemoryStore
	} else {
		memStore = memory.NewInMemoryStore()
	}

	// 2. Create MemoryPlugin (OnEvent: event persistence + causal chain + StateDelta)
	memPlugin := plugin.NewMemoryPlugin(memStore)

	// 3. Create SmartCompressor
	compressorOpts := []SmartCompressorOption{
		WithMaxTokens(cfg.MaxTokens),
	}
	if cfg.SummaryModel != nil {
		compressorOpts = append(compressorOpts, WithSummaryModel(cfg.SummaryModel))
	}
	compressor := NewSmartCompressor(compressorOpts...)

	// 4. Create ContextIntervention (BeforeModel: token budget + compress)
	tokenCounter := NewDefaultTokenCounter()
	ci := NewContextIntervention(compressor, tokenCounter, cfg.MaxTokens, cfg.CompressThreshold)

	// 5. Create ModelCallbacks
	modelCB := model.NewCallbacks()
	modelCB.RegisterBeforeModel(ci.BeforeModel)

	// Apply identity defaults
	name := cfg.Name
	if name == "" {
		name = DefaultAgentName
	}
	description := cfg.Description
	if description == "" {
		description = DefaultAgentDescription
	}

	// 5.5 Wrap all tools with OutputLimitTool to prevent oversized tool outputs
	// from consuming the agent's context window. The limit is MaxTokens/2 * 4
	// characters (approx 2x the half-token budget, since 1 token ≈ 4 chars).
	maxOutputChars := cfg.MaxTokens / 2 * 4
	if maxOutputChars > 0 && len(cfg.Tools) > 0 {
		wrapped := make([]tool.Tool, len(cfg.Tools))
		for i, t := range cfg.Tools {
			wrapped[i] = NewOutputLimitTool(t, maxOutputChars)
		}
		cfg.Tools = wrapped
	}

	// 6. Create LLMAgent
	llmAgentOpts := []llmagent.Option{
		llmagent.WithModel(cfg.Model),
		llmagent.WithInstruction(cfg.SystemPrompt),
		llmagent.WithMaxToolIterations(cfg.MaxToolIterations),
		llmagent.WithModelCallbacks(modelCB),
	}
	if len(cfg.Tools) > 0 {
		llmAgentOpts = append(llmAgentOpts, llmagent.WithTools(cfg.Tools))
	}
	llmAgent := llmagent.New(name, llmAgentOpts...)

	// 7. Create SessionService with an AppendEventHook that clones Response
	// before storage. Without this hook, session events and channel events
	// share the same *model.Response pointer. The framework's
	// ContentRequestProcessor may modify session events' Response.Choices in
	// subsequent iterations (e.g., mergeFunctionResponseEvents), racing with
	// AgentToolWrapper.Call() which reads the same Response from the channel.
	// The hook gives the session its own clone so framework modifications to
	// session events don't affect channel events.
	sessionSvc := sessioninmemory.NewSessionService(
		sessioninmemory.WithAppendEventHook(func(ctx *session.AppendEventContext, next func() error) error {
			// Create a shallow copy of the event with a cloned Response for
			// session storage. We must NOT mutate the original event's
			// Response field because other goroutines (e.g.,
			// wrapEventChannelWithTelemetry) may concurrently read it.
			// By pointing ctx.Event to a copy, next() stores the copy
			// (with cloned Response) while the original event remains
			// untouched for channel consumers.
			original := ctx.Event
			if original.Response != nil {
				evtCopy := *original
				evtCopy.Response = original.Response.Clone()
				ctx.Event = &evtCopy
			}
			err := next()
			ctx.Event = original
			return err
		}),
	)

	// 8. Create Runner with MemoryPlugin, SummaryPlugin, and session service
	r := runner.NewRunner(name, llmAgent, runner.WithPlugins(
		plugin.NewSummaryPlugin(),
		memPlugin,
	), runner.WithSessionService(sessionSvc))

	return &TagentAgent{
		llmAgent:    llmAgent,
		runner:      r,
		memStore:    memStore,
		config:      cfg,
		name:        name,
		description: description,
		closers:     []Closer{sessionSvc},
	}, nil
}

// Run implements agent.Agent interface.
// It is called by AgentToolWrapper.Call() for sub-agent invocation (local or remote via A2A).
// Top-level usage must use StartLoop/InjectMessage/StopLoop instead.
//
// Context can arrive via two paths:
//  1. RuntimeState path (remote/wrapper): inv.RunOptions.RuntimeState["external_context"]
//     contains serialized ExternalContextEntry JSON. This is the A2A-compatible path —
//     A2AAgent auto-maps metadata → RuntimeState, so remote calls work transparently.
//  2. Struct field path (direct API): pendingExternalEvents set via IngestExternalEvents.
//     Used by direct callers (tests, embedded usage).
func (ta *TagentAgent) Run(ctx context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
	// Extract or generate userID and sessionID from Invocation
	userID := "tagent-user"
	sessionID := fmt.Sprintf("tagent-session-%s", inv.InvocationID)

	// Store session context for event injection
	ta.setSessionContext(userID, sessionID)

	// Path 1: Read external context from RuntimeState (remote/wrapper path)
	if inv.RunOptions.RuntimeState != nil {
		if raw, ok := inv.RunOptions.RuntimeState[ExternalContextKey]; ok {
			var data []byte
			switch v := raw.(type) {
			case json.RawMessage:
				data = v
			case []byte:
				data = v
			case string:
				data = []byte(v)
			}
			if len(data) > 0 {
				events, err := deserializeExternalContext(data)
				if err != nil {
					log.Warnf("[Run] failed to deserialize external context: %v", err)
				} else if len(events) > 0 {
					ta.IngestExternalEvents(events)
				}
			}
		}
	}

	message := inv.Message
	if message.Content == "" {
		message = model.NewUserMessage("")
	}

	// Path 2: Prepend external event context if pending (works for both paths —
	// RuntimeState path above calls IngestExternalEvents which sets pendingExternalEvents)
	if len(ta.pendingExternalEvents) > 0 {
		message = ta.injectExternalContext(message)
	}

	return ta.runner.Run(ctx, userID, sessionID, message)
}

// RunSimple is removed. Top-level usage must use StartLoop/InjectMessage/StopLoop.
// Sub-agent invocation goes through agent.Run() via AgentToolWrapper.Call().

// Tools implements agent.Agent interface.
func (ta *TagentAgent) Tools() []tool.Tool {
	return ta.llmAgent.Tools()
}

// Info implements agent.Agent interface.
func (ta *TagentAgent) Info() agent.Info {
	return agent.Info{
		Name:        ta.name,
		Description: ta.description,
	}
}

// SubAgents implements agent.Agent interface.
func (ta *TagentAgent) SubAgents() []agent.Agent {
	return ta.llmAgent.SubAgents()
}

// FindSubAgent implements agent.Agent interface.
func (ta *TagentAgent) FindSubAgent(name string) agent.Agent {
	return ta.llmAgent.FindSubAgent(name)
}

// RegisterCloser registers a component to be closed on agent shutdown.
// Components are closed in registration order before the runner is stopped.
func (ta *TagentAgent) RegisterCloser(c Closer) {
	ta.closers = append(ta.closers, c)
}

// SetTrajectoryRecorder sets the trajectory recorder for this agent.
// When set, StartLoop will automatically call SetSessionInfo on it.
// The recorder should also be registered via RegisterCloser for graceful shutdown.
func (ta *TagentAgent) SetTrajectoryRecorder(tr *TrajectoryRecorder) {
	ta.trajectoryRecorder = tr
}

// TrajectoryRecorder returns the trajectory recorder if one is set, or nil.
func (ta *TagentAgent) TrajectoryRecorder() *TrajectoryRecorder {
	return ta.trajectoryRecorder
}

// Close closes all registered resources and the runner.
// Closers (e.g., ActionTool) are stopped first, then the MemoryStore
// (if it supports closing), and finally the runner.
func (ta *TagentAgent) Close() error {
	var errs []error

	// Stop Persistent Event Loop first if active
	if ta.loopActive.Load() {
		ta.StopLoop()
	}

	// Close registered closers first (e.g., ActionTool stops TmuxMonitor)
	for _, c := range ta.closers {
		if err := c.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close resource: %w", err))
		}
	}

	// Close memory store if it supports closing (e.g., FileSegmentStore stops lifecycle components)
	if c, ok := ta.memStore.(interface{ Close() error }); ok {
		if err := c.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close memory store: %w", err))
		}
	}

	// Close runner
	if err := ta.runner.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close runner: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

// MemStore returns the MemoryStore for direct access (e.g., by RecallTool).
func (ta *TagentAgent) MemStore() memory.MemoryStore {
	return ta.memStore
}

// Runner returns the underlying Runner (for TmuxMonitor event injection).
func (ta *TagentAgent) Runner() runner.Runner {
	return ta.runner
}

// InjectMessage injects a system message into the persistent event loop's
// mailbox. This is used by tools (e.g., TmuxMonitor) to inject asynchronous
// system messages while the Loop goroutine is processing.
//
// Requires StartLoop to be called first — the persistent Loop is the only
// execution mode. If the Loop is not active, the message is dropped with a
// warning.
func (ta *TagentAgent) InjectMessage(msg model.Message) {
	if !ta.loopActive.Load() {
		log.Warnf("[InjectMessage] persistent loop not started, message dropped")
		return
	}
	ta.mailbox <- msg
}

// setSessionContext sets the userID and sessionID (thread-safe).
func (ta *TagentAgent) setSessionContext(userID, sessionID string) {
	ta.sessionMu.Lock()
	defer ta.sessionMu.Unlock()
	ta.lastUserID = userID
	ta.lastSessionID = sessionID
}

// IngestExternalEvents queues external events to be injected as context
// into the next Run call. This is the mechanism for passing
// context from a parent agent to a tool agent via AgentToolWrapper.
//
// The events are converted to a system message summarizing the external
// context and prepended to the user message. After injection, the pending
// events are cleared.
func (ta *TagentAgent) IngestExternalEvents(events []memory.FullEvent) {
	ta.pendingExternalEvents = events
}

// injectExternalContext converts pending external events into a context message
// prepended to the user message. After injection, the pending events are cleared.
//
// Only EventSummary is injected — NOT the full Content. This keeps external context
// compact so sub-agents stay within their token budget. The sub-agent retrieves full
// event details via its own memory tools (memory_get, memory_query) if needed.
func (ta *TagentAgent) injectExternalContext(msg model.Message) model.Message {
	events := ta.pendingExternalEvents
	ta.pendingExternalEvents = nil // Clear after consumption

	if len(events) == 0 {
		return msg
	}

	// Build external context summary (EventSummary only — compact, no full Content)
	var contextBuilder string
	contextBuilder = "[External Context from Parent Agent]\n\n"
	for i, evt := range events {
		contextBuilder += fmt.Sprintf("Event %d: [%s] %s\n", i+1, evt.EventType, evt.EventSummary)
	}
	contextBuilder += "\n[End of External Context]\n\n"

	log.Infof("[InjectContext] injecting %d external events, context_len=%d", len(events), len(contextBuilder))

	// Prepend external context to the user message
	msg.Content = contextBuilder + msg.Content
	return msg
}

// ---------------------------------------------------------------------------
// Persistent Event Loop — 持久事件循环
//
// Loop goroutine 循环调用 runner.Run()。每次 Run 的 break 表示“这批事件处理完了”，
// Loop 回到 drain mailbox 等待下一批。相同 userID/sessionID 确保跨 Run 的 Session 连续性。
// 调用方通过 InjectMessage 提交消息到 mailbox，Loop goroutine 批量 drain 后合并触发 runner.Run()。
//
// 可观测性：每个 batch 创建独立的 OTLP span（tagent.loop.batch），框架内部的
// TraceChat/TraceToolCall 会自动在此 span 下创建子 span，形成 trace 层级。
// 通过 telemetry/trace.Start() 初始化 OTLP exporter 后，所有 span 将通过
// OTLP 协议导出到可观测后端（如 Jaeger/Tempo/loongcollector）。
// ---------------------------------------------------------------------------

// StartLoop 启动持久事件循环，返回持久 outputCh 供调用方接收事件。
// 调用方通过 InjectMessage 提交事件到 mailbox，Loop goroutine 批量 drain 后合并触发 runner.Run()。
// 通过 evt.GetResponse().IsFinalResponse() 判断单次响应完成。
// outputCh 在 StopLoop 后关闭。
func (ta *TagentAgent) StartLoop(userID, sessionID string) (<-chan *event.Event, error) {
	if ta.loopActive.Load() {
		return ta.outputCh, nil
	}

	ta.mailbox = make(chan model.Message, 256)
	ta.outputCh = make(chan *event.Event, 100)
	ta.loopCtx, ta.loopCancel = context.WithCancel(context.Background())
	ta.loopActive.Store(true)

	// 缓存 session 上下文供 Loop 使用
	ta.setSessionContext(userID, sessionID)

	// 设置 TrajectoryRecorder 的 session 信息
	if ta.trajectoryRecorder != nil {
		ta.trajectoryRecorder.SetSessionInfo(userID, sessionID)
	}

	ta.loopWg.Add(1)
	go ta.loop(userID, sessionID)

	log.Infof("[StartLoop] persistent event loop started user=%s session=%s", userID, sessionID)
	return ta.outputCh, nil
}

// StopLoop 停止持久事件循环。
// 取消 Loop context，等待 Loop goroutine 退出，关闭 outputCh。
func (ta *TagentAgent) StopLoop() {
	if !ta.loopActive.Load() {
		return
	}
	ta.loopActive.Store(false)
	ta.loopCancel()
	ta.loopWg.Wait()
	log.Infof("[StopLoop] persistent event loop stopped")
}

// loop 是 Persistent Event Loop 的主 goroutine。
// 循环：drain mailbox -> mergeBatch -> per-batch span -> runner.Run -> 转发事件到 outputCh。
// 当 loopCtx 被取消时退出并关闭 outputCh。
func (ta *TagentAgent) loop(userID, sessionID string) {
	defer ta.loopWg.Done()
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("[Loop] panic recovered: %v", r)
		}
		close(ta.outputCh)
	}()

	batchIndex := 0
	for {
		// 1. 批量 drain mailbox
		batch := ta.drainMailbox()
		if batch == nil {
			// loopCtx 被取消
			return
		}
		batchIndex++

		// 2. 合并为一条消息
		msg := mergeBatch(batch)

		// 3. 创建 per-batch context + span（OTLP 可观测）
		//    框架内部的 TraceChat/TraceToolCall 会自动在此 span 下创建子 span
		batchCtx, span := telemetrytrace.Tracer.Start(ta.loopCtx, "tagent.loop.batch")
		batchStart := time.Now()

		span.SetAttributes(
			attribute.Int("tagent.batch.index", batchIndex),
			attribute.Int("tagent.batch.message_count", len(batch)),
			attribute.Int("tagent.batch.merged_content_len", len(msg.Content)),
			attribute.String("tagent.batch.user_id", userID),
			attribute.String("tagent.batch.session_id", sessionID),
		)

		// 记录每条输入事件内容
		for i, m := range batch {
			log.Infof("[Loop.Batch#%d] input event#%d role=%s content_len=%d content_preview=%s",
				batchIndex, i+1, m.Role, len(m.Content), truncateString(m.Content, 200))
			span.SetAttributes(attribute.String(
				fmt.Sprintf("tagent.batch.input.%d.role", i+1), string(m.Role)))
			span.SetAttributes(attribute.String(
				fmt.Sprintf("tagent.batch.input.%d.content", i+1), truncateString(m.Content, 1000)))
		}

		log.Infof("[Loop.Batch#%d] start batch_size=%d merged_content_len=%d",
			batchIndex, len(batch), len(msg.Content))

		// 4. 调用 runner.Run（复用完整框架管道，使用 per-batch traced context）
		eventCh, err := ta.runner.Run(batchCtx, userID, sessionID, msg)
		if err != nil {
			log.Errorf("[Loop.Batch#%d] runner.Run failed: %v", batchIndex, err)
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.SetAttributes(
				attribute.String("tagent.batch.status", "error"),
				attribute.Float64("tagent.batch.duration_seconds", time.Since(batchStart).Seconds()),
			)
			span.End()
			if ta.loopCtx.Err() != nil {
				return
			}
			continue
		}

		// 5. 转发事件到持久 outputCh + 可观测记录
		var (
			eventCount    int
			toolCallCount int
			inputTokens   int
			outputTokens  int
			hasFinal      bool
			ttft          time.Duration
			lastContent   string
			lastReasoning string
		)

		for evt := range eventCh {
			eventCount++

			// 检查事件内容用于可观测性
			if evt != nil && evt.Response != nil {
				rsp := evt.Response

				// 工具调用
				if rsp.IsToolCallResponse() {
					toolCallCount += len(rsp.GetToolCallIDs())
					for _, choice := range rsp.Choices {
						for _, tc := range choice.Message.ToolCalls {
							log.Infof("[Loop.Batch#%d] event#%d TOOL_CALL id=%s func=%s args_len=%d args_preview=%s",
								batchIndex, eventCount, tc.ID, tc.Function.Name,
								len(tc.Function.Arguments), truncateString(string(tc.Function.Arguments), 200))
						}
					}
				}

				// 工具结果
				if rsp.IsToolResultResponse() && len(rsp.Choices) > 0 {
					log.Infof("[Loop.Batch#%d] event#%d TOOL_RESULT tool_id=%s content_len=%d",
						batchIndex, eventCount, rsp.Choices[0].Message.ToolID,
						len(rsp.Choices[0].Message.Content))
				}

				// Token 使用量 + TTFT
				if rsp.Usage != nil {
					inputTokens += rsp.Usage.PromptTokens
					outputTokens += rsp.Usage.CompletionTokens
					if rsp.Usage.TimingInfo != nil && rsp.Usage.TimingInfo.FirstTokenDuration > 0 {
						ttft = rsp.Usage.TimingInfo.FirstTokenDuration
					}
				}

				// 最终响应（包含 think + response）
				if rsp.IsFinalResponse() && len(rsp.Choices) > 0 {
					hasFinal = true
					lastContent = rsp.Choices[0].Message.Content
					lastReasoning = rsp.Choices[0].Message.ReasoningContent
					log.Infof("[Loop.Batch#%d] event#%d FINAL_RESPONSE model=%s content_len=%d reasoning_len=%d input_tokens=%d output_tokens=%d ttft=%s",
						batchIndex, eventCount, rsp.Model,
						len(lastContent), len(lastReasoning),
						inputTokens, outputTokens, ttft)
					if lastReasoning != "" {
						log.Infof("[Loop.Batch#%d] think_preview=%s", batchIndex, truncateString(lastReasoning, 200))
					}
					log.Infof("[Loop.Batch#%d] response_preview=%s", batchIndex, truncateString(lastContent, 200))
				}

				// 错误事件
				if evt.IsError() {
					log.Errorf("[Loop.Batch#%d] event#%d ERROR type=%s msg=%s",
						batchIndex, eventCount, rsp.Error.Type, rsp.Error.Message)
				}
			}

			select {
			case ta.outputCh <- evt:
			case <-ta.loopCtx.Done():
				// drain 剩余事件避免阻塞 runner
				for range eventCh {
				}
				span.SetAttributes(
					attribute.Int("tagent.batch.event_count", eventCount),
					attribute.String("tagent.batch.status", "cancelled"),
					attribute.Float64("tagent.batch.duration_seconds", time.Since(batchStart).Seconds()),
				)
				span.End()
				log.Infof("[Loop.Batch#%d] cancelled duration=%s events=%d",
					batchIndex, time.Since(batchStart), eventCount)
				return
			}
		}

		// 6. 设置 batch span 结束属性
		batchDuration := time.Since(batchStart)
		span.SetAttributes(
			attribute.Int("tagent.batch.event_count", eventCount),
			attribute.Int("tagent.batch.tool_call_count", toolCallCount),
			attribute.Int("tagent.batch.input_tokens", inputTokens),
			attribute.Int("tagent.batch.output_tokens", outputTokens),
			attribute.Float64("tagent.batch.ttft_seconds", ttft.Seconds()),
			attribute.Bool("tagent.batch.has_final_response", hasFinal),
			attribute.String("tagent.batch.status", "completed"),
			attribute.Float64("tagent.batch.duration_seconds", batchDuration.Seconds()),
		)
		if lastContent != "" {
			span.SetAttributes(attribute.String("tagent.batch.final_response", truncateString(lastContent, 1000)))
		}
		if lastReasoning != "" {
			span.SetAttributes(attribute.String("tagent.batch.final_reasoning", truncateString(lastReasoning, 1000)))
		}
		span.End()

		// 7. Batch 摘要日志
		log.Infof("[Loop.Batch#%d] completed duration=%s events=%d tool_calls=%d input_tokens=%d output_tokens=%d ttft=%s has_final=%v",
			batchIndex, batchDuration, eventCount, toolCallCount,
			inputTokens, outputTokens, ttft, hasFinal)

		// 8. 检查是否应该退出
		if ta.loopCtx.Err() != nil {
			return
		}
	}
}

// drainMailbox 阻塞等待第一个消息，然后非阻塞地取出所有剩余消息。
// 返回 nil 表示 loopCtx 被取消。
func (ta *TagentAgent) drainMailbox() []model.Message {
	// 阻塞等待第一个消息或取消
	select {
	case msg := <-ta.mailbox:
		batch := []model.Message{msg}
		// 非阻塞地取出剩余消息
		for {
			select {
			case msg := <-ta.mailbox:
				batch = append(batch, msg)
			default:
				return batch
			}
		}
	case <-ta.loopCtx.Done():
		return nil
	}
}

// mergeBatch 将多条消息合并为一条。
// 单消息直接返回；多消息提取 Content 用 "\n\n---\n\n" 连接，Role 设为 RoleUser。
// System 消息（Tmux 通知）和 User 消息混合时，简单拼接为一条 user 消息。
func mergeBatch(msgs []model.Message) model.Message {
	if len(msgs) == 1 {
		return msgs[0]
	}
	var parts []string
	for _, m := range msgs {
		if m.Content != "" {
			parts = append(parts, m.Content)
		}
	}
	return model.Message{
		Role:    model.RoleUser,
		Content: strings.Join(parts, "\n\n---\n\n"),
	}
}

// truncateString truncates s to at most n characters, appending "..." if truncated.
func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ---------------------------------------------------------------------------
// SwappableModel — 可运行时替换的 model.Model 包装器
//
// 用于 HTTPAPI 接收 AReaL adapter 传入的 llm_base_url 时，
// 将 LLM 请求重定向到 AReaL proxy（端口动态分配）。
// 不改变事件机制（persistent loop / InjectMessage / outputCh 不变），
// 仅替换底层 model.Model 实例。
// ---------------------------------------------------------------------------

// SwappableModel wraps a model.Model, allowing the inner model to be
// swapped at runtime without recreating the LLMAgent or Runner.
// All GenerateContent/Info calls delegate to the current inner model.
type SwappableModel struct {
	mu    sync.RWMutex
	inner model.Model
}

// NewSwappableModel creates a SwappableModel wrapping the given model.
func NewSwappableModel(m model.Model) *SwappableModel {
	return &SwappableModel{inner: m}
}

// Swap replaces the inner model atomically.
// In-flight GenerateContent calls continue with the old model;
// subsequent calls use the new model.
func (m *SwappableModel) Swap(inner model.Model) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inner = inner
}

// GenerateContent delegates to the current inner model.
func (m *SwappableModel) GenerateContent(ctx context.Context, request *model.Request) (<-chan *model.Response, error) {
	m.mu.RLock()
	inner := m.inner
	m.mu.RUnlock()
	return inner.GenerateContent(ctx, request)
}

// Info delegates to the current inner model.
func (m *SwappableModel) Info() model.Info {
	m.mu.RLock()
	inner := m.inner
	m.mu.RUnlock()
	return inner.Info()
}
