// The idle-address reclamation checks: a resident consumption instance whose
// address holds no work and no unsettled delivery is reclaimed after the idle
// TTL, an address that still works or still owes a target never is, and the
// reclamation is observable as a named counter beside a log line carrying the
// address and the reason.
//
// 契约: docs/wiki/agent/event-flow.md#runner-flow
package agent

import (
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// defaultIdleAddressTTL bounds how long a resting address with no unsettled
// delivery keeps its instance: a resident loop that served nothing for this long
// is rebuilt from the chain on the next input, exactly as a fresh mount is.
const defaultIdleAddressTTL = 10 * time.Minute

// idleReapInterval is how often the set sweeps its resting addresses. A full
// TTL is measured from the first sweep that found an address resting, so the
// interval only decides the resolution of the reclamation, never its bound.
const idleReapInterval = 30 * time.Second

// SetIdleTTL changes the idle bound of this set. A negative bound keeps every
// instance for the life of the set; a zero restores defaultIdleAddressTTL.
func (cs *Consumers) SetIdleTTL(ttl time.Duration) {
	if cs == nil {
		return
	}
	cs.mu.Lock()
	switch {
	case ttl < 0:
		cs.idleTTL = -1
	case ttl == 0:
		cs.idleTTL = defaultIdleAddressTTL
	default:
		cs.idleTTL = ttl
	}
	cs.mu.Unlock()
}

// SetDeliveryPending registers the predicate the reaper reads before it
// reclaims an address: true names a delivery account that still owes a target
// (a held or owed delivery fact), so the instance stays however long it rests.
// A nil predicate names no unsettled delivery anywhere.
func (cs *Consumers) SetDeliveryPending(pending func(event.Address) bool) {
	if cs == nil {
		return
	}
	cs.mu.Lock()
	cs.deliveryPending = pending
	cs.mu.Unlock()
}

// RecycleCount reports how many idle instances this set reclaimed.
func (cs *Consumers) RecycleCount() uint64 {
	if cs == nil {
		return 0
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.recycled
}

// RecycleIdle runs one reclamation sweep at the given clock reading and answers
// how many instances it reclaimed. An address is reclaimed only when its
// instance rests, its delivery account holds no unsettled obligation and the
// full TTL passed since the first sweep that found it resting. The first sweep
// only records that moment; a loop that starts working or gains an obligation
// loses its record, so the TTL always counts one uninterrupted rest.
func (cs *Consumers) RecycleIdle(now time.Time) int {
	if cs == nil {
		return 0
	}
	cs.mu.Lock()
	if cs.stopped {
		cs.mu.Unlock()
		return 0
	}
	ttl, pending := cs.idleTTL, cs.deliveryPending
	live := make([]*Consumer, 0, len(cs.live))
	for _, consumer := range cs.live {
		live = append(live, consumer)
	}
	cs.mu.Unlock()
	if ttl < 0 {
		return 0
	}
	reclaimed := 0
	for _, consumer := range live {
		addr := consumer.addr
		if pending != nil && pending(addr) {
			cs.clearIdle(addr, nil)
			continue
		}
		if !consumer.Resting() {
			cs.clearIdle(addr, nil)
			continue
		}
		cs.mu.Lock()
		if cs.stopped {
			cs.mu.Unlock()
			break
		}
		if _, held := cs.live[addr]; !held {
			delete(cs.idleSince, addr)
			cs.mu.Unlock()
			continue
		}
		first, seen := cs.idleSince[addr]
		if !seen {
			cs.idleSince[addr] = now
			cs.mu.Unlock()
			continue
		}
		if now.Sub(first) < ttl {
			cs.mu.Unlock()
			continue
		}
		cs.mu.Unlock()
		if cs.reclaim(consumer) {
			reclaimed++
		}
	}
	return reclaimed
}

// clearIdle drops the first-resting mark of one address unless the caller still
// holds that address at the named instance. A nil instance names no expected
// holder, so the mark is cleared whatever instance the address carries.
func (cs *Consumers) clearIdle(addr event.Address, instance *Consumer) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if instance != nil {
		if held, ok := cs.live[addr]; !ok || held != instance {
			return
		}
	}
	delete(cs.idleSince, addr)
}

// reclaim ends one resting instance: the loop registry and the set forget it
// together, the instance's context is cancelled, the named counter advances and
// the reclamation is rendered with the address and the reason. A caller that
// races another reclamation of the same address reclaims nothing, so one
// instance is counted once.
func (cs *Consumers) reclaim(consumer *Consumer) bool {
	if consumer == nil || cs.loop == nil {
		return false
	}
	l := cs.loop
	l.mu.Lock()
	held, ok := l.instances[consumer.addr]
	if !ok || held != consumer {
		l.mu.Unlock()
		return false
	}
	delete(l.instances, consumer.addr)
	l.mu.Unlock()
	cs.mu.Lock()
	current, live := cs.live[consumer.addr]
	if live && current == consumer {
		delete(cs.live, consumer.addr)
	}
	delete(cs.idleSince, consumer.addr)
	cs.recycled++
	count := cs.recycled
	cs.mu.Unlock()
	if consumer.cancel != nil {
		consumer.cancel()
	}
	consumerWarn("tagent: recycled idle consumption address agent=%s session=%s reason=idle-ttl count=%d",
		consumer.addr.AgentID, consumer.addr.SessionID, count)
	return true
}

// reapIdle sweeps the resting addresses on the reaper interval until the set is
// marked stopped. The sweep never starts work and never cancels a loop that is
// not resting, so a set beside live traffic only records what it observes.
func (cs *Consumers) reapIdle() {
	ticker := time.NewTicker(idleReapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-cs.stopping:
			return
		case now := <-ticker.C:
			cs.RecycleIdle(now)
		}
	}
}
