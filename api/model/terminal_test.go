package model

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// invariantAnswer builds one complete, non-partial answer carrying the given text and usage.
func invariantAnswer(text string, prompt, completion int) *Response {
	return &Response{
		Choices: []Choice{{Message: Message{Role: RoleAssistant, Content: text}}},
		Usage:   &Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion},
		Done:    true,
	}
}

// invariantPartial builds one response that carries content but has not finished.
func invariantPartial(text string, prompt, completion int) *Response {
	partial := invariantAnswer(text, prompt, completion)
	partial.IsPartial = true
	partial.Done = false
	return partial
}

// invariantTail builds one usage-only tail packet carrying no content at all.
func invariantTail(prompt, completion int) *Response {
	return &Response{Usage: &Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion}}
}

// invariantChannel returns a closed channel carrying the given responses in order.
func invariantChannel(items ...*Response) <-chan *Response {
	ch := make(chan *Response, len(items))
	for _, item := range items {
		ch <- item
	}
	close(ch)
	return ch
}

// invariantSeq returns an exhausted sequence yielding the given responses in order.
func invariantSeq(items ...*Response) Seq[*Response] {
	return func(yield func(*Response) bool) {
		for _, item := range items {
			if !yield(item) {
				return
			}
		}
	}
}

// invariantAlerts collects every alert the reduction reports, in order.
func invariantAlerts() (*[]Alert, func(Alert)) {
	alerts := &[]Alert{}
	return alerts, func(alert Alert) { *alerts = append(*alerts, alert) }
}

// invariantBodyText reports the text of the retained body, or an empty string when none is kept.
func invariantBodyText(outcome Outcome) string {
	if outcome.Body == nil || len(outcome.Body.Choices) == 0 {
		return ""
	}
	return outcome.Body.Choices[0].Message.Content
}

// TestTerminalReductionInvariants pins the single terminal judgement both stream shapes share.
// - both entry points reduce one response set to the same terminal, usage, body and trace signals
// - a nil stream fails the reduction as failed and alerts its caller by name
// - an exhausted producer without terminal evidence alerts unknown instead of completing
// - a cancelled context closes the reduction as cancelled without waiting for the producer to end
// - a response observed after the terminal never moves it and is kept beside its late mark
// - a usage-only tail deposits usage without replacing the body or the standing terminal
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestTerminalReductionInvariants(t *testing.T) {
	ctx := context.Background()

	t.Run("both entry points reduce one response set to the same outcome", func(t *testing.T) {
		items := []*Response{
			invariantPartial("half", 2, 1),
			invariantAnswer("final", 3, 2),
			invariantAnswer("later", 5, 4),
		}
		fromChannel := ReduceChannel(ctx, invariantChannel(items...), nil)
		fromSeq := ReduceSeq(ctx, invariantSeq(items...), nil)
		if fromChannel.Kind != KindCompleted || fromSeq.Kind != fromChannel.Kind {
			t.Fatalf("terminal kinds = (channel %v, sequence %v), want both completed",
				fromChannel.Kind, fromSeq.Kind)
		}
		if fromChannel.Usage != fromSeq.Usage {
			t.Fatalf("usage = (channel %+v, sequence %+v), want one accumulation", fromChannel.Usage, fromSeq.Usage)
		}
		if invariantBodyText(fromChannel) != invariantBodyText(fromSeq) {
			t.Fatalf("retained bodies = (%q, %q), want one body slot",
				invariantBodyText(fromChannel), invariantBodyText(fromSeq))
		}
		var channelSignals, seqSignals []Signal
		for _, item := range fromChannel.Trace {
			channelSignals = append(channelSignals, item.Signal)
		}
		for _, item := range fromSeq.Trace {
			seqSignals = append(seqSignals, item.Signal)
		}
		if !reflect.DeepEqual(channelSignals, seqSignals) {
			t.Fatalf("trace signals = (channel %v, sequence %v), want one evidence table", channelSignals, seqSignals)
		}
		if len(fromChannel.Late) != 1 || len(fromSeq.Late) != 1 {
			t.Fatalf("late responses = (channel %d, sequence %d), want the one response after the terminal",
				len(fromChannel.Late), len(fromSeq.Late))
		}
	})

	t.Run("a nil stream fails the reduction and alerts by name", func(t *testing.T) {
		alerts, collect := invariantAlerts()
		var channel chan *Response
		fromChannel := ReduceChannel(ctx, channel, collect)
		if fromChannel.Kind != KindFailed {
			t.Fatalf("nil channel terminal = %v, want %v", fromChannel.Kind, KindFailed)
		}
		if fromChannel.Kind == KindUnknown {
			t.Fatalf("nil channel terminal = %v, want a named failure instead of unknown", fromChannel.Kind)
		}
		if len(*alerts) != 1 || (*alerts)[0].Reason != AlertNilStream {
			t.Fatalf("alerts = %+v, want one %q alert", *alerts, AlertNilStream)
		}

		alerts, collect = invariantAlerts()
		var seq Seq[*Response]
		fromSeq := ReduceSeq(ctx, seq, collect)
		if fromSeq.Kind != KindFailed {
			t.Fatalf("nil sequence terminal = %v, want %v", fromSeq.Kind, KindFailed)
		}
		if len(*alerts) != 1 || (*alerts)[0].Reason != AlertNilStream {
			t.Fatalf("sequence alerts = %+v, want one %q alert", *alerts, AlertNilStream)
		}
	})

	t.Run("an exhausted producer without evidence is unknown, never completed", func(t *testing.T) {
		alerts, collect := invariantAlerts()
		outcome := ReduceChannel(ctx, invariantChannel(), collect)
		if outcome.Kind != KindUnknown {
			t.Fatalf("terminal = %v, want %v without terminal evidence", outcome.Kind, KindUnknown)
		}
		if outcome.Kind == KindCompleted {
			t.Fatalf("terminal = %v, want an exhausted producer never read as completed", outcome.Kind)
		}
		if len(*alerts) != 1 || (*alerts)[0].Reason != AlertUnknownTerminal {
			t.Fatalf("alerts = %+v, want one %q alert", *alerts, AlertUnknownTerminal)
		}
		last := outcome.Trace[len(outcome.Trace)-1]
		if last.Signal != SignalProducerDone {
			t.Fatalf("last trace signal = %v, want %v", last.Signal, SignalProducerDone)
		}
	})

	t.Run("a cancelled context closes the reduction as cancelled", func(t *testing.T) {
		alerts, collect := invariantAlerts()
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		outcome := ReduceChannel(cancelled, make(chan *Response), collect)
		if outcome.Kind != KindCancelled {
			t.Fatalf("terminal = %v, want %v", outcome.Kind, KindCancelled)
		}
		if outcome.Kind == KindFailed {
			t.Fatalf("terminal = %v, want cancellation told apart from failure", outcome.Kind)
		}
		if len(*alerts) != 0 {
			t.Fatalf("alerts = %+v, want a cancellation reported through the trace alone", *alerts)
		}
		last := outcome.Trace[len(outcome.Trace)-1]
		if last.Signal != SignalCancelled {
			t.Fatalf("last trace signal = %v, want %v", last.Signal, SignalCancelled)
		}
	})

	t.Run("a cancellation after the terminal stands moves nothing", func(t *testing.T) {
		reducer := NewReducer(nil)
		reducer.Observe(invariantAnswer("final", 1, 1))
		item := reducer.Cancel()
		if !item.Late {
			t.Fatalf("cancel item Late = false, want a late cancellation beside a standing terminal")
		}
		outcome := reducer.Outcome()
		if outcome.Kind != KindCompleted {
			t.Fatalf("terminal = %v, want the standing %v", outcome.Kind, KindCompleted)
		}
		if outcome.Kind == KindCancelled {
			t.Fatalf("terminal = %v, want the standing completed terminal kept", outcome.Kind)
		}
	})

	t.Run("a late response never moves the terminal and is kept beside its mark", func(t *testing.T) {
		alerts, collect := invariantAlerts()
		reducer := NewReducer(collect)
		reducer.Observe(invariantAnswer("final", 10, 5))
		item := reducer.Observe(invariantAnswer("late", 1, 1))
		if !item.Late {
			t.Fatalf("late item Late = false, want the response marked late")
		}
		if item.Kind != KindCompleted {
			t.Fatalf("late item Kind = %v, want the standing %v", item.Kind, KindCompleted)
		}
		if item.Dispatchable {
			t.Fatalf("late item Dispatchable = true, want every late item undispatchable")
		}
		outcome := reducer.Outcome()
		if outcome.Kind != KindCompleted {
			t.Fatalf("terminal = %v, want %v", outcome.Kind, KindCompleted)
		}
		if len(outcome.Late) != 1 || outcome.Late[0].Choices[0].Message.Content != "late" {
			t.Fatalf("late responses = %+v, want the one late response kept", outcome.Late)
		}
		if outcome.Usage.PromptTokens != 11 || outcome.Usage.CompletionTokens != 6 {
			t.Fatalf("usage after the late response = (%d, %d), want the late usage deposited once",
				outcome.Usage.PromptTokens, outcome.Usage.CompletionTokens)
		}
		if len(*alerts) != 1 || (*alerts)[0].Reason != AlertLateResponse {
			t.Fatalf("alerts = %+v, want one %q alert", *alerts, AlertLateResponse)
		}
	})

	t.Run("a usage-only tail keeps the body and the standing terminal", func(t *testing.T) {
		alerts, collect := invariantAlerts()
		reducer := NewReducer(collect)
		reducer.Observe(invariantAnswer("final", 10, 5))
		item := reducer.Observe(invariantTail(4, 2))
		if item.Signal != SignalUsageTail {
			t.Fatalf("usage tail signal = %v, want %v", item.Signal, SignalUsageTail)
		}
		if item.Kind != KindCompleted {
			t.Fatalf("usage tail Kind = %v, want the standing %v", item.Kind, KindCompleted)
		}
		if item.Err != nil {
			t.Fatalf("usage tail Err = %v, want no terminal error on a tail", item.Err)
		}
		outcome := reducer.Outcome()
		if invariantBodyText(outcome) != "final" {
			t.Fatalf("retained body = %q, want the valid content kept beside the tail", invariantBodyText(outcome))
		}
		if len(outcome.Late) != 0 {
			t.Fatalf("late responses = %+v, want a tail outside the late set", outcome.Late)
		}
		if outcome.Usage.PromptTokens != 14 {
			t.Fatalf("usage prompt tokens = %d, want the tail deposited once", outcome.Usage.PromptTokens)
		}
		if len(*alerts) != 0 {
			t.Fatalf("alerts = %+v, want a usage tail reported without an alert", *alerts)
		}
	})

	t.Run("a nil response item is a failure with its own alert", func(t *testing.T) {
		alerts, collect := invariantAlerts()
		reducer := NewReducer(collect)
		item := reducer.Observe(nil)
		if item.Signal != SignalError {
			t.Fatalf("nil item signal = %v, want %v", item.Signal, SignalError)
		}
		if item.Kind != KindFailed {
			t.Fatalf("nil item terminal = %v, want %v", item.Kind, KindFailed)
		}
		if !errors.Is(item.Err, ErrNilResponse) {
			t.Fatalf("nil item error = %v, want %v", item.Err, ErrNilResponse)
		}
		if len(*alerts) != 1 || (*alerts)[0].Reason != AlertNilResponse {
			t.Fatalf("alerts = %+v, want one %q alert", *alerts, AlertNilResponse)
		}
	})

	t.Run("partial output settles as partial-only only at the producer end", func(t *testing.T) {
		reducer := NewReducer(nil)
		reducer.Observe(invariantPartial("half", 2, 1))
		outcome := reducer.Outcome()
		if outcome.Kind != KindPartialOnly {
			t.Fatalf("terminal = %v, want %v", outcome.Kind, KindPartialOnly)
		}
		if outcome.Kind == KindCompleted {
			t.Fatalf("terminal = %v, want partial output never read as completed", outcome.Kind)
		}
		if invariantBodyText(outcome) != "half" {
			t.Fatalf("retained body = %q, want the provisional partial content", invariantBodyText(outcome))
		}

		settled := NewReducer(nil)
		settled.Observe(invariantPartial("half", 2, 1))
		item := settled.ProducerDone()
		if item.Kind != KindPartialOnly {
			t.Fatalf("producer end resolved = %v, want %v", item.Kind, KindPartialOnly)
		}

		completed := NewReducer(nil)
		completed.Observe(invariantPartial("half", 2, 1))
		completed.Observe(invariantAnswer("final", 3, 2))
		if got := completed.Outcome().Kind; got != KindCompleted {
			t.Fatalf("terminal = %v, want a complete answer to replace the provisional partial", got)
		}
	})

	t.Run("only a valid tool terminal is dispatchable", func(t *testing.T) {
		reducer := NewReducer(nil)
		toolCall := &Response{
			Choices: []Choice{{Message: Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call-1"}}}}},
			Done:    true,
		}
		item := reducer.Observe(toolCall)
		if !item.Dispatchable {
			t.Fatalf("tool terminal Dispatchable = false, want a valid tool terminal dispatchable")
		}

		answer := NewReducer(nil).Observe(invariantAnswer("final", 1, 1))
		if answer.Dispatchable {
			t.Fatalf("answer Dispatchable = true, want a non-tool terminal undispatchable")
		}
	})
}

// TestUsageAccumulationInvariants pins the one usage and body ledger of a reduction.
// - each observed response deposits its usage once, so a repeated value counts twice and never three times
// - a usage-only tail deposits usage while the body slot keeps the last valid content
// - returned totals copy the timing record, so a caller cannot write back into the reduction
// - an absent response or an absent usage record deposits nothing and never panics
// 契约: docs/wiki/agent/execution-generations.md#turn-outcome
func TestUsageAccumulationInvariants(t *testing.T) {
	answer := invariantAnswer("final", 10, 5)
	answer.Usage.PromptTokensDetails.CachedTokens = 2
	answer.Usage.PromptTokensDetails.CacheCreationTokens = 3
	answer.Usage.PromptTokensDetails.CacheReadTokens = 4
	answer.Usage.CompletionTokensDetails.ReasoningTokens = 1
	answer.Usage.TimingInfo = &TimingInfo{FirstTokenDuration: 2 * time.Second, ReasoningDuration: time.Second}

	t.Run("each observation deposits its usage exactly once", func(t *testing.T) {
		reducer := NewReducer(nil)
		reducer.Observe(answer)
		reducer.Observe(answer)
		outcome := reducer.Outcome()
		if outcome.Usage.PromptTokens != 20 || outcome.Usage.CompletionTokens != 10 || outcome.Usage.TotalTokens != 30 {
			t.Fatalf("usage = (%d, %d, %d), want each of the two observations counted once",
				outcome.Usage.PromptTokens, outcome.Usage.CompletionTokens, outcome.Usage.TotalTokens)
		}
		if outcome.Usage.PromptTokens == 10 {
			t.Fatalf("usage prompt tokens = %d, want the second observation not dropped", outcome.Usage.PromptTokens)
		}
		details := outcome.Usage.PromptTokensDetails
		if details.CachedTokens != 4 || details.CacheCreationTokens != 6 || details.CacheReadTokens != 8 {
			t.Fatalf("prompt details = %+v, want every detail accumulated once", details)
		}
		if outcome.Usage.CompletionTokensDetails.ReasoningTokens != 2 {
			t.Fatalf("reasoning tokens = %d, want two observations accumulated", outcome.Usage.CompletionTokensDetails.ReasoningTokens)
		}
		if outcome.Usage.TimingInfo == nil || outcome.Usage.TimingInfo.FirstTokenDuration != 4*time.Second ||
			outcome.Usage.TimingInfo.ReasoningDuration != 2*time.Second {
			t.Fatalf("timing = %+v, want both durations accumulated", outcome.Usage.TimingInfo)
		}
	})

	t.Run("a usage-only tail deposits usage and keeps the body slot", func(t *testing.T) {
		reducer := NewReducer(nil)
		reducer.Observe(invariantAnswer("final", 10, 5))
		reducer.Observe(invariantTail(4, 2))
		outcome := reducer.Outcome()
		if outcome.Usage.PromptTokens != 14 || outcome.Usage.CompletionTokens != 7 {
			t.Fatalf("usage after the tail = (%d, %d), want the tail deposited once",
				outcome.Usage.PromptTokens, outcome.Usage.CompletionTokens)
		}
		if invariantBodyText(outcome) != "final" {
			t.Fatalf("retained body = %q, want the valid content kept beside the tail", invariantBodyText(outcome))
		}
	})

	t.Run("returned totals copy the timing record", func(t *testing.T) {
		reducer := NewReducer(nil)
		reducer.Observe(answer)
		first := reducer.Outcome().Usage
		first.TimingInfo.FirstTokenDuration = 0
		second := reducer.Outcome().Usage
		if second.TimingInfo == nil || second.TimingInfo.FirstTokenDuration != 2*time.Second {
			t.Fatalf("timing after a caller write = %+v, want the reduction untouched", second.TimingInfo)
		}
		if first.TimingInfo == second.TimingInfo {
			t.Fatalf("returned timing records share one pointer, want a copy per result")
		}
	})

	t.Run("an absent response or usage record deposits nothing", func(t *testing.T) {
		reducer := NewReducer(nil)
		reducer.Observe(nil)
		reducer.Observe(&Response{})
		outcome := reducer.Outcome()
		if outcome.Usage != (Usage{}) {
			t.Fatalf("usage = %+v, want the zero record after two content-free observations", outcome.Usage)
		}
		if outcome.Usage.TotalTokens != 0 {
			t.Fatalf("total tokens = %d, want nothing deposited", outcome.Usage.TotalTokens)
		}
	})

	t.Run("only valid content takes the body slot", func(t *testing.T) {
		fold := &usageFold{}
		fold.retain(invariantTail(1, 1))
		if fold.retained() != nil {
			t.Fatalf("retained body = %+v, want a usage-only tail refused the slot", fold.retained())
		}
		partial := invariantPartial("half", 1, 1)
		fold.retain(partial)
		if fold.retained() != partial {
			t.Fatalf("retained body = %+v, want the valid partial content kept", fold.retained())
		}
		fold.retain(invariantTail(2, 2))
		if fold.retained() != partial {
			t.Fatalf("retained body changed by a tail, want the valid content kept")
		}
	})
}

// TestOutcomeCause checks the one read every consumer takes of a reduction's recorded error.
// - the error the retained body carries wins over anything on the trace
// - a failed reduction names the error its trace recorded
// - a completed reduction names no cause
// - a failed reduction that recorded no text names no cause
func TestOutcomeCause(t *testing.T) {
	bodyCause := &ResponseError{Message: "the retained body refused the request"}
	traceCause := &ResponseError{Message: "the trace refused the request"}

	t.Run("the retained body names the cause first", func(t *testing.T) {
		outcome := Outcome{
			Kind:  KindFailed,
			Body:  &Response{Error: bodyCause},
			Trace: []Item{{Signal: SignalError, Kind: KindFailed, Err: traceCause}},
		}
		if cause := outcome.Cause(); cause != bodyCause {
			t.Fatalf("cause = %v, want the error the retained body carries", cause)
		}
	})

	t.Run("a failed reduction names the cause its trace recorded", func(t *testing.T) {
		ch := make(chan *Response, 1)
		ch <- &Response{Error: &ResponseError{Type: ErrorTypeAPIError, Message: "provider answered 500"}}
		close(ch)

		outcome := ReduceChannel(context.Background(), ch, nil)

		if outcome.Kind != KindFailed {
			t.Fatalf("terminal = %s, want the failed terminal of the error response", outcome.Kind)
		}
		cause := outcome.Cause()
		if cause == nil || !strings.Contains(cause.Error(), "500") {
			t.Fatalf("cause = %v, want the raw error the trace recorded", cause)
		}
	})

	t.Run("a completed reduction names no cause", func(t *testing.T) {
		outcome := ReduceChannel(context.Background(), invariantChannel(invariantAnswer("done", 1, 1)), nil)
		if outcome.Kind != KindCompleted {
			t.Fatalf("terminal = %s, want the completed terminal of the answer", outcome.Kind)
		}
		if cause := outcome.Cause(); cause != nil {
			t.Fatalf("cause = %v, want none beside a completed terminal", cause)
		}
	})

	t.Run("a failed reduction without recorded text names no cause", func(t *testing.T) {
		outcome := Outcome{Kind: KindFailed, Trace: []Item{{Signal: SignalError, Kind: KindFailed}}}
		if cause := outcome.Cause(); cause != nil {
			t.Fatalf("cause = %v, want none when the reduction recorded no error", cause)
		}
	})
}
