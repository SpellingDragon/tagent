package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/plugin"
	"github.com/google/uuid"
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
//   - Compact (clean projection) → Compactor in BeforeModel callback
type ContextManager struct {
	compressor      *SmartCompressor
	compactor       *Compactor
	tokenCounter    TokenCounter
	memStore        memory.MemoryStore
	maxTokens       int
	thresholdPct    float64
	recentFullCount int

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

	Compressor   *SmartCompressor
	Compactor    *Compactor
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
// with SmartCompressor + Compactor as BeforeModel callbacks.
func NewContextManager(cfg ContextManagerConfig) *ContextManager {
	cm := &ContextManager{
		compressor:      cfg.Compressor,
		compactor:       cfg.Compactor,
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
				if evt.Source == tagentevent.TypeAgentOutput || evt.Source == "error" || evt.Source == "tool_result" {
					continue
				}
				if evt.Message == nil {
					continue
				}
				// Append new user message to the current LLM request
				args.Request.Messages = append(args.Request.Messages, *evt.Message)
				log.Infof("[InjectBusInputs] injected user message during ReAct: %s", truncateString(evt.Message.Content, 120))
			}
			return nil, nil
		})
	}

	// Callback 0: InjectEventKeys — inject [evt_KEY|type] prefix into messages.
	// This runs BEFORE SmartCompressor so that compression can parse and preserve
	// event keys from the prefix. The prefix is injected on every LLM call, not
	// just when Compactor triggers, ensuring LLM always sees event keys and can
	// pass them to sub-agent tools.
	if cfg.Projection != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			refs := cm.projection.GetAll()
			if len(refs) == 0 {
				return nil, nil
			}
			injectEventKeyPrefixes(&args.Request.Messages, refs)
			return nil, nil
		})
	}

	// Callback 1: SmartCompressor — modifies args.Request.Messages only.
	if cfg.Compressor != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			usedTokens := cm.tokenCounter.Estimate(args.Request.Messages)
			threshold := int(float64(cm.maxTokens) * cm.thresholdPct)
			if usedTokens <= threshold {
				return nil, nil
			}
			originalKeepRecent := cm.compressor.KeepRecentTasks
			defer func() { cm.compressor.KeepRecentTasks = originalKeepRecent }()
			result := cm.compressor.Compress(ctx, args.Request.Messages, nil)
			args.Request.Messages = result
			newTokens := cm.tokenCounter.Estimate(result)
			log.Infof("[ContextManager] SmartCompress: %d -> %d tokens", usedTokens, newTokens)
			return nil, nil
		})
	}

	// Callback 2: Compactor — modifies SessionProjection if still over budget.
	if cfg.Compactor != nil && cfg.Projection != nil {
		cb.RegisterBeforeModel(func(ctx context.Context, args *model.BeforeModelArgs) (*model.BeforeModelResult, error) {
			usedTokens := cm.tokenCounter.Estimate(args.Request.Messages)
			if usedTokens <= cm.maxTokens {
				return nil, nil
			}
			refs := cm.projection.GetAll()
			compacted := cm.compactor.Compact(refs)
			if len(compacted) >= len(refs) {
				return nil, nil
			}
			log.Warnf("[ContextManager] Compactor: refs %d -> %d", len(refs), len(compacted))
			cm.projection.Replace(compacted)
			messages := cm.BuildMessages(ctx, compacted)
			messages = ensureUserPrompt(messages)
			cm.InjectEventKeys(&messages, compacted)
			args.Request.Messages = messages
			return nil, nil
		})
	}

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
	if cfg.Temperature > 0 {
		temp := cfg.Temperature
		agentOpts = append(agentOpts, llmagent.WithGenerationConfig(model.GenerationConfig{
			Temperature: &temp,
		}))
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

// BuildMessages converts EventReferences to model.Messages.
// Recent references are resolved via MemoryStore; older ones use EventSummary.
func (cm *ContextManager) BuildMessages(ctx context.Context, refs []memory.EventReference) []model.Message {
	startFull := 0
	if len(refs) > cm.recentFullCount {
		startFull = len(refs) - cm.recentFullCount
	}
	messages := make([]model.Message, 0, len(refs))
	for i, ref := range refs {
		var msg model.Message
		if i >= startFull {
			msg = cm.resolveReferenceToMessage(ctx, ref)
		} else {
			msg = model.Message{
				Role:    model.Role(ref.Role),
				Content: ref.EventSummary,
			}
		}
		messages = append(messages, msg)
	}
	return messages
}

// InjectEventKeys adds [evt_KEY|type] prefix to messages.
func (cm *ContextManager) InjectEventKeys(messages *[]model.Message, refs []memory.EventReference) {
	injectEventKeyPrefixes(messages, refs)
}

// ShouldCallModel checks if the batch contains external_input events
// that should trigger a model call (filtering agent_output echoes,
// error events, and tool_result bridge events).
func (cm *ContextManager) ShouldCallModel(batch []*AgentEvent) bool {
	for _, evt := range batch {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		if evt.Source == tagentevent.TypeAgentOutput {
			continue
		}
		if evt.Source == "error" || evt.Source == "tool_result" {
			continue
		}
		return true
	}
	return false
}

// BuildInvocation merges a batch of AgentEvents into a single model.Message.
// Skips: agent_output echoes (Source == "agent_output"),
// error events (Source == "error"), tool_result bridge events (Source == "tool_result").
func (cm *ContextManager) BuildInvocation(batch []*AgentEvent) model.Message {
	var contents []string
	for _, evt := range batch {
		if evt == nil || evt.Type != tagentevent.TypeExternalInput {
			continue
		}
		if evt.Source == tagentevent.TypeAgentOutput {
			continue
		}
		if evt.Source == "error" || evt.Source == "tool_result" {
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
// For action_command events, also publishes a tool_result bridge event to EventBus.
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
		// Bridge action_command events to EventBus as tool_result
		if fwEvt != nil && isActionCommand(fwEvt) {
			cm.bridgeToolResultToBus(fwEvt)
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

// isActionCommand checks if a framework event is an action_command (tool execution result).
func isActionCommand(evt *event.Event) bool {
	if evt == nil || evt.StateDelta == nil {
		return false
	}
	if typeBytes, ok := evt.StateDelta["event_type"]; ok && len(typeBytes) > 0 {
		return string(typeBytes) == tagentevent.TypeActionCommand
	}
	// Fallback: check Response message role
	if evt.Response != nil && len(evt.Response.Choices) > 0 {
		return evt.Response.Choices[0].Message.Role == model.RoleTool
	}
	return false
}

// bridgeToolResultToBus publishes a tool_result AgentEvent to EventBus.
// This implements invariant ③: tool results flow back through the event bus.
func (cm *ContextManager) bridgeToolResultToBus(evt *event.Event) {
	if cm.bus == nil {
		return
	}
	msg := extractMessageFromEvent(evt)
	if msg.Content == "" {
		return
	}
	busEvt := &AgentEvent{
		ID:        uuid.NewString(),
		Type:      tagentevent.TypeExternalInput,
		Source:    "tool_result",
		Timestamp: time.Now(),
		Message:   &msg,
		Metadata:  make(map[string]any),
	}
	cm.bus.Publish(busEvt)
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
// Internal helpers (migrated from preprocessor.go)
// ---------------------------------------------------------------------------

func (cm *ContextManager) resolveReferenceToMessage(ctx context.Context, ref memory.EventReference) model.Message {
	if cm.memStore != nil {
		full, err := cm.memStore.GetEvent(ref.EventKey)
		if err == nil && full != nil && full.Response != nil && len(full.Response.Choices) > 0 {
			return full.Response.Choices[0].Message
		}
		log.Warnf("[ContextManager] failed to resolve event key=%d: %v", ref.EventKey, err)
	}
	return model.Message{
		Role:    model.Role(ref.Role),
		Content: ref.EventSummary,
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
	return append(messages, model.Message{
		Role:    model.RoleUser,
		Content: "（以上是对话历史摘要。如果有新任务，请告诉我。）",
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
