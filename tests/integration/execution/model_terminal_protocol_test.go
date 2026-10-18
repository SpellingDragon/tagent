package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/model"
)

// TestModelTerminalProtocol checks the single terminal reduction of the model protocol over channel and iterator shapes.
// - a valid tool terminal reduces to completed and stays dispatchable, a plain terminal reduces to completed without dispatch
// - a usage-only tail packet merges its usage and never replaces the retained body
// - a stream carrying only partial output reduces to partial-only
// - a stream error reduces to failed, a cancellation error reduces to cancelled
// - the producer end is end evidence and never a second terminal
// - a stream without terminal evidence reduces to unknown and alerts
// - a response observed after the terminal stood never revives the terminal and never dispatches tools
// - a nil response item reduces to failed and alerts, a nil stream reduces to failed and alerts
// - a cancelled context closes the reduction as cancelled without waiting for the producer to stop
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestModelTerminalProtocol(t *testing.T) {
	doneBody := func(content string) *model.Response {
		return &model.Response{
			Choices: []model.Choice{{Message: model.Message{Content: content}}},
			Done:    true,
		}
	}
	toolTurn := func() *model.Response {
		return &model.Response{
			Choices: []model.Choice{{Message: model.Message{ToolCalls: []model.ToolCall{{
				ID:       "call-1",
				Function: model.FunctionDefinitionParam{Name: "lookup", Arguments: []byte(`{"q":"x"}`)},
			}}}}},
			Done: true,
		}
	}
	partialBody := func(content string) *model.Response {
		return &model.Response{
			Choices:   []model.Choice{{Message: model.Message{Content: content}}},
			IsPartial: true,
		}
	}
	usageTail := func(usage *model.Usage) *model.Response {
		return &model.Response{Usage: usage}
	}
	failResp := func(errorType string) *model.Response {
		return &model.Response{
			Error: &model.ResponseError{Message: "stream broke", Type: errorType},
			Done:  true,
		}
	}

	type result struct {
		out    model.Outcome
		alerts []string
	}
	runBoth := func(resps []*model.Response) map[string]result {
		results := make(map[string]result, 2)
		var channelAlerts []string
		ch := make(chan *model.Response, len(resps))
		for _, resp := range resps {
			ch <- resp
		}
		close(ch)
		channelOut := model.ReduceChannel(context.Background(), ch, func(a model.Alert) {
			channelAlerts = append(channelAlerts, a.Reason)
		})
		results["channel"] = result{out: channelOut, alerts: channelAlerts}
		var iterAlerts []string
		seq := func(yield func(*model.Response) bool) {
			for _, resp := range resps {
				if !yield(resp) {
					return
				}
			}
		}
		iterOut := model.ReduceSeq(context.Background(), seq, func(a model.Alert) {
			iterAlerts = append(iterAlerts, a.Reason)
		})
		results["iterator"] = result{out: iterOut, alerts: iterAlerts}
		return results
	}
	check := func(t *testing.T, shape string, res result, wantKind model.Kind, wantAlerts ...string) {
		t.Helper()
		if res.out.Kind != wantKind {
			t.Fatalf("%s: Kind = %v, want %v", shape, res.out.Kind, wantKind)
		}
		if len(res.alerts) != len(wantAlerts) {
			t.Fatalf("%s: alerts = %v, want %v", shape, res.alerts, wantAlerts)
		}
		for i, reason := range wantAlerts {
			if res.alerts[i] != reason {
				t.Fatalf("%s: alerts = %v, want %v", shape, res.alerts, wantAlerts)
			}
		}
	}

	t.Run("valid tool terminal completes once and is dispatchable", func(t *testing.T) {
		for shape, res := range runBoth([]*model.Response{toolTurn()}) {
			check(t, shape, res, model.KindCompleted)
			if len(res.out.Trace) != 2 {
				t.Fatalf("%s: trace = %v", shape, res.out.Trace)
			}
			first := res.out.Trace[0]
			if first.Signal != model.SignalValidTerminal || first.Kind != model.KindCompleted || !first.Dispatchable {
				t.Fatalf("%s: first item = %+v", shape, first)
			}
			if res.out.Body == nil || !res.out.Body.IsToolCallResponse() {
				t.Fatalf("%s: body = %+v", shape, res.out.Body)
			}
		}
		for shape, res := range runBoth([]*model.Response{doneBody("hello")}) {
			check(t, shape, res, model.KindCompleted)
			if res.out.Trace[0].Dispatchable {
				t.Fatalf("%s: plain terminal dispatchable: %+v", shape, res.out.Trace[0])
			}
		}
	})

	t.Run("usage tail merges usage and never replaces the body", func(t *testing.T) {
		body := doneBody("hello")
		body.Usage = &model.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
		body.Usage.PromptTokensDetails.CachedTokens = 3
		body.Usage.CompletionTokensDetails.ReasoningTokens = 2
		body.Usage.TimingInfo = &model.TimingInfo{FirstTokenDuration: 10 * time.Millisecond, ReasoningDuration: 4 * time.Millisecond}
		tail := usageTail(&model.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2})
		tail.Usage.TimingInfo = &model.TimingInfo{FirstTokenDuration: time.Millisecond}
		for shape, res := range runBoth([]*model.Response{body, tail}) {
			check(t, shape, res, model.KindCompleted)
			if res.out.Body != body || res.out.Body.Choices[0].Message.Content != "hello" {
				t.Fatalf("%s: body = %+v", shape, res.out.Body)
			}
			if len(res.out.Trace) != 3 || res.out.Trace[1].Signal != model.SignalUsageTail || res.out.Trace[2].Signal != model.SignalProducerDone {
				t.Fatalf("%s: trace = %v", shape, res.out.Trace)
			}
			usage := res.out.Usage
			if usage.PromptTokens != 11 || usage.CompletionTokens != 6 || usage.TotalTokens != 17 ||
				usage.PromptTokensDetails.CachedTokens != 3 || usage.CompletionTokensDetails.ReasoningTokens != 2 ||
				usage.TimingInfo == nil || usage.TimingInfo.FirstTokenDuration != 11*time.Millisecond ||
				usage.TimingInfo.ReasoningDuration != 4*time.Millisecond {
				t.Fatalf("%s: usage = %+v", shape, usage)
			}
		}
	})

	t.Run("partial output without a complete answer is partial only", func(t *testing.T) {
		for shape, res := range runBoth([]*model.Response{partialBody("part")}) {
			check(t, shape, res, model.KindPartialOnly)
			if res.out.Trace[0].Signal != model.SignalPartialOnly {
				t.Fatalf("%s: first item = %+v", shape, res.out.Trace[0])
			}
			if res.out.Body == nil || !res.out.Body.IsPartial {
				t.Fatalf("%s: body = %+v", shape, res.out.Body)
			}
		}
	})

	t.Run("stream error reduces failed", func(t *testing.T) {
		for shape, res := range runBoth([]*model.Response{failResp(model.ErrorTypeStreamError)}) {
			check(t, shape, res, model.KindFailed)
			if res.out.Trace[0].Signal != model.SignalError || res.out.Trace[0].Err == nil {
				t.Fatalf("%s: first item = %+v", shape, res.out.Trace[0])
			}
		}
	})

	t.Run("cancellation error reduces cancelled", func(t *testing.T) {
		for shape, res := range runBoth([]*model.Response{failResp(model.ErrorTypeCancelled)}) {
			check(t, shape, res, model.KindCancelled)
			if res.out.Trace[0].Signal != model.SignalCancelled {
				t.Fatalf("%s: first item = %+v", shape, res.out.Trace[0])
			}
		}
	})

	t.Run("producer done is end evidence and never a second terminal", func(t *testing.T) {
		for shape, res := range runBoth([]*model.Response{doneBody("hello")}) {
			check(t, shape, res, model.KindCompleted)
			if len(res.out.Trace) != 2 || res.out.Trace[1].Signal != model.SignalProducerDone || res.out.Trace[1].Kind != model.KindCompleted {
				t.Fatalf("%s: trace = %v", shape, res.out.Trace)
			}
		}
		for shape, res := range runBoth([]*model.Response{partialBody("part")}) {
			check(t, shape, res, model.KindPartialOnly)
			if len(res.out.Trace) != 2 || res.out.Trace[1].Signal != model.SignalProducerDone || res.out.Trace[1].Kind != model.KindPartialOnly {
				t.Fatalf("%s: trace = %v", shape, res.out.Trace)
			}
		}
	})

	t.Run("no terminal evidence reduces unknown and alerts", func(t *testing.T) {
		for shape, res := range runBoth(nil) {
			check(t, shape, res, model.KindUnknown, model.AlertUnknownTerminal)
			if len(res.out.Trace) != 1 || res.out.Trace[0].Signal != model.SignalProducerDone || res.out.Trace[0].Kind != model.KindUnknown {
				t.Fatalf("%s: trace = %v", shape, res.out.Trace)
			}
		}
		for shape, res := range runBoth([]*model.Response{usageTail(&model.Usage{TotalTokens: 2})}) {
			check(t, shape, res, model.KindUnknown, model.AlertUnknownTerminal)
			if len(res.out.Trace) != 2 || res.out.Trace[0].Signal != model.SignalUsageTail || res.out.Trace[1].Signal != model.SignalProducerDone {
				t.Fatalf("%s: trace = %v", shape, res.out.Trace)
			}
			if res.out.Usage.TotalTokens != 2 {
				t.Fatalf("%s: usage = %+v", shape, res.out.Usage)
			}
		}
	})

	t.Run("late response never revives the terminal and never dispatches tools", func(t *testing.T) {
		for shape, res := range runBoth([]*model.Response{doneBody("hello"), doneBody("late"), toolTurn()}) {
			check(t, shape, res, model.KindCompleted, model.AlertLateResponse, model.AlertLateResponse)
			if res.out.Body == nil || res.out.Body.Choices[0].Message.Content != "hello" {
				t.Fatalf("%s: body = %+v", shape, res.out.Body)
			}
			if len(res.out.Trace) != 4 || !res.out.Trace[1].Late || res.out.Trace[1].Dispatchable {
				t.Fatalf("%s: trace = %v", shape, res.out.Trace)
			}
			if !res.out.Trace[2].Late || res.out.Trace[2].Dispatchable || res.out.Trace[2].Signal != model.SignalValidTerminal {
				t.Fatalf("%s: trace = %v", shape, res.out.Trace)
			}
			if len(res.out.Late) != 2 {
				t.Fatalf("%s: late = %v", shape, res.out.Late)
			}
		}
	})

	t.Run("nil response item reduces failed and alerts", func(t *testing.T) {
		for shape, res := range runBoth([]*model.Response{nil}) {
			check(t, shape, res, model.KindFailed, model.AlertNilResponse)
			if res.out.Trace[0].Signal != model.SignalError || !errors.Is(res.out.Trace[0].Err, model.ErrNilResponse) {
				t.Fatalf("%s: first item = %+v", shape, res.out.Trace[0])
			}
		}
	})

	t.Run("nil stream reduces failed and alerts", func(t *testing.T) {
		var channelAlerts []string
		channelOut := model.ReduceChannel(context.Background(), nil, func(a model.Alert) {
			channelAlerts = append(channelAlerts, a.Reason)
		})
		check(t, "channel", result{out: channelOut, alerts: channelAlerts}, model.KindFailed, model.AlertNilStream)
		var iterAlerts []string
		iterOut := model.ReduceSeq(context.Background(), nil, func(a model.Alert) {
			iterAlerts = append(iterAlerts, a.Reason)
		})
		check(t, "iterator", result{out: iterOut, alerts: iterAlerts}, model.KindFailed, model.AlertNilStream)
	})

	t.Run("cancelled context closes the reduction without waiting for the producer", func(t *testing.T) {
		ctx, stop := context.WithCancel(context.Background())
		stop()
		ch := make(chan *model.Response)
		var channelAlerts []string
		channelOut := model.ReduceChannel(ctx, ch, func(a model.Alert) {
			channelAlerts = append(channelAlerts, a.Reason)
		})
		check(t, "channel", result{out: channelOut, alerts: channelAlerts}, model.KindCancelled)
		var iterAlerts []string
		empty := func(yield func(*model.Response) bool) {}
		iterOut := model.ReduceSeq(ctx, empty, func(a model.Alert) {
			iterAlerts = append(iterAlerts, a.Reason)
		})
		check(t, "iterator", result{out: iterOut, alerts: iterAlerts}, model.KindCancelled)
		reducer := model.NewReducer(nil)
		reducer.Observe(partialBody("part"))
		reducer.Cancel()
		if out := reducer.Outcome(); out.Kind != model.KindCancelled {
			t.Fatalf("reducer cancel over provisional partial: Kind = %v", out.Kind)
		}
	})
}
