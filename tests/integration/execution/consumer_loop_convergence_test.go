// 契约: docs/wiki/agent/event-flow.md#runner-flow
//
// The anchors of the consumer-loop convergence: one resident loop per address
// drives every input of that address, and no decision waits for a dispatched
// call.
//
// - the slow-tool anchor: a new user message is answered while a slow tool is still running
// - the cross-source anchor: two invocations of one session batch into one model request
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
// - the first turn issues a tool call that parks inside the sender (the gated send face the intake anchor uses)
// - a second message arrives at the same session while that call is blocked
// - positive: the model serves a request that reads the second message before the gated call is released
// - the parked call is released at the end, so both turns settle before the harness closes the host
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestSlowToolDoesNotBlockNewInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider := newIntakeProvider(t)
	sender := newIntakeSender()
	host := intakeBoot(t, provider, sender)
	addr := event.Address{AgentID: intakeEntryAgent, SessionID: intakeSession}

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

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		_, _ = host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: intakeSecondInvocation + "-0",
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: "ping"}},
		})
	}()

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

	sender.release()
	<-firstDone
	<-secondDone
}

// TestCrossSourceBatching checks that two inputs from different invocations of one session enter the same model request.
// - whichever input the loop of the address pulls first opens the segment, and the other joins the next request that segment serves
// - positive: one request reads both inputs together, never one request per invocation
// - the parked send call is released at the end, so the task it admitted settles before the harness closes the host
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
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

	sender.release()

	requests := provider.requests()
	batched := false
	for _, req := range requests {
		hasFirst, hasSecond := false, false
		for _, msg := range req.Messages {
			if msg.Role == "user" && strings.Contains(msg.Content, "first question") {
				hasFirst = true
			}
			if msg.Role == "user" && strings.Contains(msg.Content, "second question") {
				hasSecond = true
			}
		}
		if hasFirst && hasSecond {
			batched = true
			break
		}
	}
	if !batched {
		t.Fatalf("no model request reads both inputs together (%s)", convergenceDescribeRequests(requests))
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
