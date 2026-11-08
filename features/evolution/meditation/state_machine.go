// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// The meditation trigger is a self-sustaining state machine, not a projected
// virtual event: external facts charge it, one activation rule discharges it, and
// the discharge is a real external_input fact whose metadata carries the complete
// cause (rule, accumulation window and source). Activation resets the charge, and
// self-managed traffic never charges it, so a reflection cannot feed itself.
package meditation

import (
	"strconv"
	"sync"
	"time"
)

// MetaKeyActivationRule and the other activation meta keys are the cause the
// existing external_input fact carries: they name mechanism fields, not a new fact
// family, so the frozen fact vocabulary does not grow.
const (
	// MetaKeyActivationRule names the rule that discharged the state machine.
	MetaKeyActivationRule = "activation.rule"
	// MetaKeyActivationWindowStart names the moment the accumulation window
	// opened (the execution watermark the pass started from).
	MetaKeyActivationWindowStart = "activation.window_start"
	// MetaKeyActivationWindowEnd names the moment the activation was reached.
	MetaKeyActivationWindowEnd = "activation.window_end"
	// MetaKeyActivationSource names the identity of the fact that charged the
	// activation.
	MetaKeyActivationSource = "activation.source"
	// MetaKeyActivationSourceLineage names the trigger-source stamp of that fact.
	MetaKeyActivationSourceLineage = "activation.source_lineage"
	// MetaKeyActivationCharges names how many external facts the discharged
	// window accumulated.
	MetaKeyActivationCharges = "activation.charges"
	// MetaKeyActivationRequest marks an external input that asks for a
	// reflection explicitly; such a fact discharges under the request rule.
	MetaKeyActivationRequest = "activation.request"
)

// activationRule is the internal transfer the state machine took.
type activationRule string

const (
	// ruleNovelty discharges on a host-facing fact recorded after the watermark:
	// the accumulation threshold every ordinary interaction charges.
	ruleNovelty activationRule = "novelty"
	// ruleQuietWindow discharges accumulated novelty once the quiet floor since
	// the last charge elapsed: the time window is a check, never the only source.
	ruleQuietWindow activationRule = "quiet-window"
	// ruleExternalRequest discharges an explicit reflection request carried by an
	// external input.
	ruleExternalRequest activationRule = "external-request"
)

// energyWindowLimit bounds the dedupe set one open window keeps: a window that
// accumulated more facts still charges every new fact once, while the persisted
// record stays bounded.
const energyWindowLimit = 256

// energyRecord is the persisted charge of one reflection loop: how many external
// facts the open window accumulated, when the window opened and when it last
// charged, which fact charged last and which rule last discharged it. It is saved
// through the anchor store beside the watermark, never in a second store.
type energyRecord struct {
	Charges      int       `json:"charges"`
	WindowStart  time.Time `json:"window_start"`
	LastChargeAt time.Time `json:"last_charge_at"`
	LastSource   string    `json:"last_source"`
	LastLineage  string    `json:"last_lineage"`
	LastRule     string    `json:"last_rule"`
	LastActiveAt time.Time `json:"last_active_at"`
	LastRequest  bool      `json:"last_request"`
	// Charged names the facts the open window already accumulated, so a held
	// pass that re-reads the same scan charges each fact once.
	Charged []string `json:"charged,omitempty"`
}

// energyStore is the persistence face the anchor store offers the state machine:
// the charge is read back on assembly and written after every transfer, so a
// restart resumes the accumulation instead of losing it.
type energyStore interface {
	// LoadEnergy returns the persisted charge; an absent record is an empty
	// charge, never an error.
	LoadEnergy() (energyRecord, error)
	// SaveEnergy persists the charge, replacing the previous one atomically.
	SaveEnergy(energyRecord) error
}

// activation is one discharge of the state machine: the rule, the window it
// accumulated over and the fact that charged it.
type activation struct {
	// Rule is the internal transfer that discharged the machine.
	Rule activationRule
	// WindowStart is the moment the accumulated window opened.
	WindowStart time.Time
	// WindowEnd is the moment the activation was reached.
	WindowEnd time.Time
	// Source is the identity of the fact that charged the activation.
	Source string
	// Lineage is the trigger-source stamp of that fact.
	Lineage string
	// Charges is how many external facts the window accumulated.
	Charges int
}

// stateMachine is the charge one reflection loop holds: external facts charge it,
// one rule discharges it and every transfer persists through the anchor store.
type stateMachine struct {
	mu    sync.Mutex
	store energyStore
	state energyRecord
}

// newStateMachine returns the state machine over one gate: a gate that carries the
// anchor extension restores its charge, and a gate that does not leaves the
// machine in memory only, where it is still the one transfer the reflection reads.
func newStateMachine(gate WatermarkGate) *stateMachine {
	machine := &stateMachine{}
	if store, ok := gate.(energyStore); ok && store != nil {
		machine.store = store
		if record, err := store.LoadEnergy(); err == nil {
			machine.state = record
		}
	}
	return machine
}

// charge accumulates one hydrated fact: a host-facing fact charges the open
// window, and self-managed traffic charges nothing. A fact already charged under
// the same identity is not counted twice, so re-evaluating a held tick does not
// inflate the window.
func (m *stateMachine) charge(fact ScannedFact) bool {
	if m == nil {
		return false
	}
	if SelfManagedLineage(fact.Attribution.Lineage) {
		return false
	}
	key := string(fact.Identity.Key)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, charged := range m.state.Charged {
		if key != "" && charged == key {
			return false
		}
	}
	if m.state.Charges == 0 {
		m.state.WindowStart = fact.Content.Timestamp
	}
	m.state.Charges++
	m.state.LastChargeAt = fact.Content.Timestamp
	m.state.LastSource = key
	m.state.LastLineage = fact.Attribution.Lineage
	m.state.LastRequest = fact.request
	if key != "" {
		m.state.Charged = append(m.state.Charged, key)
		if len(m.state.Charged) > energyWindowLimit {
			m.state.Charged = m.state.Charged[len(m.state.Charged)-energyWindowLimit:]
		}
	}
	m.persistLocked()
	return true
}

// evaluate reports the activation the open window reaches at now, without
// discharging it: an explicit request wins, then a novelty discharge, and a
// quiet window opens once the floor since the last charge elapsed. An empty
// window reaches no activation.
func (m *stateMachine) evaluate(now time.Time, quiet time.Duration) (activation, bool) {
	if m == nil {
		return activation{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Charges == 0 {
		return activation{}, false
	}
	rule := ruleNovelty
	if m.state.LastRequest {
		rule = ruleExternalRequest
	} else if quiet > 0 && !m.state.LastChargeAt.IsZero() && now.Sub(m.state.LastChargeAt) >= quiet {
		rule = ruleQuietWindow
	}
	return activation{
		Rule:        rule,
		WindowStart: m.state.WindowStart,
		WindowEnd:   now,
		Source:      m.state.LastSource,
		Lineage:     m.state.LastLineage,
		Charges:     m.state.Charges,
	}, true
}

// reset discharges the open window: activation always resets the accumulation,
// so the next activation still needs new external facts. The discharged rule is
// kept as the audit record of the last transfer.
func (m *stateMachine) reset(a activation) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Charges = 0
	m.state.WindowStart = time.Time{}
	m.state.Charged = nil
	m.state.LastRule = string(a.Rule)
	m.state.LastActiveAt = a.WindowEnd
	m.persistLocked()
}

// snapshot reports the persisted charge for a reader that audits it.
func (m *stateMachine) snapshot() energyRecord {
	if m == nil {
		return energyRecord{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// persistLocked writes the charge through the store while the caller holds the
// lock. A store that cannot write is reported by leaving the in-memory charge in
// place: the next transfer tries again, and the loop never loses its own
// accumulation on a failed write.
func (m *stateMachine) persistLocked() {
	if m.store == nil {
		return
	}
	_ = m.store.SaveEnergy(m.state)
}

// activationMeta renders the complete cause one activation fact carries. The
// window start is the accumulation window the watermark opened; an empty window
// start falls back to the activation moment so the metadata is always complete.
func activationMeta(a activation) map[string]string {
	start := a.WindowStart
	if start.IsZero() {
		start = a.WindowEnd
	}
	return map[string]string{
		MetaKeyActivationRule:          string(a.Rule),
		MetaKeyActivationWindowStart:   start.UTC().Format(time.RFC3339Nano),
		MetaKeyActivationWindowEnd:     a.WindowEnd.UTC().Format(time.RFC3339Nano),
		MetaKeyActivationSource:        a.Source,
		MetaKeyActivationSourceLineage: a.Lineage,
		MetaKeyActivationCharges:       strconv.Itoa(a.Charges),
	}
}

// The declared shapes of the machine: one fact in, one transfer out, the
// persistence face it reads and the metadata the activation fact carries.
var (
	_ func(WatermarkGate) *stateMachine = newStateMachine

	_ func(*stateMachine, ScannedFact) bool = (*stateMachine).charge

	_ func(*stateMachine, time.Time, time.Duration) (activation, bool) = (*stateMachine).evaluate

	_ func(*stateMachine, activation) = (*stateMachine).reset

	_ func(*stateMachine) energyRecord = (*stateMachine).snapshot
)
