// 契约: docs/wiki/agent/event-flow.md#trace-anchor
package agent

import (
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// TestRestartContinuesTheRoundCounter pins the round identity a restarted process mints over a chain that already committed a segment.
//   - the chain carries a closed segment under #c1 beside the final_round.request of that range
//   - positive: the first segment of the restarted process mints #c2, so the range the chain already used is never reused
//   - negative: the judgment of #c1 closes nothing of the new segment, so the round the restart runs is the ordinary one
//
// 契约: docs/wiki/agent/event-flow.md#watchdog-final-round
func TestRestartContinuesTheRoundCounter(t *testing.T) {
	before := newResidentInstanceRig(t)
	addr := before.otherAddress()
	before.landInput(addr, residentInstanceInput)
	first := before.consumers.Ensure(addr)
	if first == nil {
		t.Fatalf("the instance of %s/%s = nil, want one instance for the address its material landed on", addr.AgentID, addr.SessionID)
	}
	first.Wake()
	closed := before.awaitAnswer(t, addr)
	if closed.Err != nil {
		t.Fatalf("the segment of %s closed beside %v, want a completed turn", addr.SessionID, closed.Err)
	}
	if want := event.InvocationID(string(addr.SessionID) + "#c1"); closed.Turn.InvocationID != want {
		t.Fatalf("the first segment of %s answered under %q, want the identity of a chain beside no range (%q)", addr.SessionID, closed.Turn.InvocationID, want)
	}
	before.landFinalRoundRequest(addr, closed.Turn.InvocationID, FinalRoundRounds)
	chain := before.history.SessionFacts(addr)

	after := newResidentInstanceRig(t)
	for _, evt := range chain {
		landed := evt
		landed.Key = event.EventID("restart-" + string(evt.Key))
		after.history.land(landed)
	}
	after.landInput(addr, "the material that arrives after the restart")
	consumer := after.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatalf("the restarted run mounted no instance for %s/%s", addr.AgentID, addr.SessionID)
	}
	consumer.Wake()
	resumed := after.awaitAnswer(t, addr)
	if resumed.Err != nil {
		t.Fatalf("the segment of the restarted %s closed beside %v, want a completed turn", addr.SessionID, resumed.Err)
	}
	if resumed.Turn.FinalRound != FinalRoundNone {
		t.Fatalf("the restarted segment of %s ran the closing round %q, want none: the judgment of #c1 closes no round of another range",
			addr.SessionID, resumed.Turn.FinalRound)
	}
	if resumed.Turn.DecisionCount != 1 {
		t.Fatalf("decisions of the restarted segment of %s = %d, want the one ordinary round it drove",
			addr.SessionID, resumed.Turn.DecisionCount)
	}
	if want := event.InvocationID(string(addr.SessionID) + "#c2"); resumed.Turn.InvocationID != want {
		t.Fatalf("the restarted segment of %s answered under %q, want the identity %q of a range the chain never carried",
			addr.SessionID, resumed.Turn.InvocationID, want)
	}
}

// landFinalRoundRequest commits the judgment one producer files for a range, the
// way the final round planner lands the closing round it judged.
func (r *residentInstanceRig) landFinalRoundRequest(addr event.Address, rangeID event.InvocationID, reason FinalRoundReason) event.EventID {
	key := r.mint(addr)
	r.history.land(event.Event{
		Key:          key,
		Type:         event.FinalRoundRequestType,
		Timestamp:    time.Now(),
		Owner:        addr,
		Source:       addr,
		Target:       addr,
		InvocationID: rangeID,
		Meta: map[string]string{
			finalRoundRangeKey:  string(rangeID),
			finalRoundReasonKey: string(reason),
		},
	})
	return key
}
