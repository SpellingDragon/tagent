package agent

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	api "github.com/SpellingDragon/tagent/api/model"
)

// exitModel kills the goroutine that drives it: its GenerateContent calls
// runtime.Goexit the way a helper that fails a check inside a decision would,
// leaving the segment that admitted the call flying beside a consumer
// goroutine that already ended.
type exitModel struct{}

func (exitModel) GenerateContent(context.Context, *api.Request) (<-chan *api.Response, error) {
	runtime.Goexit()
	return nil, nil // unreachable
}

func (exitModel) Info() api.Info { return api.Info{Name: "model-exit"} }

// TestAbandonedFlyingSettlesOnAbnormalExit pins the abnormal-exit half of the
// rest criterion: a consumer goroutine that dies inside a decision settles the
// segment it left flying, so the address rests honestly and a close beside it
// reports quiesced instead of burning the whole budget beside ended work.
func TestAbandonedFlyingSettlesOnAbnormalExit(t *testing.T) {
	rig := newResidentInstanceRigWithModel(t, &exitOnceModel{})
	addr := rig.otherAddress()
	consumer := rig.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatalf("the instance of %s/%s = nil, want one consumer to hold", addr.AgentID, addr.SessionID)
	}
	rig.landInput(addr, "the input whose decision kills the driver")
	consumer.Wake()
	// The killed goroutine must first be seen driving: the decision that dies
	// proves the segment flew, so a resting answer after it is the abnormal
	// exit settling, not a consumer that never woke.
	time.Sleep(100 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if consumer.Resting() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	consumer.mu.Lock()
	busy, flying, wake := consumer.busy, consumer.flying != nil, len(consumer.wake)
	consumer.mu.Unlock()
	t.Fatalf("the consumer did not rest within 2s beside an ended goroutine: busy=%v flying=%v wake=%d", busy, flying, wake)
}

// exitOnceModel kills the goroutine that drives its first decision and answers
// every later one normally: the first call proves the abnormal exit path, the
// later ones prove the decision resources the killed one held were released —
// a model writer, a decision slot or a binding handle left held would strand
// the next decision of the address on a dead instance forever.
type exitOnceModel struct {
	mu     sync.Mutex
	served int
}

func (m *exitOnceModel) GenerateContent(context.Context, *api.Request) (<-chan *api.Response, error) {
	m.mu.Lock()
	m.served++
	first := m.served == 1
	m.mu.Unlock()
	if first {
		runtime.Goexit()
		return nil, nil // unreachable
	}
	ch := make(chan *api.Response, 1)
	ch <- &api.Response{Done: true, Choices: []api.Choice{{Message: api.Message{Role: api.RoleAssistant, Content: "alive again"}}}}
	close(ch)
	return ch, nil
}

func (m *exitOnceModel) Info() api.Info { return api.Info{Name: "model-exit-once"} }

// TestDecisionResourcesReleaseBesideAnAbnormalExit pins the resource half the
// resting criterion alone cannot see: after a decision goroutine dies, the
// model writer it began is stopped, the decision slot it held is vacated and
// the binding handle it borrowed is released, so the address can decide again.
func TestDecisionResourcesReleaseBesideAnAbnormalExit(t *testing.T) {
	rig := newResidentInstanceRigWithModel(t, &exitOnceModel{})
	addr := rig.otherAddress()
	consumer := rig.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatalf("the instance of %s/%s = nil, want one consumer to hold", addr.AgentID, addr.SessionID)
	}
	rig.landInput(addr, "the input whose decision kills the driver")
	consumer.Wake()
	time.Sleep(100 * time.Millisecond)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if stopped, live := rig.loop.Stopped(); stopped {
			if len(live) != 0 {
				t.Fatalf("writers = %v, want none beside an ended goroutine", live)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if stopped, live := rig.loop.Stopped(); !stopped {
		t.Fatalf("writers = %v, want the model writer of the dead decision released", live)
	}
	// The address must decide again: a slot or handle left held strands the
	// next driver of this address on the dead instance forever. The dead
	// consumer's run goroutine ended with its decision, so the restored drive
	// mounts a fresh instance exactly as a production ensure would.
	fresh := rig.consumers.Ensure(addr)
	if fresh == nil {
		t.Fatalf("the instance of %s/%s = nil after the abnormal exit, want a fresh one", addr.AgentID, addr.SessionID)
	}
	rig.landInput(addr, "the input the restored drive answers")
	fresh.Wake()
	answer := rig.awaitAnswer(t, addr)
	if answer.Err != nil {
		t.Fatalf("the restored drive closed beside %v, want the completed turn of the alive decision", answer.Err)
	}
	if answer.Body != "alive again" {
		t.Fatalf("restored answer = %q, want the body the alive decision produced", answer.Body)
	}
}
