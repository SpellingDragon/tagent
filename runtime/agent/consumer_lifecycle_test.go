// 契约: docs/wiki/agent/event-flow.md#runner-flow
package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"

	trpctool "trpc.group/trpc-go/trpc-agent-go/tool"
)

// TestWaitAnswerEscapesTheLoopThatEnded pins the closure a stop promises to the
// entries that wait on the segment the loop flew:
// - negative: a loop that ended bears no wait that blocks on it — a wait for an input no segment will ever consume answers instead of standing there forever
// - positive: the segment the loop left flying is closed by that loop, so the input it consumed answers the named cancellation and not a turn nobody produced
// - the calls the abandoned segment admitted read orphaned, because no delivery closes a turn that never committed
// - the abandoned segment writes nothing: no output fact lands and the delivery face is handed nothing, so a restart still owes the input it read
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestWaitAnswerEscapesTheLoopThatEnded(t *testing.T) {
	behaviour := newReceiptScript(receiptStep{calls: []model.ToolCall{receiptCall(receiptCallID, receiptAdmitName)}})
	admit := &receiptAdmit{taskID: receiptTask, admitted: make(chan struct{}, 1)}
	rig := newReceiptRig(t, behaviour, []trpctool.Tool{admit})
	addr := receiptAddress()
	key := rig.landInput(addr, receiptInput)
	consumer := rig.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatalf("the instance of %s/%s = nil, want one instance to fly the material", addr.AgentID, addr.SessionID)
	}
	rig.read(consumer)
	consumer.Wake()
	admit.awaitAdmission(t)
	rig.awaitState(t, consumer, TaskAwaited)
	if consumer.Resting() {
		t.Fatal("the address reads resting while the segment that admitted the call still flies")
	}

	rig.consumers.Stop()
	select {
	case <-consumer.Done():
	default:
		t.Fatal("the stop of the set returned beside a loop that still runs")
	}
	if !consumer.Resting() {
		t.Fatal("the loop reads not resting after it closed the segment it flew")
	}
	if got := consumer.TaskState(receiptTask); got != TaskOrphaned {
		t.Fatalf("the admitted task of the abandoned segment reads %q, want %q: no delivery closed a turn that never committed",
			got, TaskOrphaned)
	}

	// Positive: the input the abandoned segment consumed answers the named
	// cancellation instead of a turn nobody produced.
	answer, err := consumer.WaitAnswer(context.Background(), []event.EventID{key})
	if !errors.Is(err, ErrSegmentCancelled) {
		t.Fatalf("the wait on the abandoned segment = (%+v, %v), want the named ErrSegmentCancelled", answer, err)
	}
	if answer.Turn.Terminal != model.KindCancelled {
		t.Fatalf("the abandoned turn reads terminal %v, want %v", answer.Turn.Terminal, model.KindCancelled)
	}
	if len(answer.Consumed) != 1 || answer.Consumed[0] != key {
		t.Fatalf("the abandoned segment names the inputs it consumed as %v, want the one it pulled (%q)", answer.Consumed, key)
	}

	// Negative: nothing will ever consume this input, and the wait answers
	// instead of blocking on the loop that already returned.
	escaped := make(chan error, 1)
	go func() {
		_, err := consumer.WaitAnswer(context.Background(), []event.EventID{"an-input-no-segment-will-consume"})
		escaped <- err
	}()
	select {
	case err := <-escaped:
		if !errors.Is(err, ErrSegmentCancelled) {
			t.Fatalf("the wait no segment will answer = %v, want the named ErrSegmentCancelled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the wait blocked beside a loop that already ended, want the named cancellation as its escape")
	}

	// The abandoned segment committed nothing, so a restart still owes the input.
	if landed := receiptOutputFacts(rig.history, addr); landed != 0 {
		t.Fatalf("agent_output facts of the abandoned segment = %d, want none: the input it read stays owed on the chain", landed)
	}
	rig.expectNoAnswer(t, "the abandoned segment handed a turn to the delivery face it never committed")
}

// receiptOutputFacts counts the agent_output facts one address committed.
func receiptOutputFacts(history *invariantLoopHistory, addr event.Address) int {
	count := 0
	for _, evt := range history.SessionFacts(addr) {
		if evt.Type == event.TypeAgentOutput {
			count++
		}
	}
	return count
}

// TestOneInstancePerAddressAcrossSets pins the one-instance rule over the loop
// rather than over one set of instances:
// - a second set mounted over the same loop answers the instance the loop already drives instead of starting a second driver of the same decisions
// - one address carries exactly one instance however many sets ask for it, and one input drives exactly one decision
// - a set beside the driving set of the loop mounts nothing of its own, so the attach never stands between two drivers of one address
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestOneInstancePerAddressAcrossSets(t *testing.T) {
	t.Run("a second set answers the instance the loop already drives", func(t *testing.T) {
		rig := newResidentInstanceRig(t)
		addr := rig.otherAddress()
		first := rig.consumers.Ensure(addr)
		if first == nil {
			t.Fatalf("the instance of %s/%s = nil, want one instance", addr.AgentID, addr.SessionID)
		}
		other := rig.restart(t)
		second := other.Ensure(addr)
		if second == nil {
			t.Fatal("a second set mounted over the same loop answered no instance for the address it already drives")
		}
		if first != second {
			t.Fatal("a second set mounted a second driver of one address, want the instance the loop already carries")
		}
		rig.loop.mu.Lock()
		mounted := len(rig.loop.instances)
		rig.loop.mu.Unlock()
		if mounted != 1 {
			t.Fatalf("instances the loop holds for one address = %d, want exactly one", mounted)
		}

		rig.landInput(addr, residentInstanceInput)
		first.Wake()
		second.Wake()
		rig.awaitAnswer(t, addr)
		if got := len(rig.behaviour.requestMessages()); got != 1 {
			t.Fatalf("model decisions of one address = %d, want the one its single instance drove", got)
		}
		if got := len(rig.outputs(addr)); got != 1 {
			t.Fatalf("agent_output facts of one address = %d, want one closed segment", got)
		}
	})

	t.Run("a set beside the driving set mounts nothing", func(t *testing.T) {
		rig := newResidentInstanceRig(t)
		rig.loop.SetConsumers(rig.consumers)
		addr := rig.otherAddress()
		if driver := rig.consumers.Ensure(addr); driver == nil {
			t.Fatalf("the driving set mounted no instance for %s/%s", addr.AgentID, addr.SessionID)
		}
		other := rig.restart(t)
		elsewhere := event.Address{AgentID: residentInstanceOther, SessionID: "session-beside-the-driving-set"}
		if got := other.Ensure(elsewhere); got != nil {
			t.Fatal("a set beside the driving set opened a driver of its own, want none: the close of the loop would not stop it")
		}
		if got := other.Ensure(addr); got == nil {
			t.Fatal("a set beside the driving set answered no instance for an address the loop already drives")
		}
	})
}

// TestLateWaitReadsAnAnswerPastTheSegmentWindow pins the answer index of one
// loop as the keys its segments consumed rather than a window of recent
// segments:
// - positive: an entry that starts waiting long after its segment closed still reads the answer of the segment that consumed its input
// - the count of segments that closed between the two is past the sixteen a recent-segment window kept, so the answer survives the segments beside it
// - the body the answer carries is the one its own segment produced, not the text the chain ends at
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestLateWaitReadsAnAnswerPastTheSegmentWindow(t *testing.T) {
	rig := newResidentInstanceRig(t)
	addr := rig.otherAddress()
	consumer := rig.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatalf("the instance of %s/%s = nil, want one instance", addr.AgentID, addr.SessionID)
	}
	const segments = 20
	keys := make([]event.EventID, 0, segments)
	for index := 0; index < segments; index++ {
		key := rig.landInput(addr, fmt.Sprintf("the input that opened segment %d", index))
		keys = append(keys, key)
		consumer.Wake()
		answer := rig.awaitAnswer(t, addr)
		if answer.Err != nil {
			t.Fatalf("segment %d of %s closed beside %v, want a completed turn", index, addr.SessionID, answer.Err)
		}
		if len(answer.Consumed) != 1 || answer.Consumed[0] != key {
			t.Fatalf("segment %d of %s consumed %v, want the material that landed at %q", index, addr.SessionID, answer.Consumed, key)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	answer, err := consumer.WaitAnswer(ctx, keys[:1])
	if err != nil {
		t.Fatalf("a wait starting %d segments after its own closed = %v, want the answer of the segment that consumed the input",
			segments-1, err)
	}
	if want := event.InvocationID(string(addr.SessionID) + "#c1"); answer.Turn.InvocationID != want {
		t.Fatalf("the late wait answered the segment %q, want the one that consumed its input (%q)", answer.Turn.InvocationID, want)
	}
	if answer.Body != residentInstanceAnswer {
		t.Fatalf("the late wait read the body %q, want the one its own segment produced (%q)", answer.Body, residentInstanceAnswer)
	}
}

// TestStopResidentsClosesWhatTheLoopMounted pins the stop of the resident set a
// loop mounted for itself, which is the stop the close of a run asks for before
// it reads its rest criterion:
// - negative: a resident loop beside a flying segment reads not resting, so a close that read the criterion before this stop would judge a run at rest over a segment still writing
// - positive: the stop answers only once the loop returned, so the segment it flew is closed and joined before the stop returns
// - the address mounts nothing after the stop, and the wait on the input the segment consumed answers the named cancellation
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestStopResidentsClosesWhatTheLoopMounted(t *testing.T) {
	behaviour := newReceiptScript(receiptStep{calls: []model.ToolCall{receiptCall(receiptCallID, receiptAdmitName)}})
	admit := &receiptAdmit{taskID: receiptTask, admitted: make(chan struct{}, 1)}
	rig := newReceiptRig(t, behaviour, []trpctool.Tool{admit})
	addr := receiptAddress()

	// A loop assembled beside no attached set mounts its own residents, exactly
	// as the run assembled without a host does.
	residents := rig.loop.residentOf()
	if residents == nil {
		t.Fatal("a loop beside no attached set mounted no resident set of its own")
	}
	key := rig.landInput(addr, receiptInput)
	consumer := residents.Ensure(addr)
	if consumer == nil {
		t.Fatalf("the resident set mounted no instance for %s/%s", addr.AgentID, addr.SessionID)
	}
	rig.read(consumer)
	consumer.Wake()
	admit.awaitAdmission(t)
	rig.awaitState(t, consumer, TaskAwaited)
	if consumer.Resting() {
		t.Fatal("the resident loop reads resting while the segment it flies still admits a call")
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		rig.loop.StopResidents()
	}()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the stop of the residents never returned beside the loop it cancelled")
	}
	select {
	case <-consumer.Done():
	default:
		t.Fatal("the stop of the residents answered beside a loop that still runs")
	}
	if got := residents.Ensure(addr); got != nil {
		t.Fatalf("a stopped resident set mounted an instance for %s again, want none", addr.SessionID)
	}
	if rig.loop.drivesAddress(addr) {
		t.Fatal("the loop still names a driver for the address it stopped")
	}
	if _, err := consumer.WaitAnswer(context.Background(), []event.EventID{key}); !errors.Is(err, ErrSegmentCancelled) {
		t.Fatalf("the wait on the segment the stop closed = %v, want the named ErrSegmentCancelled", err)
	}
	if got := receiptOutputFacts(rig.history, addr); got != 0 {
		// The segment was parked on the settled result it admitted, so the stop
		// abandoned it without a turn: nothing landed before the stop answered and
		// nothing lands after it, which the writer join of the stop covers.
		t.Fatalf("agent_output facts of %s beside the stop = %d, want none: the input the abandoned segment read stays owed on the chain",
			addr.SessionID, got)
	}
}
