package governance

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/SpellingDragon/tagent/api/durable"
	"github.com/SpellingDragon/tagent/api/event"
)

// GoalStatus is the lifecycle state of one goal. The four words are the ones
// the governance semantics fix and they are kept verbatim: active is the goal
// in flight — it occupies governance budget and takes part in the gate ruling;
// achieved is reached, so it holds no budget and takes no part in the ruling;
// abandoned is given up on deliberately; expired is the goal that outlived its
// validity unreached, kept apart from abandoned so statistics and budget
// attribution can tell the two apart.
//
// Only two of the four are ever written: a goal is explicitly resolved to
// achieved or abandoned, and expired is derived from ExpiresMs — a goal past
// its validity simply stops counting as active and its Status is never flipped
// by the registry. The resolution face takes the two writable words and the
// tool layer's two-value enumeration guards the rest; the registry itself
// guards no terminal state.
//
// 契约: docs/wiki/agent/governance-enforcement.md#goal-registry
type GoalStatus string

const (
	// GoalActive is the goal in flight: it occupies governance budget and
	// takes part in the gate ruling.
	GoalActive GoalStatus = "active"
	// GoalAchieved is the reached goal: it holds no budget and takes no part in
	// the gate ruling.
	GoalAchieved GoalStatus = "achieved"
	// GoalAbandoned is the goal given up on deliberately, for example because
	// a better path replaced it or an operator ended it.
	GoalAbandoned GoalStatus = "abandoned"
	// GoalExpired is the goal that outlived its validity unreached. It is
	// derived from ExpiresMs and never written, kept apart from abandoned for
	// statistics and budget attribution.
	GoalExpired GoalStatus = "expired"
)

// SubtypeGoalDeclared and SubtypeGoalResolved are the goal operation words a
// goal governance event carries. The state words stay the old ones; these two
// name the operations and are the event vocabulary entry the goal contract
// fixes.
const (
	// SubtypeGoalDeclared is the governance event subtype of a declared goal.
	SubtypeGoalDeclared = "goal_declared"
	// SubtypeGoalResolved is the governance event subtype of a resolved goal.
	SubtypeGoalResolved = "goal_resolved"
)

// goalOpDeclared and goalOpResolved are the payload forms the goal events carry:
// they name the same two operations the subtypes above name, in the payload form
// the goal events have always spoken.
const (
	goalOpDeclared = "declared"
	goalOpResolved = "resolved"
)

// Goal is one autonomous goal declaration.
type Goal struct {
	// ID is the goal identity, minted in declaration order.
	ID string `json:"id"`
	// Statement is what the goal wants done.
	Statement string `json:"statement"`
	// CreatedBy names who declared the goal.
	CreatedBy string `json:"created_by"`
	// Status is the lifecycle state the goal is in.
	Status GoalStatus `json:"status"`
	// CreatedMs is the declaration time, in Unix milliseconds.
	CreatedMs int64 `json:"created_ms"`
	// ExpiresMs is the moment the goal stops counting as active, in Unix
	// milliseconds. Zero means the goal never expires.
	ExpiresMs int64 `json:"expires_ms,omitempty"`
}

// goalEventPayload is the structured JSON one goal governance event carries:
// the goal it is about, the operation it records and the fields that operation
// froze.
type goalEventPayload struct {
	GoalID    string `json:"goal_id"`
	Op        string `json:"op"`
	Statement string `json:"statement,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
	Status    string `json:"status,omitempty"`
	ExpiresMs int64  `json:"expires_ms,omitempty"`
}

// GoalRegistry is the goal registry: the goals in force in memory beside the
// governance events the declarations and resolutions also write. A declaration
// activates the governance goal gate, and a refusal of a goal-required call
// permeates the goal context into its record.
//
// The registry is concurrent safe, and its durable face is late-bound and nil
// safe: without a store it degrades to memory state instead of failing, and
// binding one folds the stored goal events that landed past the state cut this
// registry holds, so a restart keeps the goals it already declared. The cut is
// seated by the framework state slice (see StateSeat); a registry beside no seat
// holds the zero cut, so the events past it are the whole stream.
//
//   - a registry with a durable face holds the work in force alone: a goal whose resolution landed, or whose validity passed, belongs to the stream from then on, so the memory state and the cut stated from it follow the in-flight work instead of the history it accumulates (see List for the read that answers the history).
//   - a registry beside no store holds every goal it declared, because the memory state is the only place they live.
//
// 契约: docs/wiki/agent/governance-enforcement.md#goal-registry
type GoalRegistry struct {
	mu    sync.RWMutex
	goals map[string]*Goal
	seq   int

	log    durable.FactLog
	evtSeq int64

	writeMu sync.Mutex
	stopped error
}

// NewGoalRegistry builds one goal registry in memory state. Bind hands it the
// durable face whenever the wiring has one.
func NewGoalRegistry() *GoalRegistry {
	return &GoalRegistry{goals: make(map[string]*Goal)}
}

// Bind hands the registry its durable face and folds the goal events that landed
// past the state cut it holds back into it — declarations and resolutions in the
// order the operations happened, so a resolved goal is never resurrected and a
// later declaration never reuses an identity. It is nil safe, and it runs single
// threaded at construction.
//
// The error of a fold belongs to the caller that can stop the boot: a stream this
// registry could not read leaves the goals behind the chain that serves them.
func (g *GoalRegistry) Bind(ctx context.Context, log durable.FactLog) error {
	if g == nil || log == nil {
		return nil
	}
	g.mu.Lock()
	g.log = log
	cut := g.evtSeq
	g.mu.Unlock()
	return g.foldTail(ctx, log, cut)
}

// seat states the registry the cut, the declaration order and the goals one
// framework state slice restored, so a restarted registry continues both
// numberings where the stated stream ended. The declaration order never lands
// below an identity the stated goals already took: a payload that understates it
// would hand a later declaration an identity a stored goal holds.
func (g *GoalRegistry) seat(goals []*Goal, order int, cut int64) {
	if g == nil {
		return
	}
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	g.mu.Lock()
	defer g.mu.Unlock()
	seated := make(map[string]*Goal, len(goals))
	for _, goal := range goals {
		if goal == nil || goal.ID == "" {
			continue
		}
		cp := *goal
		seated[cp.ID] = &cp
		if seq := goalSeqOf(cp.ID); seq > order {
			order = seq
		}
	}
	g.goals = seated
	g.seq = order
	g.evtSeq = cut
	g.stopped = nil
}

// stated reports the cut, the declaration order and the goals the framework state
// slice states for this registry. One rule decides what the goals are: a
// registry with a durable face states the work in force alone, because the record
// of a resolved or expired goal is the stream the cut numbers, while a registry
// beside no store states every goal it holds, because nothing else keeps them. The
// state is read under the registry lock and states no history, so one cut costs the
// live work instead of the history behind it.
func (g *GoalRegistry) stated() (int64, int, []*Goal) {
	if g == nil {
		return 0, 0, nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := time.Now().UnixMilli()
	historyBehindTheStream := g.log != nil
	goals := make([]*Goal, 0, len(g.goals))
	for _, goal := range g.goals {
		if historyBehindTheStream && !inFlight(goal, now) {
			continue
		}
		cp := *goal
		goals = append(goals, &cp)
	}
	sort.Slice(goals, func(i, j int) bool { return goals[i].ID < goals[j].ID })
	return g.evtSeq, g.seq, goals
}

// Declare registers one goal and returns its identity. The declaration writes
// its governance event first and mints the goal only once that event landed, so
// the goals the gate rules on are the goals a restart reads back: a declaration
// the stream refused answers no identity and leaves the effective state where the
// stream stands. The declaration is the write boundary that takes the goals
// holding nothing in force out of the memory state (see pruneOutOfForceLocked),
// and it runs before the new identity is minted, so a goal just declared is never
// the one taken out. A registry beside no stream mints the goal on the memory
// state alone, because nothing else keeps it.
func (g *GoalRegistry) Declare(statement, createdBy string, expiresMs int64) string {
	if g == nil {
		return ""
	}
	now := time.Now().UnixMilli()
	g.mu.Lock()
	g.pruneOutOfForceLocked(now)
	g.seq++
	seq := g.seq
	log := g.log
	g.mu.Unlock()
	id := fmt.Sprintf("g-%d", seq)
	goal := &Goal{
		ID: id, Statement: statement, CreatedBy: createdBy,
		Status: GoalActive, CreatedMs: now, ExpiresMs: expiresMs,
	}
	if !g.recordsOperation(log, goalEventPayload{
		GoalID: id, Op: goalOpDeclared, Statement: statement,
		CreatedBy: createdBy, ExpiresMs: expiresMs,
	}, now, SubtypeGoalDeclared) {
		g.unmint(seq)
		return ""
	}
	g.mu.Lock()
	g.goals[id] = goal
	g.mu.Unlock()
	return id
}

// unmint hands one declaration number back when the event never landed and no
// later declaration minted past it: the identity a refused declaration reserved
// is the one the next declaration takes, so the order this registry hands out is
// the order the stream records instead of a sequence punctuated by numbers no
// goal ever held.
func (g *GoalRegistry) unmint(seq int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seq == seq {
		g.seq = seq - 1
	}
}

// recordsOperation writes one goal operation and reports whether the effective
// state may follow it: an event the stream landed, or a registry beside no stream,
// where the memory state is the only record the goal has. A stream that refused
// the event answers false, so the goal the gate rules on is the goal a restart
// reads back instead of one the stream never kept.
func (g *GoalRegistry) recordsOperation(log durable.FactLog, p goalEventPayload, ts int64, subtype string) bool {
	if log == nil {
		return true
	}
	return g.writeGoalEvent(log, p, ts, subtype)
}

// Resolve moves one goal to its resolution and reports whether the registry held
// it and the resolution became the recorded one. The two writable resolutions are
// achieved and abandoned; expired is derived and never written here. The registry
// guards no terminal state: a repeated resolution is applied as it arrives, and
// the tool layer's two-value enumeration is what keeps an unwritten word out.
//
// A resolution writes its event before the memory state follows it, so a refused
// write answers false and leaves the goal in the state the stream keeps. A landed
// resolution takes the goal out of the memory state, beside the goals that already
// left force: from then on the stream is the record of that goal and List answers
// it from there. A registry beside no store keeps it, because nothing else would.
// A goal the registry has released answers false — the live state is the work in
// force, not every goal ever declared.
func (g *GoalRegistry) Resolve(id string, status GoalStatus) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	_, ok := g.goals[id]
	now := time.Now().UnixMilli()
	log := g.log
	g.mu.Unlock()
	if !ok {
		return false
	}
	if !g.recordsOperation(log, goalEventPayload{
		GoalID: id, Op: goalOpResolved, Status: string(status),
	}, now, SubtypeGoalResolved) {
		return false
	}
	g.mu.Lock()
	if goal, held := g.goals[id]; held {
		goal.Status = status
	}
	g.pruneOutOfForceLocked(now)
	g.mu.Unlock()
	return true
}

// pruneOutOfForceLocked takes the goals holding nothing in force out of the memory
// state: a resolved goal is the stream's record from the moment its event landed,
// and a goal past its validity stops counting as active and holds no budget. The
// write boundaries that learn a goal left force — a declaration, a resolution and
// the fold a bind runs — prune it, so the cut stated from this state follows the
// work in force instead of the history the registry accumulates. Only a registry
// with a durable face prunes: without one the memory state is the only place
// every goal it declared lives, so nothing is taken out of it.
func (g *GoalRegistry) pruneOutOfForceLocked(nowMs int64) {
	if g.log == nil {
		return
	}
	for id, goal := range g.goals {
		if !inFlight(goal, nowMs) {
			delete(g.goals, id)
		}
	}
}

// List returns every goal the registry answers, in declaration order: the work in
// force it holds in memory and the history the stream behind it keeps. A registry
// with a durable face derives the history on each question, so a cut that states
// the live work alone never shortens the list of goals a reader asks about; the
// read costs the stream behind the cut, and the memory state never keeps the
// history it serves. A registry beside no store answers its memory state, which is
// every goal it declared.
func (g *GoalRegistry) List() []*Goal {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	log := g.log
	held := make(map[string]*Goal, len(g.goals))
	for id, goal := range g.goals {
		cp := *goal
		held[id] = &cp
	}
	g.mu.RUnlock()
	for id, goal := range g.streamGoals(context.Background(), log) {
		if _, inForce := held[id]; !inForce {
			held[id] = goal
		}
	}
	out := make([]*Goal, 0, len(held))
	for _, goal := range held {
		out = append(out, goal)
	}
	sort.Slice(out, func(i, j int) bool {
		if si, sj := goalSeqOf(out[i].ID), goalSeqOf(out[j].ID); si != sj {
			return si < sj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// streamGoals reads the goals the goal stream records: every declaration and its
// resolutions in the order the keys number them, so one identity answers the
// statement it was declared with and the resolution that closed it. A body the
// store will not parse is named at ERROR beside the goals around it, and a read the
// store refuses answers no goal at all — a history read that cannot reach the
// stream never invents the record it could not read.
func (g *GoalRegistry) streamGoals(ctx context.Context, log durable.FactLog) map[string]*Goal {
	if log == nil {
		return nil
	}
	stored, err := log.Scan(ctx, goalEventPrefix)
	if err != nil {
		governanceHistoryUnreadable(goalEventPrefix, err)
		return nil
	}
	keys := make([]string, 0, len(stored))
	for key := range stored {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]*Goal, len(keys))
	for _, key := range keys {
		var evt event.Event
		if err := json.Unmarshal(stored[key], &evt); err != nil {
			governanceHistoryUnreadable(key, err)
			continue
		}
		if evt.Type != event.TypeGovernance {
			continue
		}
		var p goalEventPayload
		if err := json.Unmarshal([]byte(evt.Message.Content), &p); err != nil {
			governanceHistoryUnreadable(key, err)
			continue
		}
		if p.GoalID == "" {
			continue
		}
		switch p.Op {
		case goalOpDeclared:
			out[p.GoalID] = &Goal{
				ID: p.GoalID, Statement: p.Statement, CreatedBy: p.CreatedBy,
				Status: GoalActive, CreatedMs: evt.Timestamp.UnixMilli(), ExpiresMs: p.ExpiresMs,
			}
		case goalOpResolved:
			if goal, ok := out[p.GoalID]; ok {
				goal.Status = GoalStatus(p.Status)
			}
		}
	}
	return out
}

// HasActive reports whether an unexpired active goal exists — the judgment the
// governance goal gate rules on.
func (g *GoalRegistry) HasActive() bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := time.Now().UnixMilli()
	for _, goal := range g.goals {
		if inFlight(goal, now) {
			return true
		}
	}
	return false
}

// Active returns every unexpired active goal, for diagnostics and tools.
func (g *GoalRegistry) Active() []*Goal {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := time.Now().UnixMilli()
	out := make([]*Goal, 0)
	for _, goal := range g.goals {
		if inFlight(goal, now) {
			out = append(out, goal)
		}
	}
	return out
}

// Current returns the goal the registry reads as the current goal context: the
// newest goal still in flight, or nil when none is. A refusal permeates this
// goal into its record, so the audit reads which autonomous scope the ruled
// call was hanging on.
func (g *GoalRegistry) Current() *Goal {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	now := time.Now().UnixMilli()
	var current *Goal
	for _, goal := range g.goals {
		if !inFlight(goal, now) {
			continue
		}
		if current == nil || newerGoal(goal, current) {
			current = goal
		}
	}
	if current == nil {
		return nil
	}
	cp := *current
	return &cp
}

// inFlight reports whether the goal counts as active: declared and not past
// its validity. A goal past its validity is the derived expired state — it
// stops counting here and its recorded Status is never flipped.
func inFlight(goal *Goal, nowMs int64) bool {
	return goal.Status == GoalActive && (goal.ExpiresMs == 0 || goal.ExpiresMs > nowMs)
}

// newerGoal reports whether one goal was declared after another, falling back
// to declaration order for a same-moment pair.
func newerGoal(a, b *Goal) bool {
	if a.CreatedMs != b.CreatedMs {
		return a.CreatedMs > b.CreatedMs
	}
	return goalSeqOf(a.ID) > goalSeqOf(b.ID)
}

// goalSeqOf reads the declaration order one goal identity carries.
func goalSeqOf(id string) int {
	var n int
	if _, err := fmt.Sscanf(id, "g-%d", &n); err != nil {
		return 0
	}
	return n
}

// writeGoalEvent writes one goal operation as the governance event its
// declaration or resolution requires, and numbers the stream only behind a barrier
// it confirmed: the write path is serialized, a refused write hands its number
// back, and a barrier that did not confirm stops the stream from numbering on. The
// failure is named at ERROR instead of dropping out of the durable stream in
// silence, and the caller that reads the false it answers leaves the effective
// state where the stream stands. It reports whether the event landed, because a
// landed event is what lets the caller hand the goal itself to the stream.
func (g *GoalRegistry) writeGoalEvent(log durable.FactLog, p goalEventPayload, ts int64, subtype string) bool {
	if log == nil {
		return false
	}
	g.writeMu.Lock()
	defer g.writeMu.Unlock()
	g.mu.Lock()
	stopped := g.stopped
	seq := g.evtSeq + 1
	g.mu.Unlock()
	key := fmt.Sprintf("%s%019d", goalEventPrefix, seq)
	if stopped != nil {
		governanceWriteFailure(key, subtype, p.GoalID, stopped)
		return false
	}
	content, err := json.Marshal(p)
	if err != nil {
		governanceWriteFailure(key, subtype, p.GoalID, err)
		return false
	}
	meta := map[string]string{
		MetaKeySubtype: subtype,
		"goal_op":      p.Op,
		"goal_id":      p.GoalID,
	}
	evt := event.Event{
		Key:       event.EventID(key),
		Type:      event.TypeGovernance,
		Timestamp: time.UnixMilli(ts),
		Meta:      meta,
		Message:   contentMessage(string(content)),
	}
	staged, err := putGovernanceEvent(context.Background(), log, key, evt)
	if err == nil {
		g.mu.Lock()
		if seq > g.evtSeq {
			g.evtSeq = seq
		}
		g.mu.Unlock()
		return true
	}
	if staged {
		g.mu.Lock()
		if seq > g.evtSeq {
			g.evtSeq = seq
		}
		if g.stopped == nil {
			g.stopped = fmt.Errorf("the write barrier at %s%019d did not confirm: %w", goalEventPrefix, seq, err)
		}
		g.mu.Unlock()
	}
	governanceWriteFailure(key, subtype, p.GoalID, err)
	return false
}

// foldTail folds the stored goal events that landed past one cut into the
// registry — the declared goals and their resolutions in the order the
// operations happened, with the declaration order realigned so a later
// declaration never reuses an identity a stored goal already took — and
// continues the stream numbering from the newest event it read. An event body
// this fold cannot read as a goal operation is skipped, while an event the store
// cannot parse at all fails the read that returns it (see governanceEventsPast).
// The goals the fold took out of force are then taken out of the memory state,
// because the stream this fold read is their record from then on.
func (g *GoalRegistry) foldTail(ctx context.Context, log durable.FactLog, cut int64) error {
	events, newest, err := governanceEventsPast(ctx, log, goalEventPrefix, cut)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	maxSeq := g.seq
	for _, evt := range events {
		if evt.Type != event.TypeGovernance {
			continue
		}
		var p goalEventPayload
		if err := json.Unmarshal([]byte(evt.Message.Content), &p); err != nil || p.GoalID == "" {
			continue
		}
		switch p.Op {
		case goalOpDeclared:
			g.goals[p.GoalID] = &Goal{
				ID: p.GoalID, Statement: p.Statement, CreatedBy: p.CreatedBy,
				Status: GoalActive, CreatedMs: evt.Timestamp.UnixMilli(), ExpiresMs: p.ExpiresMs,
			}
		case goalOpResolved:
			if goal, ok := g.goals[p.GoalID]; ok {
				goal.Status = GoalStatus(p.Status)
			}
		}
		if n := goalSeqOf(p.GoalID); n > maxSeq {
			maxSeq = n
		}
	}
	g.seq = maxSeq
	if newest > g.evtSeq {
		g.evtSeq = newest
	}
	g.pruneOutOfForceLocked(time.Now().UnixMilli())
	return nil
}
