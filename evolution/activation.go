package evolution

import (
	"errors"
	"sync"
	"time"
)

// 契约: docs/wiki/evolution/evolution-architecture.md#evidence-window
//
// ErrActivationUnusable reports an activation record that cannot prove a real
// consumption: one of its version, moment or evidence is missing. Such a record
// is refused instead of being folded into the consumption ledger, because an
// unusable record can never stand as the activation evidence of a window.
var ErrActivationUnusable = errors.New("evolution: activation record unusable: version, moment and evidence are all required")

// ErrCorrelationUnusable reports a git correlation that names nothing: an empty
// version or an empty registration SHA. Such a correlation is refused before any
// mapping is touched.
var ErrCorrelationUnusable = errors.New("evolution: git correlation unusable: version and registration sha are both required")

// ErrCorrelationConflict reports a git correlation that contradicts the mapping
// already on record for the version it names. The recorded mapping stands and
// the conflicting correlation is refused, so a registration can never silently
// relabel a version that consumption evidence already bound to another commit.
var ErrCorrelationConflict = errors.New("evolution: git correlation conflicts with the recorded mapping")

// Activation is one real request consumption of one improvement version: the
// version actually consumed, the moment the consumption happened and the
// evidence that proves it.
//
// This is the only thing that anchors an evidence window. A file takes effect
// when it lands on disk, so the first real consumption routinely precedes the
// git registration (t0 write < t1 consume < t2 register); the window opens at
// t1 and a registration at t2 never opens, anchors or moves it. All three
// fields are required: a record missing its version, moment or evidence cannot
// prove a consumption and is refused with ErrActivationUnusable.
type Activation struct {
	// Version names the improvement version the request really consumed.
	Version string
	// At is the moment the real consumption happened.
	At time.Time
	// Evidence names the recorded consumption that proves the moment.
	Evidence EvidenceRef
}

// usable reports whether the record carries every fact a real consumption must
// prove.
func (a Activation) usable() bool {
	return a.Version != "" && !a.At.IsZero() && a.Evidence != ""
}

// ActivationLedger is the in-memory table of real consumption evidence, rebuilt from recorded events
// and never persisted: the one-shot window, the consumptions and the git correlation per version.
//
// - The window anchor is the activation moment: the first Consume opens it, later ones never move it, and a git registration never opens one.
// - Consumption records are never relabeled: Correlate only adds the git mapping and refuses a contradicting one with ErrCorrelationConflict.
type ActivationLedger struct {
	mu            sync.Mutex
	windows       map[string]EvidenceWindow
	consumed      map[string][]Activation
	registrations map[string]string
}

// NewActivationLedger builds an empty consumption ledger: no version has a
// window, a consumption or a correlation yet.
func NewActivationLedger() *ActivationLedger {
	return &ActivationLedger{
		windows:       map[string]EvidenceWindow{},
		consumed:      map[string][]Activation{},
		registrations: map[string]string{},
	}
}

// Consume records one real request consumption of one improvement version and
// returns the version's evidence window together with whether this call opened
// it.
//
// The first consumption of a version opens the window at that activation moment
// and reports opened=true; every later consumption is appended to the
// consumption records of the version and reports opened=false with the window
// unchanged, so the anchor stays at the first real consumption no matter how
// many requests follow. A record missing its version, moment or evidence is
// refused with ErrActivationUnusable and changes nothing.
func (l *ActivationLedger) Consume(a Activation) (EvidenceWindow, bool, error) {
	if !a.usable() {
		return EvidenceWindow{}, false, ErrActivationUnusable
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consumed[a.Version] = append(l.consumed[a.Version], a)
	window, open := l.windows[a.Version]
	if open {
		return window, false, nil
	}
	window = EvidenceWindow{
		Version:            a.Version,
		ActivatedAt:        a.At,
		ActivationEvidence: a.Evidence,
	}
	l.windows[a.Version] = window
	return window, true, nil
}

// Window returns the evidence window of a version and whether it has opened. A
// version never consumed returns the zero window and false; that zero window
// never means an open window.
func (l *ActivationLedger) Window(version string) (EvidenceWindow, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	window, open := l.windows[version]
	return window, open
}

// Consumed lists every real consumption recorded for a version, in recording
// order. The list is the turn count of the version: one entry is one real
// request that consumed it. A defensive copy is returned, so a caller cannot
// rewrite the recorded facts.
func (l *ActivationLedger) Consumed(version string) []Activation {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := l.consumed[version]
	out := make([]Activation, len(entries))
	copy(out, entries)
	return out
}

// Correlate records the git registration commit of one improvement version.
//
// The registration SHA is a git correlation only: it links feedback to the real
// consumption version and never opens, anchors or moves an evaluation window.
// A version never consumed keeps no window no matter how often it is
// correlated, and this call never re-stamps the requests, feedback or window
// already attributed to the version. Correlating the same SHA twice is
// idempotent; a different SHA for a version already correlated is refused with
// ErrCorrelationConflict and the recorded mapping stands. An empty version or
// SHA is refused with ErrCorrelationUnusable.
func (l *ActivationLedger) Correlate(version, sha string) error {
	if version == "" || sha == "" {
		return ErrCorrelationUnusable
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if recorded, ok := l.registrations[version]; ok && recorded != sha {
		return ErrCorrelationConflict
	}
	l.registrations[version] = sha
	return nil
}

// RegistrationOf returns the registration commit a version correlates to and
// whether one is on record. The SHA is a git correlation only and never the
// anchor of any window.
func (l *ActivationLedger) RegistrationOf(version string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	sha, ok := l.registrations[version]
	return sha, ok
}
