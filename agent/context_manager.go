package agent

import (
	"context"
	"fmt"
	"strings"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
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
		tokenCounter:    cfg.TokenCounter,
		memStore:        cfg.MemStore,
		maxTokens:       cfg.MaxTokens,
		thresholdPct:    cfg.ThresholdPct,
		recentFullCount: 4,
		runner:          nil,
		name:            cfg.Name,
		userID:          cfg.UserID,
		sessionID:       cfg.SessionID,
		outputCh:        cfg.OutputCh,
		bus:             cfg.Bus,
		projection:      cfg.Projection,
		onEvent:         cfg.OnEvent,
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

	// Callback -1: InjectBusInputs — inject new user messages from EventBus
	// during ReAct iterations. This enables the "user → think → tool → think →
	// user → tool → think → output" event flow where new user messages are
	// inserted between ReAct iterations without waiting for RunFlow to complete.
	if cfg.Bus != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			events := cm.bus.TryPull()
			if len(events) > 0 {
				log.Infof("[InjectBusInputs] TryPull returned %d events", len(events))
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
				// Create a copy to avoid mutating the original event.
				// Convert RoleSystem → RoleUser: system-injected messages (e.g., [action_tool_result])
				// should be treated as external input by the LLM, not as system instructions.
				msg := *evt.Message
				if msg.Role == model.RoleSystem {
					msg.Role = model.RoleUser
				}
				// Append new user message to the current LLM request
				args.Request.Messages = append(args.Request.Messages, msg)
				log.Infof("[InjectBusInputs] injected message during ReAct: role=%s content=%s", msg.Role, msg.Content)
			}
			return nil, nil
		})
	}

	// Callback 0: ContextCompressor — unified compression + projection cleanup.
	// This is the ONLY BeforeModel callback that touches messages (besides
	// the diagnostic log). The old InjectEventKeys callback was removed because
	// its positional matching (message N → ref N) breaks after compression:
	// the projection no longer matches session messages 1:1, causing summary
	// ref keys to be mis-assigned to user messages and tool messages to be
	// skipped (no prefix → can't deduplicate → duplicates).
	//
	// Instead, ContextCompressor.Compress resolves ALL refs from the projection
	// (each correctly prefixed with [evt_KEY|type]) and uses content-based
	// deduplication to find new messages from ContentRequestProcessor that
	// aren't yet in the projection.
	if cm.contextCompressor != nil && cm.projection != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			refs := cm.projection.GetAll()

			result := cm.contextCompressor.Compress(ctx, refs, args.Request.Messages)
			args.Request.Messages = result.Messages
			cm.projection.Replace(result.RetainedRefs)
			return nil, nil
		})
	}

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

// RunFlow calls runner.Run and forwards events to outputCh + bus.
// Final responses are echoed back to the EventBus as agent_output events
// (filtered by BuildInvocation to prevent self-triggering).
func (cm *ContextManager) RunFlow(ctx context.Context, msg model.Message) error {
	eventCh, err := cm.runner.Run(ctx, cm.userID, cm.sessionID, msg)
	if err != nil {
		return fmt.Errorf("runner.Run: %w", err)
	}

	for fwEvt := range eventCh {
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

func injectEventKeyPrefixes(messages *[]model.Message, refs []memory.EventReference) {
	if len(refs) == 0 || len(*messages) == 0 {
		return
	}
	refIdx := 0
	for i := range *messages {
		msg := &(*messages)[i]
		if msg.Role == model.RoleSystem || msg.Role == model.RoleTool {
			continue
		}
		// Idempotent: skip messages that already have an [evt_ prefix.
		// This prevents duplicate prefixes when LLM outputs imitate the
		// prefix format and those outputs are read back from session.Events.
		if strings.HasPrefix(msg.Content, "[evt_") {
			continue
		}
		if refIdx >= len(refs) {
			break
		}
		ref := refs[refIdx]
		refIdx++
		if ref.EventKey == 0 {
			continue
		}
		eventType := ref.EventType
		if eventType == "" {
			eventType = "unknown"
		}
		msg.Content = fmt.Sprintf("[evt_%d|%s] %s", ref.EventKey, eventType, msg.Content)
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
	return len(choice.Message.ToolCalls) == 0
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
