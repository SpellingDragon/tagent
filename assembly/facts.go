package assembly

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/api/model"
	"github.com/SpellingDragon/tagent/api/plugin"
	"github.com/SpellingDragon/tagent/api/view"
	"github.com/SpellingDragon/tagent/features/telemetry"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/agent"
	"github.com/SpellingDragon/tagent/runtime/state"
	"github.com/SpellingDragon/tagent/runtime/task"
)

// mappedSummaryRunes bounds the one-line read a mapped entry carries when the
// fact stored no summary of its own: the read is a prefix of the original, not
// a rewrite of it, so the bound only decides how much of the first line the
// mapped sequence speaks.
const mappedSummaryRunes = 120

// factCommitFormat and factCommitRetention name the reference registration
// every committed fact of this run carries: the original format and the
// retention policy the recall reference is registered under.
const (
	factCommitFormat    = "text/plain"
	factCommitRetention = "keep"
)

// 契约: docs/wiki/platform/platform-subsystems.md#composition-root
//
// commitFactChecked is the one commit entry of this run's facts: an event that
// arrives without an identity is minted one here, and the whole event is kept
// exactly as it stands, so the observer restores the fact as it committed
// instead of reconstructing one.
// - The commit is the one ledger write of a fact of this run: the recorded call result, the ready input of a range, the assistant output of a drive, the settled delivery and the compression judgment all land here.
// - A committed fact is never withheld from a reader and never rolled back for what an observation of it reports.
// - A committed fact enters the mapped sequence of its own session chain right after it lands, so every consumption of that chain assembles the sequence the commits recorded.
//
// The failure is the caller's to observe, never a line this entry writes and
// swallows: the identity is empty beside a commit that did not land, a caller
// that reads the fact back by the reported identity never reads a fact the
// storage tier refused, and a caller that can neither return nor read the
// failure records it at its own call site. One identity names one immutable
// fact: a commit that repeats the fact the identity already names is answered
// as the commitment it already is — the original identity beside no second
// record — and a commit carrying other content under that identity is refused
// by name before anything under it moves. The judgment is the same one the
// storage tier answers under, so the live ledger and the durable record never
// disagree about what one identity names.
func (r *Run) commitFactChecked(ctx context.Context, evt event.Event) (event.EventID, error) {
	if evt.Key == "" {
		evt.Key = r.mintEventID()
	}
	r.mu.Lock()
	held, seen := r.facts[evt.Key]
	r.mu.Unlock()
	if seen {
		same, err := sameFactOf(held, evt)
		if err != nil {
			return "", err
		}
		if !same {
			return "", fmt.Errorf("%w: %s", memory.ErrFactKeyConflict, evt.Key)
		}
		return evt.Key, nil
	}
	if err := r.persistFact(ctx, evt); err != nil {
		return "", err
	}
	entry, mapped := mappedEntryOf(evt)
	r.mu.Lock()
	r.facts[evt.Key] = evt
	r.factOrder = append(r.factOrder, evt.Key)
	r.mu.Unlock()
	if !mapped {
		return evt.Key, nil
	}
	r.attachDeclaredReplay(evt)
	r.recordMapped(evt, entry)
	return evt.Key, nil
}

// persistFact writes one fact of this run to the storage tier it declares: the
// body commits byte for byte as the original the recall face reads back, the
// record commits behind the segment barrier under the next sequence slot of the
// fact's partition, and the observation table and the vector pipeline learn the
// event so the same process recalls what it just committed.
// - A run beside no storage tier, and a fact naming no owning agent, keep the fact in memory alone: no tier means no durable carrier to write.
// - The segment commit is the durable write: nothing enters the live ledger before it returns success, and a refused commit leaves the previous position untouched.
// - An event the tier already published is not committed twice: the very fact the identity already names is answered as the commitment it is, and another content under that identity is refused by name, so a retry of one fact keeps one record and a second fact never lands beside it.
// - Landing the query record and submitting the vector generation are consequences of a commit that already landed; their failure is reported and never rolls the fact back.
func (r *Run) persistFact(ctx context.Context, evt event.Event) error {
	r.mu.Lock()
	tier := r.tier
	supply := r.retrieval
	r.mu.Unlock()
	if tier == nil || evt.Owner.AgentID == "" {
		return nil
	}
	body, err := memory.BodyFromEvent(evt, factCommitFormat, factCommitRetention)
	if err != nil {
		return err
	}
	encoded, err := body.Encode()
	if err != nil {
		return err
	}
	stored, held, err := tier.Segments.GetEvent(ctx, evt.Key)
	if err != nil {
		return err
	}
	if held {
		return storedFactConflict(evt.Key, stored, body)
	}
	if err := tier.Originals.Put(ctx, body.Ref(), []byte(body.Message.Content)); err != nil {
		return err
	}
	partition := memory.PartitionIDFromName(string(evt.Owner.AgentID))
	window := evt.Timestamp.UnixMilli()
	if window < 0 {
		return fmt.Errorf("tagent: the fact %s carries no event time", evt.Key)
	}
	r.landing.Lock()
	seq := r.landedSeq[partition] + 1
	err = tier.Segments.StoreEvent(ctx, ledger.SegmentRecord{
		PartitionID: partition,
		WindowTS:    window,
		Seq:         seq,
		EventKey:    evt.Key,
		EventTime:   window,
		Body:        encoded,
	})
	if err == nil {
		r.landedSeq[partition] = seq
	}
	r.landing.Unlock()
	if errors.Is(err, ledger.ErrContentConflict) {
		prior, held, getErr := tier.Segments.GetEvent(ctx, evt.Key)
		if getErr != nil {
			return getErr
		}
		if !held {
			return err
		}
		return storedFactConflict(evt.Key, prior, body)
	}
	if err != nil {
		return err
	}
	if supply == nil {
		return nil
	}
	landed := ledger.StoredEvent{Key: evt.Key, PartitionID: partition, WindowTS: window, Seq: seq, Body: encoded}
	if err := supply.source.LandEvent(ctx, landed); err != nil {
		assemblyWarn("tagent: the committed fact %s is not landed in the query table err=%v", evt.Key, err)
	}
	if supply.seam != nil {
		text := body.Message.Content
		if text == "" {
			text = body.Meta[event.MetaKeyEventSummary]
		}
		supply.seam.Submit(evt.Key, text)
	}
	return nil
}

// sameFactOf reports whether one live fact and one commit name the same fact:
// the comparison runs on the bodies the two project to, exactly as the storage
// tier compares the record it holds, so the live ledger and the durable record
// judge one identity under one semantics.
func sameFactOf(held, evt event.Event) (bool, error) {
	existing, err := memory.BodyFromEvent(held, factCommitFormat, factCommitRetention)
	if err != nil {
		return false, err
	}
	candidate, err := memory.BodyFromEvent(evt, factCommitFormat, factCommitRetention)
	if err != nil {
		return false, err
	}
	return existing.SameFact(candidate), nil
}

// storedFactConflict answers one commit that met a record already stored under
// its identity: the very fact that record carries is the commitment it already
// is and commits nothing twice, and another content is refused by the named
// key conflict before anything under the identity moves.
func storedFactConflict(key event.EventID, stored ledger.StoredEvent, body memory.EventBody) error {
	existing, err := memory.DecodeEventBody(stored.Body)
	if err != nil {
		return err
	}
	if existing.SameFact(body) {
		return nil
	}
	return fmt.Errorf("%w: %s", memory.ErrFactKeyConflict, key)
}

// recordMapped appends one mapped entry to the sequence of its session and
// moves the boundary between the material that ended and the material still in
// flight: a new range is admitted by the input material that starts it under an
// invocation of its own, and that first fact is where everything the earlier
// ranges committed becomes ended material the next request presents while the
// transcript stops carrying it. Facts of the range in flight — the reasoning,
// the calls, the output of a round and the notice of a call it admitted — stay
// behind that boundary, because the transcript of the drive carries them. A
// notice fact is a mapped fact like any other, so a notification enters the
// chain's projection as it commits and the range reads it back as a projection
// increment instead of from a lane of its own.
// - The boundary never moves for a fact of the range it already stands at: input that
// lands under the invocation of the range in flight is that drive's own material, so
// the drive reads it again at its next decision boundary instead of the boundary
// presenting it as ended material (runtime/agent Loop boundaryInput).
func (r *Run) recordMapped(evt event.Event, entry replay.ProjectionEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	slot, held, carried := r.replaySlotLocked(replayAddressOf(evt))
	if !carried {
		return
	}
	held.projection.Append(context.Background(), entry)
	if admitsRange(evt) && held.rangeInvocation != evt.InvocationID {
		held.endedAt = evt.Key
	}
	held.rangeInvocation = evt.InvocationID
	r.replays[slot] = held
}

// admitsRange reports whether one committed fact starts a range: the ready
// input an invocation aggregates, and the notice fact an unattended follow-up
// turn commits as the material its drive reads at the head. Every other fact —
// the reasoning, the calls and the output of a round, and the progress notice a
// running call admits — belongs to the range already in flight and never moves
// the boundary. The notice fact is recognised by the trigger source its
// committing path stamps, so a progress notice of the same type under the same
// range is never mistaken for the start of another one. The drive's own
// boundary read keeps the same criterion (runtime/agent startsRange), so a fact
// that starts a range here and a fact a drive reads again there are one
// judgement.
func admitsRange(evt event.Event) bool {
	if evt.Type == event.TypeExternalInput {
		return true
	}
	return evt.Type == event.TypeFeedback && evt.Meta[event.MetaKeyTriggerSource] == "task"
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
// registration is left as it stands, and a session naming no window registers
// exactly as a budgeted one does: the window is the presentation budget and the
// fold measurement, never the registration gate — a declaration of no tokens
// presents its projection under no budget at all, and the fold reads the
// default window as its measure.
// A chain beside no projection still reads its notice facts: the notice read
// face attaches for it too and answers from the committed facts of the session,
// so the material of a notification reaches the follow-up decision whether or
// not the declaration registered a mapped sequence. The face is one read of one
// truth — the fact chain — and never a second input lane beside it.
func (r *Run) attachDeclaredReplay(evt event.Event) {
	addr := replayAddressOf(evt)
	if addr.SessionID == "" {
		return
	}
	r.mu.Lock()
	_, scoped := r.replays[addr]
	_, seam := r.replays[event.Address{SessionID: addr.SessionID}]
	held := scoped || seam
	window := FoldWindow{}
	if !held {
		window = foldWindowOf(specOfLocked(r.current.Plan.Declaration, agentOf(evt)))
	}
	r.mu.Unlock()
	if held {
		r.attachNoticeFeed()
		return
	}
	err := r.attachScopedReplay(addr, window, replay.NewReplayProjection())
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
	held, carried := r.replayOfSessionLocked(session)
	r.mu.Unlock()
	if !carried {
		return nil, false
	}
	return held.projection.Entries(), true
}

// noticeProjection is the incremental read face of the session chains' notice
// facts: the mapped sequence the commits of this run record, read from the
// ended-material boundary of the chain, and the committed facts of the session
// themselves for a chain that registered no mapped sequence. The face is
// attached to the loop beside the first mapped fact of a session, so a range
// reads the notifications of its own call as a projection increment — or, beside
// no projection, as the fact it committed — and the fact chain stays the only
// truth of what a notification is: a restarted process reads the same facts
// back.
//
// The redline list (3 rules):
// MUST answer a chain with a registered projection from that projection alone, and a chain without one from the committed facts of that session alone.
// MUST report only the facts under the identity prefix it was handed.
// MUST NOT write the projection or the ledger it reads: the read is a read.
type noticeProjection struct{ run *Run }

// The face satisfies the incremental notice read the loop drives.
var _ agent.NoticeFeed = noticeProjection{}

// NoticeBoundary reports the position the ended material of the named chain ends
// at, and whether that chain has a read face at all: the material before it is
// the material a request presents, so a notice committed before it has already
// reached the range and is never reported again. The position is resolved from
// the fact identity the boundary records on every read, never stored: the age
// fold rewrites the sequence the commits record, so a kept position would name
// other material after the rewrite. A chain beside no projection reads its
// notice facts from the committed facts of the session, and no fact has reached
// a request through a transcript an earlier drive committed without being that
// transcript itself, so its sequence starts at the beginning.
func (p noticeProjection) NoticeBoundary(chain agent.RequestChain) (int, bool) {
	p.run.mu.Lock()
	defer p.run.mu.Unlock()
	held, carried := p.run.replayOfSessionLocked(chain.SessionID)
	if !carried || held.projection == nil {
		return 0, true
	}
	return held.endedPrefix(), true
}

// NoticesFrom reports the notice facts of the named chain from the cursor on
// under the given identity prefix, in chain order, beside the position the read
// reached and whether the chain has a read face. A chain beside no projection
// reads the feedback facts its session committed, in commit order; a chain
// beside a projection reads the mapped sequence it recorded. The cursor is
// resolved against that sequence as it stands at the read: a position the
// caller kept across a fold rewrite names other material, or falls past the
// shortened sequence, so it re-anchors at the ended-material boundary — the
// position the chain resolves from the fact identity it records — instead of
// reading past the rewrite. A fact outside the prefix is never reported, so one
// range reads the notices of its own call alone.
func (p noticeProjection) NoticesFrom(chain agent.RequestChain, prefix event.EventID, cursor int) ([]agent.NoticeView, int, bool) {
	p.run.mu.Lock()
	defer p.run.mu.Unlock()
	held, carried := p.run.replayOfSessionLocked(chain.SessionID)
	if !carried || held.projection == nil {
		return p.run.noticesOfLocked(chain.SessionID, prefix, cursor)
	}
	entries := held.projection.Entries()
	boundary := held.endedPrefix()
	if cursor < boundary || cursor > len(entries) {
		cursor = boundary
	}
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

// noticesOfLocked reports the feedback facts one session committed, in commit
// order, as the notice sequence of a chain beside no projection: the cursor of
// that read is a position in this sequence, and the identity prefix selects the
// notice facts of one range inside it. The read is taken under the run lock the
// caller already holds and returns views the caller owns.
func (r *Run) noticesOfLocked(session event.SessionID, prefix event.EventID, cursor int) ([]agent.NoticeView, int, bool) {
	notices := make([]event.Event, 0, len(r.factOrder))
	for _, key := range r.factOrder {
		evt, kept := r.facts[key]
		if !kept || evt.Type != event.TypeFeedback || sessionOf(evt) != session {
			continue
		}
		notices = append(notices, evt)
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(notices) {
		cursor = len(notices)
	}
	var views []agent.NoticeView
	for at := cursor; at < len(notices); at++ {
		evt := notices[at]
		if !strings.HasPrefix(string(evt.Key), string(prefix)) {
			continue
		}
		views = append(views, agent.NoticeView{Key: evt.Key, Text: evt.Message.Content})
	}
	return views, len(notices), true
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
	key, err := r.commitFactChecked(ctx, evt)
	if err != nil {
		return "", fmt.Errorf("tagent: commit the recorded result: %w", err)
	}
	return key, nil
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
// target beside the mailbox time points that precede the handover — the
// delivery route, the acceptance and the claim of the executor — hands the
// settled result back into the intake of the invocation that issued the call,
// and only then records the completed consumption. The order is the invariant a
// restart reads: a notification whose consumption receipt the log holds is no
// longer owed, so the receipt MUST follow the handover, and a stop between the
// two leaves the notification owed and the restart hands the same result over
// again. The backflow drives that invocation's next decision and never reaches a
// neighbour, and a notice the mailbox refused to book is neither handed on nor
// confirmed.
func (r *Run) sink(ctx context.Context, notice task.Notice) error {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok {
		return task.ErrCallFrameMissing
	}
	seq, err := r.bookNotice(ctx, frame, notice)
	if err != nil {
		return err
	}
	r.loop.Backflow(frame.Identity.InvocationID, notice)
	return r.store.Consume(ctx, frame.Address, frame.Identity.InstanceID, seq)
}

// bookNotice records the mailbox time points of one settled notification that
// precede its handover in the state owner's own bookkeeping: the delivery route
// of the task is registered beside the call frame the notification belongs to,
// the notice is accepted into the mailbox of its target site, and the accepted
// input is claimed by the executor that hands it on. The acceptance is
// idempotent by the notification's stable identity, so a delivery the manager
// already accepted records no second fact here. The completed consumption is
// recorded by the caller after the handover, never here: a claim is not a
// confirmation, and a receipt written before the handover would drop an
// undelivered notification from the chain the restart rebuilds.
func (r *Run) bookNotice(ctx context.Context, frame task.CallFrame, notice task.Notice) (state.AcceptSeq, error) {
	if err := r.store.TrackDelivery(ctx, notice.TaskID, task.HandoffTarget{
		Target:         frame.Address,
		TargetInstance: frame.Identity.InstanceID,
	}); err != nil {
		return 0, err
	}
	if err := r.store.AcceptNotice(ctx, notice); err != nil {
		return 0, err
	}
	site, err := r.store.Site(ctx, r.inspector, frame.Address, frame.Identity.InstanceID)
	if err != nil {
		return 0, err
	}
	seq, ok := mailboxSeqOf(site, notice)
	if !ok {
		return 0, state.ErrEntryUnknown
	}
	if err := r.store.Claim(ctx, frame.Address, frame.Identity.InstanceID, seq); err != nil {
		return 0, err
	}
	return seq, nil
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
// the drives that already ended on it, as a copy the caller owns.
//
// The history of one session chain is one face: the mapped sequence its commits
// record, read through the request boundary of the next invocation. This face
// carries no second copy of it, so it answers an empty transcript for every
// chain — a session whose material exists reads it from that sequence, and a
// session whose commits recorded none has none to read. A reader that keeps a
// transcript beside the sequence would present the same facts twice, once from
// the sequence and once from the copy, and a restart would present a different
// history than the live process, because the copy is not a durability record
// while the sequence is replayed from the durable fact chain.
func (r *Run) SessionHistory(addr event.Address) []model.Message {
	return nil
}

// CommitHistory is the write side of the same one face: the transcript one
// ended drive accumulated is already carried by the mapped sequence of the
// session — every fact of the range entered it as it committed, in commit order,
// with the boundary the next request presents — so committing it here keeps no
// second copy and appends nothing.
//
// The call is kept as the seam the loop commits an ended drive's transcript
// through: a loop beside this face needs no branch of its own, and a run beside
// no mapped sequence records no transcript at all, exactly as a chain whose
// facts committed nothing has nothing to present. The history a restart reads
// comes back from the durable fact chain by replay, never from this seam.
func (r *Run) CommitHistory(addr event.Address, msgs []model.Message) {}

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
	return plugin.PortFactObservation
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
