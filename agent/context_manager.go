package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/SpellingDragon/tagent/prompt"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// ---------------------------------------------------------------------------
// TokenCounter
// ---------------------------------------------------------------------------

// TokenCounter estimates token count for message lists.
type TokenCounter interface {
	Estimate(messages []model.Message) int
}

// DefaultTokenCounter estimates tokens using a character-based heuristic.
type DefaultTokenCounter struct {
	CharsPerToken float64
}

func NewDefaultTokenCounter() *DefaultTokenCounter {
	return &DefaultTokenCounter{CharsPerToken: 2.0}
}

func (c *DefaultTokenCounter) Estimate(messages []model.Message) int {
	total := 0
	for i := range messages {
		msg := &messages[i]
		total += int(float64(len([]rune(msg.Content))) / c.CharsPerToken)
		total += 10
		if len(msg.ToolCalls) > 0 {
			total += 20 * len(msg.ToolCalls)
		}
	}
	if total < 1 {
		total = 1
	}
	return total
}

// ---------------------------------------------------------------------------
// ContextManager
// ---------------------------------------------------------------------------

// ContextManager is the unified component for message building, compression
// orchestration, and framework Flow execution. It replaces Preprocessor and
// FrameworkFlowAdapter.
//
// Prototype mapping:
//   - OnEvents (append inputs + call model) → BuildInvocation + RunFlow
//   - Compact (clean projection) → ContextCompressor in BeforeModel callback
type ContextManager struct {
	contextCompressor *ContextCompressor
	tokenCounter      TokenCounter
	memStore          memory.MemoryStore
	maxTokens         int
	thresholdPct      float64
	recentFullCount   int

	// Framework integration
	runner    runner.Runner
	name      string
	userID    string
	sessionID string

	// Event routing
	outputCh   chan *event.Event
	bus        *EventBus
	projection *SessionProjection
	onEvent    func(evt *event.Event)

	// taskController, when set, is injected into the RunFlow ctx (as a
	// TaskSpawner) so tools can hand long-running work to the task layer, and
	// is used to render the live task board at BeforeModel. nil → tools fall
	// back to synchronous execution and no board is rendered.
	taskController TaskController

	// triggerSource identifies what triggered the current RunFlow
	// (e.g., "user", "meditation", "async_result"). Set by runEventLoop
	// before calling RunFlow. Attached to outputCh events via
	// StateDelta["trigger_source"] for deterministic consumer dispatch.
	triggerSource string

	// currentMetadata holds arbitrary metadata from the source event of the
	// current RunFlow. Set by runEventLoop before calling RunFlow.
	// Propagated to derived events via StateDelta["meta_*"] in onEvent.
	currentMetadata map[string]string
	metadataMu      sync.RWMutex

	// partitionID is used for Snowflake EventKey generation when persisting
	// bus events directly (bypassing MemoryPlugin's OnEvent hook).
	partitionID int

	// systemPromptSource enables hot-reload of the system prompt.
	// When set, the system message is re-read from files before each LLM call.
	systemPromptSource *prompt.Source
}

// SetTriggerSource sets the trigger source for the next RunFlow call.
func (cm *ContextManager) SetTriggerSource(source string) {
	cm.triggerSource = source
}

// SetInvocationMetadata sets the metadata for the current RunFlow.
// These metadata are propagated to all events derived from the source event
// via event.StateDelta with "meta_" prefix in the onEvent callback.
func (cm *ContextManager) SetInvocationMetadata(md map[string]string) {
	cm.metadataMu.Lock()
	defer cm.metadataMu.Unlock()
	cm.currentMetadata = md
}

// GetInvocationMetadata returns the current RunFlow's metadata (thread-safe copy).
func (cm *ContextManager) GetInvocationMetadata() map[string]string {
	cm.metadataMu.RLock()
	defer cm.metadataMu.RUnlock()
	if cm.currentMetadata == nil {
		return nil
	}
	out := make(map[string]string, len(cm.currentMetadata))
	for k, v := range cm.currentMetadata {
		out[k] = v
	}
	return out
}

// ContextManagerConfig holds everything needed to create a ContextManager.
type ContextManagerConfig struct {
	Name         string
	UserID       string
	SessionID    string
	Model        model.Model
	Tools        []trpctool.Tool
	SystemPrompt string
	Temperature  float64
	MaxToolIters int

	// SystemPromptSource enables hot-reload of system prompt from files.
	// When set, the system message is re-read from disk before each LLM call.
	SystemPromptSource *prompt.Source

	// Thinking/reasoning controls
	ThinkingEnabled      *bool
	ThinkingTokens       *int
	ReasoningEffort      *string
	ReasoningContentMode string

	Compressor   *SmartCompressor
	TokenCounter TokenCounter
	MaxTokens    int
	ThresholdPct float64
	MemStore     memory.MemoryStore

	// Unified Runner: plugins + session service registered on the same Runner
	MemPlugin  *plugin.MemoryPlugin
	SessionSvc session.Service

	OutputCh   chan *event.Event
	Bus        *EventBus
	Projection *SessionProjection
	OnEvent    func(evt *event.Event)
}

// NewContextManager creates a ContextManager that wraps a framework LLMAgent
// with ContextCompressor as the sole BeforeModel compression callback.
func NewContextManager(cfg ContextManagerConfig) *ContextManager {
	cm := &ContextManager{
		tokenCounter:       cfg.TokenCounter,
		memStore:           cfg.MemStore,
		maxTokens:          cfg.MaxTokens,
		thresholdPct:       cfg.ThresholdPct,
		recentFullCount:    4,
		runner:             nil,
		name:               cfg.Name,
		userID:             cfg.UserID,
		sessionID:          cfg.SessionID,
		outputCh:           cfg.OutputCh,
		bus:                cfg.Bus,
		projection:         cfg.Projection,
		onEvent:            cfg.OnEvent,
		partitionID:        memory.PartitionIDFromName(cfg.Name),
		systemPromptSource: cfg.SystemPromptSource,
	}

	// Build ContextCompressor from SmartCompressor.
	if cfg.Compressor != nil {
		keepRecent := cfg.Compressor.KeepRecentTasks
		cm.contextCompressor = NewContextCompressor(
			cfg.Compressor,
			cfg.MemStore,
			cfg.TokenCounter,
			cfg.MaxTokens,
			cfg.ThresholdPct,
			keepRecent,
		)
	}

	// Build BeforeModel callback chain.
	cb := model.NewCallbacks()

	// Callback -1: System prompt hot-reload.
	// When SystemPromptSource is configured, re-reads system prompt files
	// before each LLM call and replaces the system message.
	// This enables prompt tuning without restarting the agent process.
	if cm.systemPromptSource != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			freshPrompt, err := cm.systemPromptSource.Get()
			if err != nil || freshPrompt == "" {
				return nil, nil // Graceful: keep existing system prompt
			}
			// Replace or insert system message at position 0
			if len(args.Request.Messages) > 0 && args.Request.Messages[0].Role == model.RoleSystem {
				args.Request.Messages[0].Content = freshPrompt
			} else {
				// Prepend system message
				args.Request.Messages = append(
					[]model.Message{model.NewSystemMessage(freshPrompt)},
					args.Request.Messages...,
				)
			}
			return nil, nil
		})
	}

	// Unified BeforeModel callback: InjectBusInputs + ContextCompressor.
	//
	// Design: Projection is the SINGLE source of truth for the historical
	// timeline. This callback:
	//  1. TryPulls new events from EventBus and immediately persists them
	//     (MemoryStore + Projection.Append) — no "visible but not projected" state.
	//  2. Resolves all projection refs via ContextCompressor (compress if over budget).
	//  3. Extracts current-turn messages from args.Request.Messages (tool_calls/
	//     results from the current ReAct iteration that haven't been emitted yet).
	//  4. Rebuilds args.Request.Messages = [system] + history + currentTurn.
	//
	// This eliminates content-based deduplication entirely: projection refs are
	// identified by EventKey, and current-turn messages are identified by the
	// absence of [evt_KEY|type] prefixes.
	if cm.contextCompressor != nil && cm.projection != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			// Step 1: Persist new bus events into Projection immediately.
			if cm.bus != nil {
				events := cm.bus.TryPull()
				if len(events) > 0 {
					log.Infof("[BeforeModel] TryPull returned %d events, persisting to projection", len(events))
				}
				for _, evt := range events {
					if evt == nil || evt.Type != tagentevent.TypeExternalInput {
						continue
					}
					if evt.Source == tagentevent.TypeAgentOutput || evt.Source == "error" {
						continue
					}
					if evt.Message == nil {
						continue
					}
					cm.persistBusEvent(evt)
				}
			}

			// Step 2: Resolve projection → compressed historical messages.
			refs := cm.projection.GetAll()
			result := cm.contextCompressor.Compress(ctx, refs)
			cm.projection.Replace(result.RetainedRefs)

			// Step 3: Extract current-turn messages.
			// The driving request (user) is persisted into the projection at
			// turn start (persistent loop via framework emission; sub-agent via
			// session.go persistBusEvent). So whenever the projection already
			// contains a user message, the framework's re-inserted unprefixed
			// user is a duplicate/echo and must be dropped. ReAct-internal
			// messages (assistant tool_calls + tool results) are always kept.
			var filterUser bool
			for _, m := range result.Messages {
				if m.Role == model.RoleUser {
					filterUser = true
					break
				}
			}
			currentTurn := extractCurrentTurnMessages(args.Request.Messages, filterUser)

			// Step 4: Rebuild messages = [system] + history + currentTurn.
			systemMsg, _ := splitSystemMessage(args.Request.Messages)
			var rebuilt []model.Message
			if systemMsg != nil {
				rebuilt = append(rebuilt, *systemMsg)
			}
			rebuilt = append(rebuilt, result.Messages...)
			rebuilt = append(rebuilt, currentTurn...)

			args.Request.Messages = rebuilt
			return nil, nil
		})
	} else if cm.bus != nil {
		// Fallback: if no compressor configured, still inject bus events
		// (append directly to messages, legacy behavior for tests without projection).
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			events := cm.bus.TryPull()
			for _, evt := range events {
				if evt == nil || evt.Type != tagentevent.TypeExternalInput {
					continue
				}
				if evt.Source == tagentevent.TypeAgentOutput || evt.Source == "error" {
					continue
				}
				if evt.Message == nil {
					continue
				}
				msg := *evt.Message
				if msg.Role == model.RoleSystem {
					msg.Role = model.RoleUser
				}
				args.Request.Messages = append(args.Request.Messages, msg)
				log.Infof("[InjectBusInputs] injected message during ReAct: role=%s content=%s", msg.Role, msg.Content)
			}
			return nil, nil
		})
	}

	// Callback 0.4: live task board (D6). Renders currently-active async tasks
	// fresh from the registry and injects them just before the current input —
	// a recency anchor of async state that never enters projection/history, so
	// it does NOT participate in compression. Skipped when no tasks are active.
	if cm.taskController != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			if board := renderTaskBoard(cm.taskController.List()); board != "" {
				args.Request.Messages = injectTaskBoard(args.Request.Messages, board)
			}
			return nil, nil
		})
	}

	// Callback 0.45: conversation-self-heal L2 — validate + conservatively
	// repair tool_call/tool-result pairing on the FINAL assembled message list,
	// immediately before it goes to the model. This must run AFTER compression
	// (which rebuilds history from the projection, where duplicate/orphan tool
	// results can appear). It operates on args.Request.Messages only; the
	// persistent projection is untouched (L1 idempotency governs persistence).
	cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
		if repaired, n := repairToolPairing(args.Request.Messages); n > 0 {
			log.Warnf("[msgvalidate] repaired %d tool-pairing issue(s) before send", n)
			args.Request.Messages = repaired
		}
		return nil, nil
	})

	// Callback 0.5: BeforeLLM diagnostic log — print messages after compression.
	cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
		log.Debugf("[BeforeLLM] messages:\n%s", formatMessages(args.Request.Messages))
		return nil, nil
	})

	// Build LLMAgent.
	maxIters := cfg.MaxToolIters
	if maxIters <= 0 {
		maxIters = DefaultMaxToolIterations
	}
	agentOpts := []llmagent.Option{
		llmagent.WithModel(cfg.Model),
		llmagent.WithModelCallbacks(cb),
		llmagent.WithMaxToolIterations(maxIters),
		// Parallel tool execution: a single turn's multiple tool_calls run
		// concurrently. Required by the async task model so parallel command
		// spawns each wait their own sync-wait window (blocking ≈ max, not sum;
		// D2). Safe here because tagent's tools are stateless / mutex-guarded.
		llmagent.WithEnableParallelTools(true),
	}
	if cfg.SystemPrompt != "" {
		agentOpts = append(agentOpts, llmagent.WithInstruction(cfg.SystemPrompt))
	}
	if len(cfg.Tools) > 0 {
		agentOpts = append(agentOpts, llmagent.WithTools(cfg.Tools))
	}

	// Build GenerationConfig from config fields
	genConfig := model.GenerationConfig{}
	if cfg.Temperature > 0 {
		temp := cfg.Temperature
		genConfig.Temperature = &temp
	}
	if cfg.ThinkingEnabled != nil {
		genConfig.ThinkingEnabled = cfg.ThinkingEnabled
	}
	if cfg.ThinkingTokens != nil {
		genConfig.ThinkingTokens = cfg.ThinkingTokens
	}
	if cfg.ReasoningEffort != nil {
		genConfig.ReasoningEffort = cfg.ReasoningEffort
	}
	if genConfig.Temperature != nil || genConfig.ThinkingEnabled != nil ||
		genConfig.ThinkingTokens != nil || genConfig.ReasoningEffort != nil {
		agentOpts = append(agentOpts, llmagent.WithGenerationConfig(genConfig))
	}
	// ReasoningContentMode controls how reasoning_content is handled in history
	if cfg.ReasoningContentMode != "" {
		agentOpts = append(agentOpts, llmagent.WithReasoningContentMode(cfg.ReasoningContentMode))
	}

	fwAgent := llmagent.New(cfg.Name, agentOpts...)

	// Create unified Runner: LLMAgent + MemoryPlugin + SummaryPlugin + SessionService.
	runnerOpts := []runner.Option{}
	if cfg.MemPlugin != nil {
		runnerOpts = append(runnerOpts, runner.WithPlugins(
			plugin.NewSummaryPlugin(),
			cfg.MemPlugin,
		))
	}
	if cfg.SessionSvc != nil {
		runnerOpts = append(runnerOpts, runner.WithSessionService(cfg.SessionSvc))
	}
	cm.runner = runner.NewRunner(cfg.Name, fwAgent, runnerOpts...)

	return cm
}

// ShouldCallModel checks if the batch contains external_input events
// that should trigger a model call (filtering agent_output echoes
// and error events).
func (cm *ContextManager) ShouldCallModel(batch []*AgentEvent) bool {
	for _, evt := range batch {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		if evt.Source == tagentevent.TypeAgentOutput {
			continue
		}
		if evt.Source == "error" {
			continue
		}
		return true
	}
	return false
}

// BuildInvocation merges a batch of AgentEvents into a single model.Message.
// Skips: agent_output echoes (Source == "agent_output"),
// error events (Source == "error").
func (cm *ContextManager) BuildInvocation(batch []*AgentEvent) model.Message {
	var contents []string
	for _, evt := range batch {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		if evt.Source == tagentevent.TypeAgentOutput {
			continue
		}
		if evt.Source == "error" {
			continue
		}
		if evt.Message == nil {
			continue
		}
		contents = append(contents, evt.Message.Content)
	}
	if len(contents) == 0 {
		return model.NewUserMessage("")
	}
	if len(contents) == 1 {
		return model.Message{Role: model.RoleUser, Content: contents[0]}
	}
	return model.Message{Role: model.RoleUser, Content: strings.Join(contents, "\n\n---\n\n")}
}

// persistBusEvent persists an EventBus event to MemoryStore and appends it
// to the SessionProjection immediately. This ensures that all messages
// visible to the LLM are also tracked in the projection — eliminating the
// "visible but not projected" state that caused ordering bugs.
//
// The event is stored as a FullEvent with:
//   - EventKey: Snowflake-generated (using ContextManager's partitionID)
//   - EventType: inferred from message role
//   - Content/EventSummary: from the AgentEvent's Message payload
func (cm *ContextManager) persistBusEvent(evt *AgentEvent) {
	if evt == nil || evt.Message == nil {
		return
	}

	msg := *evt.Message
	// Convert RoleSystem → RoleUser: system-injected messages (e.g.,
	// [action_tool_result]) should be treated as external input by the LLM.
	if msg.Role == model.RoleSystem {
		msg.Role = model.RoleUser
	}

	eventKey := memory.NewSnowflakeEventKey(cm.partitionID, 0)
	eventType := tagentevent.ExtractEventType(msg)
	eventSummary := tagentevent.GenerateEventSummary(msg, eventType, tagentevent.DefaultOptionsForLLMContext())

	fullEvent := memory.FullEvent{
		EventKey:     eventKey,
		PartitionID:  cm.partitionID,
		EventType:    eventType,
		EventSummary: eventSummary,
		Timestamp:    evt.Timestamp.UnixMilli(),
		Content:      msg.Content,
	}

	if cm.memStore != nil {
		if err := cm.memStore.StoreEvent(eventKey, fullEvent); err != nil {
			log.Errorf("[persistBusEvent] StoreEvent failed key=%d: %v", eventKey, err)
		}
	}

	ref := memory.EventReference{
		EventKey:     eventKey,
		PartitionID:  cm.partitionID,
		EventType:    eventType,
		EventSummary: eventSummary,
		Timestamp:    evt.Timestamp.UnixMilli(),
		Role:         string(msg.Role),
	}
	cm.projection.Append(ref)

	log.Infof("[persistBusEvent] persisted bus event key=%d type=%s source=%s content=%s",
		eventKey, eventType, evt.Source, truncateForLog(msg.Content, 80))
}

// truncateForLog truncates a string for logging purposes.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// RunFlow calls runner.Run and forwards events to outputCh + bus.
// Final responses are echoed back to the EventBus as agent_output events
// (filtered by BuildInvocation to prevent self-triggering).
func (cm *ContextManager) RunFlow(ctx context.Context, msg model.Message) error {
	// Inject the task spawner so tools can delegate long-running work to the
	// task layer (sync-wait window → inline or ack). Absent → synchronous.
	if cm.taskController != nil {
		ctx = WithTaskSpawner(ctx, cm.taskController)
	}
	eventCh, err := cm.runner.Run(ctx, cm.userID, cm.sessionID, msg)
	if err != nil {
		return fmt.Errorf("runner.Run: %w", err)
	}

	for fwEvt := range eventCh {
		if fwEvt != nil && cm.triggerSource != "" {
			// Attach trigger source to the event for deterministic
			// consumer-side dispatch (meditation vs async_result vs user).
			if fwEvt.StateDelta == nil {
				fwEvt.StateDelta = make(map[string][]byte)
			}
			fwEvt.StateDelta["trigger_source"] = []byte(cm.triggerSource)
		}
		if cm.onEvent != nil && fwEvt != nil {
			cm.onEvent(fwEvt)
		}
		if cm.outputCh != nil {
			select {
			case cm.outputCh <- fwEvt:
			case <-ctx.Done():
				return nil
			}
		}
		if isFinalResponse(fwEvt) {
			outMsg := extractMessageFromEvent(fwEvt)
			if outMsg.Content != "" || outMsg.Role != "" {
				busEvt := NewExternalInputEvent(tagentevent.TypeAgentOutput, outMsg)
				if cm.bus != nil {
					cm.bus.Publish(busEvt)
				}
			}
		}
	}
	return nil
}

// SetUserIDSessionID updates the user/session context for runner.Run.
func (cm *ContextManager) SetUserIDSessionID(userID, sessionID string) {
	cm.userID = userID
	cm.sessionID = sessionID
}

// Close releases the runner resources.
func (cm *ContextManager) Close() error {
	if r, ok := cm.runner.(interface{ Close() error }); ok {
		return r.Close()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// eventTypeToRole infers a model.Role from an event type string using a
// deterministic mapping. This is used when a FullEvent's Response is nil
// (e.g., events injected via InjectBusInputs that have no LLM response).
//
// Mapping:
//
//	external_input → user
//	agent_output   → assistant
//	action_command → tool
//	thinking_plan   → assistant
//	(default)       → user (safe degradation)
func eventTypeToRole(eventType string) model.Role {
	switch eventType {
	case "external_input":
		return model.RoleUser
	case "agent_output":
		return model.RoleAssistant
	case "action_command":
		return model.RoleTool
	case "thinking_plan":
		return model.RoleAssistant
	default:
		return model.RoleUser
	}
}

func ensureUserPrompt(messages []model.Message) []model.Message {
	for _, msg := range messages {
		if msg.Role == model.RoleUser {
			return messages
		}
	}
	// No user message found — add a neutral prompt to trigger model response.
	// Don't say "如果有新任务" which misleads the LLM into thinking previous tasks are done.
	return append(messages, model.Message{
		Role:    model.RoleUser,
		Content: "请基于以上上下文继续处理。",
	})
}

func isFinalResponse(evt *event.Event) bool {
	if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
		return false
	}
	choice := evt.Response.Choices[len(evt.Response.Choices)-1]
	// Only an assistant message without tool_calls is a final response.
	// A tool RESULT (Role=tool) also has no tool_calls but must NOT be
	// treated as final — that would cause a spurious agent_output echo and
	// (in the sub-agent path) premature turn termination.
	return choice.Message.Role == model.RoleAssistant && len(choice.Message.ToolCalls) == 0
}

func extractMessageFromEvent(evt *event.Event) model.Message {
	if evt == nil || evt.Response == nil || len(evt.Response.Choices) == 0 {
		return model.Message{}
	}
	return evt.Response.Choices[len(evt.Response.Choices)-1].Message
}

// formatMessages returns a human-readable summary of messages for debug logs.
func formatMessages(messages []model.Message) string {
	var sb strings.Builder
	for i, msg := range messages {
		role := msg.Role
		if role == "" {
			role = "unknown"
		}
		content := msg.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		toolInfo := ""
		if len(msg.ToolCalls) > 0 {
			names := make([]string, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				names = append(names, tc.Function.Name)
			}
			toolInfo = fmt.Sprintf(" tool_calls=%v", names)
		}
		if msg.ToolID != "" {
			toolInfo += fmt.Sprintf(" tool_id=%s", msg.ToolID)
		}
		sb.WriteString(fmt.Sprintf("  [%d] %s: %q%s\n", i, role, content, toolInfo))
	}
	return sb.String()
}
