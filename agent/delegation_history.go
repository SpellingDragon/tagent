// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

const childInvocationIDParam = "child_invocation_id"
const localContinuationParam = "local_continuation"

type delegationRoundRecord struct {
	TaskID            string    `json:"task_id"`
	RoundID           uint64    `json:"task_round_id"`
	RoundOwner        string    `json:"round_owner,omitempty"`
	ChildInvocationID string    `json:"child_invocation_id"`
	Phase             string    `json:"phase"`
	Input             string    `json:"input"`
	Output            string    `json:"output,omitempty"`
	OutputRefs        []int64   `json:"output_refs,omitempty"`
	Error             string    `json:"error,omitempty"`
	ErrorKind         string    `json:"error_kind,omitempty"`
	StartedAt         time.Time `json:"started_at"`
	FinishedAt        time.Time `json:"finished_at,omitempty"`
}

type delegationOutputRefsKey struct{}

type delegationOutputRefs struct {
	keys []int64
}

func (cm *ContextManager) commitDelegationRound(rd delegationRoundRecord) (int64, error) {
	if cm == nil || cm.memStore == nil {
		return 0, nil
	}
	raw, err := json.Marshal(rd)
	if err != nil {
		return 0, err
	}
	key := memory.NewSnowflakeEventKey(cm.partitionID, 0)
	at := rd.StartedAt
	if !rd.FinishedAt.IsZero() {
		at = rd.FinishedAt
	}
	ev := memory.FullEvent{
		EventKey: key, PartitionID: cm.partitionID,
		EventType:    tagentevent.TypeTaskRound,
		EventSummary: fmt.Sprintf("task %s round %d %s", rd.TaskID, rd.RoundID, rd.Phase),
		Content:      string(raw), Timestamp: at.UnixMilli(),
		Metadata: map[string]string{
			tagentevent.MetaKeyAgentName: cm.name,
			"task_id":                    rd.TaskID, tagentevent.MetaKeyTaskRoundID: strconv.FormatUint(rd.RoundID, 10),
			tagentevent.MetaKeyTaskRoundOwner: rd.RoundOwner,
			childInvocationIDParam:            rd.ChildInvocationID,
		},
	}
	if err := cm.memStore.StoreEvent(key, ev); err != nil {
		return 0, fmt.Errorf("commit delegation round %s: %w", rd.Phase, err)
	}
	return key, nil
}

type admittedDetector struct {
	mu      sync.Mutex
	started bool
	closed  bool
	ctx     context.Context
	cancel  context.CancelFunc
	fn      func(context.Context) (string, error)
	cleanup func()
	dense   time.Duration
	params  map[string]string
	ch      chan task.SettleSignal
	detach  chan struct{}
	stopped chan struct{}
}

type recoveredAdmittedDetector struct {
	*admittedDetector
	spec task.TaskSpec
}

func (d *recoveredAdmittedDetector) RecoveredSpec() task.TaskSpec { return d.spec }

func newAdmittedDetector(fn func(context.Context) (string, error), cleanup func(), dense time.Duration, params map[string]string) *admittedDetector {
	ctx, cancel := context.WithCancel(context.Background())
	if dense <= 0 {
		dense = 10 * time.Second
	}
	return &admittedDetector{
		ctx: ctx, cancel: cancel, fn: fn, cleanup: cleanup, dense: dense, params: params,
		ch: make(chan task.SettleSignal, 1), detach: make(chan struct{}), stopped: make(chan struct{}),
	}
}

func (d *admittedDetector) Start() {
	d.mu.Lock()
	if d.started || d.closed {
		d.mu.Unlock()
		return
	}
	d.started = true
	d.mu.Unlock()
	go func() {
		timer := time.NewTimer(d.dense)
		defer timer.Stop()
		select {
		case <-timer.C:
			close(d.detach)
		case <-d.stopped:
		}
	}()
	go func() {
		defer d.finish()
		out, err := d.fn(d.ctx)
		sig := task.SettleSignal{Kind: task.SettleCompleted, Output: out, Err: err}
		var notAccepted *ErrContinuationNotAccepted
		var interrupted *ErrContinuationInterrupted
		if errors.As(err, &notAccepted) {
			sig.Observation = "continuation_not_accepted"
		} else if errors.As(err, &interrupted) {
			sig.Observation = "continuation_interrupted"
			sig.Kind, sig.Final = task.SettleSuspect, true
			if sig.Output != "" {
				sig.Output += "\n"
			}
			sig.Output += "续接流已中断；需按原任务轮次对账。"
		}
		d.ch <- sig
	}()
}

func (d *admittedDetector) finish() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.closed = true
	d.mu.Unlock()
	if d.cleanup != nil {
		d.cleanup()
	}
	d.cancel()
	close(d.ch)
	close(d.stopped)
}

func (d *admittedDetector) Cancel() {
	d.cancel()
	d.mu.Lock()
	started := d.started
	if !started {
		d.started = true
	}
	d.mu.Unlock()
	if !started {
		d.finish()
	}
}

func (d *admittedDetector) Settled() <-chan task.SettleSignal    { return d.ch }
func (d *admittedDetector) Detached() <-chan struct{}            { return d.detach }
func (d *admittedDetector) Stopped() <-chan struct{}             { return d.stopped }
func (d *admittedDetector) DeclarativeParams() map[string]string { return d.params }

func prepareDelegationDetector(owner *ContextManager, rounds *subagentRounds, taskID string, round uint64, roundOwner string, inv *agent.Invocation,
	input string, dense time.Duration, cleanup func(), run func(context.Context) (string, error)) (task.SettleDetector, error) {
	rd := delegationRoundRecord{
		TaskID: taskID, RoundID: round, RoundOwner: roundOwner, ChildInvocationID: inv.InvocationID,
		Phase: "begin", Input: input, StartedAt: time.Now(),
	}
	if _, err := owner.commitDelegationRound(rd); err != nil {
		if cleanup != nil {
			cleanup()
		}
		return nil, err
	}
	return delegationDetectorForRound(owner, rounds, rd, dense, cleanup, run), nil
}

func delegationDetectorForRound(owner *ContextManager, rounds *subagentRounds, rd delegationRoundRecord,
	dense time.Duration, cleanup func(), run func(context.Context) (string, error)) task.SettleDetector {
	return newAdmittedDetector(func(ctx context.Context) (string, error) {
		refs := &delegationOutputRefs{}
		out, err := run(context.WithValue(ctx, delegationOutputRefsKey{}, refs))
		rd.Phase, rd.Output, rd.OutputRefs = "finish", out, refs.keys
		rd.Error, rd.ErrorKind = "", ""
		rd.FinishedAt = time.Now()
		if err != nil {
			rd.Error = err.Error()
		}
		var notAccepted *ErrContinuationNotAccepted
		var interrupted *ErrContinuationInterrupted
		if errors.As(err, &notAccepted) {
			rd.ErrorKind = "continuation_not_accepted"
		} else if errors.As(err, &interrupted) {
			rd.ErrorKind = "continuation_interrupted"
		}
		key, commitErr := owner.commitDelegationRound(rd)
		if commitErr != nil {
			return out, errors.Join(err, commitErr)
		}
		if rounds != nil {
			rounds.addRecord(rd, key)
		}
		return out, err
	}, cleanup, dense, map[string]string{childInvocationIDParam: rd.ChildInvocationID})
}

func cloneDelegationInvocation(inv *agent.Invocation) *agent.Invocation {
	opts := inv.RunOptions
	if opts.RuntimeState != nil {
		state := make(map[string]any, len(opts.RuntimeState)+1)
		for key, value := range opts.RuntimeState {
			state[key] = value
		}
		opts.RuntimeState = state
	}
	return agent.NewInvocation(agent.WithInvocationMessage(inv.Message), agent.WithInvocationRunOptions(opts))
}

func stampDelegationParent(inv *agent.Invocation, owner *ContextManager, id string, round uint64, roundOwner string, origin map[string]string) {
	if owner == nil || id == "" {
		return
	}
	if inv.RunOptions.RuntimeState == nil {
		inv.RunOptions.RuntimeState = make(map[string]any)
	}
	inv.RunOptions.RuntimeState[ContinuationContextKey] = ContinuationContext{
		ParentAgent: owner.name, ParentInvocationID: roundOwner, ParentTaskID: id,
		ParentTaskRound: round, Origin: origin,
	}
}

func (w *AgentToolWrapper) prepareTaskStart(ctx context.Context, owner *ContextManager, rounds *subagentRounds, template *agent.Invocation) func(*task.Task) (task.TaskSpec, task.SettleDetector, error) {
	return func(tk *task.Task) (task.TaskSpec, task.SettleDetector, error) {
		spec := tk.SpecSnapshot()
		inv := cloneDelegationInvocation(template)
		if spec.Declarative != nil {
			if spec.Declarative.Params == nil {
				spec.Declarative.Params = make(map[string]string)
			}
			for key, value := range w.delegationParams(inv.InvocationID) {
				spec.Declarative.Params[key] = value
			}
		}
		stampDelegationParent(inv, owner, tk.ID, tk.RoundID(), tk.RoundOwner(), spec.Origin)
		caller, _ := execLeaseFromContext(ctx)
		lease := caller.Derive(LeaseBackground)
		d, err := prepareDelegationDetector(owner, rounds, tk.ID, tk.RoundID(), tk.RoundOwner(), inv,
			inv.Message.Content, w.DenseDuration(), lease.Release, func(runCtx context.Context) (string, error) {
				runCtx = WithSubagentTurnBudget(runCtx, SubagentTurnBudgetFromContext(ctx))
				callCtx, release, err := w.armDeclaredCall(lease.WithContext(runCtx), w.DeclaredAgentName())
				if err != nil {
					return "", err
				}
				defer release()
				return w.runAndCollect(callCtx, inv, w.DeclaredAgentName())
			})
		if prepared, ok := d.(*admittedDetector); ok {
			prepared.params = w.delegationParams(inv.InvocationID)
		}
		spec.Start = nil
		return spec, d, err
	}
}

func (w *AgentToolWrapper) delegationParams(invocationID string) map[string]string {
	_, local := w.agent.(*TagentAgent)
	return map[string]string{childInvocationIDParam: invocationID, localContinuationParam: strconv.FormatBool(local)}
}

func restoredSubagentStart(owner *ContextManager, decl task.Declarative) func(*task.Task) (task.TaskSpec, task.SettleDetector, error) {
	return func(tk *task.Task) (task.TaskSpec, task.SettleDetector, error) {
		target, lease, err := ResolveReentryDelegation(context.Background(), owner, decl.AgentName)
		if err != nil {
			return task.TaskSpec{}, nil, err
		}
		defer lease.Release()
		opts := agent.RunOptions{RuntimeState: make(map[string]any)}
		if len(decl.EventKeys) > 0 {
			if owner == nil || owner.memStore == nil {
				return task.TaskSpec{}, nil, fmt.Errorf("subagent intent has archived context but no readable store")
			}
			events, err := owner.memStore.GetEvents(decl.EventKeys)
			if err != nil {
				return task.TaskSpec{}, nil, fmt.Errorf("subagent intent context is incomplete: %w", err)
			}
			if len(events) != len(decl.EventKeys) {
				return task.TaskSpec{}, nil, fmt.Errorf("subagent intent context contains missing facts")
			}
			raw, err := serializeExternalContext(events)
			if err != nil {
				return task.TaskSpec{}, nil, err
			}
			opts.RuntimeState[ExternalContextKey] = json.RawMessage(raw)
		}
		if decl.Overrides != nil {
			raw, err := json.Marshal(decl.Overrides)
			if err != nil {
				return task.TaskSpec{}, nil, err
			}
			opts.RuntimeState[PerCallOverridesKey] = json.RawMessage(raw)
		}
		inv := agent.NewInvocation(agent.WithInvocationMessage(model.NewUserMessage(decl.MessageBody)), agent.WithInvocationRunOptions(opts))
		return target.prepareTaskStart(lease.WithContext(context.Background()), owner, &subagentRounds{cap: target.effectiveResumeRounds()}, inv)(tk)
	}
}

func queryTaskRecords(store memory.MemoryStore, partitionID int, types []string) ([]memory.FullEvent, error) {
	var records []memory.FullEvent
	for off := 0; ; off += 500 {
		query := memory.QueryOptions{EventTypes: types, Limit: 500, Offset: off}
		query.PartitionIDs = []int{partitionID}
		refs, err := store.QueryEvents(query)
		if err != nil {
			return nil, err
		}
		keys := make([]int64, 0, len(refs))
		for _, ref := range refs {
			keys = append(keys, ref.EventKey)
		}
		batch, err := store.GetEvents(keys)
		if err != nil {
			return nil, err
		}
		if len(batch) != len(keys) {
			return nil, fmt.Errorf("task record inventory contains missing facts")
		}
		records = append(records, batch...)
		if len(refs) < 500 {
			break
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].EventKey < records[j].EventKey })
	return records, nil
}

func loadDelegationRounds(store memory.MemoryStore, partitionID int, taskID string) (*subagentRounds, error) {
	rounds := &subagentRounds{}
	type identity struct {
		round      uint64
		owner      string
		invocation string
	}
	latest := make(map[identity]subagentRound)
	records, err := queryTaskRecords(store, partitionID, []string{tagentevent.TypeTaskRound})
	if err != nil {
		return nil, err
	}
	for _, ev := range records {
		if ev.Metadata["task_id"] != taskID {
			continue
		}
		var rd delegationRoundRecord
		if err := json.Unmarshal([]byte(ev.Content), &rd); err != nil {
			return nil, fmt.Errorf("decode task round %d: %w", ev.EventKey, err)
		}
		if rd.TaskID == taskID && rd.Phase == "finish" {
			latest[identity{rd.RoundID, rd.RoundOwner, rd.ChildInvocationID}] = subagentRound{
				input: rd.Input, output: rd.Output, err: rd.Error, factKey: ev.EventKey,
				invocationID: rd.ChildInvocationID, roundID: rd.RoundID, roundOwner: rd.RoundOwner, at: rd.FinishedAt,
			}
		}
	}
	for _, rd := range latest {
		rounds.rounds = append(rounds.rounds, rd)
	}
	sort.Slice(rounds.rounds, func(i, j int) bool {
		if rounds.rounds[i].roundID != rounds.rounds[j].roundID {
			return rounds.rounds[i].roundID < rounds.rounds[j].roundID
		}
		return rounds.rounds[i].factKey < rounds.rounds[j].factKey
	})
	return rounds, nil
}

// ResumePendingDelegations attaches restored subagent tasks to their original invocation and producer round.
func (ta *TagentAgent) ResumePendingDelegations() (int, error) {
	if ta == nil || ta.taskManager == nil {
		return 0, nil
	}
	var resumed int
	var failures []error
	for _, tk := range ta.taskManager.TasksSnapshot() {
		spec := tk.SpecSnapshot()
		if spec.Kind != "subagent" || spec.Declarative == nil || !tk.Status().Live() || tk.HasProducer() {
			continue
		}
		decl := spec.Declarative
		invocationID, round := decl.Params[childInvocationIDParam], tk.ProducerRound()
		if round == 0 {
			if spec.Start == nil {
				failures = append(failures, fmt.Errorf("task %s accepted intent has no lazy start factory", tk.ID))
				continue
			}
			if err := ta.taskManager.ResumeRestoredStart(tk.ID); err != nil {
				failures = append(failures, err)
			} else {
				resumed++
			}
			continue
		}
		if invocationID == "" {
			continue
		}
		child := ta.resolveContinuationOwner(decl.AgentName)
		if child == nil {
			continue
		}
		scope, err := child.continuationByID(invocationID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if scope == nil {
			continue
		}
		scope.mu.Lock()
		address := scope.state.Address
		matches := address.ParentAgent == ta.name && address.ParentInvocationID == decl.ProducerOwner && address.ParentTaskID == tk.ID && address.ParentTaskRound == round
		rd := delegationRoundRecord{TaskID: tk.ID, RoundID: round, RoundOwner: decl.ProducerOwner,
			ChildInvocationID: invocationID, Phase: "begin", Input: decl.MessageBody, StartedAt: time.UnixMilli(decl.TTLAnchorMilli)}
		if goal := scope.state.InitialGoal; goal != nil {
			rd.StartedAt = goal.Timestamp
			if goal.Message != nil {
				rd.Input = goal.Message.Content
			}
		}
		scope.mu.Unlock()
		if !matches {
			failures = append(failures, fmt.Errorf("task %s child invocation %s has a different producer address", tk.ID, invocationID))
			continue
		}
		if ta.memStore != nil {
			records, err := queryTaskRecords(ta.memStore, ta.partitionID(), []string{tagentevent.TypeTaskRound})
			if err != nil {
				failures = append(failures, err)
				continue
			}
			for _, fact := range records {
				if fact.Metadata["task_id"] != tk.ID {
					continue
				}
				var prior delegationRoundRecord
				if json.Unmarshal([]byte(fact.Content), &prior) == nil && prior.RoundID == round && prior.ChildInvocationID == invocationID {
					rd = prior
				}
			}
		}
		if child.contextManager == nil {
			failures = append(failures, fmt.Errorf("child continuation owner %q has no execution context", decl.AgentName))
			continue
		}
		bg := child.contextManager.AcquireLease(LeaseBackground)
		if err := bg.Err(); err != nil {
			bg.Release()
			failures = append(failures, err)
			continue
		}
		target := NewAgentToolWrapper(child, "", nil, nil)
		d := delegationDetectorForRound(ta.contextManager, nil, rd, target.DenseDuration(), bg.Release,
			func(ctx context.Context) (string, error) {
				callCtx := bg.WithContext(WithSubagentTurnBudget(ctx, defaultSubAgentTimeout))
				stream, found, err := child.ResumeContinuationByID(callCtx, invocationID, tk.ID, round)
				if err != nil {
					return "", err
				}
				if !found {
					return "", &ErrContinuationInterrupted{InvocationID: invocationID}
				}
				return target.collectEventStream(callCtx, invocationID, decl.AgentName, stream)
			})
		if spec.Declarative.Params == nil {
			spec.Declarative.Params = make(map[string]string)
		}
		spec.Declarative.Params[localContinuationParam] = "true"
		d = &recoveredAdmittedDetector{admittedDetector: d.(*admittedDetector), spec: spec}
		if err := ta.taskManager.BindDetector(tk.ID, d); err != nil {
			d.Cancel()
			failures = append(failures, err)
			continue
		}
		resumed++
	}
	return resumed, errors.Join(failures...)
}

func (s *taskRecordSink) restorePendingSignal(fact memory.FullEvent) bool {
	if s == nil || s.cm == nil || fact.EventType != tagentevent.TypeTaskSignal {
		return false
	}
	var raw taskSignalRecord
	if json.Unmarshal([]byte(fact.Content), &raw) != nil || raw.Event == nil || raw.ReceiptKey <= 0 {
		return false
	}
	md := make(map[string]string, len(fact.Metadata))
	for key, value := range fact.Metadata {
		md[key] = value
	}
	receipt := memory.FullEvent{EventKey: raw.ReceiptKey, PartitionID: fact.PartitionID,
		EventType: tagentevent.TypeTaskSignalReceipt, Timestamp: fact.Timestamp,
		Content: tagentevent.FormatEventKey(fact.EventKey), EventSummary: "任务信号已受理: " + raw.Event.ID, Metadata: md}
	j := &s.delivery
	j.mu.Lock()
	if j.pending == nil {
		j.pending = make(map[string]*frozenTaskDelivery)
	}
	if j.pending[raw.Event.ID] == nil {
		j.pending[raw.Event.ID] = &frozenTaskDelivery{event: raw.Event, fact: fact, receipt: receipt}
	}
	j.mu.Unlock()
	s.retainSignalKeys(fact.EventKey, receipt.EventKey)
	return true
}
