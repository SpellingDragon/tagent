// The idle-address reclamation checks: a resident instance is reclaimed only
// when it rests, owes no delivery and passed the idle TTL, the sweep runs at the
// input boundary of the set, and the reclamation is observable as the log line
// carrying the address and the reason.
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
package agent

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SpellingDragon/tagent/api/event"

	trpclog "trpc.group/trpc-go/trpc-agent-go/log"
)

// recycleLog records the WARN lines of the reclamation face while delegating
// every other level to the default logger, so one check reads exactly the line
// it caused.
type recycleLog struct {
	trpclog.Logger
	mu    sync.Mutex
	warns []string
}

// Warnf records one formatted warning line.
func (l *recycleLog) Warnf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, fmt.Sprintf(format, args...))
}

// warned reports whether one recorded line carries every fragment.
func (l *recycleLog) warned(fragments ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.warns {
		held := true
		for _, fragment := range fragments {
			if !strings.Contains(line, fragment) {
				held = false
			}
		}
		if held {
			return true
		}
	}
	return false
}

// awaitResting waits until one consumer stands between passes.
func awaitResting(t *testing.T, consumer *Consumer) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !consumer.Resting() {
		if time.Now().After(deadline) {
			t.Fatalf("the consumer of %s/%s never rested", consumer.addr.AgentID, consumer.addr.SessionID)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestIdleAddressReclaimed pins the positive reclamation of a resting address.
// - a resting address past the TTL is reclaimed and its loop ends
// - a later Ensure mounts a fresh instance from the chain
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestIdleAddressReclaimed(t *testing.T) {
	rig := newResidentInstanceRig(t)
	addr := rig.otherAddress()
	consumer := rig.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatal("the address mounted no instance")
	}
	awaitResting(t, consumer)
	rig.consumers.SetIdleTTL(30 * time.Second)
	now := time.Now()
	if got := rig.consumers.RecycleIdle(now); got != 0 {
		t.Fatalf("the first sweep reclaimed %d instances, want it to only record the first resting moment", got)
	}
	if got := rig.consumers.RecycleIdle(now.Add(10 * time.Second)); got != 0 {
		t.Fatalf("the sweep before the TTL reclaimed %d instances", got)
	}
	if got := rig.consumers.RecycleIdle(now.Add(31 * time.Second)); got != 1 {
		t.Fatalf("the sweep past the TTL reclaimed %d instances, want the one resting address", got)
	}
	select {
	case <-consumer.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the reclaimed instance did not end")
	}
	if got := rig.instances(); got != 0 {
		t.Fatalf("the loop still drives %d instances of the reclaimed address", got)
	}
	rebuilt := rig.consumers.Ensure(addr)
	if rebuilt == nil || rebuilt == consumer {
		t.Fatalf("the address answered rebuilt=%v old=%v, want a fresh instance", rebuilt, consumer)
	}
	if got := rig.instances(); got != 1 {
		t.Fatalf("the loop drives %d instances after the rebuild, want 1", got)
	}
}

// TestActiveAddressNotReclaimed pins the two negatives.
// - an address whose segment flies or whose pass is owed is never reclaimed
// - an address whose delivery account still owes a target is never reclaimed, however long it rests
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestActiveAddressNotReclaimed(t *testing.T) {
	behaviour := newParkingLoopModel()
	flying := newResidentInstanceRigWithModel(t, behaviour)
	flyingAddr := flying.otherAddress()
	flying.landInput(flyingAddr, residentInstanceInput)
	flyingConsumer := flying.consumers.Ensure(flyingAddr)
	if flyingConsumer == nil {
		t.Fatal("the flying address mounted no instance")
	}
	flyingConsumer.Wake()
	select {
	case <-behaviour.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the segment of the address never flew")
	}
	flying.consumers.SetIdleTTL(30 * time.Second)
	now := time.Now()
	for at := 0; at < 3; at++ {
		if got := flying.consumers.RecycleIdle(now.Add(time.Duration(at) * time.Minute)); got != 0 {
			t.Fatalf("the sweep %d reclaimed %d instances beside a flying segment", at, got)
		}
	}
	behaviour.release()

	rig := newResidentInstanceRig(t)
	addr := rig.otherAddress()
	consumer := rig.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatal("the address mounted no instance")
	}
	awaitResting(t, consumer)
	rig.consumers.SetIdleTTL(30 * time.Second)
	rig.consumers.SetDeliveryPending(func(probe event.Address) bool { return probe == addr })
	now = time.Now()
	for at := 0; at < 3; at++ {
		if got := rig.consumers.RecycleIdle(now.Add(time.Duration(at) * time.Minute)); got != 0 {
			t.Fatalf("the sweep %d reclaimed %d instances beside an unsettled delivery", at, got)
		}
	}
	rig.consumers.SetDeliveryPending(nil)
	if got := rig.consumers.RecycleIdle(now.Add(3 * time.Minute)); got != 0 {
		t.Fatalf("the first sweep after the obligation cleared reclaimed %d instances, want it to record the resting moment", got)
	}
	if got := rig.consumers.RecycleIdle(now.Add(4 * time.Minute)); got != 1 {
		t.Fatalf("the sweep past the TTL reclaimed %d instances, want the cleared address", got)
	}
}

// TestRecycleObservable pins the observation face of one reclamation.
// - one reclamation renders one WARN line with the address and the reason
// - a second sweep never reclaims the same instance twice
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestRecycleObservable(t *testing.T) {
	logger := &recycleLog{Logger: trpclog.Default}
	watchConsumerLog(t, logger)
	rig := newResidentInstanceRig(t)
	addr := rig.otherAddress()
	consumer := rig.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatal("the address mounted no instance")
	}
	awaitResting(t, consumer)
	rig.consumers.SetIdleTTL(30 * time.Second)
	now := time.Now()
	rig.consumers.RecycleIdle(now)
	if got := rig.consumers.RecycleIdle(now.Add(31 * time.Second)); got != 1 {
		t.Fatalf("the sweep reclaimed %d instances, want 1", got)
	}
	if !logger.warned("recycled idle consumption", "agent="+string(addr.AgentID), "session="+string(addr.SessionID), "reason=idle-ttl") {
		t.Fatalf("warn lines = %v, want one reclamation line carrying the address and the reason", logger.warns)
	}
	if got := rig.consumers.RecycleIdle(now.Add(90 * time.Second)); got != 0 {
		t.Fatalf("a second sweep reclaimed %d instances, want the already reclaimed address reclaimed once", got)
	}
}

// TestIdleReclaimRunsOnTheInputBoundary pins the boundary the sweep runs at.
//   - the set keeps no ticker of its own, so the address that rested a full idle window is reclaimed by the next Ensure
//   - that call answers a fresh mount
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
func TestIdleReclaimRunsOnTheInputBoundary(t *testing.T) {
	rig := newResidentInstanceRig(t)
	addr := rig.otherAddress()
	consumer := rig.consumers.Ensure(addr)
	if consumer == nil {
		t.Fatal("the address mounted no instance")
	}
	awaitResting(t, consumer)
	rig.consumers.SetIdleTTL(20 * time.Millisecond)
	if got := rig.consumers.Ensure(addr); got != consumer {
		t.Fatal("an input beside the resting instance mounted a second driver of the address")
	}
	time.Sleep(50 * time.Millisecond)
	rebuilt := rig.consumers.Ensure(addr)
	if rebuilt == nil || rebuilt == consumer {
		t.Fatalf("the input boundary answered rebuilt=%v old=%v, want a fresh instance", rebuilt, consumer)
	}
	select {
	case <-consumer.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the reclaimed instance did not end")
	}
	if got := rig.instances(); got != 1 {
		t.Fatalf("the loop drives %d instances after the rebuild, want 1", got)
	}
}
