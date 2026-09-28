// 本文件负责回合结果的三态归约（完成／失败／取消）与取消时的断点保留：已认领的输入在流中途
// 取消时不得 ack。
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
// 契约: docs/wiki/platform/reincarnation-notice.md#breakpoint
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/memory"
	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// TestReduceTurnOutcome 钉住 纯判定表覆盖全部通道并锁住优先级：启动错误、响应错误、取消、正常结束。
func TestReduceTurnOutcome(t *testing.T) {
	t.Run("normal end is completed", func(t *testing.T) {
		require.Equal(t, turnCompleted, reduceTurnOutcome(nil, "", false).status)
	})
	t.Run("start error is failed", func(t *testing.T) {
		oc := reduceTurnOutcome(context.DeadlineExceeded, "", false)
		require.Equal(t, turnFailed, oc.status)
		require.Contains(t, oc.err, "deadline exceeded")
	})
	t.Run("response error is failed", func(t *testing.T) {
		oc := reduceTurnOutcome(nil, "server_error: rate limited", false)
		require.Equal(t, turnFailed, oc.status)
		require.Equal(t, "server_error: rate limited", oc.err)
	})
	t.Run("cancellation outranks errors and forms no completion", func(t *testing.T) {
		oc := reduceTurnOutcome(context.Canceled, "server_error: partial", true)
		require.Equal(t, turnCancelled, oc.status)
		require.Empty(t, oc.err)
	})
	t.Run("start error outranks response error", func(t *testing.T) {
		oc := reduceTurnOutcome(context.DeadlineExceeded, "ignored", false)
		require.Equal(t, turnFailed, oc.status)
		require.Contains(t, oc.err, "deadline exceeded")
		require.NotContains(t, oc.err, "ignored")
	})
	t.Run("error summary is bounded", func(t *testing.T) {
		long := make([]byte, maxErrSummary+200)
		for i := range long {
			long[i] = 'x'
		}
		oc := failedOutcome(string(long))
		require.LessOrEqual(t, len(oc.err), maxErrSummary+len("…(truncated)"))
		require.Contains(t, oc.err, "truncated")
	})
}

// TestRunFlow_ResponseErrorReducesFailed 钉住 模型 API 失败以携带 Response.Error 的事件到达，而 RunFlow 自身返回 nil（传输层是好的）：装配侧必须读出该错误并记为失败，否则循环会把 nil 当成成功。同步跑在测试协程上，读结果无竞争。
func TestRunFlow_ResponseErrorReducesFailed(t *testing.T) {
	outputCh := make(chan *event.Event, 100)
	m := &requestCapturingModel{resp: &model.Response{
		ID:    "err-resp",
		Done:  true,
		Error: &model.ResponseError{Type: "server_error", Message: "upstream exploded"},
	}}
	cm := newTestContextManager("resp-err", m, nil, outputCh, NewEventBus())

	err := cm.RunFlow(context.Background(), model.Message{Role: model.RoleUser, Content: "hi"})
	require.NoError(t, err, "transport return stays nil — the failure is carried in the stream, not the return")
	oc := cm.LastTurnOutcome()
	require.Equal(t, turnFailed, oc.status, "a response-internal error must reduce to failed, not the old silent 'completed'")
	require.Contains(t, oc.err, "upstream exploded", "the bounded summary is retained for the completion (§5.2)")
}

// TestRunFlow_NormalDrainCompleted 钉住 普通有产出的回合仍归约为完成——归约不得扩大化，把每个回合都判成失败。
func TestRunFlow_NormalDrainCompleted(t *testing.T) {
	outputCh := make(chan *event.Event, 100)
	m := &requestCapturingModel{resp: &model.Response{
		ID:      "ok",
		Done:    true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "a real answer"}}},
	}}
	cm := newTestContextManager("normal", m, nil, outputCh, NewEventBus())

	require.NoError(t, cm.RunFlow(context.Background(), model.Message{Role: model.RoleUser, Content: "hi"}))
	require.Equal(t, turnCompleted, cm.LastTurnOutcome().status)
}

// TestRunFlow_MidStreamCancelReducesCancelled 钉住 有产出的回合被关停取消截断时的确定性判别：排水路径既要报出上下文取消，也要记录一条取消结局——只返回 nil 会让循环把一个没到终态的回合的持久输入 ack 掉。用协程在投递阻塞时取消来同步驱动，避免退化重试掩蔽结果。
func TestRunFlow_MidStreamCancelReducesCancelled(t *testing.T) {
	outputCh := make(chan *event.Event)
	m := &requestCapturingModel{resp: &model.Response{
		ID:      "ok",
		Done:    true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "a real answer"}}},
	}}
	cm := newTestContextManager("cancel-drain", m, nil, outputCh, NewEventBus())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := cm.RunFlow(ctx, model.Message{Role: model.RoleUser, Content: "hi"})
	require.ErrorIs(t, err, context.Canceled, "cancellation must be surfaced, not swallowed as a nil 'completed' return")
	require.Equal(t, turnCancelled, cm.LastTurnOutcome().status, "a cut-short turn reduces to cancelled, never completed")
}

// TestRunEventLoop_MidStreamCancelRetainsClaim 钉住 端到端一步：回合被关停取消截断时，持久批次的领取必须保持未确认。
// - 循环在取消结局上必须先返回，而不是先去批量收尾；
// - 取消的归约本身由同步那条用例判定，这条只锁循环侧的先后次序。
func TestRunEventLoop_MidStreamCancelRetainsClaim(t *testing.T) {
	m := &requestCapturingModel{resp: &model.Response{
		ID:      "ok",
		Done:    true,
		Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: "reply"}}},
	}}
	bus, err := NewReliableEventBus(t.TempDir())
	require.NoError(t, err)
	outputCh := make(chan *event.Event)
	store := memory.NewInMemoryStore()
	ta := newDurableAgentWithStore("cancel-retains", m, store, outputCh, bus)

	bus.Publish(NewExternalInputEvent("user", model.Message{Role: model.RoleUser, Content: "hello-durable"}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ta.runEventLoop(ctx, bus, ta.contextManager)

	deadline := time.After(4 * time.Second)
	for m.requestCount() < 1 {
		select {
		case <-deadline:
			t.Fatal("durable batch never reached the model (commit path broken?)")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()

	ackWatch := time.Now().Add(2 * time.Second)
	for time.Now().Before(ackWatch) && bus.DurablePending() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	require.NotZero(t, bus.DurablePending(), "§5.1: a mid-turn shutdown cancellation must retain the claim, not ack a turn that reached no terminal state")
}
