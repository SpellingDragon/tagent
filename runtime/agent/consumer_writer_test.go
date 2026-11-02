// 契约: docs/wiki/agent/event-flow.md#runner-flow
package agent

import (
	"testing"
	"time"
)

// TestTheConsumersTurnIsAWriterOfTheRun pins the registration the close of a run
// reads one consuming loop through:
// - positive: a loop in the turn of work between one wake and the next wait stands in the writer ledger of the run, so the close reads a run that still consumes as a writer that has not stopped
// - negative: the registration closes only when that work really ended — a loop the ledger no longer names is one that returned to its wait, not one whose stream was merely cancelled
// - the name the ledger reports is the address the loop consumes, so the clause a close leaves open names the writer that left it open
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestTheConsumersTurnIsAWriterOfTheRun(t *testing.T) {
	behaviour := newParkingLoopModel()
	defer behaviour.release()
	rig := newResidentInstanceRigWithModel(t, behaviour)
	writers := NewWriters()
	rig.loop.SetWriters(writers)

	addr := rig.otherAddress()
	rig.landInput(addr, residentInstanceInput)
	consumer := rig.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatalf("the instance of %s/%s = nil, want one instance to fly a decision", addr.AgentID, addr.SessionID)
	}
	consumer.Wake()
	select {
	case <-behaviour.entered:
	case <-time.After(10 * time.Second):
		t.Fatalf("no model decision of %s/%s started: the material that landed drove nothing", addr.AgentID, addr.SessionID)
	}
	stopped, names := writers.Stopped()
	if stopped {
		t.Fatal("the ledger reports every writer stopped while a consumer loop holds a decision in flight")
	}
	want := "consumer:" + string(addr.AgentID) + "/" + string(addr.SessionID)
	found := false
	for _, name := range names {
		if name == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("the writers the close reads as open = %v, want the consuming loop of %q among them", names, want)
	}

	// The cancellation ends the decision and the loop returns to its wait: only
	// then does the registration close.
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		rig.consumers.Stop()
	}()
	behaviour.release()
	select {
	case <-joined:
	case <-time.After(10 * time.Second):
		t.Fatal("the stop of the set never returned beside the loop it cancelled")
	}
	select {
	case <-consumer.Done():
	default:
		t.Fatal("the set answered its stop beside a loop that still runs")
	}
	if stopped, names := writers.Stopped(); !stopped {
		t.Fatalf("the ledger still names the writers %v after every loop returned, want every registration closed", names)
	}
}
