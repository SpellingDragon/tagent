// 契约: docs/wiki/agent/task-lifecycle.md#restore-rebuild
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/agent/task"
	tagentevent "github.com/SpellingDragon/tagent/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

type roundFailStore struct {
	*memory.InMemoryStore
	phase string
}

func (s *roundFailStore) StoreEvent(key int64, ev memory.FullEvent) error {
	if ev.EventType == tagentevent.TypeTaskRound {
		var rd delegationRoundRecord
		if err := json.Unmarshal([]byte(ev.Content), &rd); err != nil {
			return err
		}
		if rd.Phase == s.phase {
			return errors.New("round write refused")
		}
	}
	return s.InMemoryStore.StoreEvent(key, ev)
}

// TestDelegationHistoryCommitGuardsProducer 钉住 开始记录失败时不运行，结束记录失败时不回报成功。
func TestDelegationHistoryCommitGuardsProducer(t *testing.T) {
	for _, phase := range []string{"begin", "finish"} {
		t.Run(phase, func(t *testing.T) {
			store := &roundFailStore{InMemoryStore: memory.NewInMemoryStore(), phase: phase}
			owner := &ContextManager{name: "parent", memStore: store, partitionID: sinkPartition}
			inv := agent.NewInvocation(agent.WithInvocationMessage(model.NewUserMessage("real input")))
			var runs, releases atomic.Int64
			d, err := prepareDelegationDetector(owner, &subagentRounds{}, "task", 1, "owner", inv, "real input", time.Second,
				func() { releases.Add(1) }, func(context.Context) (string, error) { runs.Add(1); return "real output", nil })
			if phase == "begin" {
				require.Error(t, err)
				require.Nil(t, d)
				require.Zero(t, runs.Load())
			} else {
				require.NoError(t, err)
				d.(task.ActivatableDetector).Start()
				sig := <-d.Settled()
				require.Equal(t, "real output", sig.Output)
				require.ErrorContains(t, sig.Err, "round write refused")
				<-d.Stopped()
				require.Equal(t, int64(1), runs.Load())
			}
			require.Equal(t, int64(1), releases.Load())
		})
	}
}

// TestAdmittedDetectorCancellationWaitsForProducer 钉住 取消是通知，生产者返回后才关闭停止凭据与释放资源。
func TestAdmittedDetectorCancellationWaitsForProducer(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var released atomic.Int64
	d := newAdmittedDetector(func(context.Context) (string, error) { close(started); <-finish; return "done", nil },
		func() { released.Add(1) }, time.Second, nil)
	d.Start()
	<-started
	d.Cancel()
	select {
	case <-d.Stopped():
		t.Fatal("cancellation reported a producer stop")
	default:
	}
	require.Zero(t, released.Load())
	close(finish)
	<-d.Stopped()
	require.Equal(t, int64(1), released.Load())
}

type notAcceptedError struct{}

func (notAcceptedError) Error() string   { return "request rejected before acceptance" }
func (notAcceptedError) RetrySafe() bool { return true }

type safeRetryAgent struct {
	mockAgent
	invocations []string
	onFailure   func()
	unsafe      bool
	partial     bool
}

func (a *safeRetryAgent) RemoteAgent() bool { return true }
func (a *safeRetryAgent) Run(_ context.Context, inv *agent.Invocation) (<-chan *event.Event, error) {
	a.invocations = append(a.invocations, inv.InvocationID)
	if len(a.invocations) == 1 && !a.partial {
		if a.onFailure != nil {
			a.onFailure()
		}
		if a.unsafe {
			return nil, errors.New("transport failed after send")
		}
		return nil, notAcceptedError{}
	}
	ch := make(chan *event.Event, 2)
	ch <- &event.Event{Response: &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage("output")}}}}
	if a.partial {
		ch <- &event.Event{Response: &model.Response{Error: &model.ResponseError{Type: "stream_error", Message: "stream lost"}}}
	}
	close(ch)
	return ch, nil
}

// TestRemoteRetryRequiresNonAcceptanceCredential 钉住 只有明确未受理的错误重试，重试保留原调用与声明目标。
func TestRemoteRetryRequiresNonAcceptanceCredential(t *testing.T) {
	old := &safeRetryAgent{mockAgent: mockAgent{name: "remote"}}
	w := NewAgentToolWrapper(old, "remote", nil, nil)
	owner := ownerRouting(t, "remote", w)
	lease := owner.AcquireLease(LeaseTurn)
	defer lease.Release()
	next := &safeRetryAgent{mockAgent: mockAgent{name: "remote"}}
	old.onFailure = func() {
		owner.PublishExecutor(&leaseCountingRunner{id: "next"}, ContextManagerConfig{Name: "next", Tools: []trpctool.Tool{NewAgentToolWrapper(next, "remote", nil, nil)}})
	}
	inv := agent.NewInvocation(agent.WithInvocationMessage(model.NewUserMessage("one request")))
	out, err := w.runAndCollect(lease.WithContext(context.Background()), inv, "remote")
	require.NoError(t, err)
	require.Equal(t, "output", out)
	require.Equal(t, []string{inv.InvocationID, inv.InvocationID}, old.invocations)
	require.Empty(t, next.invocations)
	unsafe := &safeRetryAgent{mockAgent: mockAgent{name: "remote"}, unsafe: true}
	_, err = NewAgentToolWrapper(unsafe, "remote", nil, nil).runAndCollect(context.Background(), inv, "remote")
	require.Error(t, err)
	require.Len(t, unsafe.invocations, 1)
}

// TestDelegationPartialOutputDoesNotHideStreamFailure 钉住 已产出的正文不能覆盖后续流错误，也不能据此重复副作用。
func TestDelegationPartialOutputDoesNotHideStreamFailure(t *testing.T) {
	a := &safeRetryAgent{mockAgent: mockAgent{name: "remote"}, partial: true}
	inv := agent.NewInvocation(agent.WithInvocationMessage(model.NewUserMessage("one request")))
	out, err := NewAgentToolWrapper(a, "remote", nil, nil).runAndCollect(context.Background(), inv, "remote")
	require.Equal(t, "output", out)
	require.ErrorContains(t, err, "stream lost")
	require.Len(t, a.invocations, 1)
}

// TestDelegationBindingAcceptancePrecedesProducer 钉住 实际调用身份与开始记录准备完成后，绑定拒绝仍保持零生产者。
func TestDelegationBindingAcceptancePrecedesProducer(t *testing.T) {
	store := memory.NewInMemoryStore()
	owner := &ContextManager{name: "parent", memStore: store, partitionID: sinkPartition}
	child := &mockAgent{name: "child"}
	w := NewAgentToolWrapper(child, "child", nil, store)
	w.SetParentCM(owner)
	var seen string
	tm := task.NewTaskManager(task.TaskManagerConfig{OnSpawnAccepted: func(*task.Task) bool { return true }, OnStartAccepted: func(tk *task.Task) bool {
		seen = tk.SpecSnapshot().Declarative.Params[childInvocationIDParam]
		facts, err := queryTaskRecords(store, sinkPartition, []string{tagentevent.TypeTaskRound})
		require.NoError(t, err)
		require.Len(t, facts, 1)
		return false
	}})
	defer tm.Close(context.Background())
	out, err := w.Call(task.WithTaskSpawner(context.Background(), tm), []byte(`{"request":"real input"}`))
	require.NoError(t, err)
	require.Contains(t, out, "未被任务层纳管")
	require.NotEmpty(t, seen)
	require.Nil(t, child.lastInv)
}

// TestDelegationStreamRequiresCommittedLocalOutcome 钉住 本地流的普通正文与关闭不是成功凭据，真实空结果也不会被替换。
func TestDelegationStreamRequiresCommittedLocalOutcome(t *testing.T) {
	w := NewAgentToolWrapper(&TagentAgent{name: "child"}, "child", nil, nil)
	stream := make(chan *event.Event, 1)
	stream <- &event.Event{Response: &model.Response{Done: true, Choices: []model.Choice{{Message: model.NewAssistantMessage("partial")}}}}
	close(stream)
	out, err := w.collectEventStream(context.Background(), "child-invocation", "child", stream)
	require.Equal(t, "partial", out)
	var interrupted *ErrContinuationInterrupted
	require.ErrorAs(t, err, &interrupted)
	complete := make(chan *event.Event, 1)
	complete <- &event.Event{Response: &model.Response{Done: true}, StateDelta: map[string][]byte{"delegation_outcome": []byte("completed"), "delegation_result": {}}}
	close(complete)
	out, err = w.collectEventStream(context.Background(), "child-invocation", "child", complete)
	require.NoError(t, err)
	require.Empty(t, out)
}

type historyMockAgent struct {
	mockAgent
	surface []trpctool.Tool
}

func (a *historyMockAgent) Tools() []trpctool.Tool { return a.surface }

// TestDelegationResumeUsesCurrentSurfaceAndDurableHistory 钉住 续跑读取当前面轮数，缺少召回能力时保留全文，有召回能力时返回可读票据。
func TestDelegationResumeUsesCurrentSurfaceAndDurableHistory(t *testing.T) {
	for _, readable := range []bool{false, true} {
		t.Run(fmt.Sprint(readable), func(t *testing.T) {
			old := NewAgentToolWrapper(&mockAgent{name: "plan"}, "old", nil, nil)
			old.SetResumeContextRounds(1)
			owner := ownerRouting(t, "plan", old)
			current := &historyMockAgent{mockAgent: mockAgent{name: "plan"}}
			if readable {
				current.surface = []trpctool.Tool{percallLeafTool{name: "memory_recall"}}
			}
			selected := NewAgentToolWrapper(current, "selected", nil, owner.memStore)
			selected.SetResumeContextRounds(2)
			owner.PublishExecutor(&leaseCountingRunner{id: "selected"}, ContextManagerConfig{Name: owner.name, Tools: []trpctool.Tool{selected}})
			fullInput, fullOutput := strings.Repeat("long input ", 40), strings.Repeat("full output ", 400)+"unique tail"
			var newestKey int64
			for round := uint64(1); round <= 3; round++ {
				rd := delegationRoundRecord{TaskID: "task", RoundID: round, RoundOwner: "old-owner", ChildInvocationID: fmt.Sprintf("old-%d", round),
					Phase: "finish", Input: fullInput, Output: fullOutput, StartedAt: time.Now(), FinishedAt: time.Now()}
				var err error
				newestKey, err = owner.commitDelegationRound(rd)
				require.NoError(t, err)
			}
			tm := task.NewTaskManager(task.TaskManagerConfig{})
			defer tm.Close(context.Background())
			tk := tm.RestoreTask("task", task.TaskSpec{Kind: "subagent", Desc: "plan", TTL: time.Hour,
				Declarative: &task.Declarative{Kind: "subagent", AgentName: "plan", RoundID: 3, ProducerRound: 3,
					Params: map[string]string{childInvocationIDParam: "old-3"}},
				ResumeFn: subagentResumeClosure(owner, "plan", &subagentRounds{})}, time.Now(), task.TaskCompleted)
			res, err := tm.Resume(task.WithRoundOwner(context.Background(), "new-owner"), tk.ID, "new input")
			require.NoError(t, err)
			require.True(t, res.Settled)
			require.NoError(t, res.Signal.Err)
			inv := current.lastInv
			require.NotNil(t, inv)
			entries := restoredContext(t, inv)
			require.Len(t, entries, 2)
			require.Contains(t, entries[1].EventSummary, fullInput)
			if readable {
				require.Contains(t, entries[1].EventSummary, "[evt_"+tagentevent.FormatEventKey(newestKey)+"|task_round]")
				require.NotContains(t, entries[1].EventSummary, "unique tail")
			} else {
				require.Contains(t, entries[1].EventSummary, fullOutput)
			}
			require.NotEqual(t, "old-3", inv.InvocationID)
			require.Equal(t, inv.InvocationID, tk.SpecSnapshot().Declarative.Params[childInvocationIDParam])
			address, err := continuationContextFromInvocation(inv)
			require.NoError(t, err)
			require.Equal(t, "task", address.ParentTaskID)
			require.Equal(t, uint64(4), address.ParentTaskRound)
			require.Equal(t, "new-owner", address.ParentInvocationID)
		})
	}
}

// TestDelegationHistoryFoldsRecoveredFinishesByOriginalRound 钉住 原轮恢复追加的结算不会占用两个历史轮次。
func TestDelegationHistoryFoldsRecoveredFinishesByOriginalRound(t *testing.T) {
	store := memory.NewInMemoryStore()
	cm := &ContextManager{name: "parent", memStore: store, partitionID: sinkPartition}
	rd := delegationRoundRecord{TaskID: "task", RoundID: 1, RoundOwner: "owner", ChildInvocationID: "child", Phase: "finish", Input: "input", Output: "partial", StartedAt: time.Now(), FinishedAt: time.Now()}
	_, err := cm.commitDelegationRound(rd)
	require.NoError(t, err)
	rd.Output, rd.Error = "full output", ""
	key, err := cm.commitDelegationRound(rd)
	require.NoError(t, err)
	rounds, err := loadDelegationRounds(store, sinkPartition, "task")
	require.NoError(t, err)
	prior := rounds.recent(5)
	require.Len(t, prior, 1)
	require.Equal(t, "full output", prior[0].output)
	require.Equal(t, key, prior[0].factKey)
}
