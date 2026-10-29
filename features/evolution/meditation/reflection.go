package meditation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
)

// 契约: docs/wiki/agent/agent-architecture.md#meditation-curator
//
// ErrReflectionAgentUnnamed reports a reflection assembled without the observing
// agent's own identity: an observation face cannot be resolved and no loop
// session can be addressed without it.
var ErrReflectionAgentUnnamed = errors.New("evolution: reflection requires the observing agent's namespace")

// ErrObservationScopeOutOfBounds reports an observation face that names a
// namespace outside the authorized read scope. The face is refused by name at
// assembly instead of being trimmed: reading less than declared would silently
// change what the reflection observes.
var ErrObservationScopeOutOfBounds = errors.New("evolution: the observed namespaces reach outside the authorized read scope")

// ErrReflectionInputUnkeyed reports a reflection input that committed no
// identity: without one, a consumption report cannot name the turn it settles.
var ErrReflectionInputUnkeyed = errors.New("evolution: the reflection input committed no identity")

// ErrReflectionNotPending reports an outcome for a reflection input that is not
// awaiting one: a repeated or stale report never moves the watermark.
var ErrReflectionNotPending = errors.New("evolution: no reflection input awaits an outcome")

// ErrReflectionKeyMismatch reports an outcome naming a reflection input other
// than the one awaiting consumption. The report is refused, so a report about an
// older turn can never settle a newer one.
var ErrReflectionKeyMismatch = errors.New("evolution: the outcome names a reflection input other than the one awaiting consumption")

// TickDisposition is what one reflection tick decided.
type TickDisposition string

const (
	// TickFired reports that this tick injected a reflection input.
	TickFired TickDisposition = "fired"
	// TickPending reports that an injected reflection input still awaits its
	// outcome, so this tick injected nothing.
	TickPending TickDisposition = "pending"
	// TickClosed reports that the gate stayed closed because a fact it needs
	// could not be established.
	TickClosed TickDisposition = "closed"
	// TickNoNovelty reports a readable face holding nothing newer than the
	// watermark that the freshness policy reads as new material.
	TickNoNovelty TickDisposition = "no-novelty"
	// TickRhythm reports that the rhythm floor has not elapsed since the last
	// consumed reflection.
	TickRhythm TickDisposition = "rhythm"
)

// ReflectionOutcome is what became of one injected reflection input.
type ReflectionOutcome string

const (
	// ReflectionConsumed reports a reflection turn that ran: the watermark
	// advances to the moment the turn was injected at.
	ReflectionConsumed ReflectionOutcome = "consumed"
	// ReflectionDeferred reports a reflection turn that yielded to real input at
	// either yield point: the watermark stays where it stands and the very same
	// facts are re-evaluated on the next tick.
	ReflectionDeferred ReflectionOutcome = "deferred"
	// ReflectionInFlight reports an injected input whose turn has not reached an
	// outcome yet; the input stays pending and no second one is injected.
	ReflectionInFlight ReflectionOutcome = "in-flight"
)

// ReflectionReceipt is the loop side's report of one injected reflection input:
// the identity the fact committed under, what became of its turn and the moment
// that outcome was reached.
type ReflectionReceipt struct {
	// Key is the committed identity of the injected fact.
	Key event.EventID
	// Outcome is what became of the injected turn.
	Outcome ReflectionOutcome
	// At is the moment the outcome was reached; it dates the deferral tally and
	// never moves the watermark.
	At time.Time
	// Reason is the named ground of a deferral, when the loop side reports one.
	Reason string
}

// ReflectionInput is the loop-session face one fired reflection injects its
// explicit input event through: the assembly commits the event as a fact of the
// agent's own partition, drives the observing agent's own loop session with it
// and reports what became of that turn.
//
// The receipt is the only evidence that a reflection turn ran: a fire that was
// only written never advances the watermark.
// The redline list (3 rules):
// MUST commit the reflection input as a fact of the agent's own partition.
// MUST report the outcome of the turn the injected input drove.
// MUST NOT report a reflection turn as consumed from a fire that was only written.
type ReflectionInput interface {
	// InjectReflection commits one reflection input into the agent's own loop
	// session and reports the outcome of its turn.
	InjectReflection(ctx context.Context, evt event.Event) (ReflectionReceipt, error)
}

// ReflectionInjection is one injected reflection input as the firing tick reports
// it: the identity it committed under, the moment it was injected at, the event
// itself and the digest it carried.
type ReflectionInjection struct {
	// Key is the committed identity of the injected fact.
	Key event.EventID
	// At is the injection moment, which is the watermark candidate a consumed
	// turn advances the gate to.
	At time.Time
	// Event is the explicit input event as it was handed to the loop session.
	Event event.Event
	// Digest is the self-state summary the input carried.
	Digest DigestSummary
}

// TickDecision is what one reflection tick decided and the named ground behind
// that decision.
type TickDecision struct {
	// Disposition is what this tick decided.
	Disposition TickDisposition
	// Reason is the named ground of the disposition; a closed gate names the fact
	// it could not establish.
	Reason string
	// Cause is the named failure behind a closed gate, when one was returned by a
	// read or by the watermark source.
	Cause error
}

// TickOutcome is the decision one reflection tick reached, the evidence of the
// pass it read and the injection it made.
type TickOutcome struct {
	// Decision is what this tick decided.
	Decision TickDecision
	// Scan is the evidence of the pass this tick read, when a pass ran.
	Scan *ObservationScan
	// Digest is the self-state summary rendered from that same pass.
	Digest DigestSummary
	// Injection is the reflection input this tick injected, when it fired.
	Injection *ReflectionInjection
}

// pendingReflection is one injected reflection input awaiting its outcome: the
// identity it committed under, its injection moment and the event it handed over.
type pendingReflection struct {
	key   event.EventID
	at    time.Time
	event event.Event
}

// Reflection is the reflection loop of one agent: it reads the authorized
// observation face, decides whether a reflection is due, injects the input event
// into the agent's own loop session and advances the execution watermark when that
// turn is consumed.
//
// One mechanism covers every observation face: observing the agent's own
// partition is self-maintenance and observing others' partitions is the
// side-car meditation, and the action is the same explicit input in either
// case. A fire only writes the input; the watermark moves when the turn is
// consumed, to the injection moment, so every fact a yield window covered stays
// inside the next window. A read the pass cannot complete keeps the gate closed
// by name, and no fallback criterion is substituted for an unreachable chain.
type Reflection struct {
	cfg   ReflectionConfig
	scope ReflectionScope
	addr  event.Address
	gate  WatermarkGate
	facts ObservedFacts
	input ReflectionInput

	mu         sync.Mutex
	pending    *pendingReflection
	deferred   int
	deferredAt time.Time
}

// NewReflection assembles the reflection loop of one agent over its watermark gate,
// its observed fact-chain face and the loop-session injection face. A declared
// observation face outside the authorized scope refuses the assembly by name.
func NewReflection(cfg ReflectionConfig, gate WatermarkGate, facts ObservedFacts, input ReflectionInput) (*Reflection, error) {
	scope, err := ResolveObservationScope(cfg.Loop.Agent, cfg.Face.ObservedNamespaces, cfg.Face.ReadNamespaces)
	if err != nil {
		return nil, err
	}
	return &Reflection{
		cfg:   cfg,
		scope: scope,
		addr:  event.Address{AgentID: event.AgentID(cfg.Loop.Agent), SessionID: event.SessionID(cfg.Loop.Session)},
		gate:  gate,
		facts: facts,
		input: input,
	}, nil
}

// ObservedNamespaces reports the resolved observation face of this reflection.
func (r *Reflection) ObservedNamespaces() []string {
	return append([]string(nil), r.scope.Names...)
}

// Scope reports the resolved observation face, names and partitions together.
func (r *Reflection) Scope() ReflectionScope {
	return ReflectionScope{
		Own:        r.scope.Own,
		Names:      append([]string(nil), r.scope.Names...),
		Partitions: append([]int(nil), r.scope.Partitions...),
	}
}

// FreshSince reports whether the observed face holds a fact recorded after since
// that the freshness policy reads as new material.
//
// A face that cannot be read is reported as an error and never as "nothing
// fresh": the fact chain is the judgment's only data face.
func (r *Reflection) FreshSince(ctx context.Context, since time.Time) (bool, error) {
	scan, err := observe(ctx, r.scope, r.facts, ObservationScanRequest{
		Since:     since,
		PageLimit: r.cfg.Face.PageLimit,
		FactLimit: r.cfg.Face.FactLimit,
	})
	if err != nil {
		return false, err
	}
	return scan.Verdict.Novelty, nil
}

// CommittedFact reads the fact one commit identity names through the observed
// chain. It is the read the event-driven face starts from: the commit notice
// carries an identity, and the fact itself is read back here.
func (r *Reflection) CommittedFact(ctx context.Context, id event.EventID) (event.Event, error) {
	if r.facts == nil {
		return event.Event{}, ErrObservedFaceUnwired
	}
	return r.facts.CommittedFact(ctx, id)
}

// Watermark reports the execution watermark the gate holds, refusing by name
// when no source is wired.
func (r *Reflection) Watermark(ctx context.Context) (Watermark, error) {
	return r.watermark(ctx)
}

// MinGap reports the floor between two consumed reflections this loop holds: the
// window a pass is judged against, and the one an event-driven face holds a
// suppressed pass open for.
func (r *Reflection) MinGap() time.Duration {
	return r.cfg.Cadence.MinGap
}

// Pending reports the identity of the reflection input awaiting its outcome, if
// one is pending.
func (r *Reflection) Pending() (event.EventID, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending == nil {
		return "", false
	}
	return r.pending.key, true
}

// DeferredCount reports how many reflections yielded since the last consumed one.
func (r *Reflection) DeferredCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deferred
}

// LastDeferredAt reports the moment of the most recent deferral.
func (r *Reflection) LastDeferredAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deferredAt
}

// Tick runs one reflection tick at now: it establishes the execution watermark,
// reads the observed face once, renders the digest from that same pass, applies
// the pending guard, the freshness judgment and the rhythm floor, and injects one
// explicit input event when all three open. The rhythm floor is read on both sides
// of one execution — the interval since the last consumed reflection, and the age
// of the newest host-facing fact — and a floor that holds the tick back reports the
// fact holding it, leaving the watermark and the pending input untouched.
//
// A tick whose gate cannot be established reports TickClosed with the fact it
// could not establish and injects nothing; a tick that injected reports the input
// and settles the outcome the loop side handed back. The former watermark floor
// is absorbed by the freshness judgment: the scan reads only facts newer than the
// watermark and Recent is the newest of them, so a fresh fact inside the window
// already implies the watermark floor — one judgment, not two.
func (r *Reflection) Tick(ctx context.Context, now time.Time) TickOutcome {
	if err := ctx.Err(); err != nil {
		return TickOutcome{Decision: TickDecision{Disposition: TickClosed, Reason: "the tick context ended", Cause: err}}
	}
	if outcome, held := r.pendingOutcome(now); held {
		return outcome
	}
	watermark, err := r.watermark(ctx)
	if err != nil {
		return TickOutcome{Decision: TickDecision{Disposition: TickClosed, Reason: "the execution watermark cannot be established: " + err.Error(), Cause: err}}
	}
	scan, err := observe(ctx, r.scope, r.facts, ObservationScanRequest{
		Since:     watermark.Moment,
		PageLimit: r.cfg.Face.PageLimit,
		FactLimit: r.cfg.Face.FactLimit,
	})
	if err != nil {
		return TickOutcome{Decision: TickDecision{Disposition: TickClosed, Reason: "the observed face cannot be read: " + err.Error(), Cause: err}}
	}
	digest, err := NewScanDigest(scan, r.cfg.Face.FactLimit).Render(ctx, watermark)
	if err != nil {
		return TickOutcome{Decision: TickDecision{Disposition: TickClosed, Reason: "the digest cannot be rendered: " + err.Error(), Cause: err}, Scan: scan}
	}
	if !scan.Verdict.Novelty {
		return TickOutcome{
			Decision: TickDecision{
				Disposition: TickNoNovelty,
				Reason:      "no fact newer than the watermark reads as new material under the freshness policy",
			},
			Scan:   scan,
			Digest: digest,
		}
	}
	if recent := scan.Verdict.Recent; recent != nil && r.cfg.Cadence.MinGap > 0 && now.Sub(recent.Content.Timestamp) < r.cfg.Cadence.MinGap {
		return TickOutcome{
			Decision: TickDecision{
				Disposition: TickRhythm,
				Reason:      fmt.Sprintf("the rhythm floor %s since the host-facing fact recorded at %s has not elapsed", r.cfg.Cadence.MinGap, recent.Content.Timestamp.UTC().Format(time.RFC3339Nano)),
			},
			Scan:   scan,
			Digest: digest,
		}
	}
	return r.fire(ctx, now, watermark, scan, digest)
}

// pendingOutcome reports the outcome of a tick held off by an input still awaiting
// its own: while one is pending no second input is injected and the face is not
// read again.
func (r *Reflection) pendingOutcome(now time.Time) (TickOutcome, bool) {
	r.mu.Lock()
	pending := r.pending
	r.mu.Unlock()
	if pending == nil {
		return TickOutcome{}, false
	}
	reason := fmt.Sprintf("the reflection input %s injected at %s awaits its outcome", pending.key, pending.at.UTC().Format(time.RFC3339Nano))
	if r.cfg.Cadence.Interval > 0 && now.Sub(pending.at) > 3*r.cfg.Cadence.Interval {
		reason += fmt.Sprintf("; still in flight past three intervals (%s), and no second input is injected", 3*r.cfg.Cadence.Interval)
	}
	return TickOutcome{Decision: TickDecision{Disposition: TickPending, Reason: reason}}, true
}

// watermark reports the execution watermark the gate holds, refusing by name when
// no source is wired.
func (r *Reflection) watermark(ctx context.Context) (Watermark, error) {
	if r.gate == nil {
		return Watermark{}, ErrWatermarkSourceUnwired
	}
	return r.gate.Current(ctx)
}

// fire injects one explicit reflection input into the agent's own loop session
// and settles the outcome the loop side reported: a consumed turn advances the
// watermark to the injection moment, a deferred turn leaves it where it stands,
// and an in-flight turn keeps the input pending.
func (r *Reflection) fire(ctx context.Context, now time.Time, watermark Watermark, scan *ObservationScan, digest DigestSummary) TickOutcome {
	if r.input == nil {
		return TickOutcome{Decision: TickDecision{Disposition: TickClosed, Reason: "the loop-session injection face is not wired"}, Scan: scan, Digest: digest}
	}
	evt := r.reflectionEvent(now, watermark.Moment, digest)
	receipt, err := r.input.InjectReflection(ctx, evt)
	if err != nil {
		return TickOutcome{Decision: TickDecision{Disposition: TickClosed, Reason: "the reflection input was not injected: " + err.Error(), Cause: err}, Scan: scan, Digest: digest}
	}
	if receipt.Key == "" {
		return TickOutcome{Decision: TickDecision{Disposition: TickClosed, Reason: ErrReflectionInputUnkeyed.Error(), Cause: ErrReflectionInputUnkeyed}, Scan: scan, Digest: digest}
	}
	evt.Key = receipt.Key
	injection := &ReflectionInjection{Key: receipt.Key, At: now, Event: evt, Digest: digest}
	r.mu.Lock()
	r.pending = &pendingReflection{key: receipt.Key, at: now, event: evt}
	r.mu.Unlock()
	outcome := TickOutcome{Decision: TickDecision{Disposition: TickFired}, Injection: injection, Scan: scan, Digest: digest}
	outcome.Decision.Reason = fmt.Sprintf("the reflection input %s was injected into %s", receipt.Key, r.addr.SessionID)
	if err := r.NoteOutcome(ctx, receipt); err != nil {
		return TickOutcome{
			Decision: TickDecision{
				Disposition: TickClosed,
				Reason:      "the reflection input was injected but its outcome could not be settled: " + err.Error(),
				Cause:       err,
			},
			Scan:      scan,
			Digest:    digest,
			Injection: injection,
		}
	}
	return outcome
}

// NoteOutcome settles what became of the reflection input awaiting one.
//
// A consumed turn advances the execution watermark to that input's injection
// moment and clears the deferral tally; a deferred turn clears the pending input
// and leaves the watermark in place; an in-flight turn keeps it pending. A report
// naming an input other than the pending one, or arriving while none is pending, is
// refused by name so a repeated report can never move the watermark again.
func (r *Reflection) NoteOutcome(ctx context.Context, receipt ReflectionReceipt) error {
	r.mu.Lock()
	pending := r.pending
	if pending == nil {
		r.mu.Unlock()
		return ErrReflectionNotPending
	}
	if pending.key != receipt.Key {
		r.mu.Unlock()
		return fmt.Errorf("%w: pending %s, reported %s", ErrReflectionKeyMismatch, pending.key, receipt.Key)
	}
	r.mu.Unlock()

	switch receipt.Outcome {
	case ReflectionInFlight:
		return nil
	case ReflectionDeferred:
		r.mu.Lock()
		r.pending = nil
		r.deferred++
		r.deferredAt = receipt.At
		r.mu.Unlock()
		return nil
	case ReflectionConsumed:
		if r.gate == nil {
			return ErrWatermarkSourceUnwired
		}
		if err := r.gate.Advance(ctx, pending.at); err != nil {
			return fmt.Errorf("evolution: advance the execution watermark: %w", err)
		}
		r.mu.Lock()
		r.pending = nil
		r.deferred = 0
		r.mu.Unlock()
		return nil
	default:
		return fmt.Errorf("evolution: unknown reflection outcome %q", receipt.Outcome)
	}
}

// reflectionEvent builds the explicit input event one fired reflection injects: an
// external input of the observing agent's own address, stamped with the reflection
// lineage, whose body is the static prompt led by the digest of the very pass that
// fired it.
func (r *Reflection) reflectionEvent(now, since time.Time, digest DigestSummary) event.Event {
	return event.Event{
		Type:      event.TypeExternalInput,
		Timestamp: now,
		Meta:      map[string]string{event.MetaKeyTriggerSource: LineageReflection},
		Message:   model.Message{Role: model.RoleUser, Content: r.reflectionText(now, since, digest.Body)},
		Owner:     r.addr,
		Source:    r.addr,
		Target:    r.addr,
	}
}

// reflectionText renders the body of one reflection input: the injection moment,
// the watermark the judgement started from, the digest of the pass and the static
// prompt. An empty section is omitted, so a reflection with nothing to report
// carries no empty shell.
func (r *Reflection) reflectionText(now, since time.Time, digest string) string {
	parts := []string{fmt.Sprintf("[reflection] a scheduled reflection input\n\nlast consumed reflection: %s\ninjected at: %s",
		describeMoment(since), now.UTC().Format(time.RFC3339Nano))}
	if digest != "" {
		parts = append(parts, digest)
	}
	if r.cfg.Prompt != "" {
		parts = append(parts, r.cfg.Prompt)
	}
	return strings.Join(parts, "\n\n")
}

// describeMoment renders one anchor for a human reader, naming a start with no
// prior execution as such rather than printing a zero timestamp.
func describeMoment(moment time.Time) string {
	if moment.IsZero() {
		return "none (first reflection)"
	}
	return moment.UTC().Format(time.RFC3339Nano)
}

// ObservedNamespaces and FreshSince are the two methods the declared
// observation-source face of the plugin layer names, so the loop satisfies that
// face by structure while this package keeps to the packages it may import.
var _ interface {
	ObservedNamespaces() []string
	FreshSince(ctx context.Context, since time.Time) (bool, error)
} = (*Reflection)(nil)
