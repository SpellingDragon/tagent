// 契约: docs/wiki/agent/agent-architecture.md#subagent-loop
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/agent/compress"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// ContinuationContextKey carries a child's exact result address in invocation state.
const ContinuationContextKey = "tagent.continuation_context"

// ContinuationContext identifies the parent task and producer round awaiting a result.
type ContinuationContext struct {
	ParentAgent        string            `json:"parent_agent,omitempty"`
	ParentInvocationID string            `json:"parent_invocation_id,omitempty"`
	ParentTaskID       string            `json:"parent_task_id,omitempty"`
	ParentTaskRound    uint64            `json:"parent_task_round,omitempty"`
	Origin             map[string]string `json:"origin,omitempty"`
}

// ContinuationBinding describes an original child invocation without granting a new round.
type ContinuationBinding struct {
	InvocationID       string `json:"invocation_id"`
	ParentAgent        string `json:"parent_agent,omitempty"`
	ParentInvocationID string `json:"parent_invocation_id,omitempty"`
	ParentTaskID       string `json:"parent_task_id,omitempty"`
	ParentTaskRound    uint64 `json:"parent_task_round,omitempty"`
	State              string `json:"state"`
}

type continuationInputDisposition struct {
	EventID     string `json:"event_id"`
	EventKey    int64  `json:"event_key,omitempty"`
	ReceiptKey  int64  `json:"receipt_key,omitempty"`
	Disposition string `json:"disposition"`
}

type continuationCheckpoint struct {
	Version                 int                            `json:"version"`
	InvocationID            string                         `json:"invocation_id"`
	Address                 ContinuationContext            `json:"address"`
	InitialGoal             *AgentEvent                    `json:"initial_goal,omitempty"`
	InitialPending          bool                           `json:"initial_pending"`
	ExecutionStarted        bool                           `json:"execution_started,omitempty"`
	RejectedBeforeExecution bool                           `json:"rejected_before_execution,omitempty"`
	Projection              []memory.EventReference        `json:"projection,omitempty"`
	FullBoundary            int64                          `json:"full_boundary,omitempty"`
	Routing                 settleRoutingSnapshot          `json:"routing"`
	ConsumedKeys            []int64                        `json:"consumed_keys,omitempty"`
	RetainedKeys            []int64                        `json:"retained_keys,omitempty"`
	InputDisposition        []continuationInputDisposition `json:"input_disposition,omitempty"`
	State                   string                         `json:"state"`
	Outcome                 string                         `json:"outcome,omitempty"`
	LastOutput              string                         `json:"last_output,omitempty"`
	LastBatchOutcome        string                         `json:"last_batch_outcome,omitempty"`
	LastBatchError          string                         `json:"last_batch_error,omitempty"`
	Error                   string                         `json:"error,omitempty"`
	ResultAcked             bool                           `json:"result_acked,omitempty"`
	Handoff                 *ContinuationContext           `json:"handoff,omitempty"`
	UpdatedAt               int64                          `json:"updated_at"`
}

type continuation struct {
	mu                 sync.Mutex
	stateOnlyMu        sync.Mutex
	state              continuationCheckpoint
	stateKey           int64
	protected          map[int64]struct{}
	bus                *EventBus
	cm                 *ContextManager
	cancel             context.CancelFunc
	running            bool
	done               chan struct{}
	listeners          map[chan *event.Event]struct{}
	runCfg             *TagentConfig
	modelRefs          *ModelRefSnapshot
	recovered          bool
	recoveryFolded     bool
	recoveryGoalClosed bool
	acceptedInputs     []*AgentEvent
	replayInputs       []*AgentEvent
}

type continuationRegistry struct {
	mu      sync.Mutex
	byID    map[string]*continuation
	closing bool
}

func newContinuationRegistry() *continuationRegistry {
	return &continuationRegistry{byID: make(map[string]*continuation)}
}

func (ta *TagentAgent) continuationRegistry() *continuationRegistry {
	ta.liveCMsMu.Lock()
	defer ta.liveCMsMu.Unlock()
	if ta.continuations == nil {
		ta.continuations = newContinuationRegistry()
	}
	return ta.continuations
}

func continuationContextFromInvocation(inv *agent.Invocation) (ContinuationContext, error) {
	var address ContinuationContext
	if inv == nil || inv.RunOptions.RuntimeState == nil {
		return address, nil
	}
	raw, exists := inv.RunOptions.RuntimeState[ContinuationContextKey]
	if !exists {
		return address, nil
	}
	var data []byte
	switch value := raw.(type) {
	case []byte:
		data = value
	case json.RawMessage:
		data = value
	case string:
		data = []byte(value)
	default:
		var err error
		data, err = json.Marshal(value)
		if err != nil {
			return address, fmt.Errorf("continuation address: %w", err)
		}
	}
	if err := json.Unmarshal(data, &address); err != nil {
		return address, fmt.Errorf("continuation address: %w", err)
	}
	if address.ParentTaskID != "" && (address.ParentAgent == "" || address.ParentTaskRound == 0) {
		return address, errors.New("continuation address needs parent agent and original producer round")
	}
	return address, nil
}

func continuationPathPart(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func (ta *TagentAgent) continuationInboxDir(invocationID string) string {
	if ta == nil {
		return ""
	}
	base := ""
	if ta.config != nil {
		base = ta.config.BusSpillDir
	}
	if base == "" && ta.memStore != nil {
		candidate := strings.TrimSpace(ta.memStore.GetStats().DataDir)
		if candidate != "" && candidate != ":memory:" && !strings.Contains(candidate, "://") && !strings.Contains(candidate, ":") {
			if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
				base = filepath.Join(candidate, ".tagent-continuations")
			}
		}
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, "continuations", continuationPathPart(ta.name), continuationPathPart(invocationID))
}

func (ta *TagentAgent) openContinuationBus(c *continuation) error {
	if c.bus != nil {
		return nil
	}
	bus, err := NewReliableEventBus(ta.continuationInboxDir(c.state.InvocationID))
	if err != nil {
		return fmt.Errorf("continuation %s inbox: %w", c.state.InvocationID, err)
	}
	if bus.Durable() {
		if _, ok := ta.memStore.(memory.EventReplayer); !ok {
			_ = bus.CloseDurable()
			return fmt.Errorf("continuation %s needs a replay-capable store", c.state.InvocationID)
		}
		if guard, ok := ta.memStore.(memory.RetentionGuard); ok {
			bus.SetRetentionGuard(guard)
			if err := bus.ArmRetentionFromInbox(); err != nil {
				_ = bus.CloseDurable()
				return err
			}
		}
	}
	c.bus = bus
	return nil
}

func (ta *TagentAgent) beginContinuation(ctx context.Context, inv *agent.Invocation, goal *AgentEvent, cfg TagentConfig, refs *ModelRefSnapshot, sourceLease *ExecLease, userID, sessionID string) (<-chan *event.Event, error) {
	defer sourceLease.Release()
	address, err := continuationContextFromInvocation(inv)
	if err != nil {
		return nil, err
	}
	id := ""
	if inv != nil {
		id = inv.InvocationID
	}
	if id == "" {
		id = uuid.NewString()
		goal.Metadata[metaKeyInvocationID] = id
	}
	r := ta.continuationRegistry()
	if _, err := ta.continuationByID(id); err != nil {
		return nil, err
	}
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return nil, ErrExecClosed
	}
	if existing := r.byID[id]; existing != nil {
		r.mu.Unlock()
		existing.mu.Lock()
		if existing.state.InitialGoal == nil {
			existing.mu.Unlock()
			return nil, errors.New("continuation has no original goal identity")
		}
		oldMessage, _ := json.Marshal(existing.state.InitialGoal.Message)
		newMessage, _ := json.Marshal(goal.Message)
		oldAddress, _ := json.Marshal(existing.state.Address)
		newAddress, _ := json.Marshal(address)
		existing.mu.Unlock()
		if string(oldMessage) != string(newMessage) || string(oldAddress) != string(newAddress) {
			return nil, errors.New("continuation invocation identity was reused with a different goal or result address")
		}
		ch, err := ta.startContinuation(ctx, existing, sourceLease, userID, sessionID)
		ta.trimAcknowledgedContinuations()
		return ch, err
	}
	c := &continuation{
		state:     continuationCheckpoint{Version: 1, InvocationID: id, Address: address, InitialGoal: goal, InitialPending: true, State: "running"},
		protected: make(map[int64]struct{}), listeners: make(map[chan *event.Event]struct{}),
		runCfg: &cfg, modelRefs: refs,
	}
	c.mu.Lock()
	r.byID[id] = c
	r.mu.Unlock()
	err = ta.openContinuationBus(c)
	if err == nil {
		err = ta.persistContinuationLocked(c)
	}
	if err == nil {
		_, err = c.bus.PublishContext(ctx, goal)
	}
	if err != nil {
		c.recovered = true
		c.state.State = "suspended"
		ta.rejectUnstartedContinuationLocked(c, err)
	}
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	ch, err := ta.startContinuation(ctx, c, sourceLease, userID, sessionID)
	if err != nil {
		c.mu.Lock()
		ta.rejectUnstartedContinuationLocked(c, err)
		c.mu.Unlock()
	}
	ta.trimAcknowledgedContinuations()
	return ch, err
}

func (ta *TagentAgent) startContinuation(ctx context.Context, c *continuation, sourceLease *ExecLease, userID, sessionID string) (<-chan *event.Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(chan *event.Event, 256)
	if c.running {
		c.listeners[out] = struct{}{}
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, &ErrContinuationInterrupted{InvocationID: c.state.InvocationID, Cause: err}
	}
	if err := ta.openContinuationBus(c); err != nil {
		return nil, err
	}
	if c.recovered && !c.recoveryFolded && c.state.Outcome == "" {
		facts, err := continuationFacts(ta.memStore, ta.partitionID())
		if err != nil {
			return nil, err
		}
		if err := ta.foldContinuationRecovery(c, facts); err != nil {
			return nil, err
		}
		c.recoveryFolded = true
	}
	if c.state.Outcome != "" {
		if err := ta.repairConsumedContinuationInboxLocked(ctx, c); err != nil {
			return nil, err
		}
		out <- continuationOutcomeEvent(c.state)
		close(out)
		return out, nil
	}
	if c.recovered && c.state.Outcome == "" {
		if reason := ta.continuationDriverFailureLocked(c); reason != "" {
			c.state.State, c.state.Outcome, c.state.Error = "failed", "failed", reason
			if err := ta.persistContinuationLocked(c); err != nil {
				return nil, err
			}
			out <- continuationOutcomeEvent(c.state)
			close(out)
			return out, nil
		}
	}
	cfg := ta.config
	if c.runCfg != nil {
		cfg = c.runCfg
	}
	if cfg == nil || cfg.Model == nil {
		return nil, fmt.Errorf("continuation %s has no model configuration", c.state.InvocationID)
	}
	privateCfg := *cfg
	privateCfg.MemoryStore = ta.memStore
	if privateCfg.MaxToolIterations <= 0 {
		privateCfg.MaxToolIterations = DefaultSubAgentMaxToolIterations
	}
	if userID == "" {
		userID = "tagent-user"
	}
	if sessionID == "" {
		sessionID = "tagent-session-" + c.state.InvocationID
	}
	projection := compress.NewSessionProjection()
	projection.Replace(append([]memory.EventReference(nil), c.state.Projection...))
	delivery := make(chan *event.Event, 100)
	callback := ta.makeOnEventCallback()
	cm := newContextManagerFromConfig(&privateCfg, ta, ta.memPlugin, ta.sessionSvc, c.bus, delivery, projection, func(evt *event.Event) {
		callback(evt)
		if isFinalResponse(evt) && evt.Response != nil && len(evt.Response.Choices) > 0 {
			c.mu.Lock()
			c.state.LastOutput = evt.Response.Choices[len(evt.Response.Choices)-1].Message.Content
			c.mu.Unlock()
		}
	})
	cm.SetUserIDSessionID(userID, sessionID)
	cm.continuationID = c.state.InvocationID
	if ta.taskManager != nil {
		cm.taskController = ta.taskManager
	}
	cm.settleSinks = ta.settleSinks
	if cm.contextCompressor != nil {
		cm.contextCompressor.SetFullBoundary(c.state.FullBoundary)
	}
	if summary, err := ta.reconcileDurableBusForCM(ctx, cm); err != nil || summary.Blocked > 0 || summary.Quarantined > 0 {
		cm.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("continuation inbox has unreconciled completion material")
	}
	privateBinding := cm.activeBinding()
	privateBinding.runCfg = &privateCfg
	privateBinding.modelRefs = c.modelRefs
	if source := sourceLease.pinnedBinding(); source != nil {
		privateBinding.addDeclaredHold(source)
		for _, child := range source.heldBindings() {
			privateBinding.addDeclaredHold(child)
		}
	}
	if c.recovered && c.state.InitialPending {
		if err := ta.persistContinuationLocked(c); err != nil {
			cm.Close()
			return nil, err
		}
		if err := ta.restoreInitialContinuationInputLocked(c); err != nil {
			cm.Close()
			return nil, err
		}
	}
	if c.recovered {
		if err := ta.restoreContinuationSignalsLocked(c); err != nil {
			cm.Close()
			return nil, err
		}
	}
	ta.bindSettleBus(c.state.InvocationID, c.bus)
	ta.settleSinks.restoreRouting(c.state.InvocationID, c.state.Routing, ta.taskManager)
	ta.settleSinks.reconcileAccepted(c.state.InvocationID, c.acceptedInputs)
	c.acceptedInputs = nil
	c.listeners[out] = struct{}{}
	c.state.ExecutionStarted = true
	if err := ta.persistContinuationLocked(c); err != nil {
		c.state.ExecutionStarted = false
		delete(c.listeners, out)
		cm.Close()
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	c.cancel, c.running, c.cm = cancel, true, cm
	c.done = make(chan struct{})
	c.state.State = "running"
	ta.registerLiveCM(cm)
	go ta.driveContinuation(runCtx, c, cm, delivery)
	return out, nil
}

func (ta *TagentAgent) rejectUnstartedContinuationLocked(c *continuation, cause error) {
	if c.running || c.state.ExecutionStarted {
		return
	}
	c.state.State, c.state.Outcome, c.state.Error = "failed", "failed", cause.Error()
	c.state.RejectedBeforeExecution = true
	c.state.InitialPending = false
	c.recoveryGoalClosed = true
	if err := ta.persistContinuationLocked(c); err != nil {
		log.Errorf("[continuation] rejected invocation checkpoint failed: %v", err)
	}
}

func (ta *TagentAgent) driveContinuation(ctx context.Context, c *continuation, cm *ContextManager, delivery chan *event.Event) {
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		for evt := range delivery {
			c.mu.Lock()
			for ch := range c.listeners {
				select {
				case ch <- evt:
				default:
				}
			}
			c.mu.Unlock()
		}
	}()
	disposition := turnStop
	for {
		disposition = ta.runAgentLoop(withInvocationID(withoutExecLease(ctx), c.state.InvocationID), c.bus, cm, loopSpec{invocationID: c.state.InvocationID})
		if err := ta.checkpointContinuation(context.Background(), cm, nil); err != nil {
			log.Errorf("[continuation] final checkpoint %s failed: %v", c.state.InvocationID, err)
			disposition = turnStop
		}
		if disposition != turnContinue || ctx.Err() != nil {
			break
		}
		if err := ta.handoffContinuationServices(ctx, c); errors.Is(err, errContinuationEventsPending) {
			continue
		} else if err != nil {
			log.Errorf("[continuation] %s handoff failed: %v", c.state.InvocationID, err)
			disposition = turnStop
		}
		break
	}
	c.mu.Lock()
	if disposition == turnContinue && ctx.Err() == nil {
		c.state.State, c.state.Outcome = "completed", "completed"
		if cm.lastBatchOutcome.status == turnFailed {
			c.state.State, c.state.Outcome, c.state.Error = "failed", "failed", cm.lastBatchOutcome.err
		}
	} else {
		c.state.State = "suspended"
		c.recovered, c.recoveryFolded = true, false
	}
	if err := ta.persistContinuationLocked(c); err != nil {
		c.state.State, c.state.Outcome = "suspended", ""
		c.recovered, c.recoveryFolded = true, false
		log.Errorf("[continuation] outcome checkpoint %s failed: %v", c.state.InvocationID, err)
	} else if c.state.Outcome != "" {
		c.runCfg, c.modelRefs = nil, nil
	}
	final := continuationOutcomeEvent(c.state)
	completed := c.state.Outcome != ""
	c.mu.Unlock()
	close(delivery)
	<-pumpDone
	ta.releaseInvocationCM(cm)
	c.mu.Lock()
	for ch := range c.listeners {
		if completed {
			select {
			case ch <- final:
			default:
				<-ch
				ch <- final
			}
		}
		close(ch)
		delete(c.listeners, ch)
	}
	c.running, c.cm, c.cancel = false, nil, nil
	close(c.done)
	c.mu.Unlock()
	if completed {
		ta.settleSinks.retire(c.state.InvocationID)
		ta.trimAcknowledgedContinuations()
	}
}

func withoutExecLease(ctx context.Context) context.Context {
	return context.WithValue(ctx, execLeaseCtxKey{}, (*ExecLease)(nil))
}

func continuationOutcomeEvent(state continuationCheckpoint) *event.Event {
	return &event.Event{
		InvocationID: state.InvocationID,
		Response:     &model.Response{Done: true, Choices: nil},
		StateDelta: map[string][]byte{
			"delegation_outcome": []byte(state.Outcome),
			"delegation_result":  []byte(state.LastOutput),
		},
	}
}

func (ta *TagentAgent) persistContinuationLocked(c *continuation) error {
	if ta.memStore == nil {
		return errors.New("continuation checkpoint requires the actual memory store")
	}
	c.state.UpdatedAt = time.Now().UnixMilli()
	raw, err := json.Marshal(c.state)
	if err != nil {
		return err
	}
	key := memory.NewSnowflakeEventKey(ta.partitionID(), 0)
	fact := memory.FullEvent{
		EventKey: key, PartitionID: ta.partitionID(), EventType: tagentevent.TypeContinuationState,
		EventSummary: "continuation " + c.state.InvocationID + " " + c.state.State,
		Content:      string(raw), Timestamp: c.state.UpdatedAt,
		Metadata: map[string]string{tagentevent.MetaKeyAgentName: ta.name, tagentevent.MetaKeyContinuationID: c.state.InvocationID},
	}
	if err := ta.memStore.StoreEvent(key, fact); err != nil {
		return fmt.Errorf("commit continuation checkpoint: %w", err)
	}
	old := c.stateKey
	c.stateKey = key
	ta.protectContinuationLocked(c)
	if old != 0 && old != key {
		if _, held := c.protected[old]; held {
			if guard, ok := ta.memStore.(memory.RetentionGuard); ok {
				guard.ReleaseKey(old)
			}
			delete(c.protected, old)
		}
	}
	return nil
}

func (ta *TagentAgent) protectContinuationLocked(c *continuation) {
	guard, ok := ta.memStore.(memory.RetentionGuard)
	if !ok || c.state.ResultAcked {
		return
	}
	if c.protected == nil {
		c.protected = make(map[int64]struct{})
	}
	keys := []int64{c.stateKey}
	for _, ref := range c.state.Projection {
		keys = append(keys, ref.EventKey)
	}
	keys = append(keys, c.state.ConsumedKeys...)
	keys = append(keys, c.state.RetainedKeys...)
	for _, input := range c.state.InputDisposition {
		keys = append(keys, input.ReceiptKey)
	}
	for _, key := range keys {
		if key > 0 {
			if _, held := c.protected[key]; !held {
				guard.ProtectKey(key)
				c.protected[key] = struct{}{}
			}
		}
	}
}

func (ta *TagentAgent) checkpointContinuation(ctx context.Context, cm *ContextManager, events []*AgentEvent) error {
	if ta == nil || cm == nil || cm.continuationID == "" {
		return nil
	}
	r := ta.continuationRegistry()
	r.mu.Lock()
	c := r.byID[cm.continuationID]
	r.mu.Unlock()
	if c == nil {
		return fmt.Errorf("continuation %s has no owner", cm.continuationID)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cm.projection != nil {
		c.state.Projection = cm.projection.GetAll()
	}
	if cm.contextCompressor != nil {
		c.state.FullBoundary = cm.contextCompressor.FullBoundary()
	}
	c.state.Routing = ta.settleSinks.snapshotRouting(cm.continuationID)
	if len(decisionInputs(events)) > 0 {
		c.state.LastBatchOutcome = cm.lastBatchOutcome.status.String()
		c.state.LastBatchError = cm.lastBatchOutcome.err
	}
	seenKeys := make(map[int64]bool, len(c.state.ConsumedKeys))
	seenIDs := make(map[string]bool, len(c.state.InputDisposition))
	for _, key := range c.state.ConsumedKeys {
		seenKeys[key] = true
	}
	for _, input := range c.state.InputDisposition {
		seenIDs[input.EventID] = true
	}
	for _, evt := range events {
		if evt == nil || seenIDs[evt.ID] {
			continue
		}
		input := continuationInputDisposition{EventID: evt.ID, Disposition: "processed"}
		if fact, err := taskInputFact(evt); err == nil {
			input.EventKey = fact.EventKey
		}
		fact, err := taskInputFact(evt)
		if err != nil || fact == nil || fact.EventKey <= 0 {
			continue
		}
		rawKey := fact.Metadata[tagentevent.MetaKeyObservationReceiptKey]
		if rawKey == "" {
			rawKey = evt.observationReceiptKey
		}
		input.ReceiptKey, _ = tagentevent.ParseEventKey(rawKey)
		if input.ReceiptKey <= 0 {
			continue
		}
		receipt, err := ta.memStore.GetEvent(input.ReceiptKey)
		if err != nil || receipt == nil || receipt.PartitionID != fact.PartitionID || receipt.EventType != tagentevent.TypeInputDisposition || receipt.Metadata[tagentevent.MetaKeyAgentName] != ta.name || receipt.Metadata[tagentevent.MetaKeyConsumedEventKey] != tagentevent.FormatEventKey(fact.EventKey) {
			continue
		}
		input.Disposition = receipt.Metadata[tagentevent.MetaKeyInputDisposition]
		if input.Disposition != "processed" && input.Disposition != "observed" && input.Disposition != "recovered" && input.Disposition != "cancelled" {
			continue
		}
		if input.EventKey > 0 && !seenKeys[input.EventKey] {
			c.state.ConsumedKeys = append(c.state.ConsumedKeys, input.EventKey)
			seenKeys[input.EventKey] = true
		}
		c.state.InputDisposition = append(c.state.InputDisposition, input)
		seenIDs[evt.ID] = true
		if c.state.InitialGoal != nil && c.state.InitialGoal.ID == evt.ID {
			c.state.InitialPending = false
		}
	}
	return ta.persistContinuationLocked(c)
}

func (ta *TagentAgent) markContinuationRecoveryInputs(cm *ContextManager, events []*AgentEvent) {
	if ta == nil || cm == nil || cm.continuationID == "" {
		return
	}
	r := ta.continuationRegistry()
	r.mu.Lock()
	c := r.byID[cm.continuationID]
	r.mu.Unlock()
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, evt := range events {
		if evt == nil {
			continue
		}
		var key int64
		if evt.claim != nil && len(evt.claim.PreparedFact) > 0 {
			var fact memory.FullEvent
			if json.Unmarshal(evt.claim.PreparedFact, &fact) == nil {
				key = fact.EventKey
			}
		}
		for _, input := range c.state.InputDisposition {
			if input.EventID == evt.ID || (key > 0 && input.EventKey == key) {
				evt.recoveryConsumed = true
				if input.ReceiptKey > 0 {
					if receipt, err := ta.memStore.GetEvent(input.ReceiptKey); err == nil && receipt != nil && receipt.EventType == tagentevent.TypeInputDisposition {
						evt.observationReceiptKey = tagentevent.FormatEventKey(input.ReceiptKey)
					}
				}
				break
			}
		}
		if !evt.recoveryConsumed && key > 0 {
			for _, consumed := range c.state.ConsumedKeys {
				if key == consumed {
					evt.recoveryConsumed = true
					break
				}
			}
		}
		if c.recoveryGoalClosed && c.state.InitialGoal != nil && c.state.InitialGoal.ID == evt.ID {
			evt.recoveryConsumed = true
		}
	}
}

// ResumeContinuation attaches to the original private loop and never creates a task round.
func (ta *TagentAgent) ResumeContinuation(ctx context.Context, parentTaskID string, originalRound uint64) (<-chan *event.Event, bool, error) {
	if ta == nil {
		return nil, false, nil
	}
	r := ta.continuationRegistry()
	r.mu.Lock()
	var found *continuation
	for _, c := range r.byID {
		c.mu.Lock()
		matches := c.state.Address.ParentTaskID == parentTaskID && c.state.Address.ParentTaskRound == originalRound
		c.mu.Unlock()
		if matches {
			if found != nil {
				r.mu.Unlock()
				return nil, true, fmt.Errorf("continuation result address %s/%d is ambiguous", parentTaskID, originalRound)
			}
			found = c
		}
	}
	closing := r.closing
	r.mu.Unlock()
	if found == nil {
		entries, _, err := ta.readContinuationCheckpoints()
		if err != nil {
			return nil, false, err
		}
		var recoveredID string
		for id, c := range entries {
			if c.state.Address.ParentTaskID == parentTaskID && c.state.Address.ParentTaskRound == originalRound {
				if recoveredID != "" {
					return nil, true, errors.New("continuation original result address is ambiguous")
				}
				recoveredID = id
			}
		}
		if recoveredID == "" {
			return nil, false, nil
		}
		return ta.ResumeContinuationByID(ctx, recoveredID, parentTaskID, originalRound)
	}
	if closing {
		return nil, true, ErrExecClosed
	}
	lease := ta.contextManager.AcquireLease(LeaseSubCall)
	defer lease.Release()
	if err := lease.Err(); err != nil {
		return nil, true, err
	}
	ch, err := ta.startContinuation(ctx, found, lease, "", "")
	ta.trimAcknowledgedContinuations()
	return ch, true, err
}

func (ta *TagentAgent) trimAcknowledgedContinuations() {
	r := ta.continuationRegistry()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.byID) <= 256 {
		return
	}
	type candidate struct {
		id string
		at int64
	}
	var candidates []candidate
	for id, c := range r.byID {
		c.mu.Lock()
		if c.state.Outcome == "completed" && c.state.ResultAcked && !c.running {
			candidates = append(candidates, candidate{id: id, at: c.state.UpdatedAt})
		}
		c.mu.Unlock()
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].at < candidates[j].at })
	for _, item := range candidates {
		if len(r.byID) <= 256 {
			break
		}
		c := r.byID[item.id]
		c.mu.Lock()
		if c.bus != nil {
			if err := c.bus.CloseDurable(); err != nil {
				c.mu.Unlock()
				continue
			}
			c.bus = nil
		}
		c.mu.Unlock()
		delete(r.byID, item.id)
	}
}

// CloseContinuationInboxes releases directory ownership after private drivers stop.
func (ta *TagentAgent) CloseContinuationInboxes() error {
	if ta == nil {
		return nil
	}
	r := ta.continuationRegistry()
	r.mu.Lock()
	entries := make([]*continuation, 0, len(r.byID))
	for _, c := range r.byID {
		entries = append(entries, c)
	}
	r.mu.Unlock()
	var result error
	for _, c := range entries {
		c.mu.Lock()
		if c.running {
			result = errors.Join(result, ErrExecUnconverged)
		} else if c.bus != nil {
			result = errors.Join(result, c.bus.CloseDurable())
			c.bus = nil
		}
		c.mu.Unlock()
	}
	return result
}

func (ta *TagentAgent) stopContinuations(ctx context.Context) error {
	if ta == nil {
		return nil
	}
	r := ta.continuationRegistry()
	r.mu.Lock()
	r.closing = true
	var done []<-chan struct{}
	for _, c := range r.byID {
		c.mu.Lock()
		if c.cancel != nil {
			c.cancel()
		}
		if c.running {
			done = append(done, c.done)
		}
		c.mu.Unlock()
	}
	r.mu.Unlock()
	for _, ch := range done {
		select {
		case <-ch:
		case <-ctx.Done():
			return fmt.Errorf("continuations: %w", ErrExecUnconverged)
		}
	}
	return nil
}
