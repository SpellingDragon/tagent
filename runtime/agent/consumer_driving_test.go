// 契约: docs/wiki/agent/event-flow.md#runner-flow
package agent

import (
	"testing"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// TestOrphanSettlementDrivesNewSegment pins the self-driving law of a late
// settlement: a committed fact related to an address opens the next segment of
// that address with no new input, and the observation it carries reaches the
// real model request of that segment.
//   - an address closes its first segment on the input it was handed
//   - a settlement fact lands after that segment closed, and nothing else lands
//   - the fact alone wakes the consumer and opens the next segment
//   - the observation the fact carries is the material of the next request, not
//     a value re-read from the chain after the segment closed
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestOrphanSettlementDrivesNewSegment(t *testing.T) {
	rig := newResidentInstanceRig(t)
	addr := rig.otherAddress()
	firstKey := rig.landInput(addr, "the input of the segment that closes first")
	consumer := rig.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatalf("the instance of %s/%s = nil, want the resident consumer of the address", addr.AgentID, addr.SessionID)
	}
	consumer.Wake()
	first := rig.awaitAnswer(t, addr)
	if first.Err != nil || first.Turn.Terminal != model.KindCompleted {
		t.Fatalf("the first segment of %s = (%+v, %v), want one completed turn", addr.SessionID, first, first.Err)
	}
	if len(first.Consumed) != 1 || first.Consumed[0] != firstKey {
		t.Fatalf("the first segment of %s consumed %v, want the input it was handed (%q)", addr.SessionID, first.Consumed, firstKey)
	}
	if served := len(rig.behaviour.requestMessages()); served != 1 {
		t.Fatalf("model requests after the first segment = %d, want the one decision that closed it", served)
	}

	const lateFact = event.EventID("fact-late-settlement")
	const observation = "the observation the late settlement carries"
	rig.loop.facts = func(id event.EventID) (model.Message, bool) {
		if id != lateFact {
			return model.Message{}, false
		}
		return model.Message{Role: model.RoleTool, Content: observation, ToolID: "call-late", ToolName: "late"}, true
	}
	// The settlement lands after the segment that issued the call closed, so no
	// caller waits on it and no new input opens the next segment: the committed
	// fact itself must drive the address.
	rig.loop.Backflow(first.Turn.InvocationID, task.Notice{
		TaskID: "task-late-settlement",
		Kind:   task.NoticeSettled,
		Event:  lateFact,
	})

	second := rig.awaitAnswer(t, addr)
	if second.Err != nil || second.Turn.Terminal != model.KindCompleted {
		t.Fatalf("the segment the late settlement drove = (%+v, %v), want one completed turn", second, second.Err)
	}
	if want := event.InvocationID(string(addr.SessionID) + "#c2"); second.Turn.InvocationID != want {
		t.Fatalf("the late settlement opened the segment %q, want the next segment of the address (%q)", second.Turn.InvocationID, want)
	}
	requests := rig.behaviour.requestMessages()
	if len(requests) != 2 {
		t.Fatalf("model requests of %s = %d, want the closing answer beside the one the late settlement drove", addr.SessionID, len(requests))
	}
	if !invariantCarries(requests[1], observation) {
		t.Fatalf("the request the late settlement drove carries no observation: %+v", requests[1])
	}
	if got := len(rig.outputs(addr)); got != 2 {
		t.Fatalf("agent_output facts of %s = %d, want the closed segment beside the one the settlement drove", addr.SessionID, got)
	}
}

// TestIdleMountConsumesFirstEvent pins that a fact which arrived before its
// address was mounted needs no second event: the mount reads the chain it opens
// on and consumes the driving fact by itself.
// - the event lands on an idle address, before any consumer exists for it
// - the first mount of that address reads the chain and drives the event
// - no wake and no second event stands beside the one request the event drives
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestIdleMountConsumesFirstEvent(t *testing.T) {
	rig := newResidentInstanceRig(t)
	addr := rig.otherAddress()
	key := rig.landInput(addr, "the event that lands before the mount")
	consumer := rig.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatalf("the instance of %s/%s = nil, want the resident consumer of the address", addr.AgentID, addr.SessionID)
	}
	answer := rig.awaitAnswer(t, addr)
	if answer.Err != nil || answer.Turn.Terminal != model.KindCompleted {
		t.Fatalf("the first mount of %s closed = (%+v, %v), want the event it found on the chain driven", addr.SessionID, answer, answer.Err)
	}
	if len(answer.Consumed) != 1 || answer.Consumed[0] != key {
		t.Fatalf("the mounted segment of %s consumed %v, want the event that landed before it (%q)", addr.SessionID, answer.Consumed, key)
	}
	requests := rig.behaviour.requestMessages()
	if len(requests) != 1 || !invariantCarries(requests[0], "the event that lands before the mount") {
		t.Fatalf("model requests of the first mount = %+v, want the one request carrying the event", requests)
	}
}

// TestNoOrphanNoSpin pins the negative side of the self-driving law: a wake
// beside no related fact opens no segment, so the loop never spins a model
// request out of its own wake.
// - a chain that already closed its segment carries no driving fact
// - a wake of that address opens no segment and sends no request
// - the output the chain already held stays the only one
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestNoOrphanNoSpin(t *testing.T) {
	rig := newResidentInstanceRig(t)
	addr := rig.otherAddress()
	rig.landAnswer(addr, "the answer the chain already holds")
	consumer := rig.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatalf("the instance of %s/%s = nil, want the resident consumer of the address", addr.AgentID, addr.SessionID)
	}
	consumer.Wake()
	rig.expectQuiet(t, addr, "a wake beside no related fact opened a segment out of its own wake")
	if got := len(rig.outputs(addr)); got != 1 {
		t.Fatalf("agent_output facts of %s = %d, want the one the chain already held", addr.SessionID, got)
	}
}
