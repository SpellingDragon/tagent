package exec

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Manager owns the whole life of the execution resources one runtime holds:
// the acquisition under a declared key, the lease that keeps the resource held,
// the probes that observe it and the release that frees it beside a confirmed
// stop. A resource already held under a key is never opened a second time, and
// a stop the executor cannot confirm leaves the resource held under its key.
//
// The manager reports what it observed rather than what a caller requested: a
// probe that cannot decide reports unknown, an unconfirmed stop keeps the lease
// and the open obligations of the resource, and every retained state answers an
// Explain with the facts it is held on.
//
// 契约: docs/wiki/agent/task-lifecycle.md#ttl-reclaim
type Manager struct {
	mu          sync.Mutex
	backend     Executor
	now         func() time.Time
	available   func(ResourceKey) bool
	defaultTTL  time.Duration
	resources   map[ResourceKey]*heldResource
	acquired    int
	adopted     int
	reused      int
	released    int
	escalations int
}

// DefaultLeaseTTL is the floor lease of an execution resource whose caller
// declared no TTL. No acquisition runs without a lease deadline.
const DefaultLeaseTTL = 30 * time.Minute

// ErrResourceHeld reports an acquisition refused because the resource is
// already held under that key, so the key stays owned by its single holder.
var ErrResourceHeld = errors.New("exec: the resource is already held under that key")

// ErrResourceUnknown reports a lifecycle call over a key no resource is held
// under, so there is nothing the call could observe or free.
var ErrResourceUnknown = errors.New("exec: no execution resource is held under that key")

// ErrKeyMissing reports a declaration that names no resource key, so no
// resource identity could be compared or held.
var ErrKeyMissing = errors.New("exec: the declaration names no resource key")

// ErrAdoptUnconfirmed reports an adoption refused because the resource behind
// the key was not observed alive. An undeterminable probe is not an absence.
var ErrAdoptUnconfirmed = errors.New("exec: the resource behind that key was not observed alive")

// ErrLeaseInvalid reports a renewal or acquisition over a negative lease, which
// no resource runs under.
var ErrLeaseInvalid = errors.New("exec: the lease must not be negative")

// ErrLeaseExpired reports a renewal refused because the lease of the resource
// reached its deadline before the renewal. An expired lease belongs to the
// explicit stop handling of a sweep, so a late renewal never revives it beside
// a settlement already in flight.
var ErrLeaseExpired = errors.New("exec: the lease reached its deadline before this renewal")

// heldResource is one held execution resource: the declaration it was started
// under, the lease keeping it held, the probe observations recorded against it
// and the obligations that stay open until a confirmed release.
type heldResource struct {
	decl        ExecutionDecl
	rhythm      MonitorConfig
	deadline    time.Time
	renewed     int
	adopted     bool
	unknownRun  int
	escalations int
	lastStop    *StopEvidence
	obligations []string
}

// Acquisition configures one acquisition at its call.
type Acquisition struct {
	ttl    time.Duration
	rhythm MonitorConfig
}

// AcquireOption configures one acquisition at its call.
type AcquireOption func(*Acquisition)

// WithTTL sets the lease of one acquisition. A zero TTL takes the manager
// floor, and a negative TTL is refused.
func WithTTL(ttl time.Duration) AcquireOption {
	return func(a *Acquisition) { a.ttl = ttl }
}

// WithMonitorConfig binds the monitor rhythm to the generation one acquisition
// opens, so the rhythm travels with the call that consumes it.
func WithMonitorConfig(rhythm MonitorConfig) AcquireOption {
	return func(a *Acquisition) { a.rhythm = rhythm }
}

// ManagerOption configures one manager at construction.
type ManagerOption func(*Manager)

// WithClock sets the time source the leases and sweeps read.
func WithClock(now func() time.Time) ManagerOption {
	return func(m *Manager) { m.now = now }
}

// WithAvailability sets the capability probe an acquisition consults before it
// starts a resource of any kind.
func WithAvailability(available func(ResourceKey) bool) ManagerOption {
	return func(m *Manager) { m.available = available }
}

// WithDefaultTTL sets the floor lease of an acquisition that declares no TTL.
func WithDefaultTTL(ttl time.Duration) ManagerOption {
	return func(m *Manager) { m.defaultTTL = ttl }
}

// NewManager returns the execution resource manager over one executor: every
// acquisition, probe, sweep and release runs through the executor behind it.
func NewManager(backend Executor, opts ...ManagerOption) *Manager {
	m := &Manager{
		backend:    backend,
		now:        time.Now,
		defaultTTL: DefaultLeaseTTL,
		resources:  map[ResourceKey]*heldResource{},
	}
	for _, opt := range opts {
		opt(m)
	}
	if m.available == nil {
		m.available = defaultAvailability
	}
	return m
}

// defaultAvailability reports the capability the declared kind needs: the tmux
// family rests on the real PATH probe, and any other kind needs no system
// capability of its own.
func defaultAvailability(key ResourceKey) bool {
	if key.Kind == KindTmux {
		return IsTmuxAvailable()
	}
	return true
}

// Acquire starts the execution declared by decl and returns the canonical key
// of the resource it now holds, or returns the key of the resource already held
// under that identity without starting a second one. A held resource whose last
// stop stayed unconfirmed refuses the acquisition instead, so its key is never
// handed to a second writer beside an unresolved stop. A resource whose system
// capability is missing is refused beside the degrade report of that refusal,
// and the monitor rhythm of the call rides that report.
func (m *Manager) Acquire(ctx context.Context, decl ExecutionDecl, opts ...AcquireOption) (ResourceKey, error) {
	if decl.Resource.Key == "" {
		return ResourceKey{}, ErrKeyMissing
	}
	acq := Acquisition{rhythm: DefaultMonitorConfig}
	for _, opt := range opts {
		opt(&acq)
	}
	if acq.ttl < 0 {
		return ResourceKey{}, ErrLeaseInvalid
	}
	if acq.ttl == 0 {
		acq.ttl = m.defaultTTL
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if held, ok := m.resources[decl.Resource]; ok {
		if held.lastStop != nil && !held.lastStop.Confirmed {
			return ResourceKey{}, fmt.Errorf("%w: %s", ErrResourceHeld, decl.Resource)
		}
		m.reused++
		return decl.Resource, nil
	}
	if !m.available(decl.Resource) {
		return ResourceKey{}, &DegradeError{Key: decl.Resource, Rhythm: acq.rhythm, Reason: "the execution capability behind this resource kind is missing"}
	}
	key, err := m.backend.Start(ctx, decl)
	if err != nil {
		return ResourceKey{}, err
	}
	m.resources[key] = &heldResource{
		decl:        decl,
		rhythm:      acq.rhythm,
		deadline:    m.now().Add(acq.ttl),
		obligations: []string{"the execution outcome is unsettled"},
	}
	m.acquired++
	return key, nil
}

// Adopt binds a lease to the resource already running behind key without
// starting a second execution, so a runtime that re-attaches a session takes
// over what it found. The adoption is refused beside the evidence of what the
// probe observed, and a probe that could not decide is not an absence.
func (m *Manager) Adopt(ctx context.Context, key ResourceKey, opts ...AcquireOption) error {
	acq := Acquisition{rhythm: DefaultMonitorConfig}
	for _, opt := range opts {
		opt(&acq)
	}
	if acq.ttl < 0 {
		return ErrLeaseInvalid
	}
	if acq.ttl == 0 {
		acq.ttl = m.defaultTTL
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, held := m.resources[key]; held {
		return ErrResourceHeld
	}
	observed, err := m.backend.Probe(ctx, key)
	if err != nil {
		return err
	}
	if observed.State != ProbeAlive {
		return fmt.Errorf("%w: %s", ErrAdoptUnconfirmed, observed.Detail)
	}
	m.resources[key] = &heldResource{
		rhythm:      acq.rhythm,
		deadline:    m.now().Add(acq.ttl),
		adopted:     true,
		obligations: []string{"the reattached execution outcome is unsettled"},
	}
	m.adopted++
	return nil
}

// Renew extends the lease of one held resource to a fresh deadline. A renewal
// over a key nothing is held under is refused by name, and so is a renewal over
// a lease that already reached its deadline: expiry hands the resource to the
// explicit stop handling of a sweep, so a late renewal never revives it.
func (m *Manager) Renew(_ context.Context, key ResourceKey, ttl time.Duration) error {
	if ttl < 0 {
		return ErrLeaseInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	res, ok := m.resources[key]
	if !ok {
		return fmt.Errorf("%w: %s", ErrResourceUnknown, key)
	}
	if !res.deadline.After(m.now()) {
		return fmt.Errorf("%w: %s", ErrLeaseExpired, key)
	}
	res.deadline = m.now().Add(m.leaseOf(ttl))
	res.renewed++
	return nil
}

// leaseOf reports the lease span one declared ttl stands for: the declared span
// where the caller declares one and the manager floor otherwise. It is the span
// a renewal keeps a resource held for and the span a renewing call reads the
// cadence of its own renewals out of.
func (m *Manager) leaseOf(ttl time.Duration) time.Duration {
	if ttl > 0 {
		return ttl
	}
	return m.defaultTTL
}

// Observe reports what one probe observed about the held resource. A probe that
// could not decide extends the unknown run of the resource and raises one
// diagnosis escalation: unknown observations never free the resource and never
// decide its death. A probe that does decide resets the unknown run.
func (m *Manager) Observe(ctx context.Context, key ResourceKey) (ProbeResult, error) {
	m.mu.Lock()
	res, ok := m.resources[key]
	m.mu.Unlock()
	if !ok {
		return ProbeResult{}, fmt.Errorf("%w: %s", ErrResourceUnknown, key)
	}
	observed, err := m.backend.Probe(ctx, key)
	if err != nil {
		return ProbeResult{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if current, ok := m.resources[key]; ok && current == res {
		if observed.State == ProbeUnknown {
			res.unknownRun++
			res.escalations++
			m.escalations++
		} else {
			res.unknownRun = 0
		}
	}
	return observed, nil
}

// Capture returns the output the executor recorded for one held resource.
func (m *Manager) Capture(ctx context.Context, key ResourceKey) ([]byte, error) {
	m.mu.Lock()
	_, ok := m.resources[key]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrResourceUnknown, key)
	}
	return m.backend.Capture(ctx, key)
}

// Release frees one held resource beside a confirmed stop and reports the
// evidence behind the outcome. A stop the executor could not confirm keeps the
// resource held under its key beside its open obligations, and the evidence
// reports that retention rather than an ending. The stop is observed under its
// own context, so a caller context already cancelled neither stops nor frees
// anything.
func (m *Manager) Release(ctx context.Context, key ResourceKey) (ReleaseEvidence, error) {
	m.mu.Lock()
	_, ok := m.resources[key]
	m.mu.Unlock()
	if !ok {
		return ReleaseEvidence{Resource: key}, fmt.Errorf("%w: %s", ErrResourceUnknown, key)
	}
	stop, err := m.backend.Stop(context.WithoutCancel(ctx), key)
	evidence := ReleaseEvidence{Resource: key, Stop: stop}
	if err != nil {
		evidence.Reason = err.Error()
		m.noteStop(key, stop)
		return evidence, err
	}
	if !stop.Confirmed {
		evidence.Reason = stop.Reason
		m.noteStop(key, stop)
		return evidence, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, held := m.resources[key]; held {
		delete(m.resources, key)
		m.released++
	}
	evidence.Released = true
	return evidence, nil
}

// noteStop records the stop observation one retained resource was last seen
// under, so an Explain names what kept the resource held.
func (m *Manager) noteStop(key ResourceKey, stop StopEvidence) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if res, ok := m.resources[key]; ok {
		observed := stop
		res.lastStop = &observed
	}
}

// SweepReport is what one sweep observed for one expired resource.
type SweepReport struct {
	// Key is the resource whose lease reached its deadline.
	Key ResourceKey
	// Expired reports that the lease reached its deadline before the sweep.
	Expired bool
	// Evidence is the release outcome the sweep drove for the expired lease.
	Evidence ReleaseEvidence
}

// Sweep drives the explicit stop handling of every resource whose lease reached
// its deadline and reports the outcome of each. Expiry frees nothing on its
// own: each expired resource is stopped through the executor, and one whose
// stop stays unconfirmed stays held beside the report that says so.
func (m *Manager) Sweep(ctx context.Context) []SweepReport {
	m.mu.Lock()
	now := m.now()
	var expired []ResourceKey
	for key, res := range m.resources {
		if !res.deadline.After(now) {
			expired = append(expired, key)
		}
	}
	m.mu.Unlock()
	sort.Slice(expired, func(i, j int) bool { return expired[i].String() < expired[j].String() })
	reports := make([]SweepReport, 0, len(expired))
	for _, key := range expired {
		evidence, err := m.Release(ctx, key)
		if err != nil {
			evidence.Reason = err.Error()
		}
		reports = append(reports, SweepReport{Key: key, Expired: true, Evidence: evidence})
	}
	return reports
}

// Explanation is the retained state of one held resource: the lease, the
// monitor rhythm of its generation, the probe unknowns recorded against it and
// the obligations kept open beside it.
type Explanation struct {
	// Key is the resource the explanation reports on.
	Key ResourceKey
	// Held reports whether the resource is still held under its key.
	Held bool
	// Rhythm is the monitor rhythm the generation of this resource runs under.
	Rhythm MonitorConfig
	// Deadline is the lease deadline the resource is held to.
	Deadline time.Time
	// Renewed counts the lease renewals the resource received.
	Renewed int
	// Adopted reports whether the resource was reattached rather than started.
	Adopted bool
	// ConsecutiveUnknowns counts the undecidable probes since the last probe
	// that did decide.
	ConsecutiveUnknowns int
	// Escalations counts the diagnosis escalations raised against the resource.
	Escalations int
	// LastStop is the stop observation the resource was last seen under.
	LastStop *StopEvidence
	// Obligations are the open obligations kept beside the lease.
	Obligations []string
}

// Explain reports the retained state of one held resource beside the facts it
// is held on. A key nothing is held under answers as not held.
func (m *Manager) Explain(key ResourceKey) (Explanation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res, ok := m.resources[key]
	if !ok {
		return Explanation{Key: key}, fmt.Errorf("%w: %s", ErrResourceUnknown, key)
	}
	explanation := Explanation{
		Key:                 key,
		Held:                true,
		Rhythm:              res.rhythm,
		Deadline:            res.deadline,
		Renewed:             res.renewed,
		Adopted:             res.adopted,
		ConsecutiveUnknowns: res.unknownRun,
		Escalations:         res.escalations,
		LastStop:            res.lastStop,
	}
	explanation.Obligations = append(explanation.Obligations, res.obligations...)
	return explanation, nil
}

// Held reports how many resources the manager currently holds.
func (m *Manager) Held() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.resources)
}

// Acquired reports how many resources the manager started.
func (m *Manager) Acquired() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.acquired
}

// Adopted reports how many resources the manager reattached.
func (m *Manager) Adopted() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.adopted
}

// Reused reports how many acquisitions answered from a resource already held
// instead of starting a second one.
func (m *Manager) Reused() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reused
}

// Released reports how many resources were freed beside a confirmed stop.
func (m *Manager) Released() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.released
}

// Escalated reports how many diagnosis escalations undecidable probes raised.
func (m *Manager) Escalated() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.escalations
}
