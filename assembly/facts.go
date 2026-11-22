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

// noWindowResidentFacts is the mechanism constant a session chain without a
// replay window keeps: a chain that declares no window folds nothing, so the
// band rule cannot bound it and the resident set falls back to this many recent
// entries beside the whole segment still in flight. A chain that declares a
// window is bounded by its projection instead (see trimFacts).
const noWindowResidentFacts = 2048

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
// - A committed fact advances the movement sequence the rest criterion is fenced by, because a committed fact is an input round a consumer may still owe.
//
// The failure is the caller's to observe, never a line this entry writes and
// swallows: the identity is empty beside a commit that did not land, a caller
// that reads the fact back by the reported identity never reads a fact the
// storage tier refused, and a caller that can neither return nor read the
// failure records it at its own call site. One identity names one immutable
// fact: a commit that repeats the fact the identity already names is answered
// as the commitment it already is — the original identity beside no second
// record, folded again into the consumers that state it — and a commit carrying
// other content under that identity is refused by name before anything under it
// moves. The judgment is the same one the
// storage tier answers under, so the live ledger and the durable record never
// disagree about what one identity names.
func (r *Run) commitFactChecked(ctx context.Context, evt event.Event) (event.EventID, error) {
	id, err := r.commitFactNoCadence(ctx, evt)
	if err != nil {
		return id, err
	}
	if evt.Type != event.TypeCheckpoint {
		r.noteCheckpointCommit()
	}
	return id, nil
}

// commitFactNoCadence is the commit entry that files no checkpoint and advances
// no cadence: the coordinator files its own checkpoint through it, so the fact that
// states the account never triggers the cadence that files another.
//
//   - a checkpoint fact stays on the store the boot reads it from and enters no session chain: it is a framework state checkpoint, not material of any chain, so the live ledger keeps the window of the chains it serves and never one fact per cut.
//   - the whole commit holds the quiet-point fence, so a cut that drained the pipeline reads every state this fact left behind instead of one caught between the landing and its application. The derived state of every consumer mounted on the state seam is folded inside that unit for the same reason: the fact and the state it leaves behind are one step, never a landing a consumer beside it has not read yet.
//   - the landing fence covers that step whole: the slot, the registration that states it and the fold that derives the state of every consumer run inside one critical section, so two facts of one chain reach the seam in the order the storage committed them and the derived state of this process is the state a replay of the same committed order derives. The reads that answer the identity stand outside that section, so a commit of an identity another commit just landed still meets the chain before the fence.
//   - a product is the moment the projection drops material, so the chain's checkpoint is worth refreshing whatever the cadence clock says. The request is a signal: the filer takes the cut on its own goroutine, where this commit's unit of work is not held.
//   - a fact the chain already carries as material it has presented files no position of its own: filing it would put a fact the chain committed past at the end of the order the fold states, and the fold runs over a sequence that already states the material.
//   - a repeat of the fact the chain holds is reduced again, because the repeat is what retries an earlier refusal, and the seam is told the fact repeats one the chain carries, so a holder whose idempotence rests on an identity its own rewrite can drop answers without restating the material (see reduceCommittedRepeat).
//   - the write's own answer decides what this entry does next: a fact that landed here files its position and folds, a fact the chain already held whole files no second position, and a conflict or an outcome the store cannot name answers beside no registration, no fold and no success.
//   - FactCommitted: the fact landed here, so this entry files its position and folds it below.
//   - FactAlreadyPresent: the chain holds the fact whole, so this commit states no position of that chain's own; what it does instead is read from where the identity stands against the material the chain has presented (see chainAlreadyPresented). A fact at or before that material is a repeat: it registers nothing and reduces through the repeat face, because a holder that states a sequence dropped the identity of the material its own rewrite moved out, and filing that identity again states the material a second time (see reduceCommittedRepeat). A fact beyond it is a landing this process never folded — the write the barrier answered with no outcome is exactly that fact — so this entry states the landing a commit that landed would have: registration and fold, because answering it as a plain repeat leaves the seam with state no replay of the same chain derives.
//   - FactConflict: another content stands under the identity, named by the conflict the write answered.
//   - FactUncertain: the store cannot name what it left behind, because only a fresh read decides which landed.
//   - an outcome this entry does not classify is refused rather than filed: the classification is what decides whether a position and a fold belong to it.
//   - the identity is read twice and the two answers are never one read: the first says whether the chain carried the fact before this commit looked.
//   - the second is the record this commit's own write repeats; both reads stand before the ordered unit, so no fence serializes the lookups themselves.
func (r *Run) commitFactNoCadence(ctx context.Context, evt event.Event) (event.EventID, error) {
	if evt.Key == "" {
		evt.Key = r.mintEventID()
	}
	session := sessionOf(evt)
	r.flight.enterSession(session)
	defer r.flight.leaveSession(session)
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
		r.reduceCommittedRepeat(ctx, held)
		return evt.Key, nil
	}
	_, carried, err := r.chainRecord(ctx, evt)
	if err != nil {
		return "", err
	}
	stored, chainHolds, err := r.chainRecord(ctx, evt)
	if err != nil {
		return "", err
	}
	r.landing.Lock()
	defer r.landing.Unlock()
	outcome, err := r.persistFactOrdered(ctx, evt, stored, chainHolds)
	switch outcome {
	case ledger.FactCommitted:
	case ledger.FactAlreadyPresent:
		r.mu.Lock()
		_, registered := r.facts[evt.Key]
		r.mu.Unlock()
		if registered {
			if carried && evt.Type != event.TypeCheckpoint {
				r.reduceCommittedRepeat(ctx, evt)
			}
			return evt.Key, nil
		}
		if r.chainAlreadyPresented(evt) {
			if evt.Type != event.TypeCheckpoint {
				r.reduceCommittedRepeat(ctx, evt)
			}
			return evt.Key, nil
		}
		carried = false
	case ledger.FactConflict:
		return "", err
	case ledger.FactUncertain:
		return "", fmt.Errorf("tagent: the fact %s reached an outcome the storage cannot name: %w", evt.Key, err)
	case ledger.FactRefused:
		return "", err
	default:
		return "", fmt.Errorf("tagent: the fact %s answered the storage outcome %d this entry does not classify", evt.Key, outcome)
	}
	if evt.Type == event.TypeCheckpoint {
		return evt.Key, nil
	}
	if carried {
		r.reduceCommittedRepeat(ctx, evt)
		return evt.Key, nil
	}
	_, mapped := mappedEntryOf(evt)
	r.mu.Lock()
	r.facts[evt.Key] = evt
	r.factOrder = append(r.factOrder, evt.Key)
	if session := sessionOf(evt); session != "" {
		r.sessionFactCount[session]++
	}
	r.mu.Unlock()
	r.movement.Advance()
	if mapped {
		r.attachDeclaredReplay(evt)
	}
	r.reduceCommitted(ctx, evt)
	if evt.Type == event.CompressProductType && session != "" {
		r.requestSessionCheckpoint(event.Address{AgentID: agentOf(evt), SessionID: session})
	}
	r.trimFacts(sessionOf(evt))
	return evt.Key, nil
}

// reduceCommitted folds one landed fact into the derived state of every consumer
// mounted on the state seam: the one derivation path of this run. The live commit
// and the replay of a chain past its cut both fold through the same reducer, so a
// restart derives exactly the state the process before it derived from the same
// facts instead of rebuilding it through a reader of its own.
//
// A fact is committed before it is folded, so a refusal is reported beside the
// fact rather than answered as a failed commit: the chain keeps the fact either
// way, and a consumer whose fold failed is the state this run must not read as
// current. The seam keeps the identity of that fact, so a cut refuses to state a
// position past it until the same fact lands in every consumer.
func (r *Run) reduceCommitted(ctx context.Context, evt event.Event) {
	if r == nil {
		return
	}
	states := r.stateRegistry()
	if states == nil {
		return
	}
	if err := states.Reduce(ctx, evt); err != nil {
		assemblyError("tagent: the committed fact %s was not folded into the state of its consumers: %v", evt.Key, err)
	}
}

// reduceCommittedRepeat folds one fact the chain already carries through the same
// seam the filing commit drives, told that it repeats one the chain holds instead of
// filing a position of its own. The live ledger answers a repeat of a resident fact
// and the storage tier answers a repeat of a fact this process released, so both
// commit branches reach here; a holder that states a sequence drops the identity of
// the material a rewrite moves out of it, and the repeat is the one commit that must
// not restate that material (see projectionStateSlice.ReduceRepeat).
func (r *Run) reduceCommittedRepeat(ctx context.Context, evt event.Event) {
	if r == nil {
		return
	}
	states := r.stateRegistry()
	if states == nil {
		return
	}
	if err := states.ReduceRepeat(ctx, evt); err != nil {
		assemblyError("tagent: the repeated fact %s was not folded into the state of its consumers: %v", evt.Key, err)
	}
}

// chainAlreadyPresented reports whether the session chain of one fact has already
// presented the position that fact stands at: the mapped sequence the chain registered
// carries material a request reads, and the fact was recorded at or before the newest
// of it. The commit asks this where its own registration set cannot answer — a fact the
// chain holds whole beside no registration of this process is either a retry of material
// the live window released, whose registration trimFacts dropped together with the
// entries the fold moved out, or a landing whose barrier named no outcome and which no
// sequence has stated yet — and the judgment rests on the sequence, never on the
// released set: that registration is the one signal the trim erases.
//
//   - A chain that registered no sequence presents no position, and a sequence whose ended material is empty presents none either: nothing has reached a request, so the fact is the commit that states it.
//   - The presented position reaches the end of the ended material (see replayEntry.endedPrefix): every entry before that boundary, and the output that closed a segment on it, is material a request already read, while the input that opened the range in flight is carried by the drive of that range.
//   - The two sides are compared by the time each fact was recorded: a fold rewrites the positions around the boundary, so the times the entries carry are what a rewrite leaves standing, and two facts recorded at one instant stand at one position the sequence presents once.
func (r *Run) chainAlreadyPresented(evt event.Event) bool {
	r.mu.Lock()
	_, held, carried := r.replaySlotLocked(replayAddressOf(evt))
	r.mu.Unlock()
	if !carried || held.projection == nil {
		return false
	}
	entries := held.projection.Entries()
	prefix := held.endedPrefix()
	if prefix <= 0 {
		return false
	}
	if prefix > len(entries) {
		prefix = len(entries)
	}
	presented := entries[0].Time
	for _, entry := range entries[:prefix] {
		if entry.Time.After(presented) {
			presented = entry.Time
		}
	}
	return !evt.Timestamp.After(presented)
}

// stateRegistry answers the state seam this run mounted, under the lock every other
// read of it takes.
func (r *Run) stateRegistry() *state.StateRegistry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.states
}

// trimFacts holds one session chain to what its projection still presents — and
// releases only facts that can persist: an ownerless judgment never reaches the
// durable chain, so releasing it would erase the only copy and it stays resident
// instead. A run beside no storage tier keeps the destructive window contract it
// always carried. A
// chain carrying a positive replay window releases the facts before the oldest
// original its projection still carries — the fold that replaced covered
// entries with a product released them already, so the resident set follows the
// window and not the history. A chain carrying no window folds nothing, so it
// keeps the fallback count beside the whole segment in flight. The durable
// chain of the storage tier is untouched, so a released fact is still read back
// by identity; a fact of a run beside no tier exists only here, and the window
// bound is the contract that run carries.
func (r *Run) trimFacts(session event.SessionID) {
	if session == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trimFactsLocked(session)
}

// trimFactsLocked is trimFacts under the run lock the caller holds.
func (r *Run) trimFactsLocked(session event.SessionID) {
	keys := make([]event.EventID, 0, len(r.factOrder))
	for _, key := range r.factOrder {
		if evt, held := r.facts[key]; held && sessionOf(evt) == session {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return
	}
	cut := r.keepPinnedLocked(keys[:r.liveWindowCutLocked(keys, session)])
	if len(cut) == 0 {
		return
	}
	released := cut[:0]
	for _, key := range cut {
		evt, held := r.facts[key]
		if !held || evt.Owner.AgentID == "" {
			continue
		}
		released = append(released, key)
	}
	if len(released) == 0 {
		return
	}
	cut = released
	for _, key := range cut {
		delete(r.facts, key)
	}
	r.sessionFactCount[session] = len(keys) - len(cut)
	dropped := make(map[event.EventID]struct{}, len(cut))
	for _, key := range cut {
		dropped[key] = struct{}{}
	}
	r.compactFactOrderLocked(dropped)
}

// liveWindowCutLocked reports how many leading entries of one chain its live
// window drops: the entries before the oldest original the session's windowed
// projections still present, or — for a chain carrying no positive window — the
// fallback count beside the segment in flight.
func (r *Run) liveWindowCutLocked(keys []event.EventID, session event.SessionID) int {
	position := make(map[event.EventID]int, len(keys))
	for at, key := range keys {
		position[key] = at
	}
	boundary := -1
	for addr, held := range r.replays {
		if addr.SessionID != session || held.window.MaxTokens <= 0 || held.projection == nil {
			continue
		}
		for _, entry := range held.projection.Entries() {
			if replay.SyntheticKey(entry.Key) {
				continue
			}
			at, carried := position[entry.Key]
			if !carried {
				continue
			}
			if boundary < 0 || at < boundary {
				boundary = at
			}
		}
	}
	if boundary < 0 {
		return r.fallbackCutLocked(keys)
	}
	return boundary
}

// trimSeededFacts holds every chain a restart replayed to its hot window: the
// seeded ledger is read as the live one, so a restart answers the window a
// process that stayed up carries. The chains are grouped in one pass and the
// commit order is trimmed once.
func (r *Run) trimSeededFacts() {
	r.mu.Lock()
	defer r.mu.Unlock()
	chains := make(map[event.SessionID][]event.EventID, 8)
	for _, key := range r.factOrder {
		evt, held := r.facts[key]
		if !held {
			continue
		}
		if session := sessionOf(evt); session != "" {
			chains[session] = append(chains[session], key)
		}
	}
	dropped := make(map[event.EventID]struct{}, len(r.factOrder))
	for session, keys := range chains {
		cut := r.keepPinnedLocked(keys[:r.liveWindowCutLocked(keys, session)])
		for _, key := range cut {
			dropped[key] = struct{}{}
			delete(r.facts, key)
		}
		r.sessionFactCount[session] = len(keys) - len(cut)
	}
	r.compactFactOrderLocked(dropped)
}

// fallbackCutLocked reports how many leading entries of one chain its fallback
// window drops: the most recent noWindowResidentFacts entries beside the
// segment a drive may still read, which is the turn-end fact that closed the
// previous segment together with every entry committed after it. A chain beside
// no turn-end fact is one segment in flight from its head, so it is kept whole.
func (r *Run) fallbackCutLocked(keys []event.EventID) int {
	cut := len(keys) - noWindowResidentFacts
	if cut < 0 {
		cut = 0
	}
	for at := len(keys) - 1; at >= 0; at-- {
		if r.facts[keys[at]].Type == event.TypeAgentOutput {
			if at < cut {
				cut = at
			}
			return cut
		}
	}
	return 0
}

// trimReplayProjection holds one registered projection to its resident window: a
// chain carrying a positive replay window is bounded by its age fold, so its
// projection is left as the fold built it; a chain carrying no window folds
// nothing, so a projection grown past twice the fallback count is rebuilt down
// to the fallback count and the rest is left to the durable chain. Rebuilding is
// the one way to drop the per-key material the projection accumulates beside its
// entries, so the trim leaves no map entry behind.
func trimReplayProjection(ctx context.Context, projection *replay.ReplayProjection, window FoldWindow) *replay.ReplayProjection {
	if projection == nil || window.MaxTokens > 0 || projection.Len() <= 2*noWindowResidentFacts {
		return projection
	}
	entries := projection.Entries()
	trimmed := replay.NewReplayProjection()
	for _, entry := range entries[len(entries)-noWindowResidentFacts:] {
		trimmed.Append(ctx, entry)
	}
	return trimmed
}

// compactFactOrderLocked removes the named identities from the commit order of
// this run, so the order reads the facts the live ledger still holds. A trim
// shrinks the order, so the control-plane scan cursor and count are recomputed
// over the kept prefix: controlScanned stays a valid index into the shorter
// order and controlFacts still counts the control records before it.
func (r *Run) compactFactOrderLocked(dropped map[event.EventID]struct{}) {
	if len(dropped) == 0 {
		return
	}
	kept := r.factOrder[:0]
	scanned, control := 0, 0
	for i, key := range r.factOrder {
		if _, gone := dropped[key]; gone {
			continue
		}
		if i < r.controlScanned {
			scanned++
			if evt, held := r.facts[key]; held && generationRetirementRecord(evt) {
				control++
			}
		}
		kept = append(kept, key)
	}
	r.factOrder = kept
	r.controlScanned = scanned
	r.controlFacts = control
}

// persistFactOrdered writes one fact of this run to the storage tier it declares and
// answers what the write left there: the body and the record commit as one unit
// behind the segment barrier, the record taking the next sequence slot of the fact's
// partition; a query reads the chain directly, so the same process recalls what it
// just committed without a landing step.
//
//   - The caller holds the landing fence: the write, the registration that states its position and the fold that derives the state of the consumers are one ordered unit, so the sequence the storage committed and the order the state seam derives are the same order.
//   - The record the identity already carries is the caller's read, taken before that fence: this entry judges its content and commits nothing twice, but the lookup itself stands where two commits of one identity still meet on a chain holding nothing.
//   - A run beside no storage tier, and a fact naming no owning agent, keep the fact in memory alone: no tier means no durable ledger to write, and the commit answers the fact as one that landed a position of its own.
//   - The duplicate judgment runs on this layer's semantics, which the tier's byte comparison cannot answer: the very fact the identity already names is answered as the commitment it is, and another content under that identity is refused by name, so a retry of one fact keeps one record and a second fact never lands beside it.
//   - The tier commits one fact whole or not at all, so a refused commit leaves neither the body nor the record behind.
//   - The answer is the store's own five-value outcome, handed on unwrapped: the entry that commits the fact classifies what the write left, instead of reading every refusal as one failure and every duplicate as one landing. `FactUncertain` answers a write whose durable outcome the store cannot name, which is neither the refusal nor the commitment the caller reads a position from.
func (r *Run) persistFactOrdered(ctx context.Context, evt event.Event, stored ledger.StoredEvent, chainHolds bool) (ledger.FactCommitOutcome, error) {
	r.mu.Lock()
	tier := r.tier
	r.mu.Unlock()
	if tier == nil || evt.Owner.AgentID == "" {
		return ledger.FactCommitted, nil
	}
	body, err := memory.BodyFromEvent(evt, factCommitFormat, factCommitRetention)
	if err != nil {
		return ledger.FactRefused, err
	}
	encoded, err := body.Encode()
	if err != nil {
		return ledger.FactRefused, err
	}
	partition := memory.PartitionIDFromName(string(evt.Owner.AgentID))
	if chainHolds {
		if err := storedFactConflict(evt.Key, stored, body); err != nil {
			return ledger.FactConflict, err
		}
		return ledger.FactAlreadyPresent, nil
	}
	window := evt.Timestamp.UnixMilli()
	if window < 0 {
		return ledger.FactRefused, fmt.Errorf("tagent: the fact %s carries no event time", evt.Key)
	}
	seq := r.landedSeq[partition] + 1
	record := ledger.SegmentRecord{
		PartitionID: partition,
		WindowTS:    window,
		Seq:         seq,
		EventKey:    evt.Key,
		EventTime:   window,
		Body:        encoded,
	}.WithChain(evt.Type, sessionOf(evt))
	outcome, err := ledger.CommitFact(ctx, tier, ledger.FactCommit{
		Record: record,
		Ref:    body.Ref(),
		Body:   []byte(body.Message.Content),
	})
	switch outcome {
	case ledger.FactCommitted:
		r.landedSeq[partition] = seq
		return ledger.FactCommitted, nil
	case ledger.FactAlreadyPresent:
		return ledger.FactAlreadyPresent, nil
	case ledger.FactConflict:
		prior, held, getErr := tier.Segments.GetEventInPartition(ctx, evt.Key, partition)
		if getErr != nil {
			return ledger.FactConflict, getErr
		}
		if !held {
			return ledger.FactConflict, err
		}
		if err := storedFactConflict(evt.Key, prior, body); err != nil {
			return ledger.FactConflict, err
		}
		return ledger.FactAlreadyPresent, nil
	default:
		return outcome, err
	}
}

// chainRecord answers the record the chain already carries under the identity the
// commit names, beside whether the chain carries one at all: the lookup a commit
// asks before it states a position of its own, so a retry of a fact the chain
// committed earlier lands nothing new. The read addresses the fact's partition and
// answers no body unless the identity is taken, which is why it costs one index read
// on the commits that state something. A run beside no storage tier and a fact
// naming no owning agent keep no durable record, so the live ledger is the only
// order they state.
func (r *Run) chainRecord(ctx context.Context, evt event.Event) (ledger.StoredEvent, bool, error) {
	r.mu.Lock()
	tier := r.tier
	r.mu.Unlock()
	if tier == nil || evt.Owner.AgentID == "" {
		return ledger.StoredEvent{}, false, nil
	}
	partition := memory.PartitionIDFromName(string(evt.Owner.AgentID))
	return tier.Segments.GetEventInPartition(ctx, evt.Key, partition)
}

// chainHoldsFact answers whether the chain already carries the fact under the identity
// it names, for the callers that ask nothing about the record: a retry of a fact the
// chain committed earlier lands nothing new, and a product whose identity the chain
// already states costs no second write.
func (r *Run) chainHoldsFact(ctx context.Context, evt event.Event) (bool, error) {
	_, held, err := r.chainRecord(ctx, evt)
	return held, err
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

// foldIntoProjection folds one mapped entry into the replay projection of one
// chain under the window that chain declared, and answers the projection the fold
// left beside whether the entry was newly carried: the append beside the resident
// bound is one step, taken by the live commit and by the replay of a chain past
// its cut alike, so the bound is applied to the sequence once instead of once per
// read path.
func foldIntoProjection(ctx context.Context, projection *replay.ReplayProjection, entry replay.ProjectionEntry, window FoldWindow) (*replay.ReplayProjection, bool) {
	if projection == nil {
		return nil, false
	}
	carried := projection.Append(ctx, entry)
	return trimReplayProjection(ctx, projection, window), carried
}

// foldMapped folds one mapped entry into the sequence of its session and moves
// the boundary between the material that ended and the material still in
// flight: the boundary advances only where a segment stands closed — the
// agent_output fact that closed it — so the input that starts the next range
// lands every earlier segment committed as ended material the next request
// presents while the transcript stops carrying it. Facts of the range in
// flight — the input a second entry lands beside the open segment, the
// reasoning, the calls, the output of a round and the notice of a call it
// admitted — stay behind that boundary, because the consumer of the open
// segment carries them: the resident loop pulls what arrives into the segment
// it flies, and a drive reads its own range's input again at its next decision
// boundary (runtime/agent Loop boundaryInput).
// A notice fact is a mapped fact like any other, so a notification enters the
// chain's projection as it commits and the range reads it back as a projection
// increment instead of from a lane of its own.
//
// - Boundary move on close: the agent_output that closed a segment lands the segment itself as ended material — the boundary moves onto it (inclusive, see endedPrefix), so a later arrival presents everything the closed segment committed, the input that reached the chain while it flew but no decision of it read included.
// - Boundary move on a newly carried entry only: the append answers whether the sequence took the entry at all, and only that fact can close a range. The commit tells a repeat apart from a filing before the fold runs (see reduceCommittedRepeat), so a retried commit of an earlier fact never reaches this step and leaves the boundary of the latest completed round where it stands.
func (r *Run) foldMapped(ctx context.Context, evt event.Event, entry replay.ProjectionEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	slot, held, carried := r.replaySlotLocked(replayAddressOf(evt))
	if !carried {
		return
	}
	var landed bool
	held.projection, landed = foldIntoProjection(ctx, held.projection, entry, held.window)
	if !landed {
		r.replays[slot] = held
		return
	}
	if evt.Type == event.TypeAgentOutput {
		held.endedAt = entry.Key
		held.rangeOpen = false
	}
	if admitsRange(evt) && !held.rangeOpen {
		held.endedAt = entry.Key
		held.rangeOpen = true
	}
	r.replays[slot] = held
}

// delegatedPartMeta, delegatedPartInstruction and delegatedPartMaterial name the part
// one fact of a delegated input batch states. The delegated tool commits the batch at
// its execution site — the instruction the round runs under, one projection per
// material the call handed over, and the task that closes the batch — and stamps
// each fact with the part it is, so a reader tells the parts apart from every other
// input of the chain.
const (
	delegatedPartMeta        = "delegated_part"
	delegatedPartInstruction = "instruction"
	delegatedPartMaterial    = "material"
)

// admitsRange reports whether one committed fact starts a range: the ready
// input an invocation aggregates, and the notice fact an unattended follow-up
// turn commits as the material its drive reads at the head. Every other fact —
// the reasoning, the calls and the output of a round, the progress notice a
// running call admits, and the instruction and material facts of a delegated
// input batch — belongs to the range already in flight and never moves the
// boundary. The notice fact is recognised by the trigger source its committing
// path stamps, so a progress notice of the same type under the same range is
// never mistaken for the start of another one. A delegated batch is carried into
// the range its task opens by name (runtime/agent SubmitDelegated), so its
// instruction and materials never open a range of their own: a batch whose task
// never landed leaves them as ended material no request reads as a new range.
// The drive's own boundary read keeps the same criterion (runtime/agent
// startsRange), so a fact that starts a range here and a fact a drive reads
// again there are one judgement.
func admitsRange(evt event.Event) bool {
	if evt.Type == event.TypeExternalInput {
		switch evt.Meta[delegatedPartMeta] {
		case delegatedPartInstruction, delegatedPartMaterial:
			return false
		}
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
// one-line read stored beside it, the text the model view speaks for it and the
// recorder generation the envelope names. The generation is copied verbatim and
// never truncated, so a folded line can tell which runtime instance wrote the
// fact it reads; a fact that carries no model payload maps onto no entry, so the
// mapped sequence never carries an empty message the model would read.
//
// A configuration change is control-plane material, never a transcript line: the
// fact names what moved and is read by its identity and metadata, so it maps
// onto no entry even though it carries a message body. The skip is explicit by
// type — a producer that stamps a session on the fact still cannot leak the
// change into a model request. A compression mechanism fact is refused here by
// its frozen type as well, so an internal summary can never be mapped into a
// model request even if a caller gives the fact a message body, and a checkpoint
// checkpoint is refused the same way: the checkpoint a session files is tagged with that
// session in the index, so a rebuild that walks the chain would otherwise read the
// checkpoint's payload as one line of the transcript.
// A delivery retry attempt rides the settlement family but is pipeline
// accounting, not a settlement state: the fact records how many redrives the
// chain already scheduled, so it maps onto no entry and never reaches the model
// as a line of dialogue.
//
// 契约: docs/wiki/agent/event-flow.md#projection-lifecycle
func mappedEntryOf(evt event.Event) (replay.ProjectionEntry, bool) {
	if evt.Type == event.TypeConfigChange || evt.Type == event.TypeCheckpoint || event.CompressionMechanism(evt.Type) {
		return replay.ProjectionEntry{}, false
	}
	if _, _, isRetryAttempt := deliveryRetryAttemptOf(evt); isRetryAttempt {
		return replay.ProjectionEntry{}, false
	}
	content := mappedContentOf(evt.Message)
	if content == "" {
		return replay.ProjectionEntry{}, false
	}
	return replay.WithInstanceID(replay.WithInputEvents(replay.ProjectionEntry{
		Key:     evt.Key,
		Type:    evt.Type,
		Summary: oneLineOf(evt.Meta[event.MetaKeyEventSummary], content),
		Content: content,
		Time:    evt.Timestamp,
	}, evt.Meta[event.MetaKeyInputEvents]), evt.InstanceID), true
}

// foldProjectionOf renders the projection the age fold judges from the one the
// session presents: the same mapped sequence without the delivery settlement
// facts. Delivery accounting is not dialogue. A settlement fact is presented to
// the model (the spec requires it), but it must not move the fold threshold, the
// band ages or the segment boundaries a request reads — the tokens of an owed
// receipt are not the tokens of a turn, and a held obligation pinned in the hot
// window must not make the history fold sooner. The fold therefore judges and
// mints products on this accounting-free view, while the consumer applies the
// products to the full projection, so the settlement lines stay visible where
// they belong and nowhere count as conversation.
//
// The view is derived on every judgment, never kept as a second mutable
// sequence: a fold rewrites the full projection, and the derived view follows
// that rewrite by construction instead of drifting from it.
func foldProjectionOf(ctx context.Context, projection *replay.ReplayProjection) *replay.ReplayProjection {
	if projection == nil {
		return nil
	}
	entries := projection.Entries()
	folded := replay.NewReplayProjection()
	for _, entry := range entries {
		if entry.Type == deliverySettlementType {
			continue
		}
		folded.Append(ctx, entry)
	}
	return folded
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
//
// A result that lands after the close of this run returned is booked but never
// handed on or consumed: the consumer set that would drive the range stopped
// with the close, so the entry is handed back to the pending pool and the next
// process reads the notification as the work it still owes. While the close still
// runs, the result is handed over exactly as it is beside no close at all — the
// quiesce is the turn that finishes it, not a stop that strands it.
//
// - A hand-over whose delivery fact the backflow refuses is booked and handed on exactly as it is, and is not consumed: the range drives the result in this process, while the record the restart pairs that result with is what the chain is missing, so the notification stays owed under the claim this process took and the next process hands the same result over again beside the fact that then lands.
// - That claim stays with this process instead of returning to the pending pool: the hand-over it records already ran here, so an entry the pool would offer again is the same notice handed on twice, while the entry an unconsumed seat leaves is exactly the work the next process restores.
// - The hand-over is not undone to spare the drive the result: the in-process delivery is no external action, and a record the chain refused is the one thing a restart redoes for free.
func (r *Run) sink(ctx context.Context, notice task.Notice) error {
	frame, ok := task.CallFrameFrom(ctx)
	if !ok {
		return task.ErrCallFrameMissing
	}
	seq, err := r.bookNotice(ctx, frame, notice)
	if err != nil {
		return err
	}
	if r.closeFinished() {
		return r.retainNoticeOwed(ctx, frame, notice, seq)
	}
	if refusal := r.loop.Backflow(frame.Identity.InvocationID, notice); refusal != nil {
		assemblyDebug("tagent: the notice of task %s stays owed under the claim of this process", notice.TaskID)
		return nil
	}
	return r.store.Consume(ctx, frame.Address, frame.Identity.InstanceID, seq)
}

// retainNoticeOwed keeps one booked notification owed on the mailbox rather than
// recording its consumption: the acceptance stays the record the next process
// restores, and the claimed entry is handed back to the pending pool so the
// receipt the restart reads is never written for a hand-over the chain holds no
// record of. The close of this run returned before the hand-over, so no consumer
// set of this process would drive the result, and the next process reads the
// notification as the work it still owes. A seat already consumed by the turn the
// close waited out is answered as settled and keeps its receipt.
func (r *Run) retainNoticeOwed(ctx context.Context, frame task.CallFrame, notice task.Notice, seq state.AcceptSeq) error {
	if err := r.store.Release(context.WithoutCancel(ctx), frame.Address, frame.Identity.InstanceID, seq); err != nil && !errors.Is(err, state.ErrClaimSettled) {
		return fmt.Errorf("tagent: keep the notice of task %s owed: %w", notice.TaskID, err)
	}
	assemblyDebug("tagent: the notice of task %s stays owed", notice.TaskID)
	return nil
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

// Material declares what this observer reads, when it reads it and what a failed
// reading leaves behind: one committed fact read after its commit, where a failed
// observation is a named gap and the fact keeps standing.
func (p observePlugin) Material() plugin.Material {
	return plugin.Material{
		Reads:   plugin.PortFactObservation,
		Timing:  plugin.TimingPostCommit,
		Failure: plugin.RecordGap,
	}
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
