// 契约: docs/wiki/agent/event-flow.md#runner-flow
//
// The red anchor of the fast result: a decision that admits two calls is driven
// by the first result that arrives, never by the last one. The opening decision
// issues one call that settles at once and one that parks inside the outbound
// face; the anchor asserts the model is served a request that reads the settled
// fast result while the slow call is still running.
package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/assembly"
	"github.com/SpellingDragon/tagent/runtime/agent"
)

// fastResultSession, fastResultInvocation, fastResultCallID, fastResultBody,
// fastResultOpening and fastResultWindow are the material the anchor is written
// in: one session whose opening decision issues the two send calls, one body that
// settles at once and one the outbound face parks.
const (
	fastResultSession    = "session-fast-result"
	fastResultInvocation = "inv-fast-result"
	// fastResultCallID is the call identity of the call that settles at once.
	fastResultCallID = "call-fast-result"
	// fastResultBody is the body that call delivers: the sender answers it at once.
	fastResultBody = "fast: the quick call finished"
	// fastResultOpening is the message that opens the turn.
	fastResultOpening = "fast: the message that opens the turn"
	// fastResultWindow bounds the wait for the request that reads the fast result
	// while the slow call is parked.
	fastResultWindow = 5 * time.Second
)

// TestFastResultDrivesBeforeSlowSettles checks that the first settled result drives the next request while a slower call is still running.
//
// - positive: while the slow call is parked, the model is served a request that reads the settled fast result, exactly once
// - positive: the chain of the session holds the fast call's result fact beside no result of the slow call
// - negative: the turn ends beside the slow call still parked, so no request ever waited for the slower result
// - negative: no request reads two settled observations, so the fast result was never batched with a result that had not arrived
// - consumer: every invocation is one Host.Invoke over a host booted from a real document, read back from the requests served and the run's own chain
// - the parked call is released at the end so the task it admitted settles before the host close, which demands a real termination
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestFastResultDrivesBeforeSlowSettles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	provider := newIntakeProvider(t)
	provider.firstAnswer = fastResultTwoCalls()
	sender := newIntakeSender()
	host := intakeBoot(t, provider, sender)
	addr := event.Address{AgentID: intakeEntryAgent, SessionID: fastResultSession}

	done := make(chan agent.Turn, 1)
	go func() {
		turn, _ := host.Invoke(ctx, assembly.InvokeRequest{
			InvocationID: fastResultInvocation,
			Address:      addr,
			Input:        []model.Message{{Role: model.RoleUser, Content: fastResultOpening}},
		})
		done <- turn
	}()
	sender.awaitParked(t)

	read := fastResultAwaitRequest(t, provider)
	if copies := fastResultObservations(read); copies != 1 {
		t.Fatalf("settled observations in the request that read the fast result = %d, want exactly 1: "+
			"the decision reads the result that arrived, never a second one (%s)",
			copies, intakeDescribe(provider.requests()))
	}
	if slow := fastResultSettledFacts(host.Run().SessionFacts(addr), intakeSlowCallID); slow != 0 {
		t.Fatalf("the chain holds %d result fact(s) of the slow call before its release, want 0: "+
			"a parked call has settled nothing", slow)
	}
	if fast := fastResultSettledFacts(host.Run().SessionFacts(addr), fastResultCallID); fast != 1 {
		t.Fatalf("the chain holds %d result fact(s) of the fast call, want the one fact its settlement committed", fast)
	}
	for _, request := range provider.requests() {
		if copies := fastResultObservations(request); copies > 1 {
			t.Fatalf("request %d reads %d settled observations before the slow call was released, want at most 1: "+
				"the loop returns to the pull beside a pending call instead of waiting for it (%s)",
				request.Index, copies, intakeDescribe(provider.requests()))
		}
	}

	select {
	case turn := <-done:
		if turn.Terminal != model.KindCompleted {
			t.Fatalf("turn = %v, want the completed turn the fast result drove (%s)",
				turn.Terminal, intakeDescribe(provider.requests()))
		}
	case <-time.After(intakeSettleWindow):
		t.Fatalf("the turn never ended while the slow call was parked, want it to end on the fast result alone")
	}
	if slow := fastResultSettledFacts(host.Run().SessionFacts(addr), intakeSlowCallID); slow != 0 {
		t.Fatalf("the chain holds %d result fact(s) of the slow call, want 0: the turn ended before it settled", slow)
	}

	sender.release()
}

// fastResultAwaitRequest waits for the request that reads the settled fast
// result and answers it, so a failure states every request the model saw.
func fastResultAwaitRequest(t *testing.T, provider *intakeProvider) intakeRequest {
	t.Helper()
	deadline := time.After(fastResultWindow)
	for {
		for _, request := range provider.requests() {
			if fastResultObservations(request) > 0 {
				return request
			}
		}
		select {
		case <-deadline:
			t.Fatalf("the model served %d request(s) in %s while the slow call was parked, want one that reads the settled fast result: "+
				"the fast result drives the next decision without waiting for the slow one",
				len(provider.requests()), fastResultWindow)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// fastResultObservations counts the settled observations one request reads: the
// settled result of a call enters the transcript as user material carrying the
// settled body, while the admission receipt of that call answers under the tool
// role — so the two never read as one observation.
func fastResultObservations(request intakeRequest) int {
	count := 0
	for _, message := range request.Messages {
		if message.Role == string(model.RoleUser) && strings.TrimSpace(message.Content) == fastResultSettledBody {
			count++
		}
	}
	return count
}

// fastResultSettledBody is the body a settled send call answers with: the whole
// model-visible content of a delivery that happened.
const fastResultSettledBody = "ok"

// fastResultSettledFacts counts the result facts one chain committed for the
// named call.
func fastResultSettledFacts(facts []event.Event, callID string) int {
	count := 0
	for _, fact := range facts {
		if fact.Type == event.TypeActionCommand && fact.CallID == callID {
			count++
		}
	}
	return count
}

// fastResultTwoCalls renders the chat completion that issues two send calls from
// one decision: the call that settles at once, and the call whose body the
// outbound face parks.
func fastResultTwoCalls() string {
	fast := `{"id":"` + fastResultCallID + `","type":"function","function":{"name":"send","arguments":` +
		intakeQuoted(`{"content":`+intakeQuoted(fastResultBody)+`}`) + `}}`
	slow := `{"id":"` + intakeSlowCallID + `","type":"function","function":{"name":"send","arguments":` +
		intakeQuoted(`{"content":`+intakeQuoted(intakeSlowBody)+`}`) + `}}`
	return `{"id":"scripted-tool","object":"chat.completion","created":1699200000,"choices":[{"index":0,` +
		`"message":{"role":"assistant","content":"calling","tool_calls":[` + fast + `,` + slow + `]},` +
		`"finish_reason":"tool_calls"}]}`
}
