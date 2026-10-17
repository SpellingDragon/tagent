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
)

const childInvocationIDParam = "child_invocation_id"

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
			"task_id":                    rd.TaskID, "task_round_id": strconv.FormatUint(rd.RoundID, 10),
			"round_owner":          rd.RoundOwner,
			childInvocationIDParam: rd.ChildInvocationID,
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
		d.ch <- task.SettleSignal{Kind: task.SettleCompleted, Output: out, Err: err}
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
	return newAdmittedDetector(func(ctx context.Context) (string, error) {
		refs := &delegationOutputRefs{}
		out, err := run(context.WithValue(ctx, delegationOutputRefsKey{}, refs))
		rd.Phase, rd.Output, rd.OutputRefs = "finish", out, refs.keys
		rd.FinishedAt = time.Now()
		if err != nil {
			rd.Error = err.Error()
		}
		key, commitErr := owner.commitDelegationRound(rd)
		if commitErr != nil {
			return out, errors.Join(err, commitErr)
		}
		if rounds != nil {
			rounds.addRecord(rd, key)
		}
		return out, err
	}, cleanup, dense, map[string]string{childInvocationIDParam: inv.InvocationID}), nil
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
			spec.Declarative.Params[childInvocationIDParam] = inv.InvocationID
		}
		stampDelegationParent(inv, owner, tk.ID, tk.RoundID(), tk.RoundOwner(), spec.Origin)
		caller, _ := execLeaseFromContext(ctx)
		lease := caller.Derive(LeaseBackground)
		d, err := prepareDelegationDetector(owner, rounds, tk.ID, tk.RoundID(), tk.RoundOwner(), inv,
			inv.Message.Content, w.DenseDuration(), lease.Release, func(runCtx context.Context) (string, error) {
				callCtx, release, err := w.armDeclaredCall(lease.WithContext(runCtx), w.DeclaredAgentName())
				if err != nil {
					return "", err
				}
				defer release()
				return w.runAndCollect(callCtx, inv, w.DeclaredAgentName())
			})
		spec.Start = nil
		return spec, d, err
	}
}

func queryTaskRecords(store memory.MemoryStore, partitionID int, types []string) ([]memory.FullEvent, error) {
	var records []memory.FullEvent
	for off := 0; ; off += 500 {
		refs, err := store.QueryEvents(memory.QueryOptions{
			PartitionIDs: []int{partitionID}, EventTypes: types, Limit: 500, Offset: off,
		})
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
			rounds.rounds = append(rounds.rounds, subagentRound{
				input: rd.Input, output: rd.Output, err: rd.Error, factKey: ev.EventKey,
				invocationID: rd.ChildInvocationID, roundID: rd.RoundID, roundOwner: rd.RoundOwner, at: rd.FinishedAt,
			})
		}
	}
	return rounds, nil
}
