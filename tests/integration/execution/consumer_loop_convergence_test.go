// 契约: openspec/changes/compression-and-budget-as-events/specs/consumer-loop-convergence/spec.md
//
// Red anchors for the consumer-loop convergence: each test names a behavior the
// current implementation does not hold. The convergence turns each red anchor green.
//
// - the slow-tool anchor: a new user message is answered while a slow tool is still running
// - the cross-source anchor: two invocations of one session batch into one model request
//
// 契约: openspec/changes/compression-and-budget-as-events/specs/consumer-loop-convergence/spec.md
package execution

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
)

// TestSlowToolDoesNotBlockNewInput checks that a new user message reaches the model while a slow tool is still running.
//
// The first turn issues a tool call that parks inside the sender (the gated send face the
// intake anchor uses); while that call is blocked, a second message arrives at the same
// session. The anchor asserts the model serves a request that reads the second message
// before the gated call is released. Under the current dispatch-wait behavior the model
// waits for the parked call to settle, so no such request is served and this anchor is red.
//
// 契约: openspec/changes/compression-and-budget-as-events/specs/consumer-loop-convergence/spec.md
func TestSlowToolDoesNotBlockNewInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider := newIntakeProvider(t)
	sender := newIntakeSender()
	host := intakeBoot(t, provider, sender)
	addr := event.Address{AgentID: intakeEntryAgent, SessionID: intakeSession}

	// Drive the first turn: it issues the slow send call and parks inside it.
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: intakeFirstInvocation,
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: intakeFirstBody}},
		})
	}()
	sender.awaitParked(t)

	// Send a new message while the first turn's send call is still blocking.
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		_, _ = host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: intakeSecondInvocation + "-0",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "ping"}},
		})
	}()

	// The anchor expects the model to serve a request that reads "ping" while
	// the send call is still blocking. The current implementation dispatches the
	// call and waits for its settlement before the next pull, so no request
	// reading "ping" is served until the call is released.
	pollDeadline := time.After(5 * time.Second)
	served := false
	for !served {
		select {
		case <-pollDeadline:
			requests := provider.requests()
			t.Fatalf("the model served %d request(s) in 5s while the send call was blocking; "+
				"want a request that reads \"ping\" beside the send call still running",
				len(requests))
		case <-time.After(20 * time.Millisecond):
		}
		for _, req := range provider.requests() {
			for _, msg := range req.Messages {
				if msg.Role == "user" && strings.Contains(msg.Content, "ping") {
					served = true
					break
				}
			}
		}
	}

	// Release the gated call and let both turns settle.
	sender.release()
	<-firstDone
	<-secondDone
}

// TestCrossSourceBatching checks that two inputs from different invocations of one session enter the same model request.
//
// Two invocations arrive nearly simultaneously at the same session; the consumer loop
// batches them into one model request. The current implementation starts one Drive per
// invocation and never batches two invocations of one session, so it serves two requests
// and this anchor is red.
//
// This anchor retires the standing assertion in native_conversation_roundtrip_test.go
// that "two invocations of one session decide independently and never batch".
//
// 契约: openspec/changes/compression-and-budget-as-events/specs/consumer-loop-convergence/spec.md
func TestCrossSourceBatching(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider := newIntakeProvider(t)
	sender := newIntakeSender()
	host := intakeBoot(t, provider, sender)
	addr := event.Address{AgentID: intakeEntryAgent, SessionID: intakeSession + "-batch"}

	// Two invocations of the same session, issued nearly simultaneously.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-batch-1",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "first question"}},
		})
	}()
	go func() {
		defer wg.Done()
		_, _ = host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: "inv-batch-2",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "second question"}},
		})
	}()
	wg.Wait()

	requests := provider.requests()
	// The consumer loop batches both inputs into one model request; the current
	// implementation serves one request per invocation, so two invocations that
	// arrive together are served separately.
	if len(requests) != 1 {
		t.Fatalf("model requests = %d, want exactly 1 beside two invocations of one session batched (%s)",
			len(requests), convergenceDescribeRequests(requests))
	}
	hasFirst, hasSecond := false, false
	for _, msg := range requests[0].Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "first question") {
			hasFirst = true
		}
		if msg.Role == "user" && strings.Contains(msg.Content, "second question") {
			hasSecond = true
		}
	}
	if !hasFirst || !hasSecond {
		t.Fatalf("the batched request carries both inputs (first=%t second=%t)", hasFirst, hasSecond)
	}
}

// convergenceDescribeRequests renders one line per request for the failure message.
func convergenceDescribeRequests(requests []intakeRequest) string {
	var out []string
	for at, req := range requests {
		var msgs []string
		for _, msg := range req.Messages {
			content := msg.Content
			if len(content) > 40 {
				content = content[:40] + "…"
			}
			msgs = append(msgs, string(msg.Role)+":"+content)
		}
		out = append(out, fmt.Sprintf("#%d: %s", at, strings.Join(msgs, " | ")))
	}
	return "[" + strings.Join(out, "; ") + "]"
}
