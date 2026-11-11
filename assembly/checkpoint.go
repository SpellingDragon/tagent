// 契约: docs/wiki/platform/platform-subsystems.md#framework-checkpoint
//
// The checkpoint coordinator of one run: the framework's half of state
// management. Consumers that hold recoverable state register a slice on the
// state seam; the coordinator aggregates those slices at one partition-sequence
// cut and files the aggregated state as one checkpoint fact. Recovery is the
// mirror of it — read the newest checkpoint, hand every slice back to its
// holder, then replay the facts that landed past the cut through seedFact's
// isolated rebuild (memory ledger only, no After pass, no side effects) — so no
// consumer owns a recovery read path of its own. The newest checkpoint comes from a
// backwards walk of the commit order that stops at the first entry of the
// checkpoint type, so the commits since that checkpoint bound the walk; anchor
// discovery is one pass of the resident commit order that copies nothing and
// reads no body; and the bodies read are the ones committed past each chain's
// cut, except a chain the restored slices carry beside no fold product, which
// replays its whole chain.
//
// The cut is consistent by construction, and the construction is an ordering of
// the single-writer pipeline rather than a lock over it: the coordinator reads
// the watermark, drains the pipeline to a quiet point, reads every slice there
// with no lock of the run held, and files the checkpoint through one ordinary
// append.
//   - The quiet point is a drain and not a fence: a commit holds the flight from its landing to the state it leaves its holder in, so a pipeline holding no flight has applied every fact it landed.
//   - A settlement that lands past the cut is replayed again, and its settlement is idempotent, so the restored account is at worst caught up twice and never short.
//   - Nothing is held across the holders' reads, so a holder that commits under its own lock never waits on the cut that is asking it for its state.
package assembly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/event"
	"github.com/SpellingDragon/tagent/memory"
	"github.com/SpellingDragon/tagent/memory/ledger"
	"github.com/SpellingDragon/tagent/memory/replay"
	"github.com/SpellingDragon/tagent/runtime/state"
)

// CheckpointCommitInterval is the number of commits after which the framework
// files a checkpoint whatever its period: a busy chain keeps its recovery cost
// bounded even when the period has not elapsed.
const CheckpointCommitInterval = 64

// CheckpointPeriod is the wall-clock bound between two framework checkpoints: a
// chain that commits little is a chain whose last commit may be far behind the
// boot that reads it, so the period asks for a checkpoint at the next commit even
// when the interval has not elapsed. The due is judged at a commit, so a chain
// that commits nothing files no checkpoint of its own: the close stops the filer
// and takes no cut, and the boot reads the checkpoint the last commit that reached
// a due filed.
const CheckpointPeriod = 30 * time.Second

// checkpointSliceProjection, checkpointSliceDelivery, checkpointSliceTasks,
// checkpointSliceInputs and checkpointSliceCompress name the framework's own
// state slices. A consumer mounted beside them carries a name of its own, so the
// two families never collide on the seam. The mailbox slice is not among them: it
// is keyed by state.StateSliceMailbox beside the holder that states it.
const (
	// checkpointSliceProjection carries the survivors of every session
	// projection the run presents.
	checkpointSliceProjection = "projection"
	// checkpointSliceDelivery carries the host's whole delivery account: the
	// settlement state of every obligation, the obligations still owed with
	// their body, target, lineage, attempt and hold, and the ordering sequence.
	checkpointSliceDelivery = "delivery"
	// checkpointSliceTasks carries the terminal state of every task the
	// registry settled.
	checkpointSliceTasks = "tasks"
	// checkpointSliceInputs carries the input rounds the chains of the run hold
	// with no answer, so the owed drives a restart reads are the rounds the cut
	// covered rather than only the ones that landed past it.
	checkpointSliceInputs = "inputs"
	// checkpointSliceCompress carries the compression consumer's account: the
	// judgments a producer filed and the identities a product answers, so a boot
	// that reads the checkpoint answers every judgment the cut covered instead of
	// executing the ones it skipped.
	checkpointSliceCompress = "compress"
)

// ErrCheckpointUnreadable reports a checkpoint fact whose payload does not state
// the cut and slices its identity was content-addressed over, so no state is
// restored from it.
var ErrCheckpointUnreadable = errors.New("assembly: the checkpoint fact states no readable checkpoint")

// ErrCheckpointSurvivorUnstated reports a checkpoint naming a survivor the chain
// states no entry for. The checkpoint names every survivor by identity, type,
// summary and moment and leaves the text to the chain, so a key the chain does
// not state is a checkpoint no boot can rebuild: restoring the entry beside no text
// would present the model material the process never wrote.
var ErrCheckpointSurvivorUnstated = errors.New("assembly: the checkpoint names a survivor the chain states no entry for")

// commitFence is the quiet point of the fact pipeline: one unit of work holds it
// from the landing of a fact to the state that fact leaves its holder in, and the
// coordinator drains the fence before it reads any state, so a cut is taken where
// the pipeline holds nothing half applied.
//
// The fence never excludes a commit: it is a count the coordinator waits on, not
// a lock the writer takes, so a commit that starts beside a cut lands past the
// cut's watermark and is replayed by the boot. Excluding it would be the lock over
// the commit stream this fence replaces — the one a holder's own commit waits on
// from inside the state reading that asked for it.
type commitFence struct {
	mu sync.Mutex
	// inFlight is the number of units of work the pipeline holds.
	inFlight int
	// idle is the channel a drain parks on: nil while no unit is in flight, and
	// a fresh channel closed by the unit that takes the last one off.
	idle chan struct{}
	// sessions is the per-session count of the units of work the pipeline holds.
	// A unit of a session chain counts here beside the whole, so a cut of one
	// session drains that chain alone while the framework's own cut still drains
	// every unit the run holds.
	sessions map[event.SessionID]*sessionFence
}

// sessionFence is the quiet point of one session chain: the units of work that
// chain holds while a commit lands and applies, beside the channel a drain of
// that chain parks on.
type sessionFence struct {
	inFlight int
	// idle is nil while no unit of this chain is in flight.
	idle chan struct{}
}

// enter counts one unit of work onto the fence.
func (f *commitFence) enter() {
	f.mu.Lock()
	if f.inFlight == 0 {
		f.idle = make(chan struct{})
	}
	f.inFlight++
	f.mu.Unlock()
}

// leave counts one unit of work off the fence and answers every drain parked on
// it once the pipeline holds none.
func (f *commitFence) leave() {
	f.mu.Lock()
	f.inFlight--
	if f.inFlight <= 0 && f.idle != nil {
		close(f.idle)
		f.idle = nil
	}
	f.mu.Unlock()
}

// await answers once the pipeline holds no unit of work: the quiet point a cut
// reads its state at. A unit that enters beside the returned answer lands past
// the cut the caller has already read, so the drain never waits for work the cut
// must also cover.
func (f *commitFence) await(ctx context.Context) error {
	for {
		f.mu.Lock()
		idle := f.idle
		f.mu.Unlock()
		if idle == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idle:
		}
	}
}

// enterSession counts one unit of work of a session chain onto the fence and onto
// that chain's own count. A fact that names no session chain — a configuration
// record, a checkpoint filed for no session — counts on the whole fence
// alone, so a session drain never waits for it and the framework's own cut always
// covers it.
func (f *commitFence) enterSession(session event.SessionID) {
	f.mu.Lock()
	if f.inFlight == 0 {
		f.idle = make(chan struct{})
	}
	f.inFlight++
	if session != "" {
		if f.sessions == nil {
			f.sessions = map[event.SessionID]*sessionFence{}
		}
		held := f.sessions[session]
		if held == nil {
			held = &sessionFence{}
			f.sessions[session] = held
		}
		if held.inFlight == 0 {
			held.idle = make(chan struct{})
		}
		held.inFlight++
	}
	f.mu.Unlock()
}

// leaveSession counts one unit of work of a session chain off the fence and off
// that chain's own count, answering the drains parked on either.
func (f *commitFence) leaveSession(session event.SessionID) {
	f.mu.Lock()
	f.inFlight--
	if f.inFlight <= 0 && f.idle != nil {
		close(f.idle)
		f.idle = nil
	}
	if session != "" {
		if held := f.sessions[session]; held != nil {
			held.inFlight--
			if held.inFlight <= 0 {
				if held.idle != nil {
					close(held.idle)
					held.idle = nil
				}
				delete(f.sessions, session)
			}
		}
	}
	f.mu.Unlock()
}

// awaitSession answers once the named session chain holds no unit of work: the
// quiet point a session cut reads its state at. The drain waits for the commits
// of that chain alone: a commit of another chain lands past the cut's watermark
// and is replayed by the boot, so it neither widens the wait nor widens the
// checkpoint.
func (f *commitFence) awaitSession(ctx context.Context, session event.SessionID) error {
	if session == "" {
		return f.await(ctx)
	}
	for {
		f.mu.Lock()
		held := f.sessions[session]
		var idle chan struct{}
		if held != nil {
			idle = held.idle
		}
		f.mu.Unlock()
		if idle == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-idle:
		}
	}
}

// checkpointDefaults fills the cadence dials of one run: a run that names none
// keeps the family constants, and a check that wants a checkpoint on the next
// commit shortens the interval. The period counts from the moment the cadence
// opens, so a run that has just assembled files its first checkpoint after a period
// or an interval of commits rather than an empty one beside its first fact: the
// chains of such a run are young, and the boot folds them bounded instead.
func (r *Run) checkpointDefaults() {
	if r.states == nil {
		r.states = state.NewStateRegistry()
	}
	if r.checkpointEvery <= 0 {
		r.checkpointEvery = CheckpointCommitInterval
	}
	if r.checkpointPeriod <= 0 {
		r.checkpointPeriod = CheckpointPeriod
	}
}

// StateRegistry reports the state seam of this run: the registry a consumer
// mounts its recoverable state on.
func (r *Run) StateRegistry() *state.StateRegistry {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checkpointDefaults()
	return r.states
}

// RegisterStateSlice mounts one consumer's recoverable state on the state seam
// and answers the disposer that unmounts it.
func (r *Run) RegisterStateSlice(holder state.Stateful) (func(), error) {
	if r == nil {
		return nil, errors.New("assembly: the run is nil")
	}
	return r.StateRegistry().Register(holder)
}

// checkpointWatermark reports the partition-sequence cut of this run: the last
// slot each partition has committed. The cut is the partition sequence the
// landing path numbers commits with, never the process-local identity counter,
// because that counter is neither monotonic across restarts nor comparable
// between partitions. The read takes the landing lock the slot map is guarded by,
// so a cut never reads the slots beside a commit claiming one.
func (r *Run) checkpointWatermark() event.CheckpointOffset {
	r.landing.Lock()
	defer r.landing.Unlock()
	return r.checkpointWatermarkLocked()
}

// checkpointWatermarkLocked is checkpointWatermark under the landing lock the
// caller holds.
func (r *Run) checkpointWatermarkLocked() event.CheckpointOffset {
	offset := make(event.CheckpointOffset, len(r.landedSeq))
	for partition, seq := range r.landedSeq {
		offset[partition] = seq
	}
	return offset
}

// Checkpoint takes one checkpoint of this run now and files the fact it states.
// The cut names the last fact each partition had committed when the cut began,
// and the state is read at the quiet point that follows it, with no lock of this
// run held: the coordinator drains the pipeline — no commit and no state
// application in flight — and asks every holder for its account there. A fact
// that lands while the cut waits for the drain is at worst caught up twice by the
// restore — its slice entry and its replay both apply — and never one the restore
// skips, because every replay path applies a fact by its identity.
//
// The redline list (3 rules):
// MUST read the watermark before the state, so the cut never states a position past the state it carries.
// MUST read the holders' state with no lock of this run held, so a holder that commits under its own lock never waits on the cut asking it for that state.
// MUST refuse the whole checkpoint when one slice refuses to state itself, rather than restore that consumer beside an empty state.
// MUST NOT file a checkpoint the run cannot name an owner for: a fact naming no owner never reaches the storage tier.
func (r *Run) Checkpoint(ctx context.Context) (event.EventID, bool, error) {
	if r == nil {
		return "", false, nil
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()
	return r.checkpointLocked(ctx)
}

// checkpointLocked is Checkpoint under the cut lock the caller holds. The lock
// serializes the cuts of one run with each other — one cut states one account — and
// it is taken by no commit, so it never stands between a fact and its landing.
//
//   - every cut of one run states a key no earlier cut stated: the key is the sequence the owner partition stands at, and the checkpoint's own landing takes a slot past the cut it states, so the next cut beside no new fact names a later slot instead of repeating the filed one.
func (r *Run) checkpointLocked(ctx context.Context) (event.EventID, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	r.mu.Lock()
	tier := r.tier
	owner := r.checkpointOwnerLocked()
	r.mu.Unlock()
	if tier == nil || owner.AgentID == "" {
		return "", false, nil
	}
	offset := r.checkpointWatermark()
	key := event.EventID(event.CheckpointKey(offset[memory.PartitionIDFromName(string(owner.AgentID))]))
	if err := r.flight.await(ctx); err != nil {
		return "", false, err
	}
	slices, err := r.states.SnapshotState(ctx)
	if err != nil {
		return "", false, err
	}
	checkpoint, err := event.CheckpointFor(offset, slices)
	if err != nil {
		return "", false, err
	}
	checkpoint.Key = string(key)
	evt := checkpointEventOf(checkpoint, owner)
	if _, err := r.commitFactNoCadence(ctx, evt); err != nil {
		return "", false, fmt.Errorf("tagent: file the framework checkpoint %s: %w", key, err)
	}
	r.mu.Lock()
	r.checkpointFiled = key
	r.checkpointOffset = offset
	r.checkpointCommits = 0
	r.checkpointAt = time.Now()
	r.mu.Unlock()
	return key, true, nil
}

// CheckpointSession takes one cut of the named session chain and files the fact
// it states. The cut names the last fact each partition had committed when the
// cut began — the same watermark the framework's own cut names, so a session
// checkpoint and a framework checkpoint are read back on one axis — while the state is
// the state of that chain alone: the cut drains the commits of the chain, asks
// every holder what it states about the session, and files a checkpoint whose slices
// are that session's.
//
//   - The checkpoint is filed under the run's checkpoint owner beside the session it states, so the index answers which chain a checkpoint belongs to without reading its body and without the checkpoint entering that chain's projection.
//   - A commit of another chain that lands beside the cut is past its watermark and is replayed by the boot, so the cut pays nothing for it and never waits on it.
//
// The redline list (3 rules):
// MUST read the watermark before the state, so the cut never states a position past the state it carries.
// MUST drain only the named chain, so a cut of one session waits for no commit of another.
// MUST state nothing about a holder that keeps no state of a session, rather than copy another chain's account into this checkpoint.
func (r *Run) CheckpointSession(ctx context.Context, addr event.Address) (event.EventID, bool, error) {
	if r == nil || !addr.IsComplete() {
		return "", false, nil
	}
	r.checkpointMu.Lock()
	defer r.checkpointMu.Unlock()
	return r.checkpointSessionLocked(ctx, addr)
}

// checkpointSessionLocked is CheckpointSession under the cut lock the caller
// holds: one cut states one account, and the lock is taken by no commit.
//
//   - A chain that states no slice files no checkpoint: a checkpoint of no slice would name a cut no holder answers, so no checkpoint is owed.
func (r *Run) checkpointSessionLocked(ctx context.Context, addr event.Address) (event.EventID, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	r.mu.Lock()
	tier := r.tier
	owner := r.checkpointOwnerLocked()
	r.mu.Unlock()
	if tier == nil || owner.AgentID == "" {
		return "", false, nil
	}
	offset := r.checkpointWatermark()
	key := event.EventID(event.CheckpointKey(offset[memory.PartitionIDFromName(string(owner.AgentID))]))
	if err := r.flight.awaitSession(ctx, addr.SessionID); err != nil {
		return "", false, err
	}
	slices, err := r.states.SnapshotSessionState(ctx, addr.SessionID)
	if err != nil {
		return "", false, err
	}
	if len(slices) == 0 {
		return "", false, nil
	}
	checkpoint, err := event.CheckpointFor(offset, slices)
	if err != nil {
		return "", false, err
	}
	checkpoint.Key = string(key)
	evt := checkpointEventOf(checkpoint, event.Address{AgentID: owner.AgentID, SessionID: addr.SessionID})
	if _, err := r.commitFactNoCadence(ctx, evt); err != nil {
		return "", false, fmt.Errorf("tagent: file the session checkpoint %s of session %s: %w", key, addr.SessionID, err)
	}
	names := make(map[string]bool, len(slices))
	for _, slice := range slices {
		names[slice.Name] = true
	}
	r.mu.Lock()
	r.sessionFiled[addr.SessionID] = key
	r.sessionCuts[addr.SessionID] = offset
	r.sessionSlices[addr.SessionID] = names
	r.mu.Unlock()
	return key, true, nil
}

// sessionCut reports the cut the newest checkpoint of one session states for one
// state slice: the boundary the boot replays that chain past. A session the
// restored checkpoints did not state, and a checkpoint that states no such slice,
// answer the framework's own cut instead, which is what bounds a boot that read
// no checkpoint of that chain.
func (r *Run) sessionCut(session event.SessionID, slice string) event.CheckpointOffset {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.sessionCuts[session]) != 0 && r.sessionSlices[session][slice] {
		cut := make(event.CheckpointOffset, len(r.sessionCuts[session]))
		for partition, seq := range r.sessionCuts[session] {
			cut[partition] = seq
		}
		return cut
	}
	if len(r.checkpointOffset) == 0 || !r.checkpointSlices[slice] {
		return nil
	}
	cut := make(event.CheckpointOffset, len(r.checkpointOffset))
	for partition, seq := range r.checkpointOffset {
		cut[partition] = seq
	}
	return cut
}

// checkpointEventOf renders one checkpoint as the fact it commits as: the
// partition-sequence key, the payload as the message body and the two cut
// entries in the metadata. The checkpoint is filed under the run's checkpoint owner
// beside the session it states, so the index answers which chain a checkpoint
// belongs to without reading its body. A session checkpoint names a chain in its
// owner and still enters no projection: the mapping refuses a checkpoint by its
// frozen type, so the checkpoint never reaches a model request.
func checkpointEventOf(checkpoint event.Checkpoint, owner event.Address) event.Event {
	payload, err := event.CheckpointPayloadOf(checkpoint)
	if err != nil {
		payload = nil
	}
	evt := event.Event{
		Key:       event.EventID(checkpoint.Key),
		Type:      event.TypeCheckpoint,
		Timestamp: time.Now().UTC(),
		Owner:     event.Address{AgentID: owner.AgentID, SessionID: owner.SessionID},
		Meta:      checkpoint.MetaEntries(),
	}
	evt.Meta[event.MetaKeyEventSummary] = "framework checkpoint"
	evt.Message.Content = string(payload)
	return evt
}

// checkpointOwnerLocked reports the address a framework checkpoint is filed
// under: the smallest agent the run holds a chain for, so the same run files its
// checkpoints in one partition and a restart reads them back from the index in
// the order they were committed. The read is taken under the run lock.
func (r *Run) checkpointOwnerLocked() event.Address {
	agents := make([]event.AgentID, 0, len(r.replays))
	seen := make(map[event.AgentID]bool, len(r.replays))
	for addr := range r.replays {
		if addr.AgentID == "" || seen[addr.AgentID] {
			continue
		}
		seen[addr.AgentID] = true
		agents = append(agents, addr.AgentID)
	}
	if len(agents) == 0 {
		return event.Address{}
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i] < agents[j] })
	return event.Address{AgentID: agents[0]}
}

// noteCheckpointCommit advances the commit cadence and wakes the filer once the
// interval or the period has elapsed. The call is made after one fact has landed
// and takes the run lock alone: no commit takes the cut lock and no commit waits
// for a cut, so a holder that commits under its own lock never stands between its
// fact and the checkpoint the cadence owes.
//
//   - the due stands until the filer takes it: a wake that a cut already answered files nothing, and a due reached beside a cut in progress is filed by the next wake.
func (r *Run) noteCheckpointCommit() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.checkpointCommits++
	due := r.checkpointCommits >= r.checkpointEvery || time.Since(r.checkpointAt) >= r.checkpointPeriod
	r.mu.Unlock()
	if !due {
		return
	}
	r.requestCheckpoint()
}

// requestCheckpoint wakes the filer for a checkpoint whatever the cadence clock
// says: the caller knows a cut is worth taking now. The request is a signal and
// never a cut of its own, because the caller stands inside the unit of work the
// cut would wait out — the commit that filed the material, held on the
// quiet-point fence until the state that material leaves its holder in is
// applied — and a cut taken there would drain the fence that asks for it. The
// filer takes the cut on its own goroutine, where no commit of any holder is
// held.
//
// A request that arrives beside a due already pending is the one due: the
// checkpoint states the whole account at the cut it takes, so a later request adds
// no state an earlier one would have missed.
func (r *Run) requestCheckpoint() {
	if r == nil || r.checkpointDue == nil {
		return
	}
	r.mu.Lock()
	r.checkpointDuePending = true
	r.mu.Unlock()
	select {
	case r.checkpointDue <- struct{}{}:
	default:
	}
}

// fileCheckpoints files the checkpoint whatever cadence asked for it, from a
// goroutine that holds no lock of this run and no lock of a state holder.
//
//   - The cut asks every holder for its state, and a commit of that holder may stand under the holder's own lock (the delivery flush commits its settlement under the task registry lock). A commit that took the cut itself would therefore wait on the holder whose commit is waiting on the cut, and no timeout breaks that cycle: one side of it is a held lock. The commit signals this wait instead.
//
// A cut that cannot be taken is reported at ERROR: the cadence is the bound on the
// material a restart reads, and a checkpoint this filer never filed widens that bound
// silently behind a chain that keeps committing. The close files the checkpoint it
// owes itself and answers the failure to its caller, so a filing that stays broken
// reaches the process that stops the run instead of a log line alone. A due already
// queued when the stop arrives files nothing, because the stop is read before the
// wake: the cut the close takes is the last one this goroutine takes.
func (r *Run) fileCheckpoints(ctx context.Context) {
	for {
		select {
		case <-r.checkpointStop:
			return
		default:
		}
		select {
		case <-r.checkpointStop:
			return
		case <-r.checkpointDue:
		}
		if r.takeCheckpointDue() {
			if _, _, err := r.Checkpoint(ctx); err != nil {
				assemblyError("tagent: the framework checkpoint was not filed: %v", err)
			}
		}
		for _, addr := range r.takeSessionDue() {
			if _, _, err := r.CheckpointSession(ctx, addr); err != nil {
				assemblyError("tagent: the session checkpoint of %v was not filed: %v", addr, err)
			}
		}
	}
}

// takeCheckpointDue reports whether the cadence owes a checkpoint at this wake and
// clears the due the wake stands for.
func (r *Run) takeCheckpointDue() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	due := r.checkpointDuePending
	r.checkpointDuePending = false
	return due
}

// requestSessionCheckpoint wakes the filer for a checkpoint of one session chain
// whatever the cadence clock says: the caller reached a moment the chain's state
// is worth stating — a compression product landed, the session closed. The
// request is a signal and never a cut of its own, because the caller stands
// inside the unit of work the cut would wait out — the commit that filed the
// material, held on the quiet-point fence until the state that material leaves
// its holder in is applied — and a cut taken there would drain the fence that
// asks for it. The filer takes the cut on its own goroutine, where no commit of
// any holder is held.
//
// A chain already due is the one due: the checkpoint states the whole state of the
// chain at the cut it takes, so a later request adds no state an earlier one
// would have missed.
func (r *Run) requestSessionCheckpoint(addr event.Address) {
	if r == nil || r.checkpointDue == nil || !addr.IsComplete() {
		return
	}
	r.mu.Lock()
	if r.checkpointSessionPending == nil {
		r.checkpointSessionPending = map[event.Address]struct{}{}
	}
	r.checkpointSessionPending[addr] = struct{}{}
	r.mu.Unlock()
	select {
	case r.checkpointDue <- struct{}{}:
	default:
	}
}

// takeSessionDue reports the chains a cut is owed for at this wake and clears
// them, in address order so a wake that owes several cuts files them in the same
// order it heard of them.
func (r *Run) takeSessionDue() []event.Address {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.checkpointSessionPending) == 0 {
		return nil
	}
	pending := make([]event.Address, 0, len(r.checkpointSessionPending))
	for addr := range r.checkpointSessionPending {
		pending = append(pending, addr)
	}
	r.checkpointSessionPending = nil
	sortAddresses(pending)
	return pending
}

// startCheckpointFiler starts the goroutine that files the checkpoint the cadence
// owes. The context keeps the values of the boot and none of its cancellation, so
// a cut is never refused by a caller that already returned. A run beside no wake
// channel files no cut, and states that at once, so a close of it joins nothing.
func (r *Run) startCheckpointFiler(ctx context.Context) {
	if r == nil || r.checkpointDue == nil || r.checkpointStop == nil {
		r.finishCheckpointFiler()
		return
	}
	go func() {
		defer r.finishCheckpointFiler()
		r.fileCheckpoints(context.WithoutCancel(ctx))
	}()
}

// finishCheckpointFiler states that the filer returned, so a close that joined it
// waits for a cut that exists and never for one that never started.
func (r *Run) finishCheckpointFiler() {
	if r == nil || r.checkpointDone == nil {
		return
	}
	r.checkpointDoneOnce.Do(func() { close(r.checkpointDone) })
}

// stopCheckpointFiler ends the filer at its next wake and joins it, so no cut is
// taken under the drain a close judges the run by and none is still on its way to
// the store when the close returns: a cut in flight completes before the closing
// checkpoint is taken, so that checkpoint is the last fact the process files. A
// repeated close stops nothing twice and joins nothing twice.
func (r *Run) stopCheckpointFiler() {
	if r == nil || r.checkpointStop == nil {
		return
	}
	r.checkpointStopOnce.Do(func() { close(r.checkpointStop) })
	if r.checkpointDone != nil {
		<-r.checkpointDone
	}
}

// fileClosingCheckpoint files the checkpoint a close owes the restart that reads
// this chain: the state the run reached at the fixed point of its close, so a
// process that stops well after its last cadence still leaves a checkpoint stating
// what it settled. The filer is stopped before the close takes it, so the cut is
// the last one the process files and no wake races the drain the close judges.
//
// The cut waits for the quiet point, so it is taken under the budget the close
// was given: a run that never reached that point inside the budget answers the
// checkpoint of the position it stands at, and the drain report names the work that
// kept it there. A cut that fails for any other reason is answered to the close's
// caller, because the bound on the next boot is that checkpoint and a failure to
// state it is not a fact the run may keep to itself.
func (r *Run) fileClosingCheckpoint(ctx context.Context, budget time.Time) error {
	if r == nil {
		return nil
	}
	cutCtx := context.WithoutCancel(ctx)
	if !budget.IsZero() {
		var cancel context.CancelFunc
		cutCtx, cancel = context.WithDeadline(cutCtx, budget)
		defer cancel()
	}
	if _, _, err := r.Checkpoint(cutCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			assemblyWarn("tagent: the close reached its budget before the final checkpoint was filed: %v", err)
			return nil
		}
		return fmt.Errorf("tagent: file the checkpoint the close owes: %w", err)
	}
	return nil
}

// CheckpointAfterFold asks for a checkpoint because a fold produced a product: a
// fold is the moment the projection drops material, so the state a restart reads
// is worth refreshing whatever the cadence clock says. The product lands inside
// the quiet-point fence of the commit that filed it, so this is a request and not
// a cut — the filer takes the cut and the commit that asked for it never waits on
// itself.
func (r *Run) CheckpointAfterFold() {
	r.requestCheckpoint()
}

// CheckpointFiled reports the identity of the newest checkpoint this run filed
// and the cut it states, so a caller reads the coverage of the checkpoint it holds.
func (r *Run) CheckpointFiled() (event.EventID, event.CheckpointOffset) {
	if r == nil {
		return "", nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.checkpointFiled, r.checkpointOffset
}

// newestCheckpointRef reports the newest checkpoint fact of one storage tier by
// the frozen type alone, reading no body of the history the checkpoint covers and
// copying no index: the store answers the entry its newest commit filed, so the
// lookup costs one comparison per indexed entry, allocates nothing, and reads the
// one body the answered identity names instead of the history behind it.
//
// The order is the commit order across partitions, never the highest partition:
// a run whose owner moved between processes filed its newest checkpoint in the
// partition of its newest process, and reading the highest partition instead
// would restore the state of a process that stopped earlier.
func newestCheckpointRef(ctx context.Context, tier *ledger.Tier) (anchorRef, bool, error) {
	key, held, err := tier.Segments.NewestOfType(ctx, event.TypeCheckpoint)
	if err != nil || !held {
		return anchorRef{}, false, err
	}
	return anchorRef{key: key, typ: event.TypeCheckpoint}, true, nil
}

// restoreCheckpoint reads one checkpoint fact back and hands every slice to the
// holder that states it, then reports the cut the restored state corresponds to.
// A fact that states no readable checkpoint refuses the restore by name: the
// boot never starts beside state it read as empty.
func (r *Run) restoreCheckpoint(ctx context.Context, tier *ledger.Tier, ref anchorRef) (event.CheckpointOffset, error) {
	stored, held, err := tier.Segments.GetEvent(ctx, ref.key)
	if err != nil {
		return nil, err
	}
	if !held {
		return nil, fmt.Errorf("%w: %s", ledger.ErrSegmentBodyMissing, ref.key)
	}
	body, err := memory.DecodeEventBody(stored.Body)
	if err != nil {
		return nil, err
	}
	evt := body.Event()
	evt.Key = ref.key
	checkpoint, stated := event.CheckpointOf(evt)
	if !stated {
		return nil, fmt.Errorf("%w: %s", ErrCheckpointUnreadable, ref.key)
	}
	if err := r.states.RestoreState(ctx, checkpoint.Slices); err != nil {
		return nil, err
	}
	slices := make(map[string]bool, len(checkpoint.Slices))
	for _, slice := range checkpoint.Slices {
		slices[slice.Name] = true
	}
	r.mu.Lock()
	r.checkpointFiled = event.EventID(checkpoint.Key)
	r.checkpointOffset = checkpoint.Offset
	r.checkpointSlices = slices
	r.mu.Unlock()
	return checkpoint.Offset, nil
}

// errStopIndexWalk ends an index walk the visitor has answered: the walk is read
// newest commit first, so the first entry that answers the question is the one
// the caller asked for and nothing behind it can answer better.
var errStopIndexWalk = errors.New("assembly: the index walk answered")

// newestSessionCheckpointRef answers the newest checkpoint one session chain filed,
// by the frozen type and the session the index records beside it. The walk reads
// the resident commit order newest first and stops at the first entry that
// answers — one comparison per indexed entry and no body read, the same mechanism
// the framework checkpoint's lookup uses — so the lookup costs the commits since the
// checkpoint rather than the history behind it, and it copies and sorts nothing.
func newestSessionCheckpointRef(ctx context.Context, tier *ledger.Tier, session event.SessionID) (anchorRef, bool, error) {
	if tier == nil || session == "" {
		return anchorRef{}, false, nil
	}
	held := anchorRef{}
	err := tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, partition int, seq int64, typ string, tagged event.SessionID) error {
		if typ != event.TypeCheckpoint || tagged != session {
			return nil
		}
		held = anchorRef{key: key, partition: partition, seq: seq, typ: typ, session: tagged}
		return errStopIndexWalk
	})
	if errors.Is(err, errStopIndexWalk) {
		return held, true, nil
	}
	if err != nil {
		return anchorRef{}, false, err
	}
	return anchorRef{}, false, nil
}

// restoreSessionCheckpoints reads the newest checkpoint of every session chain the
// storage holds and hands each one's slices to their holders, so a boot starts
// each chain from the state that chain last stated rather than from the account
// the framework's own checkpoint happens to cover. One walk of the index answers
// every chain's newest checkpoint — the walk is newest commit first, so the first
// entry of a chain is that chain's newest — and only the checkpoints that answer are
// read, so a boot pays the open state of every chain and never the history
// behind the cuts.
//
// A checkpoint that a later one of the same chain supersedes is not read: the slices
// of a chain are superseded whole, so the newest checkpoint of a chain states every
// slice its predecessors state. A session checkpoint the framework checkpoint supersedes
// is not read either: the framework cut states the whole account at a later
// position, so a checkpoint filed before it states no chain past that account.
func (r *Run) restoreSessionCheckpoints(ctx context.Context, tier *ledger.Tier) error {
	if tier == nil {
		return nil
	}
	newest := map[event.SessionID]anchorRef{}
	var order []event.SessionID
	err := tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) error {
		if typ != event.TypeCheckpoint {
			return nil
		}
		if session == "" {
			return errStopIndexWalk
		}
		if _, held := newest[session]; held {
			return nil
		}
		newest[session] = anchorRef{key: key, partition: partition, seq: seq, typ: typ, session: session}
		order = append(order, session)
		return nil
	})
	if err != nil && !errors.Is(err, errStopIndexWalk) {
		return err
	}
	if len(newest) == 0 {
		return nil
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	for _, session := range order {
		if err := r.restoreSessionCheckpoint(ctx, tier, newest[session]); err != nil {
			return err
		}
	}
	return nil
}

// restoreSessionCheckpoint reads one session checkpoint back and hands its slices to
// the holders that state them, recording the cut that chain's state corresponds
// to. The restore merges into the state the boot already holds: a checkpoint states
// the slices of its own chain, and the framework's checkpoint beside it states the
// whole account, so the two restores name one state per holder without one
// erasing the other.
func (r *Run) restoreSessionCheckpoint(ctx context.Context, tier *ledger.Tier, ref anchorRef) error {
	stored, held, err := tier.Segments.GetEvent(ctx, ref.key)
	if err != nil {
		return err
	}
	if !held {
		return fmt.Errorf("%w: %s", ledger.ErrSegmentBodyMissing, ref.key)
	}
	body, err := memory.DecodeEventBody(stored.Body)
	if err != nil {
		return err
	}
	evt := body.Event()
	evt.Key = ref.key
	checkpoint, stated := event.CheckpointOf(evt)
	if !stated {
		return fmt.Errorf("%w: %s", ErrCheckpointUnreadable, ref.key)
	}
	if err := r.states.RestoreState(ctx, checkpoint.Slices); err != nil {
		return err
	}
	slices := make(map[string]bool, len(checkpoint.Slices))
	for _, slice := range checkpoint.Slices {
		slices[slice.Name] = true
	}
	r.mu.Lock()
	r.sessionFiled[ref.session] = event.EventID(checkpoint.Key)
	r.sessionCuts[ref.session] = checkpoint.Offset
	r.sessionSlices[ref.session] = slices
	r.mu.Unlock()
	return nil
}

// restoredChain is the state one chain's projection slice stated: the survivors
// the chain presented at the cut, in display order. The boot hydrates them from
// the chain itself, so the private fields a mapped entry carries are recovered
// instead of invented from the payload.
type restoredChain struct {
	survivors []anchorEntry
}

// checkpointCut reports the cut the newest checkpoint of this run states for one
// state slice, or nil beside a checkpoint that states no such slice and beside a
// run that restored none: a reader that holds nil reads the whole history it is
// given.
func (r *Run) checkpointCut(slice string) event.CheckpointOffset {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.checkpointOffset) == 0 || !r.checkpointSlices[slice] {
		return nil
	}
	cut := make(event.CheckpointOffset, len(r.checkpointOffset))
	for partition, seq := range r.checkpointOffset {
		cut[partition] = seq
	}
	return cut
}

// restoreNewestCheckpoint reads the newest checkpoint fact of the storage tier,
// hands every state slice back to the holder that states it and answers the cut
// the restored state corresponds to. A tier holding no checkpoint answers no cut,
// and the boot then reads the chains in full; a checkpoint the reader cannot
// state refuses the boot by name instead of starting beside an empty state.
func (r *Run) restoreNewestCheckpoint(ctx context.Context, tier *ledger.Tier) (event.CheckpointOffset, bool, error) {
	ref, held, err := newestCheckpointRef(ctx, tier)
	if err != nil {
		return nil, false, err
	}
	if !held {
		return nil, false, nil
	}
	offset, err := r.restoreCheckpoint(ctx, tier, ref)
	if err != nil {
		return nil, false, fmt.Errorf("tagent: restore the framework checkpoint %s: %w", ref.key, err)
	}
	return offset, true, nil
}

// eachFactPastCut visits the committed facts that landed past the newest
// checkpoint cut in commit order, reading no body of the history the checkpoint
// covers: the index walk names the positions without bodies, so the recovery of
// one consumer costs the material committed since the cut instead of the whole
// chain.
func (r *Run) eachFactPastCut(ctx context.Context, tier *ledger.Tier, cut event.CheckpointOffset, visit func(ledger.StoredEvent) error) error {
	var refs []anchorRef
	if err := tier.Segments.EachIndexNewestFirst(ctx, func(key event.EventID, partition int, seq int64, typ string, session event.SessionID) error {
		ref := anchorRef{key: key, partition: partition, seq: seq, typ: typ, session: session}
		if typ == event.TypeCheckpoint {
			return nil
		}
		if !pastCut(cut, ref) {
			return nil
		}
		refs = append(refs, ref)
		return nil
	}); err != nil {
		return err
	}
	for i := len(refs) - 1; i >= 0; i-- {
		stored, held, err := tier.Segments.GetEvent(ctx, refs[i].key)
		if err != nil {
			return err
		}
		if !held {
			return fmt.Errorf("%w: %s", ledger.ErrSegmentBodyMissing, refs[i].key)
		}
		if err := visit(stored); err != nil {
			return err
		}
	}
	return nil
}

// pastCut reports whether one index reference lands after the cut it is judged
// against: a partition the cut does not name is wholly past it, because the cut
// covers only the partitions that had committed something when it was taken, and
// a fact of a partition the cut never named landed after it by definition.
func pastCut(offset event.CheckpointOffset, ref anchorRef) bool {
	covered, named := offset[ref.partition]
	if !named {
		return true
	}
	return ref.seq > covered
}

// projectionStateSlice is the state slice of the session projections the run
// presents: the survivors of every session chain, keyed by the address the chain
// answers at.
type projectionStateSlice struct {
	run *Run
}

// projectionSliceWire is the payload of the projection slice: one entry per
// session chain, in address order.
type projectionSliceWire struct {
	Sessions []projectionSliceSession `json:"sessions"`
}

// projectionSliceSession is one session chain's survivors: the address, the
// replay window the chain declared and the entries the projection presents.
type projectionSliceSession struct {
	Agent     string        `json:"agent"`
	Session   string        `json:"session"`
	MaxTokens int           `json:"max_tokens"`
	Entries   []anchorEntry `json:"entries"`
}

// StateName reports the name the projection slice is keyed by.
func (s projectionStateSlice) StateName() string { return checkpointSliceProjection }

// SnapshotState states the survivors of every session projection the run presents.
func (s projectionStateSlice) SnapshotState(ctx context.Context) ([]byte, error) {
	return s.run.projectionSlice()
}

// SnapshotSessionState states the survivors of one session chain. A chain the run
// presents no projection of answers nil, so the checkpoint of a session that landed
// no material carries no projection slice.
func (s projectionStateSlice) SnapshotSessionState(_ context.Context, session event.SessionID) ([]byte, error) {
	return s.run.projectionSliceOf(session)
}

// RestoreState puts the survivors of every session chain back, so the boot replays
// only the material committed past the cut instead of folding the chain again.
func (s projectionStateSlice) RestoreState(ctx context.Context, payload []byte) error {
	return s.run.restoreProjectionSlice(payload)
}

// Reduce folds one committed fact into the survivors of its session chain. The
// fold is the one derivation of the mapped sequence: the live commit drives it,
// and the replay of a chain past its cut folds the same entries through the same
// step, so a restart presents the sequence the process before it presented
// instead of a second reading of the same facts.
func (s projectionStateSlice) Reduce(ctx context.Context, evt event.Event) error {
	entry, mapped := mappedEntryOf(evt)
	if !mapped {
		return nil
	}
	s.run.foldMapped(ctx, evt, entry)
	return nil
}

// projectionSlice states the survivors of every session chain the run presents.
func (r *Run) projectionSlice() ([]byte, error) {
	return r.projectionSliceWhere(nil)
}

// projectionSliceOf states the survivors of one session chain, or nothing beside
// a chain the run holds no projection of.
func (r *Run) projectionSliceOf(session event.SessionID) ([]byte, error) {
	if session == "" {
		return nil, nil
	}
	payload, err := r.projectionSliceWhere(func(addr event.Address) bool { return addr.SessionID == session })
	if err != nil {
		return nil, err
	}
	var wire projectionSliceWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return nil, err
	}
	if len(wire.Sessions) == 0 {
		return nil, nil
	}
	return payload, nil
}

// projectionSliceWhere states the survivors of the session chains the keep admits,
// in address order, or of every chain beside no keep.
func (r *Run) projectionSliceWhere(keep func(event.Address) bool) ([]byte, error) {
	r.mu.Lock()
	addresses := make([]event.Address, 0, len(r.replays))
	entries := make(map[event.Address]replayEntry, len(r.replays))
	for addr, held := range r.replays {
		if held.projection == nil {
			continue
		}
		if keep != nil && !keep(addr) {
			continue
		}
		addresses = append(addresses, addr)
		entries[addr] = held
	}
	r.mu.Unlock()
	sort.Slice(addresses, func(i, j int) bool {
		if addresses[i].AgentID != addresses[j].AgentID {
			return addresses[i].AgentID < addresses[j].AgentID
		}
		return addresses[i].SessionID < addresses[j].SessionID
	})
	wire := projectionSliceWire{Sessions: make([]projectionSliceSession, 0, len(addresses))}
	for _, addr := range addresses {
		held := entries[addr]
		wire.Sessions = append(wire.Sessions, projectionSliceSession{
			Agent:     string(addr.AgentID),
			Session:   string(addr.SessionID),
			MaxTokens: held.window.MaxTokens,
			Entries:   anchorEntriesOf(held.projection.Entries()),
		})
	}
	return json.Marshal(wire)
}

// restoreProjectionSlice puts the survivors of every session chain back as the
// seeds the boot rebuilds from: each chain carries the projection the checkpoint
// stated under the window it declared, so the material committed past the cut
// appends to the state the checkpoint states instead of folding the history again.
//
//   - The checkpoints a boot reads state one state per chain: the framework's checkpoint states every chain and a session checkpoint states one, so each restore adds the chains it states and leaves the rest standing.
func (r *Run) restoreProjectionSlice(payload []byte) error {
	var wire projectionSliceWire
	if err := json.Unmarshal(payload, &wire); err != nil {
		return err
	}
	seeds := make(map[event.Address]*restoredChain, len(wire.Sessions))
	for _, session := range wire.Sessions {
		addr := event.Address{AgentID: event.AgentID(session.Agent), SessionID: event.SessionID(session.Session)}
		if !addr.IsComplete() {
			return fmt.Errorf("assembly: the projection slice names the incomplete address %v", addr)
		}
		seeds[addr] = &restoredChain{survivors: session.Entries}
	}
	r.mu.Lock()
	if r.restoredProjection == nil {
		r.restoredProjection = map[event.Address]*restoredChain{}
	}
	for addr, seed := range seeds {
		r.restoredProjection[addr] = seed
	}
	r.mu.Unlock()
	return nil
}

// takeRestoredProjection hands the seeds a checkpoint stated to the boot that
// rebuilds the chains, and empties the slot so a second boot of the same run
// never starts from the state of the one before it.
func (r *Run) takeRestoredProjection() map[event.Address]*restoredChain {
	r.mu.Lock()
	defer r.mu.Unlock()
	seeds := r.restoredProjection
	r.restoredProjection = nil
	return seeds
}

// restoredSeedOf reports the seed a checkpoint stated for one chain slot: the
// partition a chain was filed into is the partition its agent names, so a
// restored chain is looked up by the slot the index walk reports.
func restoredSeedOf(seeds map[event.Address]*restoredChain, slot anchorSlot) (*restoredChain, event.Address, bool) {
	for addr, seed := range seeds {
		if addr.SessionID != slot.session {
			continue
		}
		if memory.PartitionIDFromName(string(addr.AgentID)) != slot.partition {
			continue
		}
		return seed, addr, true
	}
	return nil, event.Address{}, false
}

// anchorEntriesOf states the projection entries one fold or one checkpoint
// carries: the fields that name the entry as the chain states it, beside the text
// a fold product minted. The text of a mapped entry stays on the chain, so the
// checkpoint of a projection is the size of its entries and never the size of the
// bodies behind them.
func anchorEntriesOf(entries []replay.ProjectionEntry) []anchorEntry {
	out := make([]anchorEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, anchorEntry{
			Key:     entry.Key,
			Type:    entry.Type,
			Summary: entry.Summary,
			Content: carriedTextOf(entry),
			Time:    entry.Time.UnixMilli(),
		})
	}
	return out
}
