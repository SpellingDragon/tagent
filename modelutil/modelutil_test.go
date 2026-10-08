package modelutil

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/model"
)

// blockingModel returns a live channel that NEVER yields a response and never
// closes: the only way Call can finish is by honouring ctx.
type blockingModel struct {
	released chan struct{}
}

func (m *blockingModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response)
	if m.released != nil {
		go func() {
			<-m.released
			select {
			case ch <- &model.Response{Choices: []model.Choice{{Message: model.Message{
				Role: model.RoleAssistant, Content: "TOO_LATE",
			}}}}:
			case <-time.After(50 * time.Millisecond):
			}
		}()
	}
	return ch, nil
}

func (m *blockingModel) Info() model.Info { return model.Info{Name: "blocking"} }

// nilStreamModel reproduces the misbehaving provider: no error, no channel.
type nilStreamModel struct{}

func (nilStreamModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	return nil, nil
}
func (nilStreamModel) Info() model.Info { return model.Info{Name: "nil-stream"} }

// closedStreamModel returns a valid-but-empty stream (closed before any send).
type closedStreamModel struct{ nilFirst bool }

func (m closedStreamModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, 2)
	if m.nilFirst {
		ch <- nil
	}
	close(ch)
	return ch, nil
}
func (closedStreamModel) Info() model.Info { return model.Info{Name: "closed-stream"} }

// scriptedModel replays prepared responses synchronously.
type scriptedModel struct{ responses []*model.Response }

func (m scriptedModel) GenerateContent(_ context.Context, _ *model.Request) (<-chan *model.Response, error) {
	ch := make(chan *model.Response, len(m.responses)+1)
	for _, r := range m.responses {
		ch <- r
	}
	close(ch)
	return ch, nil
}
func (scriptedModel) Info() model.Info { return model.Info{Name: "scripted"} }

func assistantResp(content string) *model.Response {
	return &model.Response{Choices: []model.Choice{{Message: model.Message{Role: model.RoleAssistant, Content: content}}}}
}

func deltaResp(content string) *model.Response {
	return &model.Response{Choices: []model.Choice{{Delta: model.Message{Role: model.RoleAssistant, Content: content}}}}
}

func reasoningResp(content string) *model.Response {
	return &model.Response{Choices: []model.Choice{{Message: model.Message{
		Role: model.RoleAssistant, ReasoningContent: content,
	}}}}
}

// callWithTimeout runs Call off the test goroutine so a hang becomes a FAILURE
// instead of wedging the whole package test binary.
func callWithTimeout(t *testing.T, ctx context.Context, m model.Model, wait time.Duration) (string, error, bool) {
	t.Helper()
	type outcome struct {
		text string
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		text, err := Call(ctx, m, BuildRequest([]model.Message{model.NewUserMessage("q")}, Knobs{}))
		done <- outcome{text: text, err: err}
	}()
	select {
	case o := <-done:
		return o.text, o.err, true
	case <-time.After(wait):
		return "", nil, false
	}
}

// TestSummaryDeadline_CallHonorsParentCancel pins that a summary call fails fast when the caller's context is done.
//   - a nil stream is a determinable failure, never a hang
//
// 契约: docs/wiki/agent/compression-and-telemetry.md#summary-deadline
func TestSummaryDeadline_CallHonorsParentCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	text, err, returned := callWithTimeout(t, ctx, &blockingModel{}, 500*time.Millisecond)
	if !returned {
		t.Fatal("Call ignored the parent cancellation and stayed blocked")
	}
	if err == nil {
		t.Fatalf("cancelled call must return an error, got text=%q", text)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error must be the context cancellation, got %v", err)
	}
}

func TestSummaryDeadline_CallHonorsSubDeadline(t *testing.T) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	text, err, returned := callWithTimeout(t, ctx, &blockingModel{}, 2*time.Second)
	if !returned {
		t.Fatal("Call outlived its deadline: the summary path cannot be bounded")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline must surface as context.DeadlineExceeded, got err=%v text=%q", err, text)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("deadline must abort promptly, took %s", elapsed)
	}
}

// TestSummaryDeadline_CallAbandonsLateStream pins the "no late answer" half of the contract.
//   - once the deadline fires, a response arriving afterwards never reaches the caller
//   - the caller degrades engineering-side instead
func TestSummaryDeadline_CallAbandonsLateStream(t *testing.T) {
	release := make(chan struct{})
	m := &blockingModel{released: release}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err, returned := callWithTimeout(t, ctx, m, 2*time.Second)
	if !returned {
		t.Fatal("Call blocked past the deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
	close(release)
	time.Sleep(100 * time.Millisecond)
	text2, err2, ok2 := callWithTimeout(t, context.Background(), closedStreamModel{}, time.Second)
	if !ok2 || err2 != nil || text2 != "" {
		t.Fatalf("follow-up empty call broken by the abandoned stream: text=%q err=%v", text2, err2)
	}
}

func TestSummaryDeadline_CallNilStreamIsDeterminable(t *testing.T) {
	text, err, returned := callWithTimeout(t, context.Background(), nilStreamModel{}, 500*time.Millisecond)
	if !returned {
		t.Fatal("a nil stream hung Call; the summary deadline cannot bound a range on a nil channel")
	}
	if err == nil {
		t.Fatalf("nil stream must be a named failure, got text=%q", text)
	}
	if !errors.Is(err, ErrNilStream) {
		t.Fatalf("nil stream must report ErrNilStream, got %v", err)
	}
}

// TestSummaryDeadline_CallKeepsStreamSemantics pins what the deadline must not change.
//   - nil responses are skipped and streamed deltas win over a full message
//   - reasoning content is the last fallback and an empty stream yields ("", nil)
func TestSummaryDeadline_CallKeepsStreamSemantics(t *testing.T) {
	cases := []struct {
		name string
		m    model.Model
		want string
	}{
		{"deltas_concatenated", scriptedModel{[]*model.Response{deltaResp("a"), deltaResp("b")}}, "ab"},
		{"full_message", scriptedModel{[]*model.Response{assistantResp("full")}}, "full"},
		{"reasoning_fallback", scriptedModel{[]*model.Response{reasoningResp("think")}}, "think"},
		{"nil_responses_skipped", scriptedModel{[]*model.Response{nil, assistantResp("ok"), nil}}, "ok"},
		{"empty_stream_is_not_error", closedStreamModel{}, ""},
		{"nil_only_stream", closedStreamModel{nilFirst: true}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, err, returned := callWithTimeout(t, context.Background(), tc.m, 2*time.Second)
			if !returned {
				t.Fatal("Call did not return")
			}
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			if text != tc.want {
				t.Fatalf("text = %q, want %q", text, tc.want)
			}
		})
	}
}

// TestSummaryDeadline_CallReportsProviderError keeps the resp.Error contract.
func TestSummaryDeadline_CallReportsProviderError(t *testing.T) {
	m := scriptedModel{[]*model.Response{{Error: &model.ResponseError{Message: "boom"}}}}
	text, err, returned := callWithTimeout(t, context.Background(), m, time.Second)
	if !returned {
		t.Fatal("Call did not return")
	}
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("provider error must propagate, got err=%v text=%q", err, text)
	}
}

// TestSummaryDeadline_ConcurrentCallsIsolated guards the select loop against a shared accumulator.
//   - two simultaneous drains must not mix their content
func TestSummaryDeadline_ConcurrentCallsIsolated(t *testing.T) {
	var wg sync.WaitGroup
	errs := make([]error, 8)
	got := make([]string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			text, err := Call(context.Background(), scriptedModel{[]*model.Response{
				assistantResp(strings.Repeat("x", i+1)),
			}}, BuildRequest([]model.Message{model.NewUserMessage("q")}, Knobs{}))
			got[i], errs[i] = text, err
		}(i)
	}
	wg.Wait()
	for i := 0; i < 8; i++ {
		if errs[i] != nil {
			t.Fatalf("call %d: %v", i, errs[i])
		}
		if want := strings.Repeat("x", i+1); got[i] != want {
			t.Fatalf("call %d leaked content across goroutines: %q", i, got[i])
		}
	}
}
