package tagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/api/view"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/telemetry"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// commitFact stores one fact of this run in the ledger the post-commit
// observation reads back, and reports the identity it committed under: an event
// that arrives without an identity is minted one here, and the whole event is
// kept exactly as it stands, so the observer restores the fact as it committed
// instead of reconstructing one.
// - The commit is the one ledger write of a fact of this run: the recorded call result, the ready input of a range, the assistant output of a drive and the settled delivery all land here.
// - A committed fact is never withheld from a reader and never rolled back for what an observation of it reports.
func (r *Run) commitFact(evt event.Event) event.EventID {
	if evt.Key == "" {
		evt.Key = r.mintEventID()
	}
	r.mu.Lock()
	r.facts[evt.Key] = evt
	r.mu.Unlock()
	return evt.Key
}

// record stores one wrapped call result as a fact of this run and reports the
// identity of that fact. The fact keeps the whole event as committed: the
// native tool message the result pairs with beside the call identity the
// admission frame carries, so the settled notification that points at it
// restores the pairing for the invocation's next request and the post-commit
// observer reads the fact back exactly as it committed.
func (r *Run) record(ctx context.Context, result task.Result) (event.EventID, error) {
	evt := event.Event{
		Type:      event.TypeActionCommand,
		Timestamp: time.Now(),
		Message: model.Message{
			Role:     model.RoleTool,
			ToolID:   result.CallID,
			ToolName: result.Tool,
			Content:  resultText(result),
		},
		TaskID: result.TaskID,
		CallID: result.CallID,
	}
	if frame, ok := task.CallFrameFrom(ctx); ok {
		evt.Owner = frame.Address
		evt.Source = frame.Address
		evt.Target = frame.Address
		evt.InstanceID = frame.Identity.InstanceID
		evt.InvocationID = frame.Identity.InvocationID
		evt.DecisionID = frame.Identity.DecisionID
		if evt.CallID == "" {
			evt.CallID = frame.Identity.CallID
		}
	}
	return r.commitFact(evt), nil
}

// factPipeline assembles the After pass pipeline of one agent from the binding
// generation this run published last, so a fact this run commits outside a
// dispatch — the ready input of a range, the output of a drive, the settled
// delivery of a call — is observed through the very plugin faces the published
// generation carries. A refused assembly is reported by name and lands as a gap
// beside the fact: observation is a bypass, never part of the commit.
func (r *Run) factPipeline(agentID event.AgentID) (plugin.Pipeline, error) {
	r.mu.Lock()
	binding, published := r.current.Agents[agentID]
	r.mu.Unlock()
	if !published {
		return plugin.Pipeline{}, fmt.Errorf("tagent: no published binding of agent %s to observe through", agentID)
	}
	return plugin.NewPipeline(binding.Plugins)
}

// sink admits one notification of a settled call into the mailbox of its
// target beside the three mailbox time points of that admission — acceptance,
// claim and completed consumption recorded in the state owner's own
// bookkeeping — and only then hands the settled result back into the intake of
// the invocation that issued the call. The backflow drives that invocation's
// next decision and never reaches a neighbour, and a notice the mailbox refused
// to book is not handed on as a silently accepted one.
func (r *Run) sink(ctx context.Context, notice task.Notice) error {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok {
		return task.ErrCallFrameMissing
	}
	if err := r.bookNotice(ctx, frame, notice); err != nil {
		return err
	}
	r.loop.Backflow(frame.Identity.InvocationID, notice)
	return nil
}

// bookNotice records the three mailbox time points of one settled notification
// in the state owner's own bookkeeping: the delivery route of the task is
// registered beside the call frame the notification belongs to, the notice is
// accepted into the mailbox of its target site, and the accepted input is
// claimed and consumed by the executor that hands it on. The acceptance is
// idempotent by the notification's stable identity, so a delivery the manager
// already accepted records no second fact here.
func (r *Run) bookNotice(ctx context.Context, frame task.CallFrame, notice task.Notice) error {
	if err := r.store.TrackDelivery(ctx, notice.TaskID, task.HandoffTarget{
		Target:         frame.Address,
		TargetInstance: frame.Identity.InstanceID,
	}); err != nil {
		return err
	}
	if err := r.store.AcceptNotice(ctx, notice); err != nil {
		return err
	}
	site, err := r.store.Site(ctx, r.inspector, frame.Address, frame.Identity.InstanceID)
	if err != nil {
		return err
	}
	seq, ok := mailboxSeqOf(site, notice)
	if !ok {
		return state.ErrEntryUnknown
	}
	if err := r.store.Claim(ctx, frame.Address, frame.Identity.InstanceID, seq); err != nil {
		return err
	}
	return r.store.Consume(ctx, frame.Address, frame.Identity.InstanceID, seq)
}

// mailboxSeqOf reports the acceptance sequence the mailbox of the site assigned
// to the named notification.
func mailboxSeqOf(site *state.Session, notice task.Notice) (state.AcceptSeq, bool) {
	for i := range site.Mailbox {
		if site.Mailbox[i].Notice == notice {
			return site.Mailbox[i].AcceptSeq, true
		}
	}
	return 0, false
}

// SessionHistory reports the transcript the named session accumulated across
// the drives that already ended on it, as a copy the caller owns. The history
// of one session never mixes with the history of another.
//
// This ledger is the read-out face of the execution scene the run holds, not a
// durability record: what a restarted process reads back is recovered from the
// durable fact chain by replay, so no body snapshot is kept beside the facts.
func (r *Run) SessionHistory(addr event.Address) []model.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]model.Message(nil), r.history[addr]...)
}

// CommitHistory appends the transcript one ended drive accumulated to the
// history of its session, so the next invocation of the session reads the
// range's history as it actually ended while no two invocations ever batch
// their ready input together.
func (r *Run) CommitHistory(addr event.Address, msgs []model.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.history[addr] = append(r.history[addr], msgs...)
}

// resolveFact reports the native tool message one recorded fact carries.
func (r *Run) resolveFact(id event.EventID) (model.Message, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	evt, ok := r.facts[id]
	return evt.Message, ok
}

// runFacts is the read-only committed-fact face of this run: it answers with
// the fact exactly as the ledger committed it and refuses an identity that
// never committed, so the post-commit observation resolves a real fact and
// never a reconstruction. The face reads the one ledger of the run and holds
// no state of its own.
type runFacts struct{ run *Run }

// The read face satisfies the lookup the post-commit wiring resolves through.
var _ plugin.FactLookup = runFacts{}

// CommittedFact returns the committed fact the identity names; an identity
// with no committed fact behind it is refused by name.
func (f runFacts) CommittedFact(_ context.Context, id event.EventID) (event.Event, error) {
	f.run.mu.Lock()
	defer f.run.mu.Unlock()
	evt, ok := f.run.facts[id]
	if !ok {
		return event.Event{}, fmt.Errorf("tagent: fact %s did not commit", id)
	}
	return evt, nil
}

// observePlugin is the post-commit observation element of this assembly: one
// stable name beside the commit observer it serves, so the pipeline dispatches
// the read-only observation duty of the After pass to it. The element carries
// no state of its own and observes only through the lookup and the sink it was
// wired with.
type observePlugin struct{ observer *telemetry.CommitObserver }

// The element satisfies both registration and phase faces of the pipeline.
var (
	_ plugin.Plugin             = observePlugin{}
	_ plugin.PostCommitObserver = observePlugin{}
)

// Name returns the stable plugin name recorded in every observation gap.
func (p observePlugin) Name() string { return "commit-observer" }

// Phase reports the post-commit observation phase this plugin serves.
func (p observePlugin) Phase() plugin.Phase { return plugin.PhasePostCommitObserve }

// ObserveCommit delegates one committed fact to the wired commit observer.
func (p observePlugin) ObserveCommit(ctx context.Context, id event.EventID) error {
	return p.observer.ObserveCommit(ctx, id)
}

// takePresentation keeps one After pass presentation of a committed fact on the
// diagnostic read face of this run: the views its presenters rendered beside
// the visible gaps its failed observers left, kept as the read-out the host
// inspects. The presentation is observation only — the committed fact it was
// rendered from is never rolled back or withheld for anything it carries.
func (r *Run) takePresentation(presentation view.Presentation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.presented = append(r.presented, presentation)
}

// Presentations reports the After pass presentations this run handed to its
// diagnostic read face, in the order those passes ran — each only after the
// canonical commit of its own fact — as a copy the caller owns. The
// presentations are read-out material beside the committed facts; reading them
// moves nothing.
func (r *Run) Presentations() []view.Presentation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]view.Presentation, 0, len(r.presented))
	for _, presentation := range r.presented {
		got := presentation
		got.Views = append([]view.RenderedView(nil), presentation.Views...)
		got.Gaps = append([]view.ObservationGap(nil), presentation.Gaps...)
		out = append(out, got)
	}
	return out
}

// RecordingLosses reports the training records the production points of this
// assembly could not write, in arrival order, each beside the record identity
// and the named failure that stopped the write. The account is the explicit
// gap ledger of the run: nothing the recorder refused is silently forgotten.
func (r *Run) RecordingLosses() []agent.RecordingLoss {
	return r.loop.RecordingLosses()
}

// mintEventID mints the canonical identity of one fact of this run.
func (r *Run) mintEventID() event.EventID {
	r.mu.Lock()
	r.factSeq++
	seq := r.factSeq
	r.mu.Unlock()
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("tagent: mint the event identity: " + err.Error())
	}
	return event.EventID(fmt.Sprintf("%d-%s", seq, hex.EncodeToString(buf[:])))
}

// resultText reports the tool output text one recorded result carries: the
// native output kept as it stands, or the error beside it.
func resultText(result task.Result) string {
	if result.Err != nil {
		return result.Err.Error()
	}
	switch out := result.Output.(type) {
	case nil:
		return ""
	case string:
		return out
	default:
		return fmt.Sprintf("%v", out)
	}
}
