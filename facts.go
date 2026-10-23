package tagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/api/view"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
	"github.com/SpellingDragon/tagent/telemetry"
)

// mappedSummaryRunes bounds the one-line read a mapped entry carries when the
// fact stored no summary of its own: the read is a prefix of the original, not
// a rewrite of it, so the bound only decides how much of the first line the
// mapped sequence speaks.
const mappedSummaryRunes = 120

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// commitFact stores one fact of this run in the ledger the post-commit
// observation reads back, and reports the identity it committed under: an event
// that arrives without an identity is minted one here, and the whole event is
// kept exactly as it stands, so the observer restores the fact as it committed
// instead of reconstructing one.
// - The commit is the one ledger write of a fact of this run: the recorded call result, the ready input of a range, the assistant output of a drive and the settled delivery all land here.
// - A committed fact is never withheld from a reader and never rolled back for what an observation of it reports.
// - A committed fact enters the mapped sequence of its own session chain right after it lands, so every consumption of that chain assembles the sequence the commits recorded.
func (r *Run) commitFact(evt event.Event) event.EventID {
	if evt.Key == "" {
		evt.Key = r.mintEventID()
	}
	entry, mapped := mappedEntryOf(evt)
	r.mu.Lock()
	r.facts[evt.Key] = evt
	r.factOrder = append(r.factOrder, evt.Key)
	r.mu.Unlock()
	if !mapped {
		return evt.Key
	}
	r.attachDeclaredReplay(evt)
	r.recordMapped(evt, entry)
	return evt.Key
}

// recordMapped appends one mapped entry to the sequence of its session and
// moves the boundary between the material that ended and the material still in
// flight: a new range is admitted by an external input under an invocation of
// its own, and that first fact is where everything the earlier ranges committed
// becomes ended material the next request presents while the transcript stops
// carrying it. Facts of the range in flight — the reasoning, the calls, the
// output of a round and the notice of a call it admitted — stay behind that
// boundary, because the transcript of the drive carries them. A notice fact is a
// mapped fact like any other, so a notification enters the chain's projection as
// it commits and the range reads it back as a projection increment instead of
// from a lane of its own.
func (r *Run) recordMapped(evt event.Event, entry replay.ProjectionEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	held, carried := r.replays[sessionOf(evt)]
	if !carried {
		return
	}
	position := held.projection.Len()
	held.projection.Append(context.Background(), entry)
	if evt.Type == event.TypeExternalInput && held.rangeInvocation != "" && held.rangeInvocation != evt.InvocationID {
		held.ended = position
	}
	held.rangeInvocation = evt.InvocationID
	r.replays[sessionOf(evt)] = held
}

// sessionOf reports the session chain one committed fact belongs to: the session
// of the address that owns it, and of the address it is routed to or comes from
// beside a fact that names no owner.
func sessionOf(evt event.Event) event.SessionID {
	for _, addr := range []event.Address{evt.Owner, evt.Target, evt.Source} {
		if addr.SessionID != "" {
			return addr.SessionID
		}
	}
	return ""
}

// attachDeclaredReplay opens the mapped sequence of a session whose agent
// declares a model window: the first committed fact of that session registers a
// fresh projection through AttachReplay under the window the declaration names,
// so every later consumption of the chain assembles the sequence the commits
// record and the age fold measures it against the same window. A held
// registration is left as it stands, and a session naming no window maps onto
// no projection: its commits record nothing beside the fact itself and a
// declaration that declares no window keeps the assembly as it stood.
func (r *Run) attachDeclaredReplay(evt event.Event) {
	session := sessionOf(evt)
	if session == "" {
		return
	}
	r.mu.Lock()
	_, held := r.replays[session]
	window := FoldWindow{}
	if !held {
		window = foldWindowOf(specOfLocked(r.current.Plan.Declaration, agentOf(evt)))
	}
	r.mu.Unlock()
	if held {
		r.attachNoticeFeed()
		return
	}
	if window.MaxTokens <= 0 {
		return
	}
	err := r.AttachReplay(agent.RequestChain{SessionID: session}, window, replay.NewReplayProjection())
	if err != nil {
		return
	}
	r.attachNoticeFeed()
}

// ReplayEntries reports the projection entries one session chain registered for
// replay, in canonical form, and whether the session registered one at all. The
// read is the diagnostic face of the mapped sequence: the fold products a
// commit-driven fold minted are entries of this projection, so this is how a
// caller reads the sequence the request boundary presents without driving a
// request.
func (r *Run) ReplayEntries(session event.SessionID) ([]replay.ProjectionEntry, bool) {
	r.mu.Lock()
	held, carried := r.replays[session]
	r.mu.Unlock()
	if !carried {
		return nil, false
	}
	return held.projection.Entries(), true
}

// noticeProjection is the incremental read face of the session chains' notice
// facts: the mapped sequence the commits of this run record, read from the
// ended-material boundary of the chain. The face is attached to the loop beside
// the mapped sequence of a session, so a range reads the notifications of its
// own call as a projection increment and the fact chain stays the only truth of
// what a notification is — a restarted process reads the same facts back.
//
// The redline list (3 rules):
// MUST answer a chain that carries no projection as carrying none.
// MUST report only the facts under the identity prefix it was handed.
// MUST NOT write the projection it reads: the read is a read.
type noticeProjection struct{ run *Run }

// The face satisfies the incremental notice read the loop drives.
var _ agent.NoticeFeed = noticeProjection{}

// NoticeBoundary reports the position the ended material of the named chain ends
// at, and whether that chain carries a projection at all: the material before it
// is the material a request presents, so a notice committed before it has
// already reached the range and is never reported again.
func (p noticeProjection) NoticeBoundary(chain agent.RequestChain) (int, bool) {
	p.run.mu.Lock()
	defer p.run.mu.Unlock()
	held, carried := p.run.replays[chain.SessionID]
	if !carried || held.projection == nil {
		return 0, false
	}
	return held.ended, true
}

// NoticesFrom reports the notice facts of the named chain appended after cursor
// under the given identity prefix, in chain order, beside the position the read
// reached and whether the chain carries a projection. A fact outside the prefix
// is never reported, so one range reads the notices of its own call alone.
func (p noticeProjection) NoticesFrom(chain agent.RequestChain, prefix event.EventID, cursor int) ([]agent.NoticeView, int, bool) {
	p.run.mu.Lock()
	defer p.run.mu.Unlock()
	held, carried := p.run.replays[chain.SessionID]
	if !carried || held.projection == nil {
		return nil, cursor, false
	}
	entries := held.projection.Entries()
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(entries) {
		cursor = len(entries)
	}
	var views []agent.NoticeView
	for at := cursor; at < len(entries); at++ {
		entry := entries[at]
		if entry.Type != event.TypeFeedback || !strings.HasPrefix(string(entry.Key), string(prefix)) {
			continue
		}
		text := entry.Content
		if original, kept := held.projection.Original(entry.Key); kept {
			text = original
		}
		views = append(views, agent.NoticeView{Key: entry.Key, Text: text})
	}
	return views, len(entries), true
}

// attachNoticeFeed points the loop at the incremental read face of this run's
// session chains, so the drive of a range reads the notices of its chain as the
// projection increments they are. The loop is built before the run, so the face
// is attached beside the first mapped fact of a session; the attach is
// idempotent and a run beside no loop attaches nothing.
func (r *Run) attachNoticeFeed() {
	if r.loop == nil {
		return
	}
	r.loop.SetNoticeFeed(noticeProjection{run: r})
}

// agentOf reports the agent one committed fact belongs to, by the precedence
// sessionOf reads the session under: the owner of the fact, then the address it
// is routed to, then the address it came from.
func agentOf(evt event.Event) event.AgentID {
	for _, addr := range []event.Address{evt.Owner, evt.Target, evt.Source} {
		if addr.AgentID != "" {
			return addr.AgentID
		}
	}
	return ""
}

// mappedEntryOf projects one committed fact onto the projection entry its
// consumption carries: the canonical identity of the fact, its frozen type, the
// one-line read stored beside it and the text the model view speaks for it. A
// fact that carries no model payload maps onto no entry, so the mapped sequence
// never carries an empty message the model would read.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func mappedEntryOf(evt event.Event) (replay.ProjectionEntry, bool) {
	content := mappedContentOf(evt.Message)
	if content == "" {
		return replay.ProjectionEntry{}, false
	}
	return replay.ProjectionEntry{
		Key:     evt.Key,
		Type:    evt.Type,
		Summary: oneLineOf(evt.Meta[event.MetaKeyEventSummary], content),
		Content: content,
		Time:    evt.Timestamp,
	}, true
}

// mappedContentOf reports the text one committed message speaks for on the
// replay path: the content as it stands, and otherwise the call or the tool face
// the message carries, so a call the model issued is never lost from the mapped
// sequence.
func mappedContentOf(msg model.Message) string {
	if msg.Content != "" {
		return msg.Content
	}
	calls := make([]string, 0, len(msg.ToolCalls))
	for _, call := range msg.ToolCalls {
		calls = append(calls, "[call "+call.Function.Name+"]")
	}
	if len(calls) > 0 {
		return strings.Join(calls, " ")
	}
	if msg.ToolName != "" {
		return "[tool " + msg.ToolName + " returned no text]"
	}
	return ""
}

// oneLineOf reports the one-line read a projection entry carries: the summary
// the event stored beside its original, and otherwise the first line of the
// original itself, bounded. It reads the original and never rewrites it.
func oneLineOf(summary, content string) string {
	if summary != "" {
		return summary
	}
	line := content
	if at := strings.IndexByte(line, '\n'); at >= 0 {
		line = line[:at]
	}
	runes := []rune(line)
	if len(runes) <= mappedSummaryRunes {
		return line
	}
	return string(runes[:mappedSummaryRunes]) + "…"
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
	return plugin.NewFlow(binding.Plugins)
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
// their ready input together. A session whose chain carries a mapped sequence
// commits no transcript here: that sequence carried every fact of the range as
// it committed, and the range reads back from it instead of from a second
// transcript of the same facts.
func (r *Run) CommitHistory(addr event.Address, msgs []model.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, mapped := r.replays[addr.SessionID]; mapped {
		return
	}
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

// The element satisfies both registration and port faces of the pipeline.
var (
	_ plugin.Instance           = observePlugin{}
	_ plugin.PostCommitObserver = observePlugin{}
)

// Name returns the stable plugin name recorded in every observation gap.
func (p observePlugin) Name() string { return "commit-observer" }

// Port reports the fact->observation port this instance serves.
func (p observePlugin) Port() plugin.Port {
	return plugin.Port{Reads: plugin.MaterialFact, Produces: plugin.ProductObservation}
}

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
