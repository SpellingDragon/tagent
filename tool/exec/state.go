// 契约: docs/wiki/agent/task-lifecycle.md#ttl-reclaim
//
// The state face of the execution resource manager: the responsibility range a
// resource is tracked for, the lifecycle events the manager observes after a
// call returned, and the optional observer that hands those events to the
// runtime. The face stays inside this package's own vocabulary: the root
// assembly supplies an OwnerResolver instead of the package importing the task
// or agent owners, so the dependency edge of the golden architecture holds.
package exec

import (
	"context"
	"errors"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
)

// Owner is the responsibility range one execution resource is tracked for: the
// explicit address a lifecycle event is delivered to and the task identity of
// the call that acquired the resource. It is a plain value, so the root
// assembly resolves it from the call frame it already holds without this
// package depending on the task owner.
type Owner struct {
	// Address is the agent session address the acquiring call belongs to.
	Address event.Address
	// TaskID is the task identity the acquiring call was admitted under.
	TaskID event.TaskID
}

// complete reports whether the owner names a deliverable range: an incomplete
// address or an empty task names no consumer, so the event is still observed
// but no follow-up turn is driven from it.
func (o Owner) complete() bool {
	return o.Address.IsComplete() && o.TaskID != ""
}

// OwnerResolver reports the responsibility range one execution call belongs to
// at the moment the call runs. A call beside no resolvable frame answers false
// and is tracked without an owner.
type OwnerResolver func(context.Context) (Owner, bool)

// StatePhase is the lifecycle a held execution resource reached after the call
// that acquired it returned. The phases are observations of the resource, never
// verdicts on the work the command was asked to do: stable is not success and a
// partial frame is not completion.
type StatePhase string

const (
	// StateStable reports an alive resource whose observed frame stood quiet for
	// the stable threshold. It says nothing about the outcome of the command.
	StateStable StatePhase = "stable"
	// StateSuspect reports a live resource whose frame stayed quiet past the
	// fake-dead threshold, or a probe that could not decide. It is a diagnosis
	// escalation, never a death decision.
	StateSuspect StatePhase = "suspect"
	// StateExited reports a resource observed dead and freed beside a confirmed
	// stop; ExitCode carries the observed exit status.
	StateExited StatePhase = "exited"
	// StateReleased reports a resource freed by an explicit release beside a
	// confirmed stop.
	StateReleased StatePhase = "released"
	// StateRetained reports a resource whose stop stayed unconfirmed: it is
	// still held, and the event names why instead of claiming an ending.
	StateRetained StatePhase = "retained"
)

// StateEvent is one lifecycle observation of a held execution resource: what was
// observed, the evidence behind it, and the range it is tracked for. The payload
// carries what the observer needs to render the state; it never rewrites the
// resource's obligations.
//
// The redline list (3 rules):
// MUST keep an unconfirmed stop visible beside the reason it stayed held.
// MUST report an exit code only beside an observed stop.
// MUST NOT report stable or a partial frame as success or completion.
type StateEvent struct {
	// Resource is the resource the observation belongs to.
	Resource ResourceKey
	// Phase is the lifecycle observation this event reports.
	Phase StatePhase
	// ExitCode is the exit status observed beside a confirmed stop. It is
	// meaningful only where Confirmed reports an observed ending.
	ExitCode int
	// Confirmed reports whether the stop behind this event was observed.
	Confirmed bool
	// Released reports whether the resource was freed by this event.
	Released bool
	// Detail carries the observation behind a retained or suspect event.
	Detail string
	// Owner is the responsibility range the resource is tracked for.
	Owner Owner
	// At is the observation time of the event.
	At time.Time
}

// StateObserver receives every lifecycle event of the resources one manager
// holds. The observer is a bypass: it is called outside the manager's lock and
// its failure never changes whether a resource stays held or is freed, so a
// slow or panicking observer cannot turn an ending into a leak or a leak into an
// ending. An observer MUST NOT call back into the manager synchronously.
type StateObserver func(StateEvent)

// WithStateObserver attaches the lifecycle observer of one manager. Only a
// manager carrying an observer starts the post-return tracker; a manager beside
// none keeps the call-scoped renewal and the query face of the resource
// exactly as they were.
func WithStateObserver(observer StateObserver) ManagerOption {
	return func(m *Manager) { m.observer = observer }
}

// withOwner binds the responsibility range one acquisition is tracked for. It
// is the option the run tool fills from its owner resolver and never a public
// acquisition option: an owner names the caller's own frame, not a choice the
// declaration makes.
func withOwner(owner Owner) AcquireOption {
	return func(a *Acquisition) { a.owner = owner }
}

// watch starts the post-return tracker of one held resource. The tracker owns
// the renewal of the lease from here on, so a call that returned does not end
// the lease beside itself. A manager beside no observer starts no tracker: the
// call-scoped renewal remains the only renewal then, exactly as before this
// face existed.
func (m *Manager) watch(key ResourceKey) {
	m.mu.Lock()
	res, ok := m.resources[key]
	if !ok || m.observer == nil || m.closing || res.stop != nil {
		m.mu.Unlock()
		return
	}
	res.stop = make(chan struct{})
	res.done = make(chan struct{})
	stop, done := res.stop, res.done
	m.mu.Unlock()
	go m.track(key, stop, done)
}

// track keeps one resource under observation until it is released, observed
// dead, renewed beside a lease that already expired, or the manager closes. Each
// tick renews first — the lease belongs to the tracked resource, not to the call
// that returned — then probes the resource and classifies its silence.
//
// The first observations are the tracker's baseline. A resource that ended
// inside the call's own window (dead before it was observed alive at two ticks)
// is not reported as a lifecycle event: the call's own answer and the explicit
// release are its surface. A resource observed alive at two ticks and then dead
// is an ending the call never saw, so it is settled here and reported beside the
// exit status the stop observed.
func (m *Manager) track(key ResourceKey, stop, done chan struct{}) {
	defer close(done)
	m.mu.Lock()
	res, ok := m.resources[key]
	if !ok {
		m.mu.Unlock()
		return
	}
	rhythm := rhythmWithDefaults(res.rhythm)
	ttl := m.leaseOf(res.ttl)
	m.mu.Unlock()
	cadence := rhythm.Interval
	if half := ttl / 2; half > 0 && cadence > half {
		cadence = half
	}
	if cadence <= 0 {
		return
	}
	monitor := NewMonitor(m.backend, rhythm)
	ticker := time.NewTicker(cadence)
	defer ticker.Stop()
	aliveSeen := 0
	status := MonitorWatch
	unknownNoted := false
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		if err := m.Renew(context.Background(), key, m.ttlOf(key)); err != nil {
			return
		}
		observed, err := m.Observe(context.Background(), key)
		if err != nil {
			return
		}
		switch observed.State {
		case ProbeDead:
			if aliveSeen < 2 {
				return
			}
			m.exitObserved(key)
			return
		case ProbeAlive:
			aliveSeen++
			unknownNoted = false
			next, err := monitor.Poll(context.Background(), key)
			if err != nil {
				continue
			}
			if next == status {
				continue
			}
			status = next
			switch next {
			case MonitorStable:
				m.emit(StateEvent{
					Resource: key,
					Phase:    StateStable,
					Detail:   "the observed frame stood quiet for the stable threshold",
					Owner:    m.ownerOf(key),
				})
			case MonitorSuspect:
				m.emit(StateEvent{
					Resource: key,
					Phase:    StateSuspect,
					Detail:   "the frame stayed quiet past the fake-dead threshold while the resource was alive",
					Owner:    m.ownerOf(key),
				})
			}
		default:
			if unknownNoted {
				continue
			}
			unknownNoted = true
			m.emit(StateEvent{
				Resource: key,
				Phase:    StateSuspect,
				Detail:   observed.Detail,
				Owner:    m.ownerOf(key),
			})
		}
	}
}

// exitObserved settles one tracked resource whose death the call never saw: the
// stop is observed through the executor so the exit status is read beside the
// ending, and the resource is freed only beside a confirmed stop. A stop that
// stays unconfirmed keeps the resource held and is reported as retained, never
// as an exit.
func (m *Manager) exitObserved(key ResourceKey) {
	owner := m.ownerOf(key)
	evidence, err := m.release(context.Background(), key)
	if err != nil && errors.Is(err, ErrResourceUnknown) {
		return
	}
	if evidence.Released {
		m.emit(StateEvent{
			Resource:  key,
			Phase:     StateExited,
			ExitCode:  evidence.Stop.ExitCode,
			Confirmed: true,
			Released:  true,
			Owner:     owner,
		})
		return
	}
	m.emit(StateEvent{
		Resource:  key,
		Phase:     StateRetained,
		Confirmed: evidence.Stop.Confirmed,
		Detail:    evidence.Reason,
		Owner:     owner,
	})
}

// emit hands one lifecycle event to the observer of the manager, if any. The
// observer is read under the lock and called outside it, so an observer that
// blocks or calls back never holds the resource ledger.
func (m *Manager) emit(evt StateEvent) {
	m.mu.Lock()
	observer := m.observer
	m.mu.Unlock()
	if observer == nil {
		return
	}
	if evt.At.IsZero() {
		evt.At = m.now()
	}
	observer(evt)
}

// ownerOf reports the responsibility range one held resource is tracked for.
func (m *Manager) ownerOf(key ResourceKey) Owner {
	m.mu.Lock()
	defer m.mu.Unlock()
	if res, ok := m.resources[key]; ok {
		return res.owner
	}
	return Owner{}
}

// ttlOf reports the lease span the tracker renews one held resource by: the span
// the acquisition declared, or the manager floor beside an acquisition that
// declared none.
func (m *Manager) ttlOf(key ResourceKey) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if res, ok := m.resources[key]; ok {
		return m.leaseOf(res.ttl)
	}
	return m.defaultTTL
}

// stopTracking ends the tracker of one resource, if any, and reports the range
// the resource is tracked for beside whether it is still held. The tracker is
// not joined here: it leaves at its next observation, while the caller settles
// the resource under the same key. The caller must not hold the manager lock.
func (m *Manager) stopTracking(key ResourceKey) (Owner, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res, ok := m.resources[key]
	if !ok {
		return Owner{}, false
	}
	if res.stop != nil {
		close(res.stop)
		res.stop = nil
	}
	return res.owner, true
}
