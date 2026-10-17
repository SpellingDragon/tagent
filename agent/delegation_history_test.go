package agent

import (
	"context"
	"encoding/json"
	"errors"
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
